# RIG001 Gateway

整合代收與代付流程的服務，目前代收實作已收斂為 `NewebPay` 單一 provider，並只對外提供 7 種收款方式。

## 目前代收支援的收款方式

| `pay_channel_id` | `channel_code` | 說明 |
|---|---|---|
| `1000` | `CREDIT` | 信用卡一次付清 |
| `1001` | `APPLEPAY` | Apple Pay |
| `1002` | `GOOGLEPAY` | Google Pay |
| `1005` | `WEBATM` | WebATM |
| `1006` | `VACC` | ATM 虛擬帳號 |
| `1007` | `CVS` | 超商代碼 |
| `1008` | `BARCODE` | 超商條碼 |

不再支援的舊渠道若仍被傳入，系統會回覆 `unsupported pay_channel_id`。

## 啟動

```sh
go mod tidy
go run ./cmd/api
```

## Namecheap VPS 雙環境部署

如果要在同一台 Namecheap VPS 同時跑正式與對接環境，專案已提供可直接套用的配置骨架：

- 正式 env 範例：`.env.prod.example`
- 測試 env 範例：`.env.test.example`
- 正式 Nginx：`deploy/nginx/payment.conf`
- 測試 Nginx：`deploy/nginx/payment-test.conf`
- Namecheap 啟動腳本：`deploy/namecheap/bootstrap-dual-env.sh`
- Namecheap IP 綁定 Nginx：`deploy/nginx/payment-prod-namecheap.conf`
- Namecheap IP 綁定 Nginx：`deploy/nginx/payment-test-namecheap.conf`
- 部署說明：`docs/dual-environment-namecheap-vps.md`

## API

| 方法 | 路徑 | 說明 |
|---|---|---|
| GET | `/health` | 健康檢查 |
| POST | `/api/pay_order` | RY 相容的代收建立訂單 |
| POST | `/api/query_transaction` | RY 相容的代收查單 |
| POST | `/api/payments/pay_order` | RY 相容的代付下單 |
| POST | `/api/payments/query_transaction` | RY 相容的代付查單 |
| POST | `/api/payments/balance` | RY 相容的代付餘額查詢 |
| POST | `/api/payments/callback` | RY 代付 callback，商戶需回 `OK` |
| GET | `/api/v1/deposits/{order_no}` | 查詢內部代收訂單 |
| GET | `/api/v1/deposits/{order_no}/redirect` | 取得 NewebPay 跳轉頁 |
| POST | `/api/v1/deposits/providers/{provider}/notifications` | provider 代收通知入口 |
| GET / POST | `/api/v1/deposits/payment-result` | NewebPay 前台結果頁 |

`/api/v1/deposits` 與 `/api/v1/deposits/query` 仍保留相容路由，但已標記 `Deprecation: true`，建議改用 `/api/pay_order` 與 `/api/query_transaction`。

## 文件

### 對外溝通用

| 文件 | 說明 |
|---|---|
| [`docs/對外溝通用/01_RY完整技術文件.md`](docs/對外溝通用/01_RY完整技術文件.md) | 歷史整理版 RY 文件，已補充目前服務實際支援範圍 |
| [`docs/對外溝通用/02_商戶端三方API對接文件.md`](docs/對外溝通用/02_商戶端三方API對接文件.md) | 商戶串接 RIG001 Gateway 的最新代收文件 |
| [`docs/對外溝通用/03_商戶端代付API對接文件.md`](docs/對外溝通用/03_商戶端代付API對接文件.md) | 商戶端代付對接文件 |
| [`docs/對外溝通用/04_銀行編碼.md`](docs/對外溝通用/04_銀行編碼.md) | 銀行代碼對照 |

### 給我自己看得

| 文件 | 說明 |
|---|---|
| [`docs/給我自己看得/01_文件索引與閱讀順序.md`](docs/給我自己看得/01_文件索引與閱讀順序.md) | 文件索引 |
| [`docs/給我自己看得/02_RY收款串接規格.md`](docs/給我自己看得/02_RY收款串接規格.md) | 目前代收 API 與欄位整理 |
| [`docs/給我自己看得/03_RY代付串接規格.md`](docs/給我自己看得/03_RY代付串接規格.md) | 代付串接規格 |
| [`docs/給我自己看得/04_對外API流程與端點說明.md`](docs/給我自己看得/04_對外API流程與端點說明.md) | 對外 API 流程 |
| [`docs/給我自己看得/05_AWS部署與環境變數說明.md`](docs/給我自己看得/05_AWS部署與環境變數說明.md) | 部署與環境變數 |
| [`docs/給我自己看得/07_RY對接規格確認清單.md`](docs/給我自己看得/07_RY對接規格確認清單.md) | 對接缺口與確認項 |
| [`docs/給我自己看得/10_RY收款渠道編碼對照表.md`](docs/給我自己看得/10_RY收款渠道編碼對照表.md) | 最新代收渠道對照 |
| [`docs/給我自己看得/11_支付通道與供應商架構.md`](docs/給我自己看得/11_支付通道與供應商架構.md) | provider 與通道架構 |
| [`docs/給我自己看得/12_多代收通道擴充架構.md`](docs/給我自己看得/12_多代收通道擴充架構.md) | 多 provider 擴充設計 |
| [`docs/給我自己看得/13_核心資料庫結構與帳務流程.md`](docs/給我自己看得/13_核心資料庫結構與帳務流程.md) | ERD 與帳務流程 |
| [`docs/給我自己看得/14_系統異常排查指南.md`](docs/給我自己看得/14_系統異常排查指南.md) | 異常排查 |
| [`docs/給我自己看得/15_藍新代收串接說明.md`](docs/給我自己看得/15_藍新代收串接說明.md) | NewebPay 串接備忘 |
| [`docs/給我自己看得/16_RY收款API測試集合（Postman）.json`](docs/給我自己看得/16_RY收款API測試集合（Postman）.json) | 收款 API 測試集合 |
| [`docs/給我自己看得/17_RY收款API測試環境（Postman）.json`](docs/給我自己看得/17_RY收款API測試環境（Postman）.json) | 收款 API 測試環境 |
| [`docs/給我自己看得/18_代收資料流.md`](docs/給我自己看得/18_代收資料流.md) | 最新代收資料流 |
