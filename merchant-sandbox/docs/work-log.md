# 工作紀錄

## 2026-07-21 — MVP 建立

* 建立獨立 Go 1.22 Official Reference Merchant。
* 依 payment-service 外部文件實作代收／代付 HMAC client 與公開 Callback Receiver。
* 提供 success、invalid_body、server_error、timeout 受控回應模式。
* Callback Contract Freeze 後，代收與代付 Callback 僅使用公開的 `hmac-sha256-v1` Header HMAC。

## 2026-07-21 — Milestone 3A MVP 補齊

* 補上本機 `.env` 載入、四個 API Client 的前置驗證與安全 HTTP 錯誤處理。
* Receiver 補上非敏感 JSONL callback 驗收紀錄，以及 timestamp、nonce replay、原始 body HMAC 的測試。
* CLI scripts、README、架構、Receiver 與 Smoke Test 文件同步為目前行為。

## 2026-07-21 — Milestone 4 Sandbox Callback Smoke Test

* 作為已部署 Official Reference Merchant 完成 payment-service Sandbox 真實代收 Callback Smoke Test。
* 成功鏈路 `M4S5-20260721185303` 驗證 HMAC valid、HTTP `200 OK`、平台 task `sent` 與停止重送。
* Retry 鏈路 `M4R2-20260721190704` 驗證前兩次 `503`、第三次 `200 OK`，且 retry 使用新的 timestamp、nonce 指紋與 signature 指紋。
* JSONL 驗收紀錄新增 Callback Timestamp 與 Nonce／Signature 短指紋；仍不保存 Secret、完整 nonce、完整 signature 或 payload。
