package newebpay

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"net/url"
	"testing"

	"payment-service/internal/domain"
)

func TestDecryptTradeInfoAcceptsTrimmedPrintablePrefix(t *testing.T) {
	hashKey := "12345678901234567890123456789012"
	hashIV := "1234567890123456"

	prefix := "Status=SUCCESS&MerchantOrderNo=P20260713184827104828&TradeNo=NT123&Amt=100"
	for len(prefix)%aes.BlockSize != 2 {
		prefix += "&X=1"
	}

	suffix := append(bytes.Repeat([]byte{0x01}, 29), 0x1e)
	cipherText := encryptRawCBCForTest(t, append([]byte(prefix), suffix...), hashKey, hashIV)

	plain, paddingMode, err := decryptTradeInfo(cipherText, hashKey, hashIV)
	if err != nil {
		t.Fatalf("decryptTradeInfo returned error: %v", err)
	}
	if paddingMode != "trim-nonprintable" {
		t.Fatalf("expected trim-nonprintable mode, got %q", paddingMode)
	}
	if plain != prefix {
		t.Fatalf("unexpected plaintext:\nwant: %q\ngot:  %q", prefix, plain)
	}
}

func TestVerifyDepositNotificationAcceptsTrimmedPrintablePrefix(t *testing.T) {
	hashKey := "12345678901234567890123456789012"
	hashIV := "1234567890123456"

	prefix := "Status=SUCCESS&MerchantOrderNo=P20260713184827104828&TradeNo=NT123&Amt=100"
	for len(prefix)%aes.BlockSize != 2 {
		prefix += "&X=1"
	}
	suffix := append(bytes.Repeat([]byte{0x01}, 29), 0x1e)
	tradeInfo := encryptRawCBCForTest(t, append([]byte(prefix), suffix...), hashKey, hashIV)

	client := NewDepositClient("", "MS1234567890", hashKey, hashIV, "", "")
	notification, err := client.VerifyDepositNotification(map[string]string{
		"TradeInfo": tradeInfo,
		"TradeSha":  GenerateDepositTradeSHA(tradeInfo, hashKey, hashIV),
	})
	if err != nil {
		t.Fatalf("VerifyDepositNotification returned error: %v", err)
	}
	if notification.OrderNo != "P20260713184827104828" {
		t.Fatalf("unexpected order no: %q", notification.OrderNo)
	}
	if notification.TradeNo != "NT123" {
		t.Fatalf("unexpected trade no: %q", notification.TradeNo)
	}
	if notification.AmountCents != 10000 {
		t.Fatalf("unexpected amount cents: %d", notification.AmountCents)
	}
	if notification.Status != "SUCCESS" {
		t.Fatalf("unexpected status: %q", notification.Status)
	}
}

func TestVerifyDepositNotificationRejectsMissingTradeSHA(t *testing.T) {
	hashKey := "12345678901234567890123456789012"
	hashIV := "1234567890123456"
	tradeInfo, err := EncryptDepositTradeInfo("Status=SUCCESS&MerchantOrderNo=P20260713184827104828&TradeNo=NT123&Amt=100", hashKey, hashIV)
	if err != nil {
		t.Fatal(err)
	}

	client := NewDepositClient("", "MS1234567890", hashKey, hashIV, "", "")
	if _, err := client.VerifyDepositNotification(map[string]string{"TradeInfo": tradeInfo}); err == nil {
		t.Fatal("notification without TradeSha must be rejected")
	}
}

func encryptRawCBCForTest(t *testing.T, plain []byte, hashKey, hashIV string) string {
	t.Helper()

	block, err := aes.NewCipher([]byte(hashKey))
	if err != nil {
		t.Fatal(err)
	}
	if len(plain)%block.BlockSize() != 0 {
		t.Fatalf("plain length %d is not a multiple of block size", len(plain))
	}

	cipherText := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte(hashIV)).CryptBlocks(cipherText, plain)
	return hex.EncodeToString(cipherText)
}

func TestIsTradeInfoPlaintextAcceptsURLQuery(t *testing.T) {
	if !isTradeInfoPlaintext([]byte(url.Values{
		"MerchantOrderNo": []string{"P20260713184827104828"},
		"Status":          []string{"SUCCESS"},
	}.Encode())) {
		t.Fatal("expected URL-encoded payload to be recognized as trade info")
	}
}

func TestEnrichDepositNotifyTraceExtractsIdentifiersFromTradeInfo(t *testing.T) {
	hashKey := "12345678901234567890123456789012"
	hashIV := "1234567890123456"
	plain := url.Values{
		"Status":          []string{"SUCCESS"},
		"MerchantOrderNo": []string{"P20260713184827104828"},
		"TradeNo":         []string{"NT123"},
		"Amt":             []string{"100"},
	}.Encode()
	tradeInfo, err := EncryptDepositTradeInfo(plain, hashKey, hashIV)
	if err != nil {
		t.Fatal(err)
	}

	client := NewDepositClient("", "MS1234567890", hashKey, hashIV, "", "")
	trace := client.EnrichDepositNotifyTrace(map[string]string{"TradeInfo": tradeInfo}, domain.DepositNotifyTrace{})
	if trace.ProviderOrderNo != "P20260713184827104828" {
		t.Fatalf("unexpected provider order no: %q", trace.ProviderOrderNo)
	}
	if trace.ProviderTradeNo != "NT123" {
		t.Fatalf("unexpected provider trade no: %q", trace.ProviderTradeNo)
	}
}
