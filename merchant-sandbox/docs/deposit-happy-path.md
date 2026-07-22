# Deposit Happy Path

此文件是使用 reference merchant CLI 的最短入口。正式流程與預期輸出見 [Sandbox Onboarding](../../payment-service/docs/external/sandbox-onboarding.md)；不得以此專案的 internal、payment-service DB 或平台管理工具取代公開 API。

1. 依 [Credential Guide](credential-guide.md) 複製 `.env.example` 為 `.env` 並填入 Sandbox-only values。
2. 執行 `./scripts/run-callback-receiver.sh`，以 `curl -fsS http://127.0.0.1:8281/healthz` 確認 receiver 可用。
3. 建立含公開 HTTPS `pay_notify_url` 的代收 request JSON，執行：

   ```bash
   ./scripts/run-merchant-client.sh -operation collection-create -body tmp/collection-create.json
   ```

4. 以相同 merchant order ID 執行：

   ```bash
   ./scripts/run-merchant-client.sh -operation collection-query -body tmp/collection-query.json
   ```

   付款前預期 `status=0`；保存 `view_url` 與 `expired`，不要修改 URL。`status=0` 不代表已收款，即使目前 API 回傳的 `real_amount` 與 `order_amount` 相同。
5. 在過期前以 NewebPay Sandbox 付款頁完成付款。
6. 再次執行 collection query；預期 `status=2`，且 `real_amount` 等於 `order_amount`。
7. 在 receiver 使用的同一主機／共享 volume 執行：

   ```bash
   go run ./cmd/merchant-sandbox callback-status --order-id <merchant-order-id>
   ```

   預期 `received=true`、`hmac_valid=true`、`timestamp_valid=true`、`nonce_replay_detected=false`、`signature_version="hmac-sha256-v1"`、`response_status=200`、`response_body_is_exact_ok=true`。
8. 在首次查閱後等待 30 秒（代收 callback worker 15 秒輪詢加 15 秒緩衝），再執行相同 `callback-status` 指令。預期 `received_count` 不變；成功 callback 不會再排程重送。

完成順序為：建單 → pending 查單 → 付款 → paid 查單 → callback-status → 確認 HMAC／`OK` → 等待 30 秒 → callback-status → `received_count` 不變。

payment URL 過期時，建立新的唯一 merchant order；不要重用 URL 或篡改到期時間。問題見 [Troubleshooting](troubleshooting.md)。
