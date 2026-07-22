# Infrastructure Baseline v1 Freeze

## Baseline

| 項目 | 值 |
| --- | --- |
| Baseline Version | v1 |
| 建立日期 | 2026-07-22 |
| Runtime Verification Date | 2026-07-22 |
| Status | **Infrastructure Baseline v1 Verified** |
| Registry Path | [service-port-registry.md](service-port-registry.md) |
| Source of Truth | Service Port Registry 與執行當下的 VPS Runtime；衝突時以 Runtime 證據優先 |

## 宣告

**Service Port Registry 為唯一正式來源。** 後續部署、維運、診斷與 Codex 任務必須先引用 Registry 中的 Service name、Environment、VPS path、Compose project、Host bind、Container target 與 Nginx upstream，再執行任何動作。

## Scope

* Production payment-service API、Admin、MariaDB。
* Sandbox payment-service API、Admin、MariaDB。
* Sandbox Utility merchant-sandbox Callback Receiver。
* 受管 Docker Compose project、container listener、Host published mapping、Nginx server block／upstream、network、volume 與 Configuration Precedence Chain。

## Out of Scope

* Application Business Logic、付款／Callback 正確性、Provider 行為與 Production Readiness。
* Production 或 Sandbox 資料內容、Secret、TLS private material、資料庫 health 以外的功能驗收。
* 未受管 diagnostic container 的擁有者、用途、network 與生命週期。
* Maintenance Backlog 的實際修正、任何 deployment、container rebuild、Nginx reload 或 Runtime 修改。

## Freeze Contract

本 Freeze 凍結已驗證的 Runtime 基線，不表示 Production Ready，也不授權把 Template、舊文件或舊對話反向套用到 VPS。Runtime 與 Template 出現差異時，先分類為 Runtime Drift、Template Drift 或 Documentation Drift；若是 Runtime mapping 不一致或不安全，停止操作並以 Registry 欄位及實際證據回報。

## Remaining Backlog

所有不阻擋 Runtime 的待辦集中於 [maintenance-backlog.md](maintenance-backlog.md)，目前為 Maintenance Mode。未完成項目不應阻擋一般應用開發、Sandbox 功能驗證或文件閱讀；任何部署或 Template Synchronization 仍必須先處理其相關項目與風險。
