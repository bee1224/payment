#!/usr/bin/env bash
set -euo pipefail

# Run from the WSL project root. It reads the local Sandbox env, calls the
# public Sandbox API by default, never prints the HMAC secret or full
# signature, and writes its response to /tmp for controlled inspection.
mode="${1:-valid}"
case "$mode" in
  valid|invalid) ;;
  *) echo "usage: $0 [valid|invalid]" >&2; exit 2 ;;
esac

env_value() {
  sed -n "s/^$1=//p" ./.env.sandbox | tail -n 1 | tr -d '\r'
}

GATEWAY_CUSTOMER_ID="$(env_value GATEWAY_CUSTOMER_ID)"
GATEWAY_HMAC_SECRET="$(env_value GATEWAY_HMAC_SECRET)"
PAY_NOTIFY_URL="${2:-${PAY_NOTIFY_URL:-}}"
SANDBOX_API_BASE_URL="${SANDBOX_API_BASE_URL:-https://sandbox-api.nnviopp.com}"
for required in GATEWAY_CUSTOMER_ID GATEWAY_HMAC_SECRET; do
  if [[ -z "${!required}" ]]; then
    echo "missing required sandbox setting: $required" >&2
    exit 1
  fi
done
if [[ -z "$PAY_NOTIFY_URL" ]]; then
  echo "usage: $0 [valid|invalid] https://public.example/callback" >&2
  echo "PAY_NOTIFY_URL may be supplied as the second argument instead." >&2
  exit 2
fi
if [[ ! "$PAY_NOTIFY_URL" =~ ^https:// ]]; then
  echo "pay_notify_url must be an absolute HTTPS URL" >&2
  exit 2
fi
SANDBOX_API_BASE_URL="${SANDBOX_API_BASE_URL%/}"
if [[ ! "$SANDBOX_API_BASE_URL" =~ ^https:// && ! "$SANDBOX_API_BASE_URL" =~ ^http://127\.0\.0\.1:[0-9]+$ ]]; then
  echo "SANDBOX_API_BASE_URL must be public HTTPS or an explicit local loopback URL" >&2
  exit 2
fi

timestamp="$(date +%s)"
nonce="hmac-smoke-${timestamp}-$(openssl rand -hex 8)"
order_id="HMAC-SMOKE-${timestamp}"
body="$(printf '{\"pay_customer_id\":\"%s\",\"pay_apply_date\":\"%s\",\"pay_order_id\":\"%s\",\"pay_amount\":100,\"pay_channel_id\":\"1000\",\"pay_notify_url\":\"%s\",\"pay_product_name\":\"Sandbox HMAC Smoke\"}' "$GATEWAY_CUSTOMER_ID" "$timestamp" "$order_id" "$PAY_NOTIFY_URL")"
body_hash="$(printf %s "$body" | sha256sum | awk '{print $1}')"
canonical="$(printf '%s\n%s\n%s\nPOST\n/api/pay_order\n%s' "$GATEWAY_CUSTOMER_ID" "$timestamp" "$nonce" "$body_hash")"
if [[ "$mode" == "invalid" ]]; then
  signature="intentionally-invalid-signature"
else
  signature="$(printf %s "$canonical" | openssl dgst -sha256 -hmac "$GATEWAY_HMAC_SECRET" -hex | awk '{print $2}')"
fi

response_file="/tmp/nnviopp-${mode}-pay-order-${timestamp}.json"
status="$(curl --silent --show-error --output "$response_file" --write-out '%{http_code}' \
  --request POST "${SANDBOX_API_BASE_URL}/api/pay_order" \
  --header 'Content-Type: application/json' \
  --header "X-Customer-Id: $GATEWAY_CUSTOMER_ID" \
  --header "X-Timestamp: $timestamp" \
  --header "X-Nonce: $nonce" \
  --header "X-Signature: $signature" \
  --data-binary "$body")"

printf 'mode=%s order_id=%s status=%s body_sha256=%s response_sha256=%s response_file=%s\n' \
  "$mode" "$order_id" "$status" "$body_hash" "$(sha256sum "$response_file" | awk '{print $1}')" "$response_file"
