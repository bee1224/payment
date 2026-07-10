package http

import (
	nethttp "net/http"

	"payment-service/pkg/response"
)

func HealthHandler(w nethttp.ResponseWriter, r *nethttp.Request) {
	response.JSON(w, nethttp.StatusOK, map[string]string{
		"status": "ok",
	})
}
