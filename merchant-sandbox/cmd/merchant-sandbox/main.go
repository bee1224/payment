package main

import (
	"log"
	"net/http"

	"github.com/nnviopp/merchant-sandbox/internal/callback"
	"github.com/nnviopp/merchant-sandbox/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	receiver := callback.New(callback.Config{Path: cfg.CallbackPath, MerchantID: cfg.MerchantID, CallbackKeyID: cfg.CallbackKeyID, CallbackSigningSecret: cfg.CallbackSigningSecret, ResponseMode: cfg.CallbackResponseMode, TimeoutDelay: cfg.TimeoutDelay, RecordsPath: cfg.RecordsPath})
	log.Printf("merchant-sandbox callback receiver listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, receiver.Handler()))
}
