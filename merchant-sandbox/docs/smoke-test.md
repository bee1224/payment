# Sandbox Callback Smoke Test

## 公開 HTTPS Receiver

優先使用由你控制的 Sandbox 網域與 TLS reverse proxy，將公開 HTTPS path 轉送至本機或 Sandbox 主機的 loopback port。Sandbox VPS 目前使用 `127.0.0.1:8281`。例如：

```text
https://merchant-sandbox.example/callbacks/payment → http://127.0.0.1:8281/callbacks/payment
```

開啟防火牆時只公開 reverse proxy 的 HTTPS；Receiver 不應直接暴露到 Internet。若僅做短期受控測試，可使用組織核准的 HTTPS tunnel，並確認 URL 無 Production 網域、無認證攔截且平台可連線。公開 URL 與 tunnel 是人工操作，不由本專案自動建立。

## 執行流程

1. 在 `.env` 填入 Sandbox 值，啟動 Receiver，確認 `/healthz` 回 `200`／`OK`。
2. 建立公開 HTTPS callback URL，並確認該 URL path 與 `MERCHANT_SANDBOX_CALLBACK_PATH` 相同。
3. 以 `merchant-client` 建立新的 Sandbox 代收訂單，將該公開 URL 填入 `pay_notify_url`。
4. 依 Sandbox 付款頁完成測試付款，再以 `collection-query` 查詢。建單成功不是付款成功。
5. 收到 callback 後檢查 `var/callback-records.jsonl`：`hmac_valid` 必須為 `true`；另以平台與查單結果核對 Customer ID、order、transaction、金額與狀態。JSONL 不保存完整 payload、Nonce 或 signature；只保存 Callback Timestamp 與 Nonce／Signature 的 SHA-256 短指紋，供 retry 驗證使用。
6. 對相同 Header nonce 的重送，Receiver 必須回 `409`。平台的合法 retry 會使用新的 timestamp、nonce 與 signature；以 JSONL 的 timestamp 與 nonce／signature 指紋核對，不保存完整值。
7. 另建立新的受控測試訂單，首次將模式改為 `invalid_body` 或 `server_error`；待平台依其 retry policy 重送後改回 `success`。不得人工偽造 Provider 成功或重置既有成功 task。
8. `timeout` 模式僅在平台確認 timeout 門檻後使用，並將 `MERCHANT_SANDBOX_TIMEOUT_DELAY` 設為更長值；重啟 Receiver 後再建立新的測試訂單。

## 驗收交付

提供 Sandbox Merchant ID、商戶訂單號、平台交易號、callback 接收時間、HMAC 結果、HTTP status／body、body hash、Nonce／Signature 指紋與重送冪等結果。不得提供 Secret、完整 nonce、完整 signature、完整個資或完整 payload。

## 2026-07-21 Milestone 4 實際驗收

- 已部署 Receiver：`https://merchant-sandbox.nnviopp.com`
- Callback URL：`https://merchant-sandbox.nnviopp.com/callbacks/payment`
- 成功鏈路：`M4S5-20260721185303`／`P20260721185303185320`，收到 1 次 callback，HMAC valid=`true`，response mode=`success`，HTTP `200`，平台 task `sent` 且停止重送。
- Retry 鏈路：`M4R2-20260721190704`／`P20260721190704190719`，前兩次 response mode=`server_error`、HTTP `503`，第三次 response mode=`success`、HTTP `200`；三次 body hash 相同，timestamp／nonce 指紋／signature 指紋均不同。
- 重複 Provider Notify：對成功鏈路平台訂單重送 NewebPay Sandbox notify 後，ledger/task/attempt 維持 `1/1/1`。
- Production：僅唯讀 health check，未部署、未重啟、未 migration。
