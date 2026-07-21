package newebpay

import (
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"log"
	"strings"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/provider"
)

type DepositClient struct {
	MPGURL     string
	MerchantID string
	HashKey    string
	HashIV     string
	NotifyURL  string
	ReturnURL  string
}

func NewDepositClient(mpgURL, merchantID, hashKey, hashIV, notifyURL, returnURL string) *DepositClient {
	return &DepositClient{
		MPGURL:     mpgURL,
		MerchantID: merchantID,
		HashKey:    hashKey,
		HashIV:     hashIV,
		NotifyURL:  notifyURL,
		ReturnURL:  returnURL,
	}
}

func (c *DepositClient) CreateDepositPayment(order domain.DepositOrder, itemDesc string) (provider.DepositPaymentRequest, error) {
	tradeInfo := MapDepositOrderToTradeInfo(order, c.MerchantID, c.NotifyURL, c.ReturnURL, itemDesc, time.Now())
	encrypted, err := EncryptDepositTradeInfo(tradeInfo.Encode(), c.HashKey, c.HashIV)
	if err != nil {
		return provider.DepositPaymentRequest{}, err
	}

	fields := map[string]string{
		"MerchantID":  c.MerchantID,
		"TradeInfo":   encrypted,
		"TradeSha":    GenerateDepositTradeSHA(encrypted, c.HashKey, c.HashIV),
		"Version":     mpgVersion,
		"EncryptType": "0",
	}

	request := provider.DepositPaymentRequest{
		URL:    c.MPGURL,
		Method: "POST",
		Fields: fields,
	}
	request.HTML = BuildDepositAutoSubmitForm(request.URL, request.Fields)
	return request, nil
}

func (c *DepositClient) VerifyDepositNotification(fields map[string]string) (provider.DepositNotification, error) {
	tradeInfo := fields["TradeInfo"]
	if tradeInfo == "" {
		return provider.DepositNotification{}, fmt.Errorf("missing TradeInfo")
	}
	tradeSha := strings.TrimSpace(fields["TradeSha"])
	if tradeSha == "" {
		return provider.DepositNotification{}, fmt.Errorf("missing TradeSha")
	}
	expected := GenerateDepositTradeSHA(tradeInfo, c.HashKey, c.HashIV)
	if !strings.EqualFold(tradeSha, expected) {
		return provider.DepositNotification{}, fmt.Errorf("invalid TradeSha")
	}

	plain, paddingMode, err := decryptTradeInfo(tradeInfo, c.HashKey, c.HashIV)
	if err != nil {
		return provider.DepositNotification{}, err
	}
	if paddingMode != "pkcs7" {
		log.Printf("newebpay notify accepted compatible AES padding: mode=%s", paddingMode)
	}

	parsed, err := ParseDepositNotify([]byte(plain))
	if err != nil {
		return provider.DepositNotification{}, err
	}
	return provider.DepositNotification{
		OrderNo:     parsed.MerchantOrderNo,
		AmountCents: parsed.Amt * 100,
		TradeNo:     parsed.TradeNo,
		Status:      parsed.Status,
		RawPayload:  []byte(plain),
	}, nil
}

func (c *DepositClient) EnrichDepositNotifyTrace(fields map[string]string, trace domain.DepositNotifyTrace) domain.DepositNotifyTrace {
	if strings.TrimSpace(trace.ProviderOrderNo) != "" && strings.TrimSpace(trace.ProviderTradeNo) != "" {
		return trace
	}

	tradeInfo := strings.TrimSpace(fields["TradeInfo"])
	if tradeInfo == "" {
		return trace
	}

	var payload DepositNotifyPayload
	if plain, _, err := decryptTradeInfo(tradeInfo, c.HashKey, c.HashIV); err == nil {
		if parsed, parseErr := ParseDepositNotify([]byte(plain)); parseErr == nil {
			payload = parsed
		}
	} else if isTradeInfoPlaintext([]byte(tradeInfo)) {
		if parsed, parseErr := ParseDepositNotify([]byte(tradeInfo)); parseErr == nil {
			payload = parsed
		}
	}

	if strings.TrimSpace(trace.ProviderOrderNo) == "" {
		trace.ProviderOrderNo = strings.TrimSpace(payload.MerchantOrderNo)
	}
	if strings.TrimSpace(trace.ProviderTradeNo) == "" {
		trace.ProviderTradeNo = strings.TrimSpace(payload.TradeNo)
	}
	return trace
}

func BuildDepositAutoSubmitForm(action string, fields map[string]string) string {
	var builder strings.Builder
	builder.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Redirecting</title></head><body>`)
	builder.WriteString(`<form id="newebpay-form" method="post" action="`)
	builder.WriteString(htmlpkg.EscapeString(action))
	builder.WriteString(`">`)
	for key, value := range fields {
		builder.WriteString(`<input type="hidden" name="`)
		builder.WriteString(htmlpkg.EscapeString(key))
		builder.WriteString(`" value="`)
		builder.WriteString(htmlpkg.EscapeString(value))
		builder.WriteString(`">`)
	}
	builder.WriteString(`</form><script>document.getElementById("newebpay-form").submit();</script></body></html>`)
	return builder.String()
}

func PrettyDepositFields(fields map[string]string) string {
	data, _ := json.Marshal(fields)
	return string(data)
}
