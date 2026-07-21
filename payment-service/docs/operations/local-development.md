# 本機開發

**驗證：設定 Code Verified。** 在 `payment-service/` 使用 `.env.example` 作為 placeholder 參考，不得填入或提交真實 Secret。Compose 服務為 MariaDB、payment-api、payment-admin；API／Admin 預設只 bind loopback。執行程式時 `cmd/api` 會自動執行 migration，故只可在已確認的本地 DB 執行，不能把指令指到 Sandbox／Production。

```bash
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
go test ./...
go build -buildvcs=false ./cmd/api
```

前端位於 `frontend/`，使用 `npm run test` 與 `npm run build`。一般啟動不需 `go mod tidy`。本地 smoke test 僅可使用隔離資料與 mock／sandbox credential；不要發送真實付款、Notify 或商戶 callback。

本機唯一標準環境為 WSL／Bash，路徑為 `/mnt/c/Users/tim.huang/Documents/四方聚合支付/payment-service`；不再提供或執行 PowerShell 操作流程。Docker 指令使用 `docker` 與 `docker compose`，HTTP 驗證使用 `curl`。完整前置檢查與 Sandbox 控制方式見 [WSL 操作](wsl-operations.md)。
