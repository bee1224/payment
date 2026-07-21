# Accounting and State

**驗證：Code Verified；Production 實際帳務資料與故障復原演練尚未驗證。**

所有金額以整數 cents 保存；TWD 請求金額在 Service 轉為 cents，不使用 float。Deposit 狀態機為 `pending → paid|failed|expired`；已 `paid` 再收到相同 Provider 成功事件時會比對 trade number，僅冪等回應，不重複 ledger 或 callback。

Payout 狀態詳見 [Payment Flows](payment-flows.md)。`merchant_payout_no` 與 payout transaction attempt 有 unique constraint。人工 case 以獨立表與 status history／operation log 處理。callback delivery status 與支付業務狀態分離：callback 未送達不會把已入帳／已完成支付退回，但必須保留 task、attempt、告警與重送路徑。

Ledger 保存方向、類型、來源事件、餘額前後、reference 與 reversal linkage；`merchant_balances` 以 merchant＋currency 唯一。對帳保存 run、mismatch item 與 resolution action，支援 adjustment／reversal 的資料模型與 Service；對外舊 reconciliation 管理路由目前 410，不能以它作為現行操作介面。

入帳、狀態、provider trace 與 callback outbox 在 repository transaction 內安排，以 unique constraint／原子條件作最後保護。commit 前 DB timeout／連線失敗須回錯，不能回成功；commit 後若回應遺失，重送依冪等鍵與 event key 恢復。程式碼無法證實特定 DB 斷線時的 VPS 實測表現，標記為尚未驗證。
