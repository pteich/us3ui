package config

import (
	"github.com/pteich/configstruct"
)

type S3Config struct {
	Endpoint  string `cli:"endpoint" env:"ENDPOINT"`
	AccessKey string `cli:"accesskey" env:"ACCESS_KEY"`
	SecretKey string `cli:"secretkey" env:"SECRET_KEY"`
	Bucket    string `cli:"bucket" env:"BUCKET"`
	UseSSL    bool   `cli:"usessl" env:"USE_SSL"`
}

func NewS3Config() (S3Config, error) {
	cfg := S3Config{
		Endpoint: "play.min.io:9000",
		Bucket:   "mybucket",
	}

	err := configstruct.Parse(&cfg)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}
