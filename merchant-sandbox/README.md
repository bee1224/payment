# merchant-sandbox

Official Reference Merchant。此服務模擬完全獨立的外部系統商，以 Sandbox 驗證 payment-service 的公開代收／代付 API、HMAC 與 callback 契約。

它不 import payment-service，僅使用公開 HTTP 文件；不得填入 Production URL、憑證、真實訂單或真實個資。

## 快速開始

在 WSL／Bash：

```bash
cp .env.example .env
# 以受控管道填入 Sandbox placeholder 的實值，勿提交 .env
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
./scripts/run-callback-receiver.sh
curl -fsS http://127.0.0.1:8281/healthz
```

Callback 預設路徑為 `/callbacks/payment`，成功回應為 `200` 與大寫純文字 `OK`。完整操作從 [Deposit Happy Path](docs/deposit-happy-path.md) 開始；Credential 規則見 [Credential Guide](docs/credential-guide.md)，Callback 契約見 [Callback Receiver](docs/callback-receiver.md)。

Receiver 與操作 CLI 必須讀取同一份 `MERCHANT_SANDBOX_RECORDS_PATH`。在該商戶控制的 receiver 主機上，可查閱某筆代收 callback 的非敏感 acceptance metadata：

```bash
go run ./cmd/merchant-sandbox callback-status --order-id <merchant-order-id>
```

已編譯為 `merchant-sandbox` 時，等價指令為 `merchant-sandbox callback-status --order-id <merchant-order-id>`。此為本機／商戶受控主機 CLI，不會建立公開 HTTP 查詢 API。

## API Client

`cmd/merchant-client` 提供代收建單／查單與代付建單／查單，並以**實際送出的 JSON bytes**建立 API Request HMAC。建議透過 script 讓本機 `.env` 自動載入：

```bash
./scripts/run-merchant-client.sh -operation collection-create -body request.json
```

可用操作：`collection-create`、`collection-query`、`payout-create`、`payout-query`。request JSON 欄位依 payment-service 的公開文件；請不要將 request 檔提交至版本控制。

Client 會覆寫 request body 中的 Customer ID／Merchant ID／API Key 為本機 Sandbox 設定，並在送出前驗證必填欄位、正整數金額、Unix timestamp 與公開 HTTPS callback URL。它絕不輸出 Secret、HMAC signature 或上游失敗 response body。

## 文件

* [架構](docs/architecture.md)
* [Credential Guide](docs/credential-guide.md)
* [Deposit Happy Path](docs/deposit-happy-path.md)
* [Callback Receiver](docs/callback-receiver.md)
* [Troubleshooting](docs/troubleshooting.md)
* [Sandbox Callback Smoke Test](docs/smoke-test.md)
* [External Merchant Validation Report](docs/external-merchant-validation-report.md)
* [Fresh Session Validation Report](docs/fresh-session-validation-report.md)
* [工作紀錄](docs/work-log.md)
