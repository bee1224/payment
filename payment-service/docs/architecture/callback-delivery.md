# Callback Delivery

**驗證：Code Verified；Pending External Smoke Test。**

Provider Notify 是外部 Provider 呼叫我方：NewebPay 的 canonical 路由為 `POST /api/v1/deposits/providers/newebpay/notifications`，驗證 Provider payload 與來源 allowlist 後才更新業務狀態。Merchant Callback 則是我方由資料庫 task 主動通知商戶，兩者不可混稱。

代收的 callback 目標是**逐筆訂單**處理：`POST /api/pay_order` 的 `pay_notify_url` 會保存於該筆 `provider_transactions.request_payload.gateway.callback_url`；載入訂單、Provider Notify、到期處理與 outbox recovery 都會以此保存值覆寫 merchant 預設 callback，再寫入 `merchant_deposit_callback_tasks.callback_url`。`MERCHANT_CALLBACK_URL` 僅是 merchant bootstrap／歷史資料的預設值，不能取代新建代收訂單的 `pay_notify_url`。若既有歷史訂單沒有保存 request payload，該舊資料的 fallback 行為須個別確認。

## Deposit／一般 Payout callback

代收完成或到期後建立 `merchant_deposit_callback_tasks`，一般代付使用 `merchant_payout_callback_tasks`。Worker claim 到期 task、記錄 attempt，依退避重試並透過 worker lease 防止多副本重複處理；deposit task 另有 claim expiry、stale recovery、event key 與 attempt table。服務重啟後資料仍在 DB，因此可重新 claim。HTTP transport 限時、只允許 public HTTPS、固定 DNS 解析結果、拒絕 private IP 與 redirect，降低 SSRF。

現行 shared `PublicHTTPSCallbackDeliveryEngine` 對 Deposit／一般 Payout 的成功定義為 **HTTP 2xx 且 `strings.TrimSpace(body) == "OK"`**；大小寫不折疊，前後空白允許。非 2xx、非 `OK`、網路與讀取錯誤依錯誤類型記錄並決定是否 retry；最大次數由 `CALLBACK_MAX_ATTEMPTS` 設定。

## Manual Payout callback

Manual Payout **只確認共用 Delivery Engine（transport／SSRF 保護）**，不共用 Deposit／一般 Payout 的 Repository、Worker 或 Task Model。它使用 `callback_jobs`／`callback_attempts` 與 `ManualCallbackWorker`，帶 `Idempotency-Key`；退避為 1、5、15 分鐘後每小時。

**程式行為 Code Verified：** `ManualCallbackWorker` 在 delivery 後只以 HTTP status 是否為 2xx 決定成功，未檢查 response body；共享 engine 雖會對非 `OK` body 產生 error，但該 worker 不以 `result.Error` 作成功判斷。**測試／外部契約：尚未驗證。** 目前沒有找到 Manual Callback Worker 專屬成功條件測試，也沒有保留可證實的商戶契約。因此這不是「既有商戶一定接受任意 2xx」的結論，亦不能推論兩類 callback 的正式契約一定不同；外部 smoke test 與契約確認是必要項目。

重新評估 DB-backed model 的條件：task 積壓超過既有批次與輪詢能力、跨多主機協調不足、或需要多個獨立 consumer／嚴格事件串流。
