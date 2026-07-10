package newebpay

import (
	"encoding/json"
	"net/url"
	"strconv"
)

type DepositNotifyPayload struct {
	Status          string `json:"Status"`
	MerchantOrderNo string `json:"MerchantOrderNo"`
	TradeNo         string `json:"TradeNo"`
	Amt             int64  `json:"Amt"`
}

func ParseDepositNotify(payload []byte) (DepositNotifyPayload, error) {
	var envelope struct {
		Status string          `json:"Status"`
		Result json.RawMessage `json:"Result"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil {
		if len(envelope.Result) > 0 {
			result, err := parseNotifyJSON(envelope.Result)
			if err == nil && result.MerchantOrderNo != "" {
				if result.Status == "" {
					result.Status = envelope.Status
				}
				return result, nil
			}
		}

		direct, err := parseNotifyJSON(payload)
		if err == nil && direct.MerchantOrderNo != "" {
			if direct.Status == "" {
				direct.Status = envelope.Status
			}
			return direct, nil
		}
	}

	values, err := url.ParseQuery(string(payload))
	if err != nil {
		return DepositNotifyPayload{}, err
	}
	amt, _ := strconv.ParseInt(values.Get("Amt"), 10, 64)
	return DepositNotifyPayload{
		Status:          values.Get("Status"),
		MerchantOrderNo: values.Get("MerchantOrderNo"),
		TradeNo:         values.Get("TradeNo"),
		Amt:             amt,
	}, nil
}

func parseNotifyJSON(payload []byte) (DepositNotifyPayload, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return DepositNotifyPayload{}, err
	}
	return DepositNotifyPayload{
		Status:          stringValue(raw["Status"]),
		MerchantOrderNo: stringValue(raw["MerchantOrderNo"]),
		TradeNo:         stringValue(raw["TradeNo"]),
		Amt:             intValue(raw["Amt"]),
	}, nil
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}

func intValue(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case string:
		parsed, _ := strconv.ParseInt(v, 10, 64)
		return parsed
	default:
		return 0
	}
}
