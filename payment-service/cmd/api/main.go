package main

import (
	"log"
	"time"

	"payment-service/internal/app"
	"payment-service/internal/config"
	"payment-service/internal/repository"
)

func main() {
	taipei, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		log.Fatalf("load Asia/Taipei timezone: %v", err)
	}
	time.Local = taipei
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	warnings, err := cfg.Validate()
	for _, warning := range warnings {
		log.Printf("config warning: %s", warning)
	}
	if err != nil {
		log.Fatalf("invalid config: %v", err)
	}
	if err := repository.Migrate(cfg.Database.DSN, "migrations"); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	server := app.NewServer(cfg)
	if err := server.Run(); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
