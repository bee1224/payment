# Sandbox Credential Guide

所有 Credential 僅限 Sandbox，必須由平台以受控管道交付並保存在商戶伺服器端 Secret store 或受限環境檔。不得提交版本控制、寫入前端、log、文件、截圖、Email 或聊天明文。需要輪替、懷疑洩漏或環境切換時，請向平台申請新組，不要自行重用其他類別的值。

| Credential | 用途與出現位置 | 持有端／記錄規則 | 不可混用 |
| --- | --- | --- | --- |
| Sandbox API Base URL | API request 的 HTTPS base URL | 商戶 server；可記錄 URL | Production URL |
| Customer ID | 代收 body `pay_customer_id` 與 `X-Customer-Id` | 商戶 server；可作受控識別 | Merchant ID、Callback Key ID |
| Customer Request Signing Secret | 代收 request HMAC | 商戶 server；不可記錄或明文傳送 | Merchant Request／Callback Signing Secret、API Key |
| Merchant ID | 一般代付 body `merchant_id` 與 `X-Merchant-Id` | 商戶 server；可作受控識別 | Customer ID、Callback Key ID |
| Merchant Request Signing Secret | 一般代付 request HMAC | 商戶 server；不可記錄或明文傳送 | Customer／Callback Signing Secret、API Key |
| API Key | 一般代付 body `api_key` | 商戶 server；視同 Secret，不可記錄或明文傳送 | 任一 HMAC Secret |
| Callback Key ID | `X-Callback-Key-Id` 的輪替識別 | 平台送出、商戶比對；可記錄 | Customer／Merchant ID |
| Callback Signing Secret | 驗證平台 Callback HMAC-SHA256 | 商戶 receiver；不可記錄或明文傳送 | 任一 API request secret、API Key |
| Merchant Callback URL | 每筆代收的 `pay_notify_url` | 商戶控制的公開 HTTPS URL；可記錄網域與 path | localhost、私有網路或 Production destination |

## Header／流程對照

- 代收：`X-Customer-Id`、`X-Timestamp`、`X-Nonce`、`X-Signature`；以 Customer Request Signing Secret 計算。
- 一般代付：`X-Merchant-Id`、`X-Timestamp`、`X-Nonce`、`X-Signature`；以 Merchant Request Signing Secret 計算，body 另含 API Key。
- Callback：平台帶 `X-Callback-*` Headers；商戶以 Callback Signing Secret 驗證，不產生 API request signature。

## 交付與輪替

平台不得以 Email／聊天明文交付 Secret 或 API Key。商戶不得在 support case 貼出 Secret；僅提供非敏感 request／callback metadata。輪替時保留平台指定的過渡流程與 Callback Key ID 對照，完成後撤銷舊值。
