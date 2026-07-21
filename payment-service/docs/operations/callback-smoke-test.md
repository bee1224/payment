# 外部系統商 Callback Smoke Test Runbook

**狀態：Sandbox Verified（2026-07-21）。** 僅在 Sandbox，以新建測試 Merchant、sandbox callback URL／secret、sandbox Provider credential 與無真實個資訂單執行；不得連正式 callback 或 Production DB／volume。Production Ready 仍為否。

## 前置條件

- 外部系統商提供公開 HTTPS `pay_notify_url`，接收端已能依公開契約驗證 Callback HMAC Headers 與原始 JSON body，且成功時回覆 `2xx` 與大寫純文字 `OK`。
- 平台與系統商約定測試窗口；平台取得可完成 NewebPay Sandbox 付款的互動式付款方式。
- 在 WSL 專案根目錄以 `scripts/sandbox-hmac-pay-order-smoke.sh valid "$PAY_NOTIFY_URL"` 建立**新的**訂單。腳本只讀取本機 `.env.sandbox`、預設呼叫 `https://sandbox-api.nnviopp.com`，不印出 Secret 或完整簽章；回應檔僅供受控檢視。只有已確認為本機隔離 Sandbox 時，才可明確以 `SANDBOX_API_BASE_URL=http://127.0.0.1:<port>` 覆寫。
- `pay_notify_url` 會逐筆保存於這筆訂單，正式 callback task 讀取保存值，而非以 `MERCHANT_CALLBACK_URL` 取代。

## 系統商操作與平台觀測

1. 系統商啟用 callback endpoint 的請求紀錄，但不得記錄 Secret；平台建立新訂單，雙方記錄 merchant order ID、platform transaction ID、UTC/Taipei 時間與 callback URL path。
2. 平台以 NewebPay Sandbox 付款頁完成真實 Sandbox 付款。不可用本地 mock、管理端 test callback、手工 HTTP 或人工偽造 Provider Notify 取代。
3. 系統商收到 callback 後，以原始 body 和 `X-Callback-Merchant-Id`、`X-Callback-Key-Id`、`X-Callback-Timestamp`、`X-Callback-Nonce`、`X-Callback-Signature-Version`、`X-Callback-Signature` 驗簽；核對 merchant/order/transaction/amount/status，完成自身冪等交易後回 `200` 與 `OK`。
4. 平台在 Sandbox DB 與 worker log 驗證 paid、單筆 ledger、單一 callback task、成功 attempt 與 sent task。
5. 以**另一筆新的測試訂單**進行 retry：系統商第一次回非 2xx 或非 `OK`，保留首次接收證據；重送後回 `200` 與 `OK`。平台確認 task 先回到 pending、存在 failed attempt，後續為 success/sent，成功後不再建立或執行額外 attempt。

## 平台人工 NewebPay Sandbox 付款

1. 平台取得系統商公開 HTTPS `pay_notify_url` 與測試窗口後，才以 Golden Integration Case 建立新訂單；確認回應 HTTP 200、`code=0`，保存 merchant order ID、transaction ID、`view_url` 與建立時間。不要重用既有或接近到期訂單。
2. 在互動式瀏覽器開啟本次 `view_url`，確認導向的是 NewebPay **Sandbox** 付款頁；使用 NewebPay Sandbox 已授權的測試付款方式完成付款。測試卡號、OTP、登入資訊或任何 Provider credential 不得寫入本文件、終端輸出或交付紀錄。
3. 完成頁面操作後，僅以 Provider Notify、DB 狀態與 callback task 證實付款；瀏覽器 return page、redirect 或「付款完成」畫面本身不構成驗收證據。
4. 若付款頁需要真人輸入、OTP 或 Provider 帳號操作，平台操作人完成步驟後立即通知系統商維持 callback endpoint 可用，並進入下列唯讀觀測。

## 付款後立即取證

