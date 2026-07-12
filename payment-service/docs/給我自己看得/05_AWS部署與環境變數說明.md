# VPS 部署與環境變數說明

> 最後更新：2026-07-11
> 注意：檔名歷史上叫 AWS，但現在部署目標已改為 Namecheap VPS

## 目前部署目標

- 平台：Namecheap VPS
- OS：Ubuntu 24.04
- 主機：`server1.nnviopp.com`
- IP：`159.198.40.128`
- 建議部署目錄：`/opt/payment-service`

## 建議部署模型

- Nginx
- Docker Compose
- `payment-api`
- `mysql` 或外部資料庫

如果同一台 Namecheap VPS 要同時承載正式與對接環境，建議：

- 正式網域：`api.nnviopp.com`
- 對接網域：`test-api.nnviopp.com`
- 兩套 env：`.env.prod`、`.env.test`
- 兩個 database：`payment_prod`、`payment_test`
- 兩個 host port：`127.0.0.1:8080`、`127.0.0.1:8081`
- 兩份 Nginx site config

## 重要環境變數

| 變數 | 用途 |
|---|---|
| `APP_PORT` | API 服務 port，預設 `8080` |
| `APP_BIND_HOST` | compose 對外綁定 host，預設 `127.0.0.1` |
| `APP_HOST_PORT` | VPS host 端口，可用於正式/測試分流 |
| `DATABASE_DSN` | MySQL DSN |
| `NEWEBPAY_MPG_URL` | 藍新收款 gateway URL |
| `NEWEBPAY_MERCHANT_ID` | 藍新商店代號 |
| `NEWEBPAY_HASH_KEY` | 藍新 HashKey |
| `NEWEBPAY_HASH_IV` | 藍新 HashIV |
| `NEWEBPAY_NOTIFY_URL` | 藍新通知回我方 URL |
| `NEWEBPAY_RETURN_URL` | 藍新前端返回 URL |
| `GATEWAY_BASE_URL` | 上游代付 gateway base URL |
| `GATEWAY_CUSTOMER_ID` | 上游 customer id |
| `GATEWAY_SIGN_KEY` | 上游 sign key |
| `GATEWAY_PAYOUT_NOTIFY_URL` | 上游回我方 payout callback URL |
| `GATEWAY_HTTP_TIMEOUT_SECONDS` | 上游 HTTP timeout |
| `GATEWAY_MAX_SKEW_SECONDS` | 收款驗 timestamp 容許誤差 |
| `MERCHANT_CODE` | 本地 bootstrap merchant code，現在建議 `RIG001` |
| `MERCHANT_NAME` | 本地 bootstrap merchant name |
| `MERCHANT_API_KEY` | bootstrap merchant 原始 API key |
| `PAYOUT_REVIEW_TOKEN` | payout 審核 token |

## 部署前檢查

1. 不要再用 Lightsail 相關腳本
2. `.env` 應只存在 VPS，不要 commit
3. `MERCHANT_API_KEY` 與商戶發放 key 要分清楚是否為正式可用值
4. `GATEWAY_*` 是正式命名，`RY_*` 只是相容讀取

## 上板順序

1. 建立 `/opt/payment-service`
2. 上傳程式碼
3. 準備伺服器 `.env`
4. `docker compose build`
5. `docker compose up -d`
6. 驗證 `/health`

## 同機雙環境補充

- `COMPOSE_PROJECT_NAME` 要分開，例如 `payment-prod`、`payment-test`
- 正式與測試不要共用同一個 database
- 測試環境 callback URL 要全數改成 `test-api.nnviopp.com`
- 對方提供的測試白名單 IP 應只打到測試環境
