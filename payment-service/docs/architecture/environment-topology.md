# Environment Topology

**驗證：設定 Code Verified；實際 VPS container、Nginx、TLS、volume 與備份狀態為尚未驗證／Production Unverified，除非已有 Sandbox 驗收紀錄。**

| 資源 | Production | Sandbox | 必須隔離 |
| -- | -- | -- | -- |
| 部署路徑／網域 | `/opt/payment/payment-service`、`api.nnviopp.com` | `/opt/payment/payment-service-sandbox`、`sandbox-api.nnviopp.com`、`sandbox.nnviopp.com` | 是 |
| Compose／container／network | `nnviopp-production`、prod 名稱 | `nnviopp-sandbox`、sandbox 名稱 | 是 |
| MariaDB、帳號、volume、receipt | production 命名與資料 | sandbox 命名與資料 | 是 |
| Secret、Merchant／Provider credential、callback | production-only | sandbox-only | 是 |
| Provider endpoint | `core.newebpay.com` | `ccore.newebpay.com`（mock 未啟用時） | 是 |
| Nginx、TLS、Public IP、Docker Engine | 同一 VPS 主機層 | 同一 VPS 主機層 | 主機層共用，變更需同時評估 |

本地工作區是 `/mnt/c/Users/tim.huang/Documents/四方聚合支付/payment-service`，唯一標準 shell 是 WSL Bash。Compose 將 API 與 Admin bind 到 loopback；Nginx 才對外代理。環境驗證會 fail closed：Production 禁止 mock 與 test deposit callback，需 production NewebPay host／正式 public URL；Sandbox 需 sandbox NewebPay host、sandbox DB 名稱與 sandbox public URL。`GATEWAY_BASE_URL` 必須指向實際上游出金 Provider，不能回指本服務公開 API。

發布：本地修改 → 本地測試 → Sandbox 部署 → Sandbox internal smoke test → 外部串接驗收 → Production Deployment Candidate →（明確授權）Production 部署 → Production smoke test。Production 不是日常聯調環境，Sandbox Verified 不等於 Production Ready；不得在 VPS 直接分叉修改程式碼。
