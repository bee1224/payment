# nnviopp Gateway 串接指南

本資料夾提供商戶與外部系統商使用的正式串接文件；不包含平台技術實作、維運內容或敏感設定。

## 文件閱讀順序

1. [商戶代收介面串接文件](商戶代收介面串接文件.md)
2. [商戶代付介面串接文件](商戶代付介面串接文件.md)
3. [雜湊訊息驗證碼簽章規格](雜湊訊息驗證碼簽章規格.md)
4. [回調通知規格](回調通知規格.md)
5. [Sandbox 串接與驗收指南](Sandbox串接與驗收指南.md)
6. [錯誤碼與重試處理](錯誤碼與重試處理.md)
7. [銀行代碼對照表](銀行代碼對照表.md)

## 測試環境（Sandbox）

目前串接請使用測試環境（Sandbox）。Production 僅於正式驗收後提供。請向平台窗口取得該環境的 API Base URL、Merchant／Customer ID 與 Secret；不得將 Sandbox 與 Production 的 URL、ID、Secret、訂單或回調網址混用。

## 注意事項

- 所有請求必須完成 HMAC 簽章、Timestamp 與 Nonce 驗證。代收的 `pay_apply_date` 也是 Unix timestamp（秒），不是日期字串。
- 每次重送請求必須使用新的 Nonce；相同業務訂單請使用既有訂單編號以取得冪等結果。
- 回調（Callback）接收端必須使用公開 HTTPS 網址，並依文件驗證簽章。
- 代收與一般代付回調成功條件為 HTTP 2xx 且回應本文為 `OK`；Manual Payout 的外部契約尚未驗證，請勿自行假設其成功條件。
