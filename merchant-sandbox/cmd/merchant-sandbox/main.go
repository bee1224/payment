package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/nnviopp/merchant-sandbox/internal/callback"
	"github.com/nnviopp/merchant-sandbox/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "callback-status" {
		callbackStatus(cfg)
		return
	}
	receiver := callback.New(callback.Config{Path: cfg.CallbackPath, MerchantID: cfg.MerchantID, CallbackKeyID: cfg.CallbackKeyID, CallbackSigningSecret: cfg.CallbackSigningSecret, ResponseMode: cfg.CallbackResponseMode, TimeoutDelay: cfg.TimeoutDelay, RecordsPath: cfg.RecordsPath})
	log.Printf("merchant-sandbox callback receiver listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, receiver.Handler()))
}

func callbackStatus(cfg config.Config) {
	flags := flag.NewFlagSet("callback-status", flag.ExitOnError)
	orderID := flags.String("order-id", "", "merchant order ID")
	_ = flags.Parse(os.Args[2:])
	status, err := callback.LoadAcceptanceStatus(cfg.RecordsPath, *orderID)
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		log.Fatal(err)
	}
	_, _ = fmt.Println(string(encoded))
}
