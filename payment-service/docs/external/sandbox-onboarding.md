# Sandbox Onboarding：代收 Happy Path

本流程給完全獨立的外部系統商使用。範例以 [merchant-sandbox](../../../merchant-sandbox/README.md) CLI 示範；你可以自行實作，但必須遵守相同公開 API／HMAC 契約。

## 1. 準備 Credential 與 Receiver

```bash
cd merchant-sandbox
cp .env.example .env
# 以受控管道填入 Sandbox-only values；不要提交 .env。
./scripts/run-callback-receiver.sh
curl -fsS http://127.0.0.1:8281/healthz
```

預期：health endpoint 回 `OK`。下一步：將公開 HTTPS callback URL 設為 `pay_notify_url`。注意：本機 health 成功不代表平台可連線；callback 必須是公開 HTTPS URL。

## 2. 建立新代收訂單

建立受 Git ignore 的 `tmp/collection-create.json`，內容如下；訂單號、Unix 秒數與 callback URL 均需替換，再執行：

```json
{"pay_customer_id":"","pay_apply_date":"<unix-seconds>","pay_order_id":"MERCHANT-UNIQUE-<timestamp>","pay_amount":100,"pay_channel_id":"1000","pay_notify_url":"https://<your-public-host>/callbacks/payment","pay_product_name":"Sandbox order"}
```

```bash
./scripts/run-merchant-client.sh -operation collection-create -body tmp/collection-create.json
```

預期：HTTP 成功的 JSON response，`data` 含 `order_id`、`transaction_id`、`view_url`、`expired`。下一步：立即用相同 merchant order ID 查單。注意：CLI 會以 `.env` 的 Customer ID 覆寫 placeholder 並使用實際送出的 JSON bytes 簽章；不要手動複製 signature。

## 3. 查詢 pending

建立 `tmp/collection-query.json`，內容如下，再執行：

```json
{"pay_customer_id":"","pay_apply_date":"<unix-seconds>","pay_order_id":"MERCHANT-UNIQUE-<timestamp>"}
```

```bash
./scripts/run-merchant-client.sh -operation collection-query -body tmp/collection-query.json
```

預期：成功 response 的 `data` 為陣列；付款前 `status=0`。目前 `real_amount` 會回傳與 `order_amount` 相同的格式化名目金額，不可把它視為付款已完成證據。下一步：從建單／查單 response 開啟未修改的 `view_url`。注意：`expired` 後不得重用 payment URL，應建立全新商戶訂單。

## 4. 完成 Sandbox 付款並驗證 Callback

在 NewebPay Sandbox 付款頁以平台核准的測試方式完成付款。Receiver 會在 `var/callback-records.jsonl` 寫入非敏感 metadata；只檢查 HMAC、timestamp、signature version、HTTP status 與 response mode，不要輸出完整 nonce、signature 或 payload。

預期：`hmac_valid=true`、`timestamp_valid=true`、`nonce_replay_detected=false`、`signature_version=hmac-sha256-v1`、HTTP `200`、`response_body_is_exact_ok=true`。在 receiver 同一主機／共享 volume 執行 `go run ./cmd/merchant-sandbox callback-status --order-id <merchant-order-id>` 查閱這些非敏感 metadata。首次成功後等待 30 秒再查，預期 `received_count` 不變；下一步：再次查單。注意：receiver 的成功 body 必須是精確 bytes `OK`，不是 JSON、`OK\n` 或 `{"status":"OK"}`。

## 5. 確認 paid

重跑第 3 步的 `collection-query`。

預期：`status=2`、`real_amount` 等於 `order_amount`，且保留相同的 merchant order／transaction ID。完成：保存非敏感驗收資訊後結束；不需要讀取 payment-service DB、ledger 或 worker。

遇到錯誤請閱讀 [Troubleshooting](troubleshooting.md)；完整 Callback 契約見 [回調通知規格](回調通知規格.md)。
