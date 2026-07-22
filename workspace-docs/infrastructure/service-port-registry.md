# Payment Platform Service Port & Configuration Registry

> 本文件為 Payment Platform 服務 Port、部署路徑、Compose project、Nginx upstream 與組態來源的唯一正式參考。後續部署、診斷與 Codex 任務不得僅憑記憶或舊對話推測，應先以本文件及實際 Runtime 驗證為準。

驗證日期：2026-07-22。Baseline 狀態：**Infrastructure Baseline v1 Verified**（Runtime recheck：2026-07-22）。Runtime 證據來自 VPS `docker ps`／`docker inspect`、container listener、Host `ss -lntp`、已載入的 `nginx -T` 與三個部署目錄的 `docker compose config --quiet`。本文件不記錄 Secret、DSN、API Key、密碼、憑證或 `.env` 值。

## 架構規範

1. Application Port、Container Target Port、Host Published Port 與 Nginx Upstream 是不同概念；數字相同仍須分欄記錄。
2. 各層不必刻意使用不同 Port；VPS 必須避免衝突的是 Host bind address 與 Host published port 的組合。
3. Nginx 只能代理到本文件明確記錄的 Host upstream，不得依記憶猜測。
4. Docker Compose 操作必須使用本文件記錄的 Compose project，避免預設 project name 建立平行 container。
5. `.env.example`、程式 fallback、Dockerfile `EXPOSE`、Compose target、healthcheck 與正式文件應與已決定架構一致；Runtime 與 Template 不一致時，先判定 Runtime Bug、Template Drift 或 Documentation Drift，不得直接覆蓋穩定 Runtime。
6. `.env` 是實際環境設定；`.env.example` 是鍵名與範例契約，兩者不得混用。
7. Sandbox 與 Production 必須分開記錄。未來部署 Prompt 必須包含 Service name、Environment、VPS path、Compose project、Host bind、Container target 與 Nginx upstream。

## Configuration Precedence Chain

### payment-service API

`Shell environment / Compose --env-file` → Compose interpolation（工作目錄 `.env` 或明確指定的 env file）→ Compose `environment: APP_PORT` → Runtime container environment `APP_PORT` → 程式讀取 `config/config.yaml` → `applyEnv` 覆蓋 YAML → 未設定時 Go fallback `8080` → `http.Server` 監聽 `:<APP_PORT>`。

Dockerfile `ENV APP_PORT=8080` 的優先序低於 Compose `environment`。Dockerfile `EXPOSE 8080` 只提供 image metadata，不決定 listener。Runtime container 只能證明最終值，無法回推該值當次由 Shell、`.env` 或 `--env-file` 注入；部署指令必須明確記錄 env file。

### payment-service Admin

`Shell environment / Compose --env-file` → Compose interpolation `VITE_API_BASE_URL` → Docker build arg／build-time `ENV` → 靜態前端產物；執行期由 Nginx image 監聽 `0.0.0.0:80` → Compose `ports` 發布到 Host。Admin 沒有 application runtime port env key；其服務 port 是 Nginx container 的 `80`。

### MariaDB

Compose `environment` 提供資料庫初始化參數（不在本文件記錄值）→ MariaDB image 預設監聽 `3306` → Compose network 內服務名 `mysql`。兩個受管 MariaDB 都沒有 `ports` published mapping；healthcheck 於 container 內使用 `127.0.0.1:3306`。

### merchant-sandbox

`Shell environment` → Compose interpolation（`.env` 或 Shell）→ Compose `environment: MERCHANT_SANDBOX_LISTEN_ADDR`（fallback `:8281`）→ Runtime container environment → 程式 `loadDotEnv` 僅填入**未設定**的環境變數 → `value` fallback `:8281` → receiver 監聽該位址。

因此 Compose 注入的 Runtime container environment 優先於 receiver 讀取的 `.env`；Dockerfile 沒有 `ENV`，`EXPOSE 8281` 僅為 image metadata。Runtime 仍無法回推 Compose interpolation 最初取自 Shell 或 `.env`。

## Current Runtime Registry

### payment-service API — Production

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Production / `/opt/payment/payment-service` |
| Compose Project / Service / Container | `payment-service` / `payment-api` / `payment-service-api` |
| Public Domain | `api.nnviopp.com`（HTTPS；Nginx 亦監聽 HTTP 轉址） |
| Application Listen | `:::8080`；設定鍵 `APP_PORT`；Go fallback `8080` |
| Dockerfile EXPOSE / Container Target | `8080/tcp` / `8080/tcp` |
| Host Bind / Published Mapping | `127.0.0.1:8080` / `127.0.0.1:8080→8080/tcp` |
| Network / Persistent Volume | `payment-service_payment-net` / receipt volume 至 `/var/lib/payment-service/receipts` |
| Nginx | `api.nnviopp.com`，`80`、`443`，upstream `http://127.0.0.1:8080` |
| Paths / Healthcheck | API base `/api/...`；health `/health`；container healthcheck 未設定 |
| Runtime evidence | container env `APP_PORT=8080`、container socket、Docker inspect、Host `ss`、Nginx enabled config |

