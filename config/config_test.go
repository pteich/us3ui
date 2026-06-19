package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