1. 在 Provider Notify 到達後，保存 API worker log 中的 order number、provider trade number、Notify 成功與 callback worker delivery 結果；不得輸出完整 Provider payload、Secret 或完整 signature。
2. 執行本 Runbook 的唯讀 SQL，確認 `orders.status=paid`、`provider_transactions.status=paid`、`deposit_paid_ledger_count=1`、`callback_task_count=1`。
3. 保存 task 的 `event_key`、`callback_url`（必要時只保留網域與 path）、status、sent time，以及 attempt 的 attempt number、status、HTTP status、response summary、error code 與 retry time。
4. 系統商保存 callback 原始 body 的安全雜湊、HMAC Headers 驗證結果、HTTP response status 與大寫 `OK`；雙方以 order／transaction ID 對照時間。
5. 成功後等待至少一個 `CALLBACK_WORKER_INTERVAL` 加上合理排程緩衝，再重新查詢 task／attempt；task 必須為 `sent`，attempt 數不得增加。實際等待長度與結果記錄在驗收證據，未執行前不得宣告停止重送已驗證。

## 平台唯讀觀測命令

先在**已確認為 Sandbox** 的 VPS：確認 hostname、`/opt/payment/payment-service-sandbox`、Compose project `nnviopp-sandbox`、Sandbox container/network；不得在 Production 或未指定環境執行。以 Bash 將測試訂單號放入 `ORDER_NO`（不可帶入 shell 特殊字元）。DB service 是 `mysql`；密碼只由該 container 的環境使用，命令不得輸出它。

```bash
cd /opt/payment/payment-service-sandbox
export ORDER_NO='平台交易編號'
docker compose --env-file .env.sandbox ps
curl --fail --silent --show-error https://sandbox-api.nnviopp.com/health
docker compose --env-file .env.sandbox logs --since 30m payment-api | rg --fixed-strings "$ORDER_NO"
read -r -d '' SQL <<'SQL' || true
SELECT o.order_no,o.merchant_order_no,o.status AS order_status,pt.provider_trade_no,pt.status AS provider_status,
       COUNT(DISTINCT CASE WHEN le.type='deposit_paid' THEN le.id END) AS deposit_paid_ledger_count,
       COUNT(DISTINCT t.id) AS callback_task_count
FROM orders o
JOIN provider_transactions pt ON pt.order_id=o.id
LEFT JOIN ledger_entries le ON le.order_id=o.id
LEFT JOIN merchant_deposit_callback_tasks t ON t.order_id=o.id
WHERE o.order_no='__ORDER_NO__'
GROUP BY o.id,pt.id;
SELECT t.id,t.event_key,t.callback_url,t.status,t.retry_count,t.sent_at,t.last_error,t.created_at,t.updated_at,
       a.attempt_no,a.status AS attempt_status,a.http_status,a.error_code,a.next_retry_at,a.finished_at
FROM merchant_deposit_callback_tasks t
LEFT JOIN merchant_deposit_callback_attempts a ON a.task_id=t.id
JOIN orders o ON o.id=t.order_id
WHERE o.order_no='__ORDER_NO__'
ORDER BY t.id,a.attempt_no;
SQL
SQL="${SQL//__ORDER_NO__/$ORDER_NO}"
printf '%s\n' "$SQL" | docker compose --env-file .env.sandbox exec -T mysql sh -lc 'mariadb -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"'
```

不要把 `.env.sandbox` source 到互動式 shell、不要把密碼放入終端歷史、文件或回報。`ORDER_NO` 只能使用本次平台產生的英數訂單號；若訂單號契約改為可含 SQL 特殊字元，必須先改用 parameterized read-only query，不能直接套用此 runbook。

## 驗收證據格式

| Case | 系統商證據 | 平台證據 | 通過條件 |
| --- | --- | --- | --- |
| 成功 callback | 接收時間、URL path、payload 欄位核對、HMAC 驗證結果、`2xx + OK` | `order=paid`、provider transaction=paid、`deposit_paid_ledger_count=1`、一筆 task、attempt=`success`、task=`sent` | 全部相符 |
| 失敗後重送 | 首次非成功回覆與下一次正確 `OK` 的時間／結果 | 首次 attempt=`failed`、task 先 pending 並有 next retry，後續 attempt=`success`、task=`sent` | 有退避重送、無資料遺失 |
| 成功後停止 | 成功回覆後窗口內沒有新 callback | task=`sent` 且沒有新增 attempt；worker log 無同 task 的再次 delivery | 成功後停止 |

