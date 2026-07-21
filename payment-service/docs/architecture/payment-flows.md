# Payment Flows

**驗證：Code Verified；外部端對端 callback 為 Pending External Smoke Test。**

## 代收

`POST /api/pay_order` → HMAC／timestamp／nonce → DepositHandler／DepositService → `orders`、`provider_transactions` → NewebPay MPG payment form／redirect → 使用者付款 → `POST /api/v1/deposits/providers/newebpay/notifications` → Notify 驗證 → 同一資料庫交易更新訂單、provider transaction、Ledger、callback outbox → Deposit Callback Worker → 商戶 callback。

狀態為 `pending`、`paid`、`failed`、`expired`。`(merchant_id, merchant_order_no)` 唯一性與建立時同內容重送回復既有訂單；不同內容 fail closed。Notify 金額、Provider trade number 及已付款狀態會核對；相同成功 Notify 不重複入帳或新增 callback。callback event key 以訂單與狀態建立，資料庫 unique constraint 防重。到期處理由 worker 掃描，屬 `expired` 後可建立相應 callback task。未成功的 Provider Notify 會留下追蹤／失敗紀錄；資料庫錯誤不把未完成狀態偽裝為成功。

## 代付與人工代付

`POST /api/payouts` → 商戶認證／冪等鍵 `merchant_payout_no` → `payout_orders`（`pending_review`）與受款人／帳務保留 → 管理員登入 → `POST /api/admin/payouts/{payout_no}/start-processing` claim → 人工轉帳 → 收據上傳 → `confirm-success` → 完成狀態、audit／operation log、callback job → Manual Callback Worker → 商戶 callback。

Payout domain 狀態：`pending_review`、`approved`、`submitting`、`processing`、`completed`、`failed`、`reversed`、`rejected`、`cancelled`；人工 case 狀態獨立，以大寫保存，避免被 Gateway worker 誤處理。claim 與版本／條件更新用於阻止多人同時開始；重複確認由 case／狀態與 callback job idempotency key 保護。收據僅接受實際 magic bytes 為 PDF/JPEG/PNG/WebP、最大 10 MB、隨機不可猜 storage key、0600 權限，並拒絕 path traversal。失敗、取消與 callback retry 都留下 audit／attempt；告警寫入 `payout_operational_alerts`，可選 webhook 通知。

Provider Gateway 代付 callback 入口為 `/api/payments/callback`。`/api/payments/pay_order` 已退休（410），不會建立本地代付；高風險 review、reconciliation 與 API-key 舊公開路由也已退休（410），目前不應描述為可供商戶或管理員操作。
