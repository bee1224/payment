# Infrastructure Maintenance Backlog

本 Backlog 僅收錄已驗證、目前不阻擋 Runtime 的 Infrastructure／Operations 待辦。執行任何項目前，必須先以 [Service Port & Configuration Registry](service-port-registry.md) 與當下 Runtime 重核；不得以本文件直接推導部署操作。

| ID | 環境／服務 | 類型 | 待辦與證據 | Priority | Impact | Status |
| --- | --- | --- | --- | --- | --- | --- |
| INF-001 | Production / payment-service | Template Drift | `.env.production.example` 的 Compose project 與 Admin Host port 與 Runtime `payment-service`／`127.0.0.1:8081` 不一致。 | P1 | Deployment | Pending |
| INF-002 | Production / payment-service | Missing explicit setting | Production container label 未記錄 environment file；直接在 VPS 目錄執行 `docker compose config --quiet` 會出現未設定 interpolation warning。 | P1 | Deployment, Developer Experience | Pending |
| INF-003 | Sandbox / payment-service | Stale deployment artifact | `deploy/nginx/nnviopp-sandbox.conf.example` 指向 `127.0.0.1:8081`，實際 Sandbox upstream 為 API `8181`、Admin `8183`。 | P2 | Deployment | Pending |
| INF-004 | Sandbox / payment-service | Template Drift | `scripts/sandbox.sh` 與 `Invoke-Sandbox.ps1` 的 `APP_HOST_PORT` fallback 為 `8081`，Runtime Sandbox API Host port 為 `8181`。 | P2 | Developer Experience | Pending |
| INF-005 | Sandbox / merchant-sandbox | Template/Image Metadata Drift | Milestone 6A rebuild 已使 Dockerfile EXPOSE、container target、listener 與 published mapping 均為 `8281/tcp`。保留此項作為已解決 drift 的歷史紀錄。 | P3 | Documentation, Developer Experience | Resolved（2026-07-22） |
| INF-006 | Sandbox Utility | Unmanaged runtime artifact | `nnviopp-sandbox-mariadb-diagnostic-20260720T161240Z` 沒有 Compose project label 或 Host published mapping；用途、network、生命週期尚未驗證。 | P2 | Runtime, Deployment | Pending |

## 執行原則

* Backlog 不授權直接修改 Runtime、Nginx、`.env`、資料、container 或部署。
* INF-001 至 INF-005 應先判定已驗證 Runtime 是否為正式架構，再做最小 Template／文件修正；不以 Template 覆蓋穩定 Runtime。
* INF-006 必須先以唯讀方式確認擁有者與用途；在未確認前，不得停止、刪除或納入受管 topology。
