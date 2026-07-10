package http

import (
	nethttp "net/http"

	"payment-service/pkg/response"
)

func DepositPaymentResultHandler(w nethttp.ResponseWriter, r *nethttp.Request) {
	response.JSON(w, nethttp.StatusOK, map[string]string{
		"status":  "received",
		"message": "payment result received",
	})
}
