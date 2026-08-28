package db

// 8/28 凭据静态加密替换：GM/SM4-CGO → 纯 Go 标准库。
// SM3-PBKDF2 + SM4-GCM 改为 PBKDF2-SHA256 + AES-256-GCM（安全等级不低于原实现），
// 消除全项目唯一 CGO/gmssl 依赖与 macOS -framework Security 链接卡壳。
// 密钥仍由用户口令派生（PBKDF2-SHA256，100k 迭代）；磁盘格式沿用 {salt,iter,iv,data}。
// 无国密合规要求（用户 8/28 确认），故可安全替换。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	credsKeySize    = 32 // AES-256
	credsSaltSize   = 16
	credsIVSize     = 12 // GCM 标准 nonce
	credsPBKDF2Iter = 100000
)

func init() {
	loadImpl = loadPure
	saveImpl = savePure
	loadProfileImpl = loadPureProfile
	saveProfileImpl = savePureProfile
}

// pbkdf2SHA256 实现 RFC 2898 PBKDF2（HMAC-SHA256）。取代原 sm3Pbkdf2（gmssl CGO）。
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	dk := make([]byte, 0, numBlocks*hashLen)
	var idx [4]byte
	for i := 1; i <= numBlocks; i++ {
		prf.Reset()
		prf.Write(salt)
		idx[0] = byte(i >> 24)
		idx[1] = byte(i >> 16)
		idx[2] = byte(i >> 8)
		idx[3] = byte(i)
		prf.Write(idx[:])
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for j := 1; j < iter; j++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func aesGcmEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(iv) != gcm.NonceSize() {
		return nil, fmt.Errorf("iv size %d != gcm nonce size %d", len(iv), gcm.NonceSize())
	}
	return gcm.Seal(nil, iv, plaintext, nil), nil
}

func aesGcmDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(iv) != gcm.NonceSize() {
		return nil, fmt.Errorf("iv size %d != gcm nonce size %d", len(iv), gcm.NonceSize())
	}
	return gcm.Open(nil, iv, ciphertext, nil)
}

func cryptoRandBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// loadPureProfile 解密指定 profile 的凭据文件（AES-256-GCM）。
func loadPureProfile(password []byte, profile string) (*CredentialStore, error) {
	defer clearBytes(password)
	path := credentialsPath(profile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && profile == "" {
		legacy := filepath.Join(aipmcDir(), "credentials")
		data, err = os.ReadFile(legacy)
		path = legacy
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("invalid credentials file: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(cf.Salt)
	if err != nil {
		return nil, fmt.Errorf("bad salt: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(cf.IV)
	if err != nil {
		return nil, fmt.Errorf("bad iv: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(cf.Data)
	if err != nil {
		return nil, fmt.Errorf("bad data: %w", err)
	}
	if cf.Iter == 0 {
		return nil, fmt.Errorf("invalid credentials: iter=0")
	}
	key := pbkdf2SHA256(password, salt, int(cf.Iter), credsKeySize)
	plaintext, err := aesGcmDecrypt(key, iv, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong password?): %w", err)
	}
	var store CredentialStore
	if err := json.Unmarshal(plaintext, &store); err != nil {
		return nil, fmt.Errorf("invalid decrypted data: %w", err)
	}
	store.UnlockSession(password)
	return &store, nil
}

func loadPure(password []byte) (*CredentialStore, error) {
	return loadPureProfile(password, "")
}

// savePureProfile 用 AES-256-GCM 加密写回指定 profile。
func savePureProfile(store *CredentialStore, password []byte, profile string) error {
	defer clearBytes(password)
	plaintext, err := json.Marshal(store)
	if err != nil {
		return err
	}
	salt, err := cryptoRandBytes(credsSaltSize)
	if err != nil {
		return err
	}
	iv, err := cryptoRandBytes(credsIVSize)
	if err != nil {
		return err
	}
	key := pbkdf2SHA256(password, salt, credsPBKDF2Iter, credsKeySize)
	ciphertext, err := aesGcmEncrypt(key, iv, plaintext)
	if err != nil {
		return err
	}
	cf := credentialsFile{
		Salt: base64.StdEncoding.EncodeToString(salt),
		Iter: credsPBKDF2Iter,
		IV:   base64.StdEncoding.EncodeToString(iv),
		Data: base64.StdEncoding.EncodeToString(ciphertext),
	}
	path := credentialsPath(profile)
	os.MkdirAll(filepath.Dir(path), 0755)
	out, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}

func savePure(store *CredentialStore, password []byte) error {
	return savePureProfile(store, password, "")
}