### payment-service Admin — Production

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Production / `/opt/payment/payment-service` |
| Compose Project / Service / Container | `payment-service` / `payment-admin` / `payment-service-admin` |
| Public Domain | 尚未驗證；已載入 Nginx 未找到此 Admin 的 server block |
| Application Listen | Nginx container `0.0.0.0:80`；無 application port 設定鍵 |
| Dockerfile EXPOSE / Container Target | `80/tcp` / `80/tcp` |
| Host Bind / Published Mapping | `127.0.0.1:8081` / `127.0.0.1:8081→80/tcp` |
| Network / Persistent Volume | `payment-service_payment-net` / 無 |
| Nginx / Paths | 對外 upstream、health 與 admin path 均尚未驗證 |
| Runtime evidence | container socket、Docker inspect、Host `ss` |

### MariaDB — Production

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Production / `/opt/payment/payment-service` |
| Compose Project / Service / Container | `payment-service` / `mysql` / `payment-service-mysql` |
| Application Listen / EXPOSE / Target | `0.0.0.0:3306`、`[::]:3306` / `3306/tcp` / `3306/tcp` |
| Host Bind / Published Mapping | 無 / 未 published |
| Network / Persistent Volume | `payment-service_payment-net` / named volume 至 `/var/lib/mysql` |
| Healthcheck | container `mariadb-admin` 對 `127.0.0.1:3306`；健康檢查已設定 |
| Public Domain / Nginx | 無；Docker internal only |

### payment-service API — Sandbox

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Sandbox / `/opt/payment/payment-service-sandbox` |
| Compose Project / Service / Container | `nnviopp-sandbox` / `payment-api` / `nnviopp-sandbox-api` |
| Public Domain | `sandbox-api.nnviopp.com`（HTTP、HTTPS） |
| Application Listen | `:::8080`；設定鍵 `APP_PORT`；Go fallback `8080` |
| Dockerfile EXPOSE / Container Target | `8080/tcp` / `8080/tcp` |
| Host Bind / Published Mapping | `127.0.0.1:8181` / `127.0.0.1:8181→8080/tcp` |
| Network / Persistent Volume | `nnviopp-sandbox_payment-net` / receipt volume 至 `/var/lib/payment-service/receipts` |
| Nginx | `sandbox-api.nnviopp.com`，`159.198.42.146:80`、`:443`，upstream `http://127.0.0.1:8181` |
| Paths / Healthcheck | API base `/api/...`；health `/health`；container healthcheck 未設定 |
| Runtime evidence | container env `APP_PORT=8080`、container socket、Docker inspect、Host `ss`、Nginx enabled config |

### payment-service Admin — Sandbox

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Sandbox / `/opt/payment/payment-service-sandbox` |
| Compose Project / Service / Container | `nnviopp-sandbox` / `payment-admin` / `nnviopp-sandbox-admin` |
| Public Domain | `sandbox.nnviopp.com`（HTTP、HTTPS） |
| Application Listen | Nginx container `0.0.0.0:80`；無 application port 設定鍵 |
| Dockerfile EXPOSE / Container Target | `80/tcp` / `80/tcp` |
| Host Bind / Published Mapping | `127.0.0.1:8183` / `127.0.0.1:8183→80/tcp` |
| Network / Persistent Volume | `nnviopp-sandbox_payment-net` / 無 |
| Nginx | `sandbox.nnviopp.com`，`159.198.42.146:80`、`:443`，upstream `http://127.0.0.1:8183` |
| Paths / Healthcheck | Admin UI routes；container healthcheck 未設定 |

### MariaDB — Sandbox

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Sandbox / `/opt/payment/payment-service-sandbox` |
| Compose Project / Service / Container | `nnviopp-sandbox` / `mysql` / `nnviopp-sandbox-mysql` |
| Application Listen / EXPOSE / Target | `0.0.0.0:3306`、`[::]:3306` / `3306/tcp` / `3306/tcp` |
| Host Bind / Published Mapping | 無 / 未 published |
| Network / Persistent Volume | `nnviopp-sandbox_payment-net` / named volume 至 `/var/lib/mysql` |
| Healthcheck | container `mariadb-admin` 對 `127.0.0.1:3306`；健康檢查已設定 |
| Public Domain / Nginx | 無；Docker internal only |

### merchant-sandbox Callback Receiver — Sandbox Utility

