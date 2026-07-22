# 外部商戶 Troubleshooting

本頁只處理正常串接排查。回報平台前，請保留非敏感 metadata；不得傳送 Secret、API Key、完整 signature、完整 nonce、完整 payload、付款人資料或 Production 資料。

| 症狀 | 最可能原因 | 商戶端檢查 | 提供平台的非敏感資料 |
| --- | --- | --- | --- |
| `signature verification failed`／code `1003` | identifier、path、timestamp、nonce 或 raw body 簽錯 | 確認簽的是實際送出的 bytes、method 為 `POST`、path 不含 host/query | 時間、HTTP status、code、identifier 類別、method、path、body SHA-256、signature 指紋 |
| Request path 簽錯 | canonical path 含 host、query、舊路由或少了 path segment | 僅簽公開文件中的 path，例如 `/api/pay_order` | method、path、HTTP status、code、body SHA-256 |
| Raw JSON 與實際送出 body 不一致 | 簽後重新 marshal、格式化或 middleware 改寫 body | 先 serialize，再對同一 bytes 簽章並送出；不得重排欄位 | body SHA-256、內容長度、method、path；不提供 body 原文 |
| Customer ID／Merchant ID 混用 | 代收誤用 Merchant credential，或反向 | 代收只用 Customer；一般代付才用 Merchant + API Key | endpoint、identifier 類別、code；不提供值 |
| API Key 與 HMAC Secret 混用 | 將 API Key 作為簽章 key | 代付 API Key 只放 body；簽章用 Merchant Request Signing Secret | endpoint、code、request credential 類別 |
| timestamp 被拒絕 | 秒／毫秒混用、時鐘偏移或過期 | 使用目前 Unix 秒數，重新簽章 | timestamp、當前時區、code；不提供 signature |
| Callback URL 無法使用 | 非公開 HTTPS、private IP、DNS/TLS 問題 | 從 Internet 驗證 HTTPS 與正確 path，不用 localhost/VPN-only URL | callback 網域與 path、DNS／TLS 錯誤摘要 |
| Receiver 回 JSON 或 `OK\n` | 成功 response 不符合精確 bytes 契約 | 回 HTTP 2xx，body 只寫 ASCII `OK` | HTTP status、response body 長度與 SHA-256 摘要 |
| Payment URL 過期 | 超過 `expired` | 建立全新唯一 merchant order；不可改寫或重用過期 URL | merchant order ID、transaction ID、expired time |
| 查單仍為 pending | 尚未付款或 Provider Notify 尚未完成 | 核對付款完成後，使用相同 merchant order 重查 | merchant order ID、transaction ID、查詢時間、status、amount |
| 已付款但 Callback 未到 | callback URL 可達性、驗簽或平台 delivery 尚在處理 | 保持 receiver 可用，查 receiver health／非敏感 acceptance metadata | transaction ID、callback path、接收時間窗、HMAC 結果、HTTP status |
| Callback 成功後仍看到 retry | 回應非精確 `OK`、非 2xx，或觀察到舊 delivery | 檢查每次 callback 的 status／body bytes 與 event idempotency | transaction ID、callback timestamp、response status、body SHA-256、HMAC 結果 |

網路逾時或 5xx 時，先以原 merchant order ID 查單；若需重送 API request，產生新的 timestamp、nonce、signature，且不改變同一筆商務訂單的內容。詳見 [錯誤碼與重試處理](錯誤碼與重試處理.md)。
