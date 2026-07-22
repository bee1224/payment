# 外部系統商 Sandbox 串接入口

本目錄是外部系統商唯一需要閱讀的 payment-service 文件入口。只使用公開 HTTPS API 與本目錄的契約；不需要 payment-service 原始碼、資料庫、內部工具或平台部署權限。

## Recommended reading order

1. [Sandbox Onboarding](sandbox-onboarding.md)：可複製的代收 Happy Path。
2. [Sandbox Credential Guide](merchant-credential-guide.md)：每一組識別與 Secret 的用途及保管方式。
3. [商戶代收介面串接文件](商戶代收介面串接文件.md)：建單、查單與 status 對照。
4. [雜湊訊息驗證碼簽章規格](雜湊訊息驗證碼簽章規格.md)：商戶 Request HMAC 與 Golden Example。
5. [回調通知規格](回調通知規格.md)：平台 Callback HMAC、replay protection、`OK` 契約與成功後的觀察規則。
6. [錯誤碼與重試處理](錯誤碼與重試處理.md) 與 [Troubleshooting](troubleshooting.md)。

一般代付另見 [商戶代付介面串接文件](商戶代付介面串接文件.md)；它不是本代收 Happy Path 的前置條件。

## Quick-start checklist

- 已由平台以受控管道提供 Sandbox API Base URL 與 Sandbox-only Credential。
- Callback URL 是可由 Internet 存取的公開 HTTPS URL；不可用 localhost、私有 IP、VPN-only URL 或 Production URL。
- 建單與查單都以 Customer ID／Customer Request Signing Secret 簽章；每次 request 使用新 timestamp 與 nonce。
- Callback 使用專屬 Callback Key ID／Callback Signing Secret 驗簽；成功必須回 HTTP 2xx 且 body bytes **精確為** `OK`。reference merchant 可用其受控主機上的 `callback-status` CLI 查閱非敏感 acceptance metadata。
- 建單後保存 `order_id`、`transaction_id`、`view_url`、`expired`；在 `expired` 前開啟付款頁，過期即建立全新訂單。

## Sandbox boundary

Sandbox endpoint 由平台提供；目前 reference merchant 使用 `https://sandbox-api.nnviopp.com`。不得把 Sandbox／Production URL、ID、Secret、訂單或 callback destination 混用。Production 不在本文件或本階段驗收範圍。

## Credential terms

| 類別 | 用途 | 不得混用為 |
| --- | --- | --- |
| Customer ID + Customer Request Signing Secret | 代收 API Request HMAC | Merchant／Callback credential |
| Merchant ID + Merchant Request Signing Secret + API Key | 一般代付 API | 代收或 Callback credential |
| Callback Key ID + Callback Signing Secret | 驗證平台送至商戶的 Callback | API request signing secret 或 API Key |

完整規則見 [Sandbox Credential Guide](merchant-credential-guide.md)。

## Happy Path 完成條件

外部商戶可獨立完成：代收建單、pending 查單、Sandbox 付款、Callback HMAC 驗證、精確 `OK` 回應、等待規定觀察時間確認未重送，以及再次查單確認 paid。平台端的 Provider Notify、ledger、task 與 attempt 是平台內部驗收證據，非商戶正常串接步驟。

## 向平台回報問題

提供 Sandbox 環境、時間（含時區）、merchant order ID、platform transaction ID、HTTP status、API `code`、Callback path、Callback timestamp、body SHA-256、HMAC 驗證結果及 response status／body 摘要。不得提供任何 Secret、API Key、完整 signature、完整 nonce、完整 payload、付款人資料或 Production 資料。
