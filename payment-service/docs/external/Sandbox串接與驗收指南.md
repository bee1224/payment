# Sandbox 串接與驗收指南

本指南讓外部系統商以 Sandbox 完成代收與 callback 驗收。一般代付是獨立流程，僅在商戶啟用該功能後依[代付文件](商戶代付介面串接文件.md)執行；Production 憑證、URL、callback URL 與訂單資料不可使用於本流程。完整代收操作順序見 [Sandbox Onboarding](sandbox-onboarding.md)。

## 取得資料

請向平台窗口取得 Sandbox API Base URL、Customer ID（代收）、Merchant ID（代付）、Sandbox Secret／API Key、可用渠道與測試窗口。Secret 僅能保存在伺服器端，不能放在瀏覽器、App 或原始碼庫。

## 串接順序

1. 依[簽章規格](雜湊訊息驗證碼簽章規格.md)完成 HMAC 產生與驗證。
2. 呼叫[代收 API](商戶代收介面串接文件.md)建立訂單，以回傳的 `view_url` 導轉付款人完成 Sandbox 付款。
3. 以 `POST /api/query_transaction` 查詢交易；建單成功不等於付款成功。
4. 完成下列 callback smoke test。

完成本指南的代收 Happy Path 不以一般代付為前置或完成條件。

## Callback Smoke Test

callback 接收端必須是公開 HTTPS URL，並可從網際網路連線。不得使用 localhost、私有 IP 或需 VPN 才能存取的網址。

1. 準備公開 HTTPS `pay_notify_url`；接收端必須能從網際網路存取、驗證 HMAC-SHA256 Callback Headers，並對同一 `transaction_id` 與最終狀態冪等處理。此 URL 會隨本次建單逐筆保存，請勿假設平台會使用其他訂單或全域 callback 設定。
2. 建立一筆新的 Sandbox 代收訂單，保存商戶訂單編號與平台交易編號。
3. 透過 Sandbox 付款頁完成測試付款；建單成功與 redirect 可達不等於付款、Provider Notify 或 callback 已驗證。
4. 接收 `POST` callback，以收到的**原始 body**與 HTTP headers 驗證 HMAC，並記錄 callback URL path、接收時間與驗證結果（不得提交 Secret 或完整 signature）。
5. 核對 Customer ID、訂單號、交易號、金額與付款成功狀態。
6. 在資料庫交易或等價的冪等機制完成自身更新後，回覆：

   ```text
   HTTP/1.1 200 OK

   OK
   ```

7. 以同一 callback 再送一次或由平台受控重送時，接收端不得重複入帳，但仍應回覆 `200` 與 `OK`。
8. 另建立一筆新的測試訂單，在受控測試窗口首次回覆非 2xx 或非 `OK` 一次；平台重送後再回覆 `200` 與 `OK`。不可把第一筆成功訂單的 task 人為重置或手工偽造 Provider 成功當作重試驗收。

## 驗收提交項目

請向平台窗口提供：Sandbox Merchant／Customer ID、代收訂單號、代付訂單號（若適用）、callback 接收時間、驗簽結果、最終 HTTP status／body，以及重複 callback 的冪等處理結果。請勿提供 Secret、完整個資或完整 Authorization／signature 值。

完成條件：代收建單、付款、查單、callback HMAC 驗證與 `2xx + OK` 都成功；如有代付需求，代付建單、查詢及 callback 亦須完成。平台會以自身 callback task／attempt 紀錄交叉驗證。
