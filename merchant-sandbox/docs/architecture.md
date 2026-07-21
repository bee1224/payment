# 架構

```text
merchant-sandbox client ── HTTPS + HMAC ──> payment-service Sandbox public API
payment-service Sandbox ── HTTPS + HMAC ──> merchant-sandbox callback receiver
```

技術選型為 Go 1.22 標準庫、單一可執行 Receiver、標準庫 HTTP Client 與 JSONL 本機驗收紀錄；不使用資料庫或前端框架。此專案是獨立 module，沒有 payment-service 程式碼依賴。

Client 對代收使用 `X-Customer-Id`，對代付使用 `X-Merchant-Id`。兩者都依公開規格以 `identifier`、timestamp、nonce、HTTP method、path 與原始 request body 的 SHA-256 組成 HMAC-SHA256 canonical string。

Receiver 以收到的原始 body、HTTP method 與 request path 驗證 HTTP HMAC，並在預設 ±300 秒時間窗內以 `merchant_id + key_id + nonce` 防止重放。重放回 `409`；記憶體 TTL cache 有容量上限，Receiver 重啟後紀錄會遺失。它把驗收所需的非敏感 metadata、Callback Timestamp、Nonce／Signature 短指紋、body SHA-256、HMAC 結果與回應結果寫入 JSONL，不會新增帳務資料，也不會記錄 payload、Secret、完整 Nonce 或完整 Signature。
