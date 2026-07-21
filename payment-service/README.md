# nnviopp Payment Service

## 專案簡介

Go 1.22／MariaDB 10.11 支付服務，提供代收、代付工作流、人工代付、帳務、商戶通知與 React/Vite 管理端。`RIG001` 是目前主要 Merchant／Customer 識別，不是系統名稱；既有對外欄位與路由維持 RY-compatible contract。NewebPay 是目前代收 Provider。

## 第一階段狀態

**Code Verified / Test Verified：** 第一階段核心程式、Migration 與雙環境設定已存在。**Sandbox Verified：** 部署狀態以既有驗收紀錄為準。**Pending External Smoke Test：** 外部商戶 callback smoke test。**Production Unverified：** 未因程式或歷史紀錄而宣告 Production Ready。

## 技術組成與能力

- Go API、MariaDB、Docker Compose、Nginx、DB-backed background workers。
- NewebPay 代收、付款跳轉、Notify、Ledger 與商戶 callback。
- 代付建立／查詢、人工 claim、收據、確認、Audit、告警與 callback retry。
- 管理端登入、Session、CSRF、MFA 與 RBAC。

支援代收渠道：`1000 CREDIT`、`1001 APPLEPAY`、`1002 GOOGLEPAY`、`1005 WEBATM`、`1006 VACC`、`1007 CVS`、`1008 BARCODE`。

## 本地開發與驗證

在 `payment-service/`：

```bash
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
go test ./...
go build -buildvcs=false ./cmd/api
```

目前唯一的本機開發與操作環境是 WSL／Bash；設定及本地 Compose 請見 [本機開發](docs/operations/local-development.md) 與 [WSL 操作](docs/operations/wsl-operations.md)。一般啟動不需要執行 `go mod tidy`。

## Production／Sandbox

Production：`api.nnviopp.com`、`/opt/payment/payment-service`；Sandbox：`sandbox-api.nnviopp.com`、`sandbox.nnviopp.com`、`/opt/payment/payment-service-sandbox`。兩者必須隔離 DB、帳號、Secret、Provider credential、callback、network、volume 與資料。詳細拓樸見 [Environment Topology](docs/architecture/environment-topology.md)。

## 文件入口

請從 [文件索引](docs/README.md) 開始。可直接提供外部系統商的文件位於 [外部系統串接閱讀指南](docs/external/README.md)；內部 Backend 設計與部署文件仍分別位於 `docs/backend/` 與 `docs/operations/`。

## 安全與部署提醒

payment-service 是 API Provider。對外 API 與商戶 Callback 都採 HMAC-SHA256、timestamp、nonce 與防重放；Callback 使用商戶專屬且可輪替的 Callback Signing Secret。Merchant API Key、API Request Signing Secret 與 Callback Signing Secret 不得混用，Sandbox 與 Production 必須隔離。Callback 成功條件為 HTTP 2xx 且 response body bytes 精確為大寫純文字 `OK`，否則進入重送流程。
