package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveFiltersTransientAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	normal1 := S3Config{Name: "prod", Endpoint: "s3.example.com", Bucket: "my-bucket"}
	normal2 := S3Config{Name: "staging", Endpoint: "s3-staging.example.com", Bucket: "staging-bucket"}
	transient := S3Config{Name: Transient, Endpoint: "transient.example.com"}

	c := &Config{
		filepath: settingsPath,
		Settings: Settings{
			Connections: []S3Config{normal1, transient, normal2},
		},
	}

	if err := c.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	f, err := os.Open(settingsPath)
	if err != nil {
		t.Fatalf("failed to open saved file: %v", err)
	}
	defer f.Close()

	var loaded Settings
	if err := json.NewDecoder(f).Decode(&loaded); err != nil {
		t.Fatalf("failed to decode saved settings: %v", err)
	}

	if len(loaded.Connections) != 2 {
		t.Fatalf("expected 2 connections after save, got %d", len(loaded.Connections))
	}

	for _, conn := range loaded.Connections {
		if conn.Name == Transient {
			t.Errorf("Transient connection should not be persisted")
		}
	}

	// Verify round-tripped fields match
	if loaded.Connections[0].Endpoint != normal1.Endpoint {
		t.Errorf("Connections[0].Endpoint = %q, want %q", loaded.Connections[0].Endpoint, normal1.Endpoint)
	}
	if loaded.Connections[1].Endpoint != normal2.Endpoint {
		t.Errorf("Connections[1].Endpoint = %q, want %q", loaded.Connections[1].Endpoint, normal2.Endpoint)
	}
}

func TestSaveMovesSecretToKeychain(t *testing.T) {
	keyring.MockInit()

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	c := &Config{
		filepath: settingsPath,
		Settings: Settings{
			Connections: []S3Config{
				{Name: "c1", SecretKey: "topsecret", Endpoint: "e"},
			},
		},
	}

	if err := c.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// (a) on-disk file must not contain the plaintext secret
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if strings.Contains(string(raw), "topsecret") {
		t.Errorf("saved file still contains plaintext secret key")
	}

	// (b) keychain must hold the secret
	got, err := keyring.Get("us3ui", "c1")
	if err != nil {
		t.Fatalf("keyring.Get returned error: %v", err)
	}
	if got != "topsecret" {
		t.Errorf("keyring secret = %q, want %q", got, "topsecret")
	}

	// (c) in-memory secret must still be intact
	if c.Settings.Connections[0].SecretKey != "topsecret" {
		t.Errorf("in-memory SecretKey = %q, want %q", c.Settings.Connections[0].SecretKey, "topsecret")
	}
}

func TestLoadSecretsMigratesPlaintext(t *testing.T) {
	keyring.MockInit()

	c := &Config{
		Settings: Settings{
			Connections: []S3Config{
				{Name: "c1", SecretKey: "plaintextsecret", Endpoint: "e"},
			},
		},
	}

	migrated := c.loadSecrets()
	if !migrated {
		t.Errorf("loadSecrets() returned false, expected true when migrating plaintext secret")
	}

	got, err := keyring.Get("us3ui", "c1")
	if err != nil {
		t.Fatalf("keyring.Get returned error: %v", err)
	}
	if got != "plaintextsecret" {
		t.Errorf("keyring secret = %q, want %q", got, "plaintextsecret")
	}
}

func TestLoadSecretsPopulatesFromKeychain(t *testing.T) {
	keyring.MockInit()

	if err := keyring.Set("us3ui", "c2", "fromring"); err != nil {
		t.Fatalf("keyring.Set returned error: %v", err)
	}

	c := &Config{
		Settings: Settings{
			Connections: []S3Config{
				{Name: "c2", SecretKey: "", Endpoint: "e"},
			},
		},
	}

	c.loadSecrets()

	if c.Settings.Connections[0].SecretKey != "fromring" {
		t.Errorf("SecretKey = %q, want %q", c.Settings.Connections[0].SecretKey, "fromring")
	}
}

func TestSavePermissions(t *testing.T) {
	c := &Config{
		filepath: filepath.Join(t.TempDir(), "settings.json"),
		Settings: Settings{Connections: []S3Config{{Name: "x", Endpoint: "e"}}},
	}

	if err := c.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(c.filepath)
		if err != nil {
			t.Fatalf("os.Stat() returned error: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file permissions = %04o, want 0600", perm)
		}
	}
}
