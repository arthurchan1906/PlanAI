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
	loadProfileImpl = loadCGOWithProfile
	saveProfileImpl = saveCGOWithProfile
}

// loadCGOWithProfile loads credentials from a specific profile (or active if empty).
func loadCGOWithProfile(password []byte, profile string) (*CredentialStore, error) {
	defer clearBytes(password)

	// Try new profile path first
	path := credentialsPath(profile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && profile == "" {
		// Fall back to legacy path
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
	store.UnlockSession(password)
	return &store, nil
}

// loadCGO is the legacy signature used by the init() dispatch table.
func loadCGO(password []byte) (*CredentialStore, error) {
	return loadCGOWithProfile(password, "")
}

// saveCGOWithProfile saves credentials to a specific profile (or active if empty).
func saveCGOWithProfile(store *CredentialStore, password []byte, profile string) error {
	defer clearBytes(password)
	plaintext, err := json.Marshal(store)
	if err != nil {
		return err
	}

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

	path := credentialsPath(profile)
	os.MkdirAll(filepath.Dir(path), 0755)
	out, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}

// saveCGO is the legacy signature used by the init() dispatch table.
func saveCGO(store *CredentialStore, password []byte) error {
	return saveCGOWithProfile(store, password, "")
}
