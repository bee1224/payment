# 四方聚合支付 Workspace

此 Workspace 用於管理彼此獨立、但以公開契約整合的正式專案。

## 專案

* `payment-service/`：正式支付服務，提供代收、代付及商戶 callback 等公開 API。
* `merchant-sandbox/`：Official Reference Merchant，以獨立商戶系統的角度驗證 payment-service Sandbox 公開 API、HMAC 與 callback；不 import 或共用 payment-service 程式碼。

外部系統商的唯一 Sandbox 串接入口是 [payment-service/docs/external/README.md](payment-service/docs/external/README.md)。它不需要 payment-service internal、DB 或部署權限。

## 專案狀態

目前目標是 **External Merchant Sandbox Integration Platform**；目前狀態為 **External Merchant Sandbox Ready**，下一階段為 **External Merchant Sandbox Pilot**。這不代表 Production Ready。完整狀態見 [Payment Platform Status](docs/project-status.md)。

## 邊界

各專案擁有自己的程式碼、設定、Secret、資料、Docker 資源、環境與部署流程。跨專案只允許透過公開 HTTP API 與明確文件契約整合；不得共用 `internal`、Model、簽章函式或環境檔。

## 基礎設施

Production、Sandbox 與 merchant-sandbox 的網路與 Port 唯一完整清單見 [Service Port & Configuration Registry](workspace-docs/infrastructure/service-port-registry.md)。
已凍結的 Runtime 基線見 [Infrastructure Baseline v1](workspace-docs/infrastructure/baseline-v1.md)；非阻擋維運待辦維持於獨立的 [Maintenance Backlog](workspace-docs/infrastructure/maintenance-backlog.md)。

## 本機標準

唯一標準操作環境為 WSL／Bash。Go 使用使用者層級快取：

```bash
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
```

不得使用或建立專案內 `.gocache/`、`.gomodcache/`、`.gopath/`。各專案的專屬規則請閱讀其 `AGENTS.md`。
