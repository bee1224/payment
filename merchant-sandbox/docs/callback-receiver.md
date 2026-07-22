# Callback Receiver

Receiver 只接受設定的 `POST` Callback path（預設 `/callbacks/payment`）的 HMAC-SHA256 Callback Contract。它要求 `X-Callback-Merchant-Id`、`X-Callback-Key-Id`、`X-Callback-Timestamp`、`X-Callback-Nonce`、`X-Callback-Signature-Version=hmac-sha256-v1` 與 `X-Callback-Signature`。

Canonical string 與 payment-service 對外 [回調通知規格](../../payment-service/docs/external/回調通知規格.md) 相同。Receiver 以 `MERCHANT_SANDBOX_CALLBACK_SIGNING_SECRET` 驗證，並可用 `MERCHANT_SANDBOX_CALLBACK_KEY_ID` 限制目前 key。驗證失敗回 401；不接受其他簽章欄位。

Timestamp 必須是合法 Unix 秒數且落在預設 ±300 秒內；過期或過度超前回 `401`。Nonce 不得為空，同一 `merchant_id + key_id + nonce` 在有效時間窗內只能接受一次，重放回 `409`。驗簽使用常數時間比較。MVP 的 nonce cache 是有容量上限的記憶體資料；重啟後會遺失，Production Receiver 必須改用共享持久化儲存。

`success` 模式成功回覆 HTTP 200 與 body bytes 精確為 `OK`。`invalid_body` 回 HTTP 200 與 `NOT_OK`；`server_error` 回 HTTP 503；`timeout` 會延遲 `MERCHANT_SANDBOX_TIMEOUT_DELAY` 後才回 `OK`。每次通過驗簽的 callback 都會追加 JSONL 紀錄，只保存從公開代收 payload 解析的 merchant order ID、接收時間、method、path、Merchant／Key ID、Callback Timestamp、signature version、body SHA-256、HMAC／timestamp 結果、nonce replay 結果、受控回應模式、HTTP status、exact-`OK` 結果，以及 Nonce／Signature 的 SHA-256 短指紋；不保存 payload、Secret、完整 Nonce 或完整 Signature。

## Callback acceptance status

在 receiver 寫入紀錄的同一主機或共享持久化 volume 執行：

```bash
go run ./cmd/merchant-sandbox callback-status --order-id <merchant-order-id>
```

輸出為單一非敏感 JSON 摘要，包含 `merchant_order_id`、`received`、`received_count`、`first_received_at`、`last_received_at`、`hmac_valid`、`timestamp_valid`、`nonce_replay_detected`、`signature_version`、`response_status` 與 `response_body_is_exact_ok`。尚未收到時 `received=false`，其餘 acceptance 欄位為 `null`；CLI 不輸出 callback payload、headers、Secret、完整 Nonce 或完整 Signature，也不提供公開 HTTP endpoint。

代收 callback worker 每 15 秒輪詢。首次看到成功摘要後等待 30 秒，再查一次；若 `received_count` 不變，即符合平台收到 HTTP 2xx 與精確 `OK` 後停止重送的 Sandbox Happy Path。

固定 Golden Contract 與 payment-service 使用同一組測試值：`M-GOLDEN`、`cb-v1`、`1700000000`、`POST /callbacks/payment`、測試 Secret `golden-callback-secret`；完整 body、SHA-256 與預期 signature 見 payment-service 對外 Callback 規格。
