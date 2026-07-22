# Credential Guide

reference merchant 的 `.env` 名稱對照正式 [Sandbox Credential Guide](../../payment-service/docs/external/merchant-credential-guide.md)。本頁只說明設定位置，不複製 API／Callback 規格。

| `.env` key | 正式 Credential | 用途 |
| --- | --- | --- |
| `PAYMENT_SANDBOX_BASE_URL` | Sandbox API Base URL | 代收／代付公開 HTTPS API。 |
| `PAYMENT_CUSTOMER_ID` | Customer ID | 代收 body 與 `X-Customer-Id`。 |
| `PAYMENT_CUSTOMER_SECRET` | Customer Request Signing Secret | 代收 Request HMAC。 |
| `PAYMENT_MERCHANT_ID` | Merchant ID | 一般代付 body 與 `X-Merchant-Id`。 |
| `PAYMENT_MERCHANT_SECRET` | Merchant Request Signing Secret | 一般代付 Request HMAC。 |
| `PAYMENT_API_KEY` | API Key | 一般代付 body。 |
| `MERCHANT_SANDBOX_CALLBACK_KEY_ID` | Callback Key ID | receiver 比對 `X-Callback-Key-Id`。 |
| `MERCHANT_SANDBOX_CALLBACK_SIGNING_SECRET` | Callback Signing Secret | receiver 驗證 Callback HMAC。 |

只填 Sandbox 值；`.env` 已被 Git ignore，但仍不得傳送、輸出或提交 Secret／API Key。reference merchant 不接受 `https://api.nnviopp.com` 作為 Sandbox API Base URL。