成功：全部契約相符且 task 最終 `sent`／attempt 成功。失敗：非預期 payload／HMAC、非預期 status／body、無 retry／attempt、ledger 非 1 或跨環境流量。若支付狀態已提交但當下 callback task 寫入失敗，deposit worker 的 terminal-order recovery 會在下一次 recovery tick 補建缺失的 outbox task；仍須檢查最終 task／attempt 紀錄。測試資料依 Sandbox 保留政策處理，不能複製至 Production。

## 2026-07-21 Milestone 4 驗收紀錄

- Reference Merchant：`https://merchant-sandbox.nnviopp.com`
- Callback URL：`https://merchant-sandbox.nnviopp.com/callbacks/payment`
- Callback key id：`sandbox-callback-20260721`（Secret 未記錄）
- Sandbox API health：`GET https://sandbox-api.nnviopp.com/health` 回 `200 {"status":"ok"}`
- Production health（唯讀）：`GET https://api.nnviopp.com/health` 回 `200 {"status":"ok"}`；Production container 維持既有 uptime，未重啟或部署。

成功鏈路：

| 項目 | 證據 |
| --- | --- |
| 商戶訂單／平台訂單 | `M4S5-20260721185303`／`P20260721185303185320` |
| Provider Notify | 成功進入；provider trade no `26072118534813412` |
| 訂單狀態 | order=`paid`、provider transaction=`paid` |
| Ledger | `deposit_paid` 1 筆，entry id `14` |
| Callback task | task id `40`，event key `merchant.deposit:P20260721185303185320:deposit.paid`，status=`sent` |
| Attempt | attempt no `1`，status=`success`，HTTP `200`，response summary=`OK` |
| Reference Merchant | body hash `c5899025bcf61daff17818a3907cb42c9628a7afe8cf2ab6761bcec5fd0fd6fa`，HMAC valid=`true`，response mode=`success` |
| 停止重送 | 成功後等待超過一個 worker interval，attempt 仍為 1 |
| 重複 Provider Notify | 對同一平台訂單重送 NewebPay Sandbox notify；provider callback count 增加，ledger/task/attempt 仍為 `1/1/1` |

Retry 鏈路：

| 項目 | 證據 |
| --- | --- |
| 商戶訂單／平台訂單 | `M4R2-20260721190704`／`P20260721190704190719` |
| Provider Notify | 成功進入；provider trade no `26072119080713441` |
| 訂單狀態 | order=`paid`、provider transaction=`paid` |
| Ledger | `deposit_paid` 1 筆，entry id `16` |
| Callback task | task id `42`，event key `merchant.deposit:P20260721190704190719:deposit.paid`，最終 status=`sent` |
| Attempts | no `1` 與 `2`：HTTP `503`、`http_5xx`；no `3`：HTTP `200`、response summary=`OK` |
| Reference Merchant | 3 次 callback body hash 均為 `8e14c655f1f07e494eb7f925bed34b3cdae83ddf07c6e404eee436d77b5ba684`，HMAC valid 均為 `true` |
| Retry Header 更新 | 3 次 `X-Callback-Timestamp` 不同；Nonce 指紋分別為 `ce8ce2572a1a6c43`、`628f2bc73eee8647`、`b630aafda2203d52`；Signature 指紋分別為 `3005e5c485ba8201`、`059260cc5fc1700e`、`08dc19003cbbb99b` |
| 停止重送 | 最終成功後等待，ledger/task/attempt count 為 `1/1/3`，未新增 attempt |

未計入通過的歷史測試單：`M4S-20260721182140`（付款頁失效）、`M4S2-20260721183457`、`M4S3-20260721184223`、`M4S4-20260721185002`（Sandbox worker lease、SQL alias 或 Reference Merchant 設定問題導致不符合完整驗收條件）。上述問題均已在 Sandbox 修正並以 `M4S5`／`M4R2` 重新驗收。
