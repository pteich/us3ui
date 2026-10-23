package main

import (
	"log"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/internal/ui"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	ui.Run(cfg)
}
