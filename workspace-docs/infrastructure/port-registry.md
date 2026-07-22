# Workspace Port Registry（歷史摘要）

正式唯一參考已移至 [Service Port & Configuration Registry](service-port-registry.md)。本文件保留先前的局部盤點與歷史證據，不得再作為部署、診斷或 Port 決策的唯一來源。

## 使用原則

* **Current Runtime**：以 VPS 的 `docker inspect`、container label、`ss -lntp`、容器 listener、已載入的 `nginx -T` 與受控 HTTP health check 為準。
* **本地模板**：以可追蹤的 Compose 與 `.env.example` 為準，只表示預期設定，不能覆蓋 Runtime 事實。
* **尚未驗證**：沒有直接 Runtime 證據時必須保留此狀態；不得以文件、命名或舊紀錄推論。
* 本文件不記錄任何 Secret、DSN、API Key、密碼、憑證或其值。

驗證日期：2026-07-22。範圍為唯讀盤點，不代表應用功能、付款流程或 Production Readiness 已驗證。

## Port 層級說明

1. **Application Listen Port**：程式 process 在容器或 Host process 內實際監聽的 Port。
2. **Container Port**：Docker 容器網路命名空間中可被 Docker 映射或同網路服務存取的 Port。
3. **Host Bind Port**：VPS Host 上的 published port，例如 `127.0.0.1:8281:8281`。
4. **Nginx Upstream**：Nginx 實際代理的位置，例如 `http://127.0.0.1:8281`。
5. **Public Endpoint**：外部使用者實際存取的 HTTPS domain；外部商戶不得直接使用 Host Port。

### merchant-sandbox 的三個層級

* Local template 的 `MERCHANT_SANDBOX_LISTEN_ADDR=:8281` 表示 Container 內 Application Listen Port；它**不是** Host Port。
* Current Runtime 的 Docker mapping 是 `127.0.0.1:8281:8281`，容器取得的 `MERCHANT_SANDBOX_LISTEN_ADDR=:8281`，且 process listener 已觀測為 Container 內 `:8281`。Nginx upstream 亦為 `http://127.0.0.1:8281`。
* `127.0.0.1:8081` 在 VPS Current Runtime 是 Production `payment-admin` 的 Host bind，不是 merchant-sandbox Callback Receiver。
* 外部商戶僅可使用 `https://merchant-sandbox.nnviopp.com/callbacks/payment`；不得直接存取 `8281` 或 `8081`。

Local template 與 Current Runtime 的 listener 已同步；本回合不修改 Compose Runtime 或環境實值。

## Current Runtime

Exposure 僅使用下列值：`Public via Nginx`、`Host loopback only`、`Docker internal only`、`Not exposed`、`尚未驗證`。

### Sandbox

| Environment | Service | Compose Project / Service | Container Name | Application Listen Port | Container Target Port | Host Bind Address | Host Port | Public Domain / Protocol | Nginx Upstream | Exposure | Health Endpoint | Callback／API Path | Source of Truth | Status / Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Sandbox | payment-service API | `nnviopp-sandbox` / `payment-api` | `nnviopp-sandbox-api` | `8080` | `8080` | `127.0.0.1` | `8181` | `sandbox-api.nnviopp.com` / HTTPS | `http://127.0.0.1:8181` | Public via Nginx | `/health` | Public API routes under `/api/...` | Docker inspect、Host listener、已載入 Nginx | Current Runtime verified |
| Sandbox | payment-service Admin | `nnviopp-sandbox` / `payment-admin` | `nnviopp-sandbox-admin` | `80` | `80` | `127.0.0.1` | `8183` | `sandbox.nnviopp.com` / HTTPS | `http://127.0.0.1:8183` | Public via Nginx | 尚未驗證 | Admin UI routes | Docker inspect、Host listener、已載入 Nginx | Current Runtime verified；僅網路配置已驗證 |
| Sandbox | MariaDB | `nnviopp-sandbox` / `mysql` | `nnviopp-sandbox-mysql` | `3306` | `3306` | 無 | 無 | 無 / 無 | 不適用 | Docker internal only | MariaDB healthcheck | 不對外提供 API | Docker inspect、Compose network | Current Runtime verified；未 published 到 Host |
| Sandbox | merchant-sandbox Callback Receiver | `merchant-sandbox-sandbox` / `merchant-sandbox` | `merchant-sandbox-sandbox-merchant-sandbox-1` | `8281` | `8281` | `127.0.0.1` | `8281` | `merchant-sandbox.nnviopp.com` / HTTPS | `http://127.0.0.1:8281` | Public via Nginx | `/healthz` | `/callbacks/payment` | Docker inspect published mapping、`MERCHANT_SANDBOX_LISTEN_ADDR`、容器 listener、Host route check、已載入 Nginx | Current Runtime verified；未簽章 callback 回 `401` 是預期 fail-closed 行為 |

### Production

