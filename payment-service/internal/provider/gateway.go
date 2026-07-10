package provider

import "payment-service/internal/domain"

type DepositGateway interface {
	CreateDepositPayment(order domain.DepositOrder, itemDesc string) (DepositPaymentRequest, error)
	VerifyDepositNotification(fields map[string]string) (DepositNotification, error)
}

type DepositPaymentRequest struct {
	URL    string            `json:"url"`
	Method string            `json:"method"`
	Fields map[string]string `json:"fields"`
	HTML   string            `json:"html"`
}

type DepositNotification struct {
	OrderNo     string
	AmountCents int64
	TradeNo     string
	Status      string
	RawPayload  []byte
}
