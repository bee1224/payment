# Provider、資料庫與 Worker

**驗證：Code Verified。** NewebPay adapter 目前提供七個代收 channel 的 MPG 建單與 Notify 驗證；Provider registry／資料表為擴充預留，但其他 Provider 未視為已啟用。

Migration 000001 建立 merchants、API keys、nonce、provider/channel、orders、transactions、balances、ledger、payout、callback、reconciliation、manual payout、receipt、audit、callback job、admin 與 worker lease 等資料；000002 加 MFA；000003 加 callback claim；000004 建立 reliable deposit outbox event key／attempt；000005 加 claim expiry／attempt count。已發布 migration 不可改語意，修正只能新增 migration。

Worker：deposit callback 每 15 秒（batch 20）與 expiry 每分鐘；payout callback 與 dispatch reconciliation 每 15／20 秒；daily reconciliation 啟動後與每 24 小時；manual callback 使用 `CALLBACK_WORKER_INTERVAL`。全部以 `worker_leases` 協調。callback 超時、最大次數與 lease 秒數均由設定控制；實際排程／VPS 日誌狀態尚未驗證。

Receipt 使用 Compose `receipt-data` volume 掛至 `/var/lib/payment-service/receipts`。API key 具 hash、status、primary、expiry、revoke、audit 的資料與 Service 能力，但對外舊 API key 路由已退休，現行管理操作流程尚未由可用 API 完整驗證。
