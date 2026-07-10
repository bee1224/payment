# 商戶端代付 API 對接文件

> 文件定位：提供下游商戶串接我方「代付 / 提款 API」使用。
>
> 最後對齊：2026-07-07

## 一句話先講清楚

商戶若要串接我方正式代付流程，請使用 `/api/payouts/*`。  
`/api/payments/*` 是我方對接 RY 的上游代理層，不建議商戶直接拿來當正式提款流程接口。

## 1. 建立提款申請

### Endpoint

`POST /api/payouts`

### 完整網址

`POST https://payment-service.0f2006wzt5v7m.ap-east-1.cs.amazonlightsail.com/api/payouts`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `merchant_id` | string | Y | 商戶代碼 |
| `api_key` | string | Y | 商戶 API Key |
| `merchant_payout_no` | string | Y | 商戶提款單號，需唯一 |
| `amount` | string | Y | 提款金額，支援小數兩位 |
| `currency` | string | N | 幣別，預設 `TWD` |
| `callback_url` | string | N | 提款結果通知網址 |
| `pay_account_name` | string | Y | 收款戶名 |
| `pay_card_no` | string | Y | 收款帳號 |
| `pay_bank_name` | string | Y | 銀行代碼，需為 3 碼且在白名單內 |
| `pay_sub_branch` | string | N | 分行名稱 |
| `pay_sub_branch_code` | string | N | 分行代碼 |
| `pay_city` | string | N | 縣市 |
| `pay_validate_id` | string | N | 特殊渠道欄位 |
| `pay_currency` | string | N | 特殊渠道欄位 |

### 請求範例

```json
{
  "merchant_id": "M10001",
  "api_key": "merchant-api-key",
  "merchant_payout_no": "PO202607070001",
  "amount": "100.00",
  "currency": "TWD",
  "callback_url": "https://merchant.example.com/payout-callback",
  "pay_account_name": "王小明",
  "pay_card_no": "8123456789012345",
  "pay_bank_name": "013",
  "pay_sub_branch": "台北分行",
  "pay_sub_branch_code": "0012",
  "pay_city": "台北市"
}
```

### 成功回應範例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "payout_no": "W202607070001120001",
    "merchant_id": "M10001",
    "merchant_payout_no": "PO202607070001",
    "status": "pending_review",
    "amount": "100.00",
    "fee": "0.00",
    "currency": "TWD",
    "callback_url": "https://merchant.example.com/payout-callback",
    "created_at": "2026-07-07T12:00:01+08:00",
    "updated_at": "2026-07-07T12:00:01+08:00"
  }
}
```

## 2. 查詢提款單

### 方式 A：POST 查詢

`POST /api/payouts/query`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `merchant_id` | string | Y | 商戶代碼 |
| `api_key` | string | Y | 商戶 API Key |
| `payout_no` | string | N | 我方提款單號 |
| `merchant_payout_no` | string | N | 商戶提款單號 |

> `payout_no` 與 `merchant_payout_no` 至少要帶一個。

### 請求範例

```json
{
  "merchant_id": "M10001",
  "api_key": "merchant-api-key",
  "merchant_payout_no": "PO202607070001"
}
```

### 方式 B：GET 查詢

`GET /api/payouts/{payout_no}?merchant_id=M10001&api_key=merchant-api-key`

### 成功回應範例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "payout_no": "W202607070001120001",
    "merchant_id": "M10001",
    "merchant_payout_no": "PO202607070001",
    "provider_order_no": "P202607071200010001",
    "provider_trade_no": "P202607071200010001",
    "status": "processing",
    "amount": "100.00",
    "fee": "0.00",
    "currency": "TWD",
    "callback_url": "https://merchant.example.com/payout-callback",
    "submitted_at": "2026-07-07T12:05:00+08:00",
    "created_at": "2026-07-07T12:00:01+08:00",
    "updated_at": "2026-07-07T12:05:00+08:00"
  }
}
```

## 3. 審核相關接口

以下接口通常給我方後台 / 營運使用，不是給一般商戶前台直接呼叫。

### 審核通過

`POST /api/payouts/{payout_no}/approve`

### 審核拒絕

`POST /api/payouts/{payout_no}/reject`

請求 body：

```json
{
  "reason": "資料不符"
}
```

### 取消提款單

`POST /api/payouts/{payout_no}/cancel`

請求 body：

```json
{
  "reason": "商戶主動取消"
}
```

### 權限驗證

以上三支需帶審核權限 token：

- `X-Payout-Review-Token: <token>`
- 或 `Authorization: Bearer <token>`

> 注意：取消僅限尚未真正送上游的安全狀態。已成功出款的訂單不會受取消 API 影響。

## 4. 提款結果回調

若建單時有帶 `callback_url`，當提款單進入終態時，我方會主動通知商戶。

### 商戶回調成功條件

請回應：

```text
OK
```

### 回調欄位

| 欄位 | 型別 | 說明 |
|---|---|---|
| `merchant_id` | string | 商戶代碼 |
| `merchant_payout_no` | string | 商戶提款單號 |
| `payout_no` | string | 我方提款單號 |
| `provider_order_no` | string | 上游單號 |
| `status` | string | 提款狀態 |
| `amount` | string | 提款金額 |
| `fee` | string | 手續費 |
| `currency` | string | 幣別 |
| `completed_at` | string | 完成時間 |
| `sign` | string | 簽名 |

### 回調範例

```json
{
  "merchant_id": "M10001",
  "merchant_payout_no": "PO202607070001",
  "payout_no": "W202607070001120001",
  "provider_order_no": "P202607071200010001",
  "status": "completed",
  "amount": "100.00",
  "fee": "0.00",
  "currency": "TWD",
  "completed_at": "2026-07-07 12:08:30",
  "sign": "UPPERCASE_MD5_SIGNATURE"
}
```

## 5. 狀態說明

| 狀態 | 說明 |
|---|---|
| `pending_review` | 待審核 |
| `approved` | 已審核通過，待送單 / 可補查 |
| `submitting` | 送單中 |
| `processing` | 上游處理中 |
| `completed` | 出款成功 |
| `failed` | 出款失敗 |
| `reversed` | 已沖正 |
| `rejected` | 審核拒絕 |
| `cancelled` | 已取消 |

## 6. 注意事項

1. 請以 `merchant_payout_no` 做商戶端冪等控制。
2. 若查詢結果仍在 `approved` / `submitting` / `processing`，請勿自行重送相同提款申請。
3. 若收到我方回調，請以回調狀態更新商戶端訂單，並回應 `OK`。
