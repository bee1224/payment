# 專案登錄

| 專案 | 角色 | 狀態 | 整合邊界 |
| --- | --- | --- | --- |
| `payment-service` | 正式支付服務 | Sandbox Callback Smoke Test 已驗收；Production Ready：否 | 對外提供已文件化的 HTTP API 與 callback 契約。 |
| `merchant-sandbox` | Official Reference Merchant | v1 MVP + Sandbox Deployment + Milestone 4 Callback Smoke Test 已完成 | 僅以 Sandbox HTTP API 與 payment-service 溝通；不得使用 Production URL、憑證或資料。 |

未來新增 SDK、Portal 或工具時，先在本表登錄角色、擁有邊界、環境與對外契約，再建立專案。