| Environment | Service | Compose Project / Service | Container Name | Application Listen Port | Container Target Port | Host Bind Address | Host Port | Public Domain / Protocol | Nginx Upstream | Exposure | Health Endpoint | Callback／API Path | Source of Truth | Status / Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Production | payment-service API | `payment-service` / `payment-api` | `payment-service-api` | `8080` | `8080` | `127.0.0.1` | `8080` | `api.nnviopp.com` / HTTPS 可達；HTTP 轉址 | `http://127.0.0.1:8080` | Public via Nginx | `/health` | Public API routes under `/api/...` | Docker inspect、Host listener、已載入 Nginx、受控 public GET | Current Runtime 部分驗證；Host Nginx 的 active TLS termination 與外部 HTTPS path 不在本次直接對應範圍 |
| Production | payment-service Admin | `payment-service` / `payment-admin` | `payment-service-admin` | `80` | `80` | `127.0.0.1` | `8081` | 尚未驗證 | 尚未驗證 | Host loopback only | 尚未驗證 | Admin UI routes | Docker inspect、Host listener；已載入 Nginx 未找到 `admin.nnviopp.com` block；DNS lookup 失敗 | Current Runtime host bind verified；不得將本地模板的 `admin.nnviopp.com` 當成已部署事實 |
| Production | MariaDB | `payment-service` / `mysql` | `payment-service-mysql` | `3306` | `3306` | 無 | 無 | 無 / 無 | 不適用 | Docker internal only | MariaDB healthcheck | 不對外提供 API | Docker inspect、Compose network | Current Runtime verified；未 published 到 Host |

## Compose Project 操作

Compose 會依 project name 將 container、network 與 volume 分組。在正確目錄直接執行未指定 project name 的 `docker compose ps`，可能因預設 project name 不同而錯誤判定「沒有服務」。Current Runtime project name 為：

| Environment | 專案 | Current Runtime Compose Project | 唯讀查詢範例 |
| --- | --- | --- | --- |
| Sandbox | payment-service | `nnviopp-sandbox` | `docker compose -p nnviopp-sandbox --env-file .env.sandbox ps` |
| Sandbox | merchant-sandbox | `merchant-sandbox-sandbox` | `docker compose -p merchant-sandbox-sandbox --env-file .env ps` |
| Production | payment-service | `payment-service` | `docker compose -p payment-service ps` |

也可不依賴工作目錄，以 Docker label 做唯讀查詢：

```bash
docker ps --filter label=com.docker.compose.project=merchant-sandbox-sandbox
docker inspect merchant-sandbox-sandbox-merchant-sandbox-1
ss -lntp
nginx -T
```

以上命令僅用於盤點，不會啟動、停止、重建或 reload 服務。執行前仍須確認目前目錄與環境，避免將 Sandbox 與 Production 的 Compose 檔案混用。

## 本地模板差異與待處理項目

| 項目 | Local Template | Current Runtime | 判定 |
| --- | --- | --- | --- |
| merchant-sandbox listener／port mapping | `.env.example`、Go fallback、Dockerfile `EXPOSE` 與 `compose.yaml` 預設皆為 `8281` | Application Listen `:8281`；Container target `:8281`；Host `127.0.0.1:8281`；Nginx upstream `127.0.0.1:8281` | Template 與 Current Runtime 一致；Host Port 與 Application Port 已分開記錄 |
| Sandbox Nginx draft | `payment-service/deploy/nginx/nnviopp-sandbox.conf.example` 指向 `127.0.0.1:8081` | 已載入 Sandbox Nginx 指向 API `127.0.0.1:8181`、Admin `127.0.0.1:8183` | Draft 與 Current Runtime 不一致；部署時不得使用 draft 覆蓋 Runtime |
| Production Compose project | `.env.production.example` 預期 `nnviopp-production` | Running containers labels 為 `payment-service` | 模板與 Runtime 不一致；維運唯讀查詢須用 Current Runtime project |
| Production Admin host port | `.env.production.example` 預期 `127.0.0.1:8082` | Running container bind 為 `127.0.0.1:8081` | 模板與 Runtime 不一致；且 `8081` 不屬於 merchant-sandbox |
| Production Admin public endpoint | `.env.production.example` 的 allowed origin 提及 `admin.nnviopp.com` | VPS DNS 無法解析；已載入 Nginx 未找到該 server block | 尚未驗證；不可作為商戶或維運入口 |

## 檢查清單

* 表內沒有 Secret、API Key、密碼、DSN 或憑證內容。
* Current Runtime 已確認的 Host bind 沒有重複：Sandbox API `8181`、Sandbox Admin `8183`、merchant-sandbox `8281`、Production API `8080`、Production Admin `8081`。
* 所有列為已驗證的 Public Domain 均可對應至已證實的 Nginx upstream；Production Admin 的 Public Domain 與 Nginx upstream 尚未驗證。
* Sandbox 與 Production MariaDB 均未 published 至 Host，僅在各自 Docker network 內；其 network 名稱不同。
* 本文件沒有將 Sandbox 驗證推論為 Production Ready。
