# Phase 1 Documentation QA Mapping

**範圍：** 前輪回報稱 57 份，但逐一重建清單後確認實際為 **58 份** Markdown：`對外溝通用/` 同時有 `04_Sandbox聯測驗收清單.md` 與 `04_銀行編碼.md`。本表以 58 份原清單、現行文件與程式作追溯。`完整保留`指「現行有效知識已承接」，不代表逐字保存；已淘汰、重複、敏感或僅歷史筆記的原文不保留。未能從現行資料重建的歷史驗收細節一律標記為尚未驗證。

| 原文件 | 處理方式 | 新位置 | 是否完整保留內容 | 備註 |
| --- | --- | --- | --- | --- |
| `docs/admin-rbac-mfa.md` | 整併 | `frontend/admin-console.md` | 是（現行） | RBAC/MFA 以現行 Admin route 說明。 |
| `docs/backup-restore-and-failure-drills.md` | 整併 | `operations/operations-and-troubleshooting.md` | 否（歷史 drill 細節） | 保留 runbook 原則；實測紀錄尚未驗證。 |
| `docs/callback-delivery-framework.md` | 整併 | `architecture/callback-delivery.md` | 是（現行） | task、attempt、claim、retry 承接。 |
| `docs/test-deposit-callback.md` | 整併 | `operations/smoke-test.md` | 是（現行） | 明確非外部 smoke test。 |
| `docs/文件索引.md` | 更名／更新 | `docs/README.md` | 是 | 新文件入口。 |
| `docs/環境隔離設計.md` | 整併 | `architecture/environment-topology.md` | 是（現行） | 以現行雙環境設定為準。 |
| `docs/backend/04_人工代付操作流程.md` | 整併 | `architecture/payment-flows.md` | 是（現行） | 操作流程承接。 |
| `docs/backend/05_人工代付安全控制.md` | 整併 | `architecture/payment-flows.md`、`frontend/admin-console.md` | 是（現行） | claim、receipt、audit 承接。 |
| `docs/backend/06_人工代付維運手冊.md` | 整併 | `operations/operations-and-troubleshooting.md` | 是（現行） | 維運症狀與處置承接。 |
| `docs/backend/Namecheap虛擬主機雙環境部署.md` | 整併 | `operations/sandbox-deployment.md`、`operations/production-deployment.md` | 是（現行） | 依 VPS 雙環境重寫。 |
| `docs/backend/代付管理後台.md` | 整併 | `frontend/admin-console.md` | 是（現行） | 前端與 Admin API 承接。 |
| `docs/backend/後端文件索引.md` | 刪除（內容已完全整併） | `docs/README.md` | 是 | 重複索引。 |
| `docs/backend/後端目前實作基準.md` | 整併 | `architecture/system-overview.md`、`backend/provider-data-workers.md` | 是（現行） | 模組與 worker 承接。 |
| `docs/backend/正式環境回呼IP白名單.md` | 整併 | `backend/hmac-and-security.md` | 是（現行） | 不保留具體環境值。 |
| `docs/backend/正式環境部署.md` | 整併 | `operations/production-deployment.md` | 是（現行） | 明確標示需授權。 |
| `docs/backend/對外溝通用/01_RY完整技術文件.md` | 整併 | `architecture/system-overview.md`、`backend/merchant-api.md` | 是（現行） | RY 降為相容歷史。 |
| `docs/backend/對外溝通用/02_商戶端三方API對接文件.md` | 整併 | `backend/merchant-api.md`、`backend/hmac-and-security.md` | 部分 | 完整 request/response 欄位未逐字保留；需以 Handler 為準。 |
| `docs/backend/對外溝通用/03_商戶端代付API對接文件.md` | 整併 | `backend/merchant-api.md`、`architecture/payment-flows.md` | 部分 | 退休／相容 API 已更正。 |
| `docs/backend/對外溝通用/04_Sandbox聯測驗收清單.md` | 整併 | `operations/smoke-test.md`、`operations/callback-smoke-test.md` | 是（現行） | 改為可追溯驗收門檻。 |
| `docs/backend/對外溝通用/04_銀行編碼.md` | 整併 | `backend/bank-codes.md` | 部分 | 程式 bank-code set 為事實來源；舊手工名稱表未逐字保留。 |
| `docs/backend/對外溝通用/05_三方密鑰回覆文件.md` | 刪除（無保留必要） | `backend/hmac-and-security.md` | 否（敏感交付內容） | 不應保存密鑰交付材料。 |
| `docs/backend/對外溝通用/06_pay_md5_sign驗簽規則說明.md` | 更名／整併 | `backend/hmac-and-security.md` | 是（現行） | MD5 僅列淘汰歷史。 |
| `docs/backend/對外溝通用/07_商戶APIKey提供與更換流程.md` | 整併 | `backend/provider-data-workers.md`、`backend/phase-1-risk-register.md` | 部分 | 退休公開管理 API，操作契約尚未驗證。 |
| `docs/backend/對外溝通用/08_代付HMAC簽章驗證規格.md` | 整併 | `backend/hmac-and-security.md` | 是（現行） | 統一 Header 規格。 |
| `docs/backend/給我自己看得/00_部署前CodeReview總結.md` | 整併 | `backend/phase-1-risk-register.md` | 是（未解項） | 已完成問題未逐字保留。 |
| `docs/backend/給我自己看得/01_文件索引與閱讀順序.md` | 刪除（內容已完全整併） | `docs/README.md` | 是 | 重複索引。 |
| `docs/backend/給我自己看得/02_RY收款串接規格.md` | 整併 | `backend/merchant-api.md`、`architecture/payment-flows.md` | 部分 | 細節欄位應自 Handler 重生文件。 |
| `docs/backend/給我自己看得/03_RY代付串接規格.md` | 整併 | `backend/merchant-api.md`、`architecture/payment-flows.md` | 是（現行） | 移除 review-token 過時說明。 |
| `docs/backend/給我自己看得/04_對外API流程與端點說明.md` | 整併 | `backend/merchant-api.md` | 是（現行） | 加入 Current／Deprecated／Retired 分類。 |
| `docs/backend/給我自己看得/05_Namecheap虛擬主機部署與環境變數說明.md` | 整併 | `architecture/environment-topology.md`、`operations/*deployment.md` | 是（現行） | 環境值不保留。 |
| `docs/backend/給我自己看得/06_RY程式待辦與未完成項目.md` | 整併 | `backend/phase-1-risk-register.md` | 是（未完成項） | 已完成待辦移除。 |
| `docs/backend/給我自己看得/07_RY對接規格確認清單.md` | 整併 | `operations/callback-smoke-test.md` | 是（現行） | 聯測前置條件承接。 |
| `docs/backend/給我自己看得/08_RY代付持久化規劃草案.md` | 整併 | `backend/provider-data-workers.md`、`architecture/accounting-and-state.md` | 是（現行） | 草案性內容未保留。 |
| `docs/backend/給我自己看得/09_PM對接缺口檢查清單.md` | 整併 | `backend/phase-1-risk-register.md` | 是（未完成項） | PM 歷史筆記不保留。 |
| `docs/backend/給我自己看得/10_RY收款渠道編碼對照表.md` | 整併 | `backend/merchant-api.md` | 是 | 七種渠道承接。 |
| `docs/backend/給我自己看得/11_支付通道與供應商架構.md` | 整併 | `architecture/system-overview.md`、`backend/provider-data-workers.md` | 是 | Provider 邊界承接。 |
| `docs/backend/給我自己看得/12_多代收通道擴充架構.md` | 整併 | `architecture/system-overview.md` | 部分 | 未啟用多 Provider 僅作範圍說明。 |
| `docs/backend/給我自己看得/13_核心資料庫結構與帳務流程.md` | 整併 | `architecture/accounting-and-state.md` | 是（現行） | schema 詳情以 migration 為準。 |
| `docs/backend/給我自己看得/14_系統異常排查指南.md` | 整併 | `operations/operations-and-troubleshooting.md` | 是（現行） | 已修復個案不保留。 |
| `docs/backend/給我自己看得/15_藍新代收串接說明.md` | 整併 | `architecture/payment-flows.md`、`backend/provider-data-workers.md` | 是（現行） | NewebPay 承接。 |
| `docs/backend/給我自己看得/18_代收資料流.md` | 整併 | `architecture/payment-flows.md` | 是 | 流程承接。 |
| `docs/backend/給我自己看得/19_客戶資料_下游商戶_RIG001.md` | 刪除（無保留必要） | — | 否（敏感指紋） | Merchant 資料／key 指紋不得在 docs。 |
| `docs/backend/給我自己看得/20_★_正式環境重要資料總表.md` | 整併 | `architecture/environment-topology.md`、`backend/merchant-api.md` | 部分 | 具體 IP／測試數字不保留；需由受控設定確認。 |
| `docs/backend/給我自己看得/21_正式聯調驗證紀錄.md` | 刪除（歷史紀錄未完整保留） | `backend/phase-1-risk-register.md` | 否 | 僅保留「外部 callback 尚未驗證」結論。 |
| `docs/backend/給我自己看得/22_資安風險整理.md` | 整併 | `backend/hmac-and-security.md`、`backend/phase-1-risk-register.md` | 是（未解風險） | 已完成項不逐字保留。 |
| `docs/backend/給我自己看得/23_代付控制項評估.md` | 整併 | `architecture/payment-flows.md`、`backend/phase-1-risk-register.md` | 是（現行） | 控制點承接。 |
| `docs/backend/給我自己看得/24_IP白名單部署說明.md` | 整併 | `backend/hmac-and-security.md` | 是（原則） | 實際 IP 值尚未驗證。 |
| `docs/backend/給我自己看得/25_正式環境整改清單_P0_P1_P2.md` | 整併 | `backend/phase-1-risk-register.md` | 部分 | 優先序歷史細節未完整保留。 |
| `docs/backend/給我自己看得/26_技術架構總覽.md` | 整併 | `architecture/system-overview.md` | 是（現行） | 系統邊界承接。 |
| `docs/backend/給我自己看得/27_代收技術架構.md` | 整併 | `architecture/payment-flows.md` | 是（現行） | 代收流承接。 |
| `docs/backend/給我自己看得/28_代付技術架構.md` | 整併 | `architecture/payment-flows.md`、`architecture/callback-delivery.md` | 是（現行） | 代付與 manual 分流承接。 |
| `docs/backend/給我自己看得/29_正式對帳與調帳流程.md` | 整併 | `architecture/accounting-and-state.md` | 部分 | 資料模型已承接；正式操作 API 已退休。 |
| `docs/frontend/人工代付管理後台前端規格.md` | 整併 | `frontend/admin-console.md` | 是（現行） | 依 React 實作重寫。 |
| `docs/frontend/前端完整功能需求與分期藍圖.md` | 整併 | `frontend/admin-console.md`、`backend/phase-1-risk-register.md` | 部分 | 第二階段藍圖未保留。 |
| `docs/frontend/前端文件索引.md` | 刪除（內容已完全整併） | `docs/README.md` | 是 | 重複索引。 |
| `docs/operations/2026-07-19-production-change-gate.md` | 整併 | `operations/production-deployment.md` | 是（現行） | 授權／回滾門檻承接。 |
| `docs/operations/daily-reconciliation-provider-statement-and-alert-sop.md` | 整併 | `architecture/accounting-and-state.md`、`operations/operations-and-troubleshooting.md` | 部分 | 每日 SOP 細節未完整保留。 |
| `docs/operations/本機開發環境.md` | 更名／更新 | `operations/local-development.md` | 是（現行） | 移除 `go mod tidy`。 |

## QA 結論

現行架構、支付、callback、部署、晉級、smoke test、排錯、ADR 與 risk register 均有入口。資訊遺失風險集中於標示為「部分」或「否」的歷史 API 欄位表、銀行名稱表、歷史正式聯調／drill／整改／日對帳細節；它們不應憑目前資料重建。若這些歷史證據對稽核或第二階段決策必要，應從受控備份或版本庫由使用者授權後另行復原與分類，不能猜測補寫。
