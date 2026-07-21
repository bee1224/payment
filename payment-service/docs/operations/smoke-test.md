# Smoke Test

| 類型 | 範圍 | 目前狀態 |
| -- | -- | -- |
| Local | unit／integration 測試、build、`/health` | Test Verified（本次最終驗證另列） |
| Sandbox Internal | health、登入、CORS／CSRF、列表、隔離設定、worker logs、HMAC 代收建單 | HMAC 建單與付款 redirect：Sandbox Verified（2026-07-21）；其餘項目需以既有驗收紀錄確認 |
| Sandbox External Callback | 真實測試商戶 callback、HMAC、`OK`、retry／attempt／audit | Pending External Smoke Test |
| Production | health、Nginx/TLS、logs、worker、非真實付款檢查 | Production Unverified |

管理端的 test deposit callback 僅在 Sandbox 設定允許且 MFA-authenticated admin 下可使用，且不改訂單／Ledger、不入 retry；它不是外部商戶 callback smoke test 的通過證據。

2026-07-21 的 HMAC Golden Integration Case 位於 [外部 HMAC 簽章規格](../external/雜湊訊息驗證碼簽章規格.md)。該次僅驗證建單與 redirect，沒有執行真實付款或偽造 Provider Notify。
