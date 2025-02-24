package config

import (
	"github.com/pteich/configstruct"
)

const Transient = "<Transient>"

type S3Config struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint" cli:"endpoint" env:"ENDPOINT"`
	AccessKey string `json:"accessKey" cli:"accesskey" env:"ACCESS_KEY"`
	SecretKey string `json:"secretKey" cli:"secretkey" env:"SECRET_KEY"`
	Bucket    string `json:"bucket" cli:"bucket" env:"BUCKET"`
	Prefix    string `json:"prefix" cli:"prefix" env:"PREFIX"`
	Region    string `json:"region" cli:"region" env:"REGION"`
	UseSSL    bool   `json:"usessl" cli:"usessl" env:"USE_SSL"`
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
