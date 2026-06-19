package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kirsle/configdir"
)

const Name = "us3ui"

type Config struct {
	filepath string
	Settings Settings
}

type Settings struct {
	Connections []S3Config `json:"connections"`
}

func New() (*Config, error) {
	configPath := configdir.LocalConfig(Name)
	err := configdir.MakePath(configPath)
	if err != nil {
		return nil, err
	}

	var settings Settings

	cfg := &Config{
		filepath: filepath.Join(configPath, "settings.json"),
		Settings: Settings{Connections: make([]S3Config, 0)},
	}

	s3cfg, err := NewS3Config()
	if err == nil && s3cfg.Name != "" {
		cfg.Settings.Connections = append(settings.Connections, s3cfg)
	}

	_, err = os.Stat(cfg.filepath)
	if !os.IsNotExist(err) {
		f, err := os.Open(cfg.filepath)
		if err != nil {
			return cfg, err
		}

		defer f.Close()

		err = json.NewDecoder(f).Decode(&settings)
		if err != nil {
			return cfg, err
		}

		if len(cfg.Settings.Connections) > 0 {
			settings.Connections = append(cfg.Settings.Connections, settings.Connections...)
		}

		cfg.Settings = settings
	}

	if cfg.loadSecrets() {
		// Legacy plaintext secrets were migrated to the keychain; rewrite the
		// file without them (best effort).
		_ = cfg.Save()
	}

	return cfg, nil
}

// loadSecrets reconciles connection secret keys with the OS keychain.
// For each saved connection: a non-empty (legacy plaintext) SecretKey is pushed
// to the keychain and reported via the return value so the caller can rewrite
// the file without it; an empty SecretKey is filled from the keychain when
// present. Best effort: keychain failures leave the in-memory value as-is so the
// app still works without a keychain backend.
func (c *Config) loadSecrets() (migrated bool) {
	for i := range c.Settings.Connections {
		conn := &c.Settings.Connections[i]
		if conn.Name == "" || conn.Name == Transient {
			continue
		}
		if conn.SecretKey != "" {
			if err := secretSet(conn.Name, conn.SecretKey); err == nil {
				migrated = true
			}
			continue
		}
		if s, err := secretGet(conn.Name); err == nil {
			conn.SecretKey = s
		}
	}
	return migrated
}

func (c *Config) Save() error {
	f, err := os.OpenFile(c.filepath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Ensure an already-existing file is tightened to owner-only; OpenFile
	// does not change the mode of a file that already exists.
	if err := f.Chmod(0o600); err != nil {
		return err
	}

	connections := make([]S3Config, 0)
	for _, conn := range c.Settings.Connections {
		if conn.Name != Transient {
			connections = append(connections, conn)
		}
	}
	c.Settings.Connections = connections

	// Build the copy that gets written to disk: move each secret into the OS
	// keychain and blank it in the file. If the keychain is unavailable, fall
	// back to writing the secret in the file (preserves current behavior so the
	// app keeps working without a keychain backend).
	serialized := make([]S3Config, len(connections))
	copy(serialized, connections)
	for i := range serialized {
		if serialized[i].SecretKey == "" || serialized[i].Name == "" {
			continue
		}
		if err := secretSet(serialized[i].Name, serialized[i].SecretKey); err == nil {
			serialized[i].SecretKey = ""
		}
	}

	return json.NewEncoder(f).Encode(Settings{Connections: serialized})
}
