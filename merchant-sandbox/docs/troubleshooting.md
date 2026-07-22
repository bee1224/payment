# Troubleshooting

先閱讀平台的 [外部商戶 Troubleshooting](../../payment-service/docs/external/troubleshooting.md)。reference merchant 的專屬檢查如下：

| 症狀 | 檢查 |
| --- | --- |
| CLI 拒絕 API URL | `PAYMENT_SANDBOX_BASE_URL` 必須是 HTTPS Sandbox URL，不能是 Production host。 |
| CLI 在送出前拒絕 request | 確認 Unix 秒數、正整數金額、唯一 order ID、公開 HTTPS callback URL。 |
| `/healthz` 成功但收不到 Callback | 本機 health 不代表公開可達；確認 reverse proxy／公開 DNS／TLS／callback path。 |
| Callback 401 | 確認 Callback Key ID、Signing Secret、raw body、timestamp 與 signature version；不要記錄完整 headers。 |
| Callback 409 | 同一 nonce 被重放；保留冪等結果，不要把它當成付款失敗。 |
| 平台持續重送 | receiver 必須回 2xx 且 response body bytes 精確為 `OK`；不要回 JSON 或 `OK\n`。 |
| 無法確認 callback 是否成功 | 在 receiver 的同一主機／共享 volume 執行 `go run ./cmd/merchant-sandbox callback-status --order-id <merchant-order-id>`；確認 `received`、HMAC、timestamp、signature version、response 與 count，不要讀取 payment-service DB。 |
| 成功後疑似仍重送 | 首次成功 callback-status 後等待 30 秒再查；`received_count` 必須不變。若增加，回報兩次查閱時間與非敏感摘要。 |

回報時提供 order／transaction ID、時間、callback path、HTTP status、body SHA-256 與 HMAC 結果；不得提供 `.env`、Secret、API Key、完整 nonce、signature 或 payload。
