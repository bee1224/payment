package newebpay

import (
	"net/url"
	"strconv"
	"time"

	"payment-service/internal/domain"
)

const mpgVersion = "2.3"

type DepositTradeInfo struct {
	MerchantID      string
	RespondType     string
	TimeStamp       int64
	Version         string
	MerchantOrderNo string
	Amt             int64
	ItemDesc        string
	NotifyURL       string
	ReturnURL       string
}

func MapDepositOrderToTradeInfo(order domain.DepositOrder, merchantID, notifyURL, returnURL, itemDesc string, now time.Time) DepositTradeInfo {
	if itemDesc == "" {
		itemDesc = "Deposit"
	}
	return DepositTradeInfo{
		MerchantID:      merchantID,
		RespondType:     "JSON",
		TimeStamp:       now.Unix(),
		Version:         mpgVersion,
		MerchantOrderNo: order.OrderNo,
		Amt:             order.AmountCents / 100,
		ItemDesc:        itemDesc,
		NotifyURL:       notifyURL,
		ReturnURL:       returnURL,
	}
}

func (t DepositTradeInfo) Encode() string {
	values := url.Values{}
	values.Set("MerchantID", t.MerchantID)
	values.Set("RespondType", t.RespondType)
	values.Set("TimeStamp", strconv.FormatInt(t.TimeStamp, 10))
	values.Set("Version", t.Version)
	values.Set("MerchantOrderNo", t.MerchantOrderNo)
	values.Set("Amt", strconv.FormatInt(t.Amt, 10))
	values.Set("ItemDesc", t.ItemDesc)
	if t.NotifyURL != "" {
		values.Set("NotifyURL", t.NotifyURL)
	}
	if t.ReturnURL != "" {
		values.Set("ReturnURL", t.ReturnURL)
	}
	return values.Encode()
}
