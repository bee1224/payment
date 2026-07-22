# 文件索引

本索引是 Phase 1 Closeout 與 Milestone 5A 的現行文件入口。驗證標籤：**Code Verified**（程式／設定核對）、**Test Verified**（測試存在或已執行）、**Sandbox Deployment Complete**、**Callback Smoke Test Complete**、**Sandbox Verified**（既有部署驗收）、**Production Unverified**、**Historical Record**。

## 建議閱讀順序

1. [System Overview](architecture/system-overview.md)
2. [Payment Flows](architecture/payment-flows.md)
3. [Environment Topology](architecture/environment-topology.md)
4. [Merchant API](backend/merchant-api.md)
5. [Operations Runbooks](operations/)
6. [ADR](architecture/adr.md)
7. [Risk Register](backend/phase-1-risk-register.md)

## Architecture（Internal Design）

- [系統概覽](architecture/system-overview.md)：角色、邊界、模組與第一階段範圍。
- [支付流程](architecture/payment-flows.md)：代收、代付與人工代付資料流。
- [Callback Delivery](architecture/callback-delivery.md)：Notify、Delivery Engine、重試與成功條件。
- [環境拓樸](architecture/environment-topology.md)：Local／Sandbox／Production 隔離。
- [帳務與狀態](architecture/accounting-and-state.md)：狀態機、Ledger、對帳與冪等性。
- [ADR](architecture/adr.md)：現行架構決策與歷史脈絡。

## Backend（External Contract / Internal Design）

- [Merchant API](backend/merchant-api.md)：現行外部路由、相容路由與七種渠道。
- [HMAC 與來源驗證](backend/hmac-and-security.md)：Header、簽章、rotation、IP／proxy。
- [Provider、資料與 Worker](backend/provider-data-workers.md)：NewebPay、Migration、Repository／Worker。
- [銀行與渠道編碼](backend/bank-codes.md)：代付銀行碼的程式事實來源與維護規則。
- [Phase 1 Risk Register](backend/phase-1-risk-register.md)：未完成、風險與第二階段候選。

## Frontend（Internal Design）

- [管理端](frontend/admin-console.md)：登入、Session、CSRF、人工代付與建置。

## External（External Contract）

可直接提供外部系統商的文件入口：[外部系統串接閱讀指南](external/README.md)。本區只包含商戶 API、HMAC、Callback 與銀行代碼，不包含內部架構或維運資訊。

## Closeout（Historical Record）

- [Phase 1 Documentation Coverage Report](phase-1-closeout-documentation-report.md)：本次盤點、處理原則、coverage 與敏感資料處置快照。
- [Phase 1 Documentation QA Mapping](phase-1-documentation-qa.md)：58 份舊文件逐份處理映射與資訊保留風險；並更正前輪 57 份統計誤差。

## Operations（Operations Only）

- [本機開發](operations/local-development.md)、[WSL 操作](operations/wsl-operations.md)、[Sandbox 部署](operations/sandbox-deployment.md)、[Production 部署](operations/production-deployment.md)、[環境晉級](operations/environment-promotion.md)。
- [Migration](operations/migration-runbook.md)、[Smoke Test](operations/smoke-test.md)、[Callback Smoke Test](operations/callback-smoke-test.md)、[維運與排錯](operations/operations-and-troubleshooting.md)。

舊 Lightsail、RY 命名、已淘汰簽章欄位、臨時 tunnel 與逐日聯調筆記均未保留為現行操作文件；歷史決策僅保留於 ADR。
