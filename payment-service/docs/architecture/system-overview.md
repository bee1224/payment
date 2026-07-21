# System Overview

**驗證：Code Verified；部署實況除既有 Sandbox 驗收外均為 Production Unverified。**

nnviopp Payment Service 是單體支付整合服務。Merchant 以 HMAC 呼叫本服務；Gateway 是代付上游介面；NewebPay 是目前代收 Provider；終端付款人前往 Provider 付款頁；商戶接收本服務的結果 callback。`RIG001` 是目前設定中的 Merchant／Customer 識別。RY-compatible contract 是為既有串接保留的欄位／路由相容層，非新的產品名稱或永久架構邊界。

## 元件與依賴

```text
React/Vite Admin -> Nginx -> Go HTTP handlers -> Services -> Repositories -> MariaDB
Merchant -> HMAC API ----^       |                    |-> DB-backed workers
NewebPay Notify -> Handler ------+-> Provider adapters
```

Go API 包含 HTTP Handler、Service、Repository、Provider；MariaDB 保存訂單、帳務、callback task、nonce、audit 與 lease；Docker Compose 提供 API／Admin／MariaDB；Nginx 位於主機層反向代理。Worker 由資料庫 task 與 lease 協調，不需要 Kafka、RabbitMQ 或 Kubernetes 來支援目前單 VPS 與負載；只有多主機高可用、持續積壓、吞吐／延遲目標超出 DB polling 能力或需要跨服務事件消費時才重新評估。

## 第一階段範圍

代收建單／查單／付款跳轉／NewebPay Notify／Ledger／callback；代付工作流與人工處理、收據、audit、告警；Admin Session／CSRF／MFA；HMAC、nonce、allowlist 與 trusted proxy；對帳資料模型與排程。未包含：多 Provider 的完整商業啟用、跨可用區 HA、訊息佇列、Production 外部 callback 成功驗收。

Sandbox 與 Production 使用同一程式碼來源及不同設定；現仍待外部商戶 callback smoke test。Sandbox Verified 不代表 Production Ready。
