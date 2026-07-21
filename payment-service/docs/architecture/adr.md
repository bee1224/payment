# Architecture Decision Records

## ADR-001：採用 Go 單體服務

**Status：Accepted — Code Verified。**

- **Context：** Phase 1 的 API、支付流程與管理端由同一服務及 MariaDB 支援，部署目標為單一 VPS。
- **Decision：** 採 Go 單體，並維持 Handler → Service → Repository 分層。
- **Consequences：** 交易與部署邊界直接；模組責任必須清楚，不能以微服務取代必要的帳務一致性。
- **Revisit Conditions：** 多團隊獨立交付、容量／故障域需要隔離，且有量測證據證明單體邊界不足時。

## ADR-002：新增 Production／Sandbox 雙環境

**Status：Accepted — 設定 Code Verified；早期聯調歷史為 Historical Record。**

- **Context：** 第一版曾直接以 Production 外部聯調，造成 Notify、Return、callback、HMAC、IP、TLS、測試訂單及資料污染風險。
- **Decision：** 建立獨立 Sandbox；Production 不再作一般串接測試場。
- **Consequences：** 每次晉級須核對兩套設定、Provider endpoint、callback 與隔離資源；Sandbox Verified 不等於 Production Ready。
- **Revisit Conditions：** 僅在兩環境已有實體隔離或部署策略改變、且完成風險審查時調整。

## ADR-003：由 AWS Lightsail 遷移至 Namecheap VPS

**Status：Accepted — Historical Record；現役部署實況 Production Unverified。**

- **Context：** AWS Lightsail 已不再是現役部署環境。
- **Decision：** 現行部署依據為 `/opt/payment/payment-service` 與 `/opt/payment/payment-service-sandbox` 的 Namecheap VPS 雙環境。
- **Consequences：** 舊 Lightsail 操作文件不得作現行 runbook；僅保留本 ADR 的歷史脈絡。
- **Revisit Conditions：** 基礎設施或部署供應商變更，並完成新的拓樸、回滾與隔離審查時。

## ADR-004：Callback 採 DB-backed Worker

**Status：Accepted — Code Verified。**

- **Context：** callback 要能保存 task／attempt、重試、claim 與服務重啟後的恢復。
- **Decision：** 使用 MariaDB-backed task、attempt 與 worker lease，不導入 Kafka、RabbitMQ 或 Kubernetes。
- **Consequences：** 適合目前單 VPS／負載；需監測 DB 可用性、task 積壓與 polling 能力。
- **Revisit Conditions：** 跨主機協調、積壓、吞吐／延遲目標或獨立 consumer 需求超出 DB polling 模型時。

## ADR-005：Manual Payout 只共用 Delivery Engine

**Status：Accepted — Code Verified；外部 callback contract 尚未驗證。**

- **Context：** Manual Payout 是人工工作流，保存獨立 case、job、attempt、audit 及 retry 語意。
- **Decision：** 僅共用安全 HTTP transport／SSRF 保護；不合併 Repository、Worker 或 Task Model。
- **Consequences：** 避免把人工流程與 Deposit／一般 Payout 的狀態及成功條件強行耦合；需維護不同 task 的觀測與測試。
- **Revisit Conditions：** 契約、成功條件、retry、audit 與狀態語意已明確收斂，且有回歸／外部證據時。

## ADR-006：由舊 `pay_md5_sign` 遷移至 HMAC Header

**Status：Accepted — Code Verified。**

- **Context：** 舊 `pay_md5_sign` 不再是可接受的商戶驗證方式。
- **Decision：** 使用 `X-Customer-Id`、`X-Timestamp`、`X-Nonce`、`X-Signature`、HMAC-SHA256、current／previous secret 與 nonce 防重放，所有失敗 fail closed。
- **Consequences：** 商戶必須完成 Header 遷移；rotation 期間才可短期接受 previous secret，不得記錄 secret 值。
- **Revisit Conditions：** 安全標準、secret rotation 模式或對外簽章版本變更且完成相容性分析時。
