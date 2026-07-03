package db

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"
)

// ── Credential Store (encrypted API key storage) ───────────────────────

// credentialsPath returns ~/.aipmc/credentials.
func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", "credentials")
}

// CredentialStore holds decrypted provider→API-key mappings in memory.
type CredentialStore struct {
	Keys       map[string]string `json:"keys"`       // provider name → api key
	sessionKey []byte            // master password for session (zeroed on lock)
}

// credentialsFile is the on-disk JSON format (encrypted).
type credentialsFile struct {
	Salt string `json:"salt"` // base64, 8–64 bytes recommended
	Iter uint   `json:"iter"` // PBKDF2 iteration count
	IV   string `json:"iv"`   // base64, 12 bytes for GCM
	Data string `json:"data"` // base64 ciphertext + GCM tag
}

// session state
var (
	globalStore    atomic.Value // stores *CredentialStore
	unlockedAt     time.Time
	sessionTimeout = 30 * time.Minute
)

// SetCredentialStore sets the global in-memory credential store and starts the session timer.
func SetCredentialStore(cs *CredentialStore) {
	globalStore.Store(cs)
	unlockedAt = time.Now()
}

// GetCredentialStore returns the global store. Never expires — used by proxy routing.
func GetCredentialStore() *CredentialStore {
	v := globalStore.Load()
	if v == nil { return nil }
	return v.(*CredentialStore)
}

// IsUnlocked returns true if store is set AND within session timeout. Used by Web UI.
func IsUnlocked() bool {
	if time.Since(unlockedAt) > sessionTimeout { return false }
	return GetCredentialStore() != nil
}

// Lock clears the session and zeroes the stored password.
func Lock() {
	if cs := GetCredentialStore(); cs != nil {
		cs.Lock()
	}
	globalStore.Store((*CredentialStore)(nil))
	unlockedAt = time.Time{}
}

// Lock zeroes the session key and clears keys.
func (c *CredentialStore) Lock() {
	for i := range c.sessionKey { c.sessionKey[i] = 0 }
	c.sessionKey = nil
}

// SessionPassword returns a copy of the session key for spawning subprocesses.
func (c *CredentialStore) SessionPassword() []byte {
	if len(c.sessionKey) == 0 { return nil }
	p := make([]byte, len(c.sessionKey))
	copy(p, c.sessionKey)
	return p
}

// UnlockSession sets the session password so SaveToFile works without re-prompting.
func (c *CredentialStore) UnlockSession(password []byte) {
	c.sessionKey = make([]byte, len(password))
	copy(c.sessionKey, password)
}

// SaveToFile encrypts and writes the current keys to disk using the session key.
func (c *CredentialStore) SaveToFile() error {
	if len(c.sessionKey) == 0 { return errors.New("session locked — no password available") }
	return SaveCredentials(c, c.sessionKey)
}

// Get returns the API key for a provider, or empty string.
func (c *CredentialStore) Get(provider string) string {
	if c == nil {
		return ""
	}
	return c.Keys[provider]
}

// Set adds or updates an API key.
func (c *CredentialStore) Set(provider, key string) {
	if c.Keys == nil {
		c.Keys = map[string]string{}
	}
	c.Keys[provider] = key
}

// Remove deletes an API key. Returns false if not found.
func (c *CredentialStore) Remove(provider string) bool {
	if c.Keys == nil {
		return false
	}
	if _, ok := c.Keys[provider]; !ok {
		return false
	}
	delete(c.Keys, provider)
	return true
}

// List returns all provider names.
func (c *CredentialStore) List() []string {
	var names []string
	for k := range c.Keys {
		names = append(names, k)
	}
	return names
}

// CredentialsExist returns true if ~/.aipmc/credentials exists.
func CredentialsExist() bool {
	_, err := os.Stat(credentialsPath())
	return err == nil
}

// PromptPassword reads a password from stdin without echo.
func PromptPassword() ([]byte, error) {
	fd := int(syscall.Stdin)
	return term.ReadPassword(fd)
}

// ── Public API (dispatched to CGO or stub) ──────────────────────────────

var (
	errNoCGO = errors.New("credentials require SM4-GCM via CGO — rebuild with GmSSL: ./build.sh")
)

// loadImpl and saveImpl are set by credentials_cgo.go init() when CGO is enabled.
var loadImpl func([]byte) (*CredentialStore, error)
var saveImpl func(*CredentialStore, []byte) error

// LoadCredentials reads and decrypts the credentials file.
func LoadCredentials(password []byte) (*CredentialStore, error) {
	if loadImpl != nil {
		return loadImpl(password)
	}
	return nil, errNoCGO
}

// SaveCredentials encrypts and writes the credentials file.
func SaveCredentials(store *CredentialStore, password []byte) error {
	if saveImpl != nil {
		return saveImpl(store, password)
	}
	return errNoCGO
}

// ValidatePassword checks whether the given password can decrypt the credentials file.
func ValidatePassword(password []byte) error {
	_, err := LoadCredentials(password)
	return err
}

// ChangePassword decrypts with old password, re-encrypts with new password.
func ChangePassword(oldPass, newPass []byte) error {
	store, err := LoadCredentials(oldPass)
	if err != nil {
		return err
	}
	if store == nil {
		return errors.New("no credentials file found")
	}
	return SaveCredentials(store, newPass)
}
