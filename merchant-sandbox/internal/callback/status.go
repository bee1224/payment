package callback

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type AcceptanceStatus struct {
	MerchantOrderID     string  `json:"merchant_order_id"`
	Received            bool    `json:"received"`
	ReceivedCount       int     `json:"received_count"`
	FirstReceivedAt     *string `json:"first_received_at"`
	LastReceivedAt      *string `json:"last_received_at"`
	HMACValid           *bool   `json:"hmac_valid"`
	TimestampValid      *bool   `json:"timestamp_valid"`
	NonceReplayDetected *bool   `json:"nonce_replay_detected"`
	SignatureVersion    *string `json:"signature_version"`
	ResponseStatus      *int    `json:"response_status"`
	ResponseBodyExactOK *bool   `json:"response_body_is_exact_ok"`
}

func LoadAcceptanceStatus(recordsPath, merchantOrderID string) (AcceptanceStatus, error) {
	merchantOrderID = strings.TrimSpace(merchantOrderID)
	if merchantOrderID == "" {
		return AcceptanceStatus{}, fmt.Errorf("merchant order ID is required")
	}
	status := AcceptanceStatus{MerchantOrderID: merchantOrderID}
	f, err := os.Open(recordsPath)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		return AcceptanceStatus{}, fmt.Errorf("open callback records: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record acceptanceRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return AcceptanceStatus{}, fmt.Errorf("read callback record: %w", err)
		}
		if record.MerchantOrderID != merchantOrderID {
			continue
		}
		status.Received = true
		status.ReceivedCount++
		if status.FirstReceivedAt == nil {
			status.FirstReceivedAt = stringPointer(record.ReceivedAt)
		}
		status.LastReceivedAt = stringPointer(record.ReceivedAt)
		status.HMACValid = boolPointer(record.HMACValid)
		status.TimestampValid = boolPointer(record.TimestampValid)
		status.NonceReplayDetected = boolPointer(record.NonceReplayDetected)
		status.SignatureVersion = stringPointer(record.SignatureVersion)
		status.ResponseStatus = intPointer(record.HTTPStatus)
		status.ResponseBodyExactOK = boolPointer(record.ResponseBodyExactOK)
	}
	if err := scanner.Err(); err != nil {
		return AcceptanceStatus{}, fmt.Errorf("scan callback records: %w", err)
	}
	return status, nil
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func intPointer(value int) *int          { return &value }
