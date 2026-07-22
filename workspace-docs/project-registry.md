ㄕ# 專案登錄

| 專案 | 角色 | 狀態 | 整合邊界 |
| --- | --- | --- | --- |
| `payment-service` | 正式支付服務 | Milestone 5、Milestone 6、Milestone 6A 已完成；External Merchant Sandbox Ready；Production Ready：否 | 對外提供已文件化的 HTTP API 與 callback 契約；唯一外部入口為 `payment-service/docs/external/README.md`。 |
| `merchant-sandbox` | Official Reference Merchant | Sandbox Deployment、Milestone 4、Milestone 5、Milestone 6A 與 Fresh Session Happy Path 已完成 | 僅以 Sandbox HTTP API 與 payment-service 溝通；不得使用 Production URL、憑證或資料。 |

基礎設施網路與 Port 的唯一完整清單見 [Service Port & Configuration Registry](infrastructure/service-port-registry.md)。

未來新增 SDK、Portal 或工具時，先在本表登錄角色、擁有邊界、環境與對外契約，再建立專案。
