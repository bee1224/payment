package main

import (
	"log"

	"payment-service/internal/app"
	"payment-service/internal/config"
	"payment-service/internal/repository"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := repository.Migrate(cfg.Database.DSN, "migrations/001_init.sql"); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	server := app.NewServer(cfg)
	if err := server.Run(); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