| 欄位 | 值 |
| --- | --- |
| Environment / VPS Path | Sandbox Utility / `/opt/payment/merchant-sandbox` |
| Compose Project / Service / Container | `merchant-sandbox-sandbox` / `merchant-sandbox` / `merchant-sandbox-sandbox-merchant-sandbox-1` |
| Public Domain | `merchant-sandbox.nnviopp.com`（HTTP、HTTPS） |
| Application Listen | `:::8281`；設定鍵 `MERCHANT_SANDBOX_LISTEN_ADDR`；Go fallback `:8281` |
| Dockerfile EXPOSE / Container Target | template `8281/tcp` / Runtime mapping target `8281/tcp` |
| Host Bind / Published Mapping | `127.0.0.1:8281` / `127.0.0.1:8281→8281/tcp` |
| Network / Persistent Volume | `merchant-sandbox-sandbox_default` / `/opt/payment/merchant-sandbox/var:/app/var` |
| Nginx | `merchant-sandbox.nnviopp.com`，`80`、`443`，upstream `http://127.0.0.1:8281` |
| Health / Callback | `/healthz`、`/callbacks/payment`；container healthcheck 未設定 |
| Runtime evidence | container env `MERCHANT_SANDBOX_LISTEN_ADDR=:8281`、container socket、Docker inspect、Host `ss`、Nginx enabled config |

### Unmanaged diagnostic container — Sandbox Utility

`nnviopp-sandbox-mariadb-diagnostic-20260720T161240Z` 正在執行 MariaDB image，僅宣告 `3306/tcp`，未見 Compose project label 或 Host published mapping。它不屬於受管 payment-service／merchant-sandbox topology；其用途、network 與生命週期尚未驗證，不得把它當成 Sandbox MariaDB 的部署依據。

## Host Port Collision Review

Host loopback published ports 為 Production API `8080`、Production Admin `8081`、Sandbox API `8181`、Sandbox Admin `8183`、merchant-sandbox `8281`。每個 `(127.0.0.1, port)` 唯一，無 collision。受管 MariaDB 沒有 Host published port；Nginx 佔用公開 `80`、`443`。

## Nginx Upstream Review

所有已載入且屬 payment 平台的 server block 都能對應到已存在的 loopback published mapping：`api.nnviopp.com→8080`、`sandbox-api.nnviopp.com→8181`、`sandbox.nnviopp.com→8183`、`merchant-sandbox.nnviopp.com→8281`。Production Admin 沒有已驗證的 Nginx server block，因此不可宣稱已公開。

## Configuration Drift Findings

| 分類 | 證據與影響 | 最小建議 |
| --- | --- | --- |
| Template drift | `payment-service/.env.production.example` 期望 Compose project `nnviopp-production` 與 Admin Host `8082`；Runtime labels／mapping 是 `payment-service` 與 `8081`。 | 先決定 Runtime 是否為正式目標；若是，只同步範例與部署文件，不重建 Production。 |
| Stale deployment artifact | `payment-service/deploy/nginx/nnviopp-sandbox.conf.example` 仍指向 `127.0.0.1:8081`；實際 Sandbox API/Admin 是 `8181/8183`。 | 以已載入 Nginx 為準，另行更新 draft；禁止用 draft 覆蓋 Runtime。 |
| Template drift | `payment-service/scripts/sandbox.sh` 與 `Invoke-Sandbox.ps1` 的未設定 fallback 仍為 `8081`；Sandbox Runtime API Host port 是 `8181`。 | 更新 fallback 或要求明確 `APP_HOST_PORT`；不改 Runtime。 |
| Resolved template/image metadata drift | merchant-sandbox Sandbox image 已於 Milestone 6A rebuild；Dockerfile EXPOSE、container target、listener 與 published mapping 均為 `8281/tcp`。 | 無需 Runtime 修改；後續 rebuild 仍須以本 Registry 核對。 |
| Missing explicit setting | 在 VPS 目錄直接執行 payment-service `docker compose config --quiet` 會對多個未設定的非 port interpolation 發出 warning，但仍完成解析；Production label 未記錄 environment file。 | 部署 runbook 明確記載 Production／Sandbox 使用的 env file 與 Compose command，不輸出值。 |
| No issue | 舊 `workspace-docs/infrastructure/port-registry.md` 已降級為歷史摘要；所有已知入口均連至本文件。 | 後續只維護本文件。 |
| No issue | 受管服務 Host mapping、container listener、Nginx upstream、network isolation 與 MariaDB 未對外 published 的 Runtime 一致。 | 不需 Runtime 修改。 |

## 使用與更新規則

任何 Template Synchronization 必須先引用本文件的 Runtime 欄位，再以最小修改處理明確分類的 drift。若 Runtime mapping 出現不一致或安全問題，停止修改並附上 container、Host listener 與 Nginx 證據及影響範圍。
