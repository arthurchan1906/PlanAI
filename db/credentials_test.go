package db

import (
	"bytes"
	"testing"
)

// The CGO implementations wipe the password buffer they receive
// (defer clearBytes(password)). The dispatch layer must hand them a private
// copy so caller-owned slices survive — CredentialStore.sessionKey passed by
// SaveToFile is the critical case. These tests simulate the CGO behavior with
// a fake impl and therefore also run in CGO_ENABLED=0 builds.

func TestLoadCredentialsProfileProtectsCallerPassword(t *testing.T) {
	old := loadProfileImpl
	defer func() { loadProfileImpl = old }()

	loadProfileImpl = func(password []byte, profile string) (*CredentialStore, error) {
		store := &CredentialStore{Keys: map[string]string{"anthropic": "sk-1"}}
		store.UnlockSession(password)
		clearBytes(password) // simulate the CGO impl wiping its input
		return store, nil
	}

	pass := []byte("s3cret-password")
	store, err := LoadCredentialsProfile(pass, "default")
	if err != nil {
		t.Fatalf("LoadCredentialsProfile: %v", err)
	}
	if string(pass) != "s3cret-password" {
		t.Fatalf("caller password was zeroed by load: %q", pass)
	}
	if got := store.SessionPassword(); !bytes.Equal(got, pass) {
		t.Fatalf("session key corrupted after load: got %q want %q", got, pass)
	}
}

func TestSaveCredentialsToProfileProtectsCallerPassword(t *testing.T) {
	old := saveProfileImpl
	defer func() { saveProfileImpl = old }()

	saveProfileImpl = func(store *CredentialStore, password []byte, profile string) error {
		clearBytes(password) // simulate the CGO impl wiping its input
		return nil
	}

	pass := []byte("s3cret-password")
	if err := SaveCredentialsToProfile(&CredentialStore{Keys: map[string]string{}}, pass, "default"); err != nil {
		t.Fatalf("SaveCredentialsToProfile: %v", err)
	}
	if string(pass) != "s3cret-password" {
		t.Fatalf("caller password was zeroed by save: %q", pass)
	}
}

// Regression: SaveToFile used to pass c.sessionKey straight to the CGO impl,
// which zeroed it. The next save then encrypted with an all-zero key and the
// real password could never decrypt the file again.
func TestSaveToFileDoesNotZeroSessionKey(t *testing.T) {
	old := saveProfileImpl
	defer func() { saveProfileImpl = old }()

	saveProfileImpl = func(store *CredentialStore, password []byte, profile string) error {
		clearBytes(password) // simulate the CGO impl wiping its input
		return nil
	}

	store := &CredentialStore{Keys: map[string]string{"anthropic": "sk-123"}}
	store.UnlockSession([]byte("master-pw"))
	for i := 0; i < 2; i++ {
		if err := store.SaveToFile(); err != nil {
			t.Fatalf("SaveToFile #%d: %v", i+1, err)
		}
	}
	if got := store.SessionPassword(); string(got) != "master-pw" {
		t.Fatalf("session key corrupted after SaveToFile: %q", got)
	}
}
