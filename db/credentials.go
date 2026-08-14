package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"
)

// ── Credential Store (encrypted API key storage) ─────────────────────────

// aipmcDir returns ~/.aipmc.
func aipmcDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc")
}

// credentialsDir returns ~/.aipmc/credentials.d.
func credentialsDir() string {
	return filepath.Join(aipmcDir(), "credentials.d")
}

// credentialsPath returns the filesystem path for a profile.
// Empty profile defaults to "default".
func credentialsPath(profile string) string {
	if profile == "" {
		profile = "default"
	}
	return filepath.Join(credentialsDir(), profile)
}

// CredentialStore holds decrypted provider→API-key mappings in memory.
type CredentialStore struct {
	Keys       map[string]string `json:"keys"`       // provider name → api key
	sessionKey []byte            // master password for session (zeroed on lock)
	Profile    string            // which profile this store was loaded from
}

// credentialsFile is the on-disk JSON format (encrypted).
type credentialsFile struct {
	Salt string `json:"salt"` // base64, 8–24 bytes recommended
	Iter uint   `json:"iter"` // PBKDF2 iteration count
	IV   string `json:"iv"`   // base64, 12 bytes for GCM
	Data string `json:"data"` // base64 ciphertext + GCM tag
}

// clearBytes zeroes a byte slice in place. Used to wipe password buffers
// after use; implementations must only ever wipe private copies.
func clearBytes(b []byte) { for i := range b { b[i] = 0 } }

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

// GetCredentialStore returns the global store. Never expires – used by proxy routing.
func GetCredentialStore() *CredentialStore {
	v := globalStore.Load()
	if v == nil { return nil }
	return v.(*CredentialStore)
}

// CurrentProfile returns the profile name of the currently loaded store, or empty.
func CurrentProfile() string {
	if cs := GetCredentialStore(); cs != nil {
		return cs.Profile
	}
	return ""
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
	if len(c.sessionKey) == 0 { return errors.New("session locked – no password available") }
	return SaveCredentialsToProfile(c, c.sessionKey, c.Profile)
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

// CredentialsExistForProfile returns true if the profile file exists.
func CredentialsExistForProfile(profile string) bool {
	_, err := os.Stat(credentialsPath(profile))
	return err == nil
}

// CredentialsExist returns true if any credentials file exists (old path or new profiles).
func CredentialsExist() bool {
	profiles := ListProfiles()
	if len(profiles) > 0 {
		return true
	}
	return legacyCredentialsExist()
}

// PromptPassword reads a password from stdin without echo.
func PromptPassword() ([]byte, error) {
	fd := int(syscall.Stdin)
	return term.ReadPassword(fd)
}

// ── Public API (dispatched to CGO or stub) ────────────────────────────────

var (
	errNoCGO = errors.New("credentials require SM4-GCM via CGO – rebuild with GmSSL: ./build.sh")
)

var loadImpl func([]byte) (*CredentialStore, error)
var saveImpl func(*CredentialStore, []byte) error
var loadProfileImpl func([]byte, string) (*CredentialStore, error)
var saveProfileImpl func(*CredentialStore, []byte, string) error

// LoadCredentials loads from the "default" profile (backward-compat).
func LoadCredentials(password []byte) (*CredentialStore, error) {
	return LoadCredentialsProfile(password, "default")
}

// LoadCredentialsProfile reads and decrypts a specific profile.
func LoadCredentialsProfile(password []byte, profile string) (*CredentialStore, error) {
	if profile == "" {
		profile = "default"
	}
	// Hand the implementation a private copy so it can wipe its buffer
	// without destroying caller-owned slices (e.g. a password reused for
	// save after a load).
	pw := make([]byte, len(password))
	copy(pw, password)
	defer clearBytes(pw)
	if loadProfileImpl != nil {
		store, err := loadProfileImpl(pw, profile)
		if err == nil && store != nil {
			store.Profile = profile
		}
		return store, err
	}
	return nil, errNoCGO
}

// SaveCredentials writes to the "default" profile (backward-compat).
func SaveCredentials(store *CredentialStore, password []byte) error {
	return SaveCredentialsToProfile(store, password, "default")
}

// SaveCredentialsToProfile encrypts and writes to a specific profile.
func SaveCredentialsToProfile(store *CredentialStore, password []byte, profile string) error {
	if profile == "" {
		profile = "default"
	}
	// Private copy again: CredentialStore.SaveToFile passes sessionKey in,
	// and the CGO implementation wipes the buffer it receives. Without a
	// copy, every save would zero the session key and re-encrypt with an
	// all-zero key, locking the user out permanently.
	pw := make([]byte, len(password))
	copy(pw, password)
	defer clearBytes(pw)
	if saveProfileImpl != nil {
		return saveProfileImpl(store, pw, profile)
	}
	if saveImpl != nil {
		return saveImpl(store, pw)
	}
	return errNoCGO
}

// ValidatePassword checks whether the given password can decrypt the default profile credentials file.
func ValidatePassword(password []byte) error {
	return ValidatePasswordForProfile(password, "default")
}

// ValidatePasswordForProfile checks password against a specific profile.
func ValidatePasswordForProfile(password []byte, profile string) error {
	_, err := LoadCredentialsProfile(password, profile)
	return err
}

// ChangePassword decrypts with old password, re-encrypts with new password.
// Uses the "default" profile; prefer ChangePasswordForProfile for explicit profiling.
func ChangePassword(oldPass, newPass []byte) error {
	return ChangePasswordForProfile(oldPass, newPass, "default")
}

// ChangePasswordForProfile decrypts a specific profile with old password, re-encrypts with new password.
func ChangePasswordForProfile(oldPass, newPass []byte, profile string) error {
	if profile == "" {
		profile = "default"
	}
	store, err := LoadCredentialsProfile(oldPass, profile)
	if err != nil {
		return err
	}
	if store == nil {
		return errors.New("no credentials file found for profile " + profile)
	}
	store.Profile = profile
	return SaveCredentialsToProfile(store, newPass, profile)
}

// ── Profile management (no persistent "active" profile) ───────────────────

// ListProfiles returns all profile names found in credentials.d/.
func ListProfiles() []string {
	dir := credentialsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// CreateProfile initializes a new profile with empty keys.
func CreateProfile(name, password string) error {
	if name == "" {
		return fmt.Errorf("profile name required")
	}
	path := credentialsPath(name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("profile %q already exists", name)
	}
	store := &CredentialStore{Keys: map[string]string{}, Profile: name}
	return SaveCredentialsToProfile(store, []byte(password), name)
}

// DeleteProfile removes a credentials profile file.
func DeleteProfile(name string) error {
	if name == "" || name == "default" {
		return fmt.Errorf("cannot delete built-in profile %q", name)
	}
	path := credentialsPath(name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("profile %q does not exist", name)
	}
	return os.Remove(path)
}

// CredentialsPathForProfile returns the filesystem path for a named profile.
func CredentialsPathForProfile(name string) string {
	return credentialsPath(name)
}

// legacyCredentialsExist checks for the old ~/.aipmc/credentials file.
func legacyCredentialsExist() bool {
	legacy := filepath.Join(aipmcDir(), "credentials")
	_, err := os.Stat(legacy)
	return err == nil
}
