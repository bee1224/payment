# Callback Delivery

**驗證：** 代收 Callback Smoke Test Complete、Sandbox Verified；一般代付與 Manual Payout 仍未完成端對端 Sandbox 驗收。Production Ready：否。

Provider Notify 是外部 Provider 呼叫我方：NewebPay 的 canonical 路由為 `POST /api/v1/deposits/providers/newebpay/notifications`，驗證 Provider payload 與來源 allowlist 後才更新業務狀態。Merchant Callback 則是我方由資料庫 task 主動通知商戶，兩者不可混稱。

代收的 callback 目標是**逐筆訂單**處理：`POST /api/pay_order` 的 `pay_notify_url` 會保存於該筆 `provider_transactions.request_payload.gateway.callback_url`；載入訂單、Provider Notify、到期處理與 outbox recovery 都會以此保存值覆寫 merchant 預設 callback，再寫入 `merchant_deposit_callback_tasks.callback_url`。`MERCHANT_CALLBACK_URL` 僅是 merchant bootstrap／歷史資料的預設值，不能取代新建代收訂單的 `pay_notify_url`。若既有歷史訂單沒有保存 request payload，該舊資料的 fallback 行為須個別確認。

## Deposit／一般 Payout callback

代收完成或到期後建立 `merchant_deposit_callback_tasks`，一般代付使用 `merchant_payout_callback_tasks`。Worker claim 到期 task、記錄 attempt，依退避重試並透過 worker lease 防止多副本重複處理；deposit task 另有 claim expiry、stale recovery、event key 與 attempt table。服務重啟後資料仍在 DB，因此可重新 claim。HTTP transport 限時、只允許 public HTTPS、固定 DNS 解析結果、拒絕 private IP 與 redirect，降低 SSRF。

現行 shared `PublicHTTPSCallbackDeliveryEngine` 與所有 Callback worker 的唯一成功定義為 **HTTP 2xx 且 response body 原始 bytes 精確等於 ASCII `OK`**。前後空白、換行、大小寫差異、空 body、JSON body 及非 2xx 都失敗；失敗依錯誤類型記錄並進入既有 retry／attempt／dead-letter 流程。

## Manual Payout callback

Manual Payout **只確認共用 Delivery Engine（transport／SSRF 保護）**，不共用 Deposit／一般 Payout 的 Repository、Worker 或 Task Model。它使用 `callback_jobs`／`callback_attempts` 與 `ManualCallbackWorker`，帶 `Idempotency-Key`；退避為 1、5、15 分鐘後每小時。

`ManualCallbackWorker` 透過同一 Delivery Engine 的結果判定成功，因此同樣要求 HTTP 2xx 與原始 bytes 精確 `OK`；不因其獨立的 Repository、Worker 或 Task Model 而放寬。Manual Payout 的完整操作與外部 Sandbox 驗收仍未完成。

重新評估 DB-backed model 的條件：task 積壓超過既有批次與輪詢能力、跨多主機協調不足、或需要多個獨立 consumer／嚴格事件串流。
