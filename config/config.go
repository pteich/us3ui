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

	return cfg, nil
}

func (c *Config) Save() error {
	f, err := os.Create(c.filepath)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(c.Settings)
}
