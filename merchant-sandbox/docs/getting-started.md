# Getting Started

## 前置條件

使用 WSL／Bash、Go 1.22 與 Docker Compose。向平台以受控管道取得 Sandbox API Base URL、Customer ID／Secret、Merchant ID／Secret 與 API Key；絕不使用 Production 值。

```bash
cp .env.example .env
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
go test ./...
go build ./...
```

`.env` 僅能在本機保存。Receiver 可在尚未填入 Secret 時啟動 health check，但會 fail closed 拒絕 callback；Sandbox smoke test 前必須填入對應 Sandbox HMAC Secret。

## 啟動

```bash
./scripts/run-callback-receiver.sh
curl -i http://127.0.0.1:8081/healthz
docker compose config --quiet
docker compose up --build
```

正式 Sandbox callback URL 必須是公開 HTTPS，例如 `https://<controlled-host>/callbacks/payment`。不可使用 localhost、private IP、Production 網域或需 VPN 的網址。

## Client request 範例

建立 `request.json`（不要提交）：

```json
{
  "pay_customer_id": "<sandbox-customer-id>",
  "pay_apply_date": "<unix-seconds>",
  "pay_order_id": "MERCHANT-SMOKE-<unique>",
  "pay_amount": 100,
  "pay_channel_id": "1000",
  "pay_notify_url": "https://<controlled-host>/callbacks/payment",
  "pay_product_name": "Merchant Sandbox Smoke"
}
```

執行 `./scripts/run-merchant-client.sh -operation collection-create -body request.json`。Client 會為每次 API request 產生新的 timestamp、nonce 與 HMAC signature；重送相同商務訂單時，仍須使用新的 nonce。建單成功不等於付款或 callback 成功。

其他操作為：

```bash
./scripts/run-merchant-client.sh -operation collection-query -body collection-query.json
./scripts/run-merchant-client.sh -operation payout-create -body payout-create.json
./scripts/run-merchant-client.sh -operation payout-query -body payout-query.json
```

Client 只接受這四個 operation；缺少 request 檔、未知 operation、無效 Unix timestamp、非正整數金額或非 HTTPS callback URL 都會在送出前失敗。
