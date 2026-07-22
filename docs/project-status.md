# Payment Platform Status

## Current Goal

建立可供外部系統商自行完成串接的 Sandbox Integration Platform。

## Completed

- Infrastructure Baseline v1
- Service Port Registry
- Configuration Registry 與 Configuration Precedence Chain
- Merchant Sandbox
- Callback Status
- Fresh Session Happy Path
- External Merchant Validation
- External Merchant Onboarding
- Milestone 6A

## Current Status

**External Merchant Sandbox Ready**

Sandbox 驗收不代表 Production Ready。Runtime Port、Compose project、Host mapping、Nginx upstream 與設定優先序以 [Service Port & Configuration Registry](../workspace-docs/infrastructure/service-port-registry.md) 為唯一正式參考；Baseline 與待辦分別見 [Infrastructure Baseline v1](../workspace-docs/infrastructure/baseline-v1.md) 與獨立的 [Maintenance Backlog](../workspace-docs/infrastructure/maintenance-backlog.md)。

## Next Phase

**External Merchant Sandbox Pilot**

Pilot 僅限 Sandbox，應使用 Sandbox-only Credential 與公開串接文件；不得將此狀態描述為 Production Ready。
