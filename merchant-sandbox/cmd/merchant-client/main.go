package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/nnviopp/merchant-sandbox/internal/config"
	"github.com/nnviopp/merchant-sandbox/internal/gateway"
)

func main() {
	operation := flag.String("operation", "", "collection-create|collection-query|payout-create|payout-query")
	bodyPath := flag.String("body", "", "path to a JSON request body")
	flag.Parse()
	if *operation == "" || *bodyPath == "" {
		log.Fatal("-operation and -body are required")
	}
	raw, err := os.ReadFile(*bodyPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	client, err := gateway.NewClient(gateway.Credentials{BaseURL: cfg.PaymentBaseURL, CustomerID: cfg.CustomerID, CustomerSecret: cfg.CustomerSecret, MerchantID: cfg.MerchantID, MerchantSecret: cfg.MerchantSecret, APIKey: cfg.APIKey})
	if err != nil {
		log.Fatal(err)
	}
	var response []byte
	switch *operation {
	case "collection-create":
		var request gateway.CollectionCreateRequest
		err = json.Unmarshal(raw, &request)
		if err == nil {
			response, err = client.CreateCollection(context.Background(), request)
		}
	case "collection-query":
		var request gateway.CollectionQueryRequest
		err = json.Unmarshal(raw, &request)
		if err == nil {
			response, err = client.QueryCollection(context.Background(), request)
		}
	case "payout-create":
		var request gateway.PayoutCreateRequest
		err = json.Unmarshal(raw, &request)
		if err == nil {
			response, err = client.CreatePayout(context.Background(), request)
		}
	case "payout-query":
		var request gateway.PayoutQueryRequest
		err = json.Unmarshal(raw, &request)
		if err == nil {
			response, err = client.QueryPayout(context.Background(), request)
		}
	default:
		err = fmt.Errorf("unsupported operation %q", *operation)
	}
	if err != nil {
		log.Fatal(err)
	}
	_, _ = os.Stdout.Write(append(response, '\n'))
}
