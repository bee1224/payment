# Phase 1 Closeout Documentation Coverage Report

**本文件為 2026-07-21 文件盤點快照，不是 Production 驗收紀錄。**

## 盤點分類與處理

舊根目錄架構／隔離／callback／備份文件：部分過時、互相重複，已整併至 architecture／operations。舊 `backend/對外溝通用`：部分過時（RY／舊 MD5／已退休路由），有效契約已整併至 `backend/merchant-api.md`、`hmac-and-security.md`、`bank-codes.md`。舊 `backend/給我自己看得`：應搬移或刪除；架構、流程、部署、風險、歷史決策分別整併，且移除其中的敏感 key fingerprint。舊 frontend 規格與藍圖：需求與實作混合，整併至 `frontend/admin-console.md` 並以尚未驗證標註。舊 operations notes：已整併為可執行 runbook；Lightsail 僅保留 ADR 歷史。

## Coverage

| 主題 | 主要文件 | 狀態 | 驗證程度 | 尚缺內容 |
| -- | -- | -- | -- |
| 架構／邊界 | architecture/system-overview | Complete | Code Verified | Production 負載數據 |
| API／渠道 | backend/merchant-api | Complete | Code Verified | 外部相容性實測 |
| HMAC／IP | backend/hmac-and-security | Complete | Code Verified | 實際 allowlist 值驗收 |
| 代收／帳務 | architecture/payment-flows、accounting-and-state | Complete | Code Verified | Production 交易驗證 |
| callback | architecture/callback-delivery、operations/callback-smoke-test | Partial | Pending External Smoke Test | 外部商戶 smoke test |
| manual payout／receipt | payment-flows、frontend/admin-console | Complete | Code Verified | Sandbox 操作驗收 |
| Worker／migration | backend/provider-data-workers、operations/migration-runbook | Complete | Code Verified | VPS 排程／備份演練 |
| Sandbox／Production | architecture/environment-topology | Partial | Sandbox Verified / Production Unverified | 實際 host 層複驗 |
| Reconciliation／API keys | accounting-and-state、provider-data-workers | Partial | Code Verified | 可用 MFA 管理介面／外部流程 |

## 舊關鍵字與敏感資料

`ri-you.com`、Lightsail、`trycloudflare.com`、`pay_md5_sign`、舊 env 名稱、Production Notes 與直接正式聯調筆記已由現行文件移除；僅 ADR 保留 Lightsail／MD5 的歷史決策。掃描發現舊內部文件包含 API key SHA-256 指紋，已從文件樹移除，回報時不得輸出值。是否已有版本庫、備份或外部副本需人工確認；若該 key 曾有效，應依事件流程人工輪替。
