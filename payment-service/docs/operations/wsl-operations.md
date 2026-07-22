# WSL／Bash 操作標準

**狀態：已完成。** 本機唯一標準環境為 WSL Bash，專案根目錄固定為 `/mnt/c/Users/tim.huang/Documents/四方聚合支付/payment-service`。不再提供或執行 PowerShell 流程；舊 `.ps1` 僅保留為歷史參考，不能作為現行 runbook。本階段已結案，後續任務不再重複處理環境切換。

## 工具前置檢查

```bash
cd /mnt/c/Users/tim.huang/Documents/四方聚合支付/payment-service
pwd
go version
docker --version
docker compose version
curl --version | head -n1
openssl version
ssh -V
jq --version
docker info --format 'Server={{.ServerVersion}} OSType={{.OSType}} Name={{.Name}}'
```

Docker Desktop 必須在 Windows 端啟用目前 WSL 發行版的 WSL integration；沒有 `docker` 命令或無法執行 `docker info` 時，先修正 integration，不得改用 Windows PowerShell 或直接操作 Production。Go 使用 WSL 使用者層級快取：`export GOCACHE="$HOME/.cache/go-build"`、`export GOPATH="$HOME/go"`。

## 本機驗證與 Sandbox 控制

```bash
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
go test ./...
go build -buildvcs=false ./cmd/api
./scripts/sandbox.sh config
./scripts/sandbox.sh health
curl --fail --silent --show-error https://sandbox-api.nnviopp.com/health
```

`sandbox.sh up`、`down` 與 `rollback` 只接受 `.env.sandbox`，仍會改變**本機 Sandbox** container；執行前須確認目標環境。不得以這個腳本部署、重啟或修改 Production。多節點與備份還原 drill 使用 `scripts/invoke-multi-node-failure-drill.sh`、`scripts/invoke-backup-restore-drill.sh`，僅限隔離 drill 資源。

現有代收 Golden Integration Case 使用 `scripts/sandbox-hmac-pay-order-smoke.sh valid 'https://公開系統商網域/path'`。它預設呼叫公開 Sandbox API，並以本機 `.env.sandbox` 的 Sandbox HMAC 設定簽章；請先確認本機設定與 Sandbox 相符，否則會 fail closed。沒有外部系統商公開 HTTPS URL 時不得執行 `valid` 模式建立訂單。

## 路徑與敏感資料

使用 Linux 路徑、正斜線、`curl`、`jq`、OpenSSL 與 Linux `ssh`／`scp`／`rsync`。`.env`、`.env.sandbox`、憑證與部署範本保留在專案原位置，僅可確認存在與權限，不得 `cat`、複製或寫入回報。Bash 腳本須具 executable bit 且使用 LF；執行前可用 `bash -n scripts/*.sh` 驗證語法。

### Local Development Environment Hardening Gap

目前 Workspace 位於 WSL 的 `/mnt/c` 掛載時，Unix mode 可能無法可靠呈現或保存 `chmod 600`。這是本機開發環境的 hardening gap，不代表 Credential sync failed、Sandbox configuration unavailable 或 Sandbox Happy Path blocked；仍須維持 Secret 不輸出與 Git ignore 保護。未來可選擇將含 Secret 的執行目錄移至 WSL Linux filesystem，或啟用 WSL metadata mount；本 runbook 不會因此搬移 Workspace 或修改掛載設定。

## 工作紀錄

- 2026-07-21：修正 `migrationSourceURL()` 的 Windows／WSL 相容性；Windows drive path 不再依目前 OS 的 volume 判斷，並以標準 file URL escaping 處理空白與非 ASCII 路徑。`go test ./internal/repository`、`go test ./...` 與 `go build -buildvcs=false ./cmd/api` 均通過。
- 2026-07-21：PowerShell → WSL 開發與操作環境切換驗收完成；後續唯一工作目標為外部系統商 Sandbox 真實支付與 Callback Smoke Test。
