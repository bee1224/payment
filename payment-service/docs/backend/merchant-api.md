# Merchant API

**驗證：Code Verified。** 本文件只定義目前文件化的商戶正式契約；所有商戶請求依 [HMAC 與來源驗證](hmac-and-security.md)，詳細 body 欄位以 Handler／測試為準。下列「active provider protocol」是程式仍提供的 Provider-facing 介面，不因 handler 存在即成為商戶正式 API。

| 類別 | Method／Path | 狀態 |
| -- | -- | -- |
| 代收建單 | `POST /api/pay_order` | Canonical，RY-compatible |
| 代收查單 | `POST /api/query_transaction` | Canonical，RY-compatible |
| 代收明細／跳轉 | `GET /api/v1/deposits/{order_no}`、`.../redirect` | 現行 |
| Provider Notify | `POST /api/v1/deposits/providers/{provider}/notifications` | 現行；NewebPay provider |
| Payment result | `GET|POST /api/v1/deposits/payment-result` | 現行前台 Return |
| 代付建單／查詢／明細 | `POST /api/payouts`、`POST /api/payouts/query`、`GET /api/payouts/{payout_no}` | 現行 workflow |
| Gateway 查詢／餘額／callback | `POST /api/payments/query_transaction`、`/balance`、`/callback` | Active Provider protocol；非商戶正式 API，外部契約尚未驗證 |

## API 分類

- **Current API：** `/api/pay_order`、`/api/query_transaction`、`/api/v1/deposits/{order_no}`、`/api/v1/deposits/{order_no}/redirect`、`/api/v1/deposits/providers/{provider}/notifications`、`/api/v1/deposits/payment-result`、`/api/payouts`、`/api/payouts/query`、`/api/payouts/{payout_no}`。
- **Deprecated compatibility API：** `/deposits`、`/api/v1/deposits`、`/api/v1/deposits/query`、`/deposits/{order_no}`、`/deposits/{order_no}/redirect`、`/notify/{provider}`、`/notify/newebpay`、`/payment/result`。程式明確回 `Deprecation: true` 與 successor Link；新串接不得採用。
- **Active Provider protocol（非商戶正式 API）：** `/api/payments/query_transaction`、`/api/payments/balance`、`/api/payments/callback`。是否仍是已簽訂的上游外部契約，尚未驗證。
- **Retired API：** `/api/payments/pay_order` 及公開 review、reconciliation、merchant API key 管理舊路由。程式回 410 是其 retired 實作證據，不是本分類的唯一依據；後者必須經 MFA-backed admin surface 才可重新設計並公開。

代收渠道僅支援：

| pay_channel_id | channel_code | 說明 |
| -- | -- | -- |
| 1000 | CREDIT | 信用卡一次付清 |
| 1001 | APPLEPAY | Apple Pay |
| 1002 | GOOGLEPAY | Google Pay |
| 1005 | WEBATM | WebATM |
| 1006 | VACC | ATM 虛擬帳號 |
| 1007 | CVS | 超商代碼 |
| 1008 | BARCODE | 超商條碼 |

不支援渠道會被拒絕（Service 的 channel-code 路徑錯誤為 `unsupported channel_code: ...`；相容 `pay_channel_id` 對應的精確錯誤以 Handler 為準，尚未驗證外部實測）。代收與一般代付 callback 成功條件是 2xx＋trim 後 `OK`；Manual Payout 例外請見 [Callback Delivery](../architecture/callback-delivery.md)。
