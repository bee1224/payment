# 商戶端代付 API 對接文件

> 文件版本：2026-07-11  
> 適用範圍：商戶端代付 / 提款接口對接

本文件說明商戶如何串接 `RIG001 Gateway` 之代付能力。請合作方以本文件列示之 `/api/payouts/*` 路由作為正式對接接口。

## 主要接口一覽

為利合作方技術人員快速閱讀與對照，代付相關主要接口整理如下：

| 功能 | 方法 | 路徑 | 說明 |
|---|---|---|---|
| 建立提款申請 | `POST` | `/api/payouts` | 建立代付訂單 |
| 查詢提款單 | `POST` | `/api/payouts/query` | 依我方單號或商戶單號查詢 |
| 查詢提款單明細 | `GET` | `/api/payouts/{payout_no}` | 取得指定提款單最新資訊 |
| 查詢代付餘額 | `POST` | `/api/payments/balance` | 查詢目前可用餘額資訊 |
| 商戶結果通知 | `POST` | 商戶自填 `callback_url` | 我方於訂單進入終態後主動回調商戶 |

如需正式網址，請使用以下網域組合：

- `POST https://api.nnviopp.com/api/payouts`
- `POST https://api.nnviopp.com/api/payouts/query`
- `GET https://api.nnviopp.com/api/payouts/{payout_no}`
- `POST https://api.nnviopp.com/api/payments/balance`

商戶回調網址 `callback_url` 由商戶端提供，我方將於代付狀態進入終態後主動發送通知。

## 1. 建立提款申請

### Endpoint

`POST /api/payouts`

### 完整網址

`POST https://api.nnviopp.com/api/payouts`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `merchant_id` | string | Y | 商戶代碼 |
| `api_key` | string | Y | 商戶 API Key |
| `merchant_payout_no` | string | Y | 商戶提款單號，需唯一 |
| `amount` | string | Y | 提款金額，支援小數兩位 |
| `currency` | string | N | 幣別，預設為 `TWD` |
| `callback_url` | string | N | 提款結果通知網址 |
| `pay_account_name` | string | Y | 收款戶名 |
| `pay_card_no` | string | Y | 收款帳號 |
| `pay_bank_name` | string | Y | 三碼銀行代碼，且需符合支援清單 |
| `pay_sub_branch` | string | N | 分行名稱 |
| `pay_sub_branch_code` | string | N | 分行代碼 |
| `pay_city` | string | N | 縣市 |
| `pay_validate_id` | string | N | 特殊渠道欄位 |
| `pay_currency` | string | N | 特殊渠道欄位 |

### 請求範例

```json
{
  "merchant_id": "RIG001",
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
    "merchant_id": "RIG001",
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

說明：
`payout_no` 與 `merchant_payout_no` 至少須提供一項。

### 請求範例

```json
{
  "merchant_id": "RIG001",
  "api_key": "merchant-api-key",
  "merchant_payout_no": "PO202607070001"
}
```

### 方式 B：GET 查詢

`GET /api/payouts/{payout_no}?merchant_id=RIG001&api_key=merchant-api-key`

### 成功回應範例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "payout_no": "W202607070001120001",
    "merchant_id": "RIG001",
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

## 3. 提款結果回調

若建單時有提供 `callback_url`，當提款單進入終態後，系統將主動通知商戶端。

### 商戶回調成功條件

商戶端收到通知後，請回應：

```text
OK
```

### 回調欄位

| 欄位 | 型別 | 說明 |
|---|---|---|
| `merchant_id` | string | 商戶代碼 |
| `merchant_payout_no` | string | 商戶提款單號 |
| `payout_no` | string | 我方提款單號 |
| `provider_order_no` | string | 上游訂單號 |
| `status` | string | 提款狀態 |
| `amount` | string | 提款金額 |
| `fee` | string | 手續費 |
| `currency` | string | 幣別 |
| `completed_at` | string | 完成時間 |
| `sign` | string | 簽名 |

### 回調範例

```json
{
  "merchant_id": "RIG001",
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

## 4. 代付餘額查詢

### Endpoint

`POST /api/payments/balance`

### 完整網址

`POST https://api.nnviopp.com/api/payments/balance`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `pay_customer_id` | string | Y | 商戶編號 |
| `pay_apply_date` | string | Y | Unix timestamp |
| `pay_md5_sign` | string | Y | MD5 簽名 |

### 請求範例

```json
{
  "pay_customer_id": "RIG001",
  "pay_apply_date": "1783555200",
  "pay_md5_sign": "UPPERCASE_MD5_SIGNATURE"
}
```

### 成功回應範例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "balance": "10000.00",
    "balance_original": "10000.00",
    "balance_available": "9500.00",
    "balance_unsettlement": "500.00"
  }
}
```

### 回應欄位說明

| 欄位 | 型別 | 說明 |
|---|---|---|
| `balance` | string | 目前餘額 |
| `balance_original` | string | 原始餘額 |
| `balance_available` | string | 可用餘額 |
| `balance_unsettlement` | string | 未結算金額 |

## 5. 狀態說明

| 狀態 | 說明 |
|---|---|
| `pending_review` | 待審核 |
| `approved` | 已審核通過，待送單或待補查 |
| `submitting` | 送單中 |
| `processing` | 上游處理中 |
| `completed` | 出款成功 |
| `failed` | 出款失敗 |
| `reversed` | 已沖正 |
| `rejected` | 審核拒絕 |
| `cancelled` | 已取消 |

## 6. 注意事項

1. 請以 `merchant_payout_no` 作為商戶端冪等控制依據。
2. 若查詢結果仍為 `approved`、`submitting` 或 `processing`，請勿重複送出相同提款申請。
3. 商戶收到我方回調後，請以回調結果更新商戶端訂單狀態，並回應 `OK`。
4. `pay_bank_name` 須傳入三碼銀行代碼，請參照 `04_銀行編碼.md`。
