//go:build cgo

package db

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func init() {
	loadImpl = loadCGO
	saveImpl = saveCGO
}

func clearBytes(b []byte) { for i := range b { b[i] = 0 } }

func loadCGO(password []byte) (*CredentialStore, error) {
	defer clearBytes(password)
	path := credentialsPath()
	data, err := os.ReadFile(path)
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

	// Must use the exact iter from the file — it was used to derive the key.
	// Changing it would produce a different key and decryption would fail.
	if cf.Iter == 0 {
		return nil, fmt.Errorf("invalid credentials: iter=0")
	}

	key, err := sm3Pbkdf2(string(password), salt, uint(cf.Iter), uint(Sm4KeySize))
	if err != nil {
		return nil, fmt.Errorf("pbkdf2: %w", err)
	}

	plaintext, err := sm4GcmDecrypt(key, iv, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong password?): %w", err)
	}

	var store CredentialStore
	if err := json.Unmarshal(plaintext, &store); err != nil {
		return nil, fmt.Errorf("invalid decrypted data: %w", err)
	}
	return &store, nil
}

func saveCGO(store *CredentialStore, password []byte) error {
	defer clearBytes(password)
	plaintext, err := json.Marshal(store)
	if err != nil {
		return err
	}

	// Security parameters: PBKDF2 100K iterations, 16-byte salt (NIST SP 800-132)
	const saltSize = 16
	const pbkdf2Iter = 100000

	salt, err := randBytes(saltSize)
	if err != nil {
		return fmt.Errorf("rand salt: %w", err)
	}
	iv, err := randBytes(int(Sm4GcmDefaultIVSize))
	if err != nil {
		return fmt.Errorf("rand iv: %w", err)
	}

	key, err := sm3Pbkdf2(string(password), salt, uint(pbkdf2Iter), uint(Sm4KeySize))
	if err != nil {
		return fmt.Errorf("pbkdf2: %w", err)
	}

	ciphertext, err := sm4GcmEncrypt(key, iv, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	cf := credentialsFile{
		Salt: base64.StdEncoding.EncodeToString(salt),
		Iter: pbkdf2Iter,
		IV:   base64.StdEncoding.EncodeToString(iv),
		Data: base64.StdEncoding.EncodeToString(ciphertext),
	}

	path := credentialsPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	out, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}
