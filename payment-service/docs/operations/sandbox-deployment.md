# Sandbox 部署 Runbook

**驗證：設定 Code Verified；實際 VPS 狀態尚未驗證。** 目標固定為 `/opt/payment/payment-service-sandbox`、`sandbox-api.nnviopp.com`、`sandbox.nnviopp.com`，使用 Sandbox 專屬 `.env.sandbox`、Compose project `nnviopp-sandbox`、container／network／DB／volume。設定必須是 `APP_ENV=sandbox`、sandbox DB 名稱、`ccore.newebpay.com`（未使用 mock 時）、sandbox public／notify／return URL，及 sandbox merchant／callback／secret。

部署前確認 hostname、目標路徑、project、container、network，並確認不觸及共用主機層 Nginx／TLS／Docker daemon。若需改 Nginx，先同時評估 Production。執行前備份 Sandbox DB／receipt volume；上線後驗證 `/health`、管理端登入、worker 的 `deposit_callback_worker` start log、受控內部 smoke test 與 logs。確認 `GATEWAY_BASE_URL` 是實際 Sandbox 上游代付 Provider，不能保留 placeholder 或回指本服務。外部 callback 驗收請依 [Callback Smoke Test](callback-smoke-test.md)，不能用管理端 test callback 或自行模擬成功取代。

回滾：停止使用新版本的 sandbox containers，依已驗證的前版映像／程式碼與原 sandbox env 啟動；若 migration 已執行，先依 migration rollback 限制與備份決定，不得刪除 volume。
