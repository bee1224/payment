# Production 部署 Runbook

Production 與 Sandbox 的 Current Runtime Port、Nginx upstream 與 Compose project 以 [Service Port & Configuration Registry](../../../workspace-docs/infrastructure/service-port-registry.md) 為唯一完整清單；本 Runbook 不重複維護 Port 表格。

**Production Unverified。需要使用者明確授權才可執行。** 目標固定為 `/opt/payment/payment-service`、`api.nnviopp.com`，使用 production-only env、`nnviopp-production`、production DB／network／receipt volume。不得複製 Sandbox DB、volume、Secret、Merchant、Provider credential 或 callback。

變更提案必須先列出：目標版本、hostname／路徑／project／container／network、影響、完整指令、migration、Provider endpoint、callback destination、備份、health／worker 驗證與回滾。Production 必須 `MOCK_PROVIDER_ENABLED=false`、`TEST_DEPOSIT_CALLBACKS_ENABLED=false`、production NewebPay host，且 `GATEWAY_BASE_URL` 不可回指自身。不得以真實付款作一般部署 smoke test。

部署後驗證 `/health`、容器 health、migration、Nginx/TLS、logs、worker lease、receipt volume 掛載與本次安全／付款流程；外部 callback 與 Provider 真實交易另需受控變更窗口。回滾優先回到相容前版；資料 schema 不可直接倒退時，以備份還原與新增 forward migration 規劃處理。
