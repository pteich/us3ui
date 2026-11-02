package config

import (
	"github.com/pteich/configstruct"
)

const Transient = "<Transient>"

// Connection types, stored in S3Config.Type. An empty type means S3, so
// connections saved before the field existed stay valid.
const (
	TypeS3 = "s3"
)

type S3Config struct {
	Type      string `json:"type,omitempty" cli:"type" env:"TYPE"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint" cli:"endpoint" env:"ENDPOINT"`
	AccessKey string `json:"accessKey" cli:"accesskey" env:"ACCESS_KEY"`
	SecretKey string `json:"secretKey" cli:"secretkey" env:"SECRET_KEY"`
	Bucket    string `json:"bucket" cli:"bucket" env:"BUCKET"`
	Prefix    string `json:"prefix" cli:"prefix" env:"PREFIX"`
	Region    string `json:"region" cli:"region" env:"REGION"`
	UseSSL    bool   `json:"usessl" cli:"usessl" env:"USE_SSL"`
}

// ConnType returns the connection's protocol, S3 for an unset type.
func (c S3Config) ConnType() string {
	if c.Type == "" {
		return TypeS3
	}
	return c.Type
}

func NewS3Config() (S3Config, error) {
	cfg := S3Config{}

	err := configstruct.Parse(&cfg)
	if err != nil {
		return cfg, err
	}

	if cfg.Endpoint != "" && cfg.AccessKey != "" {
		cfg.Name = Transient
	}

	return cfg, nil
}
