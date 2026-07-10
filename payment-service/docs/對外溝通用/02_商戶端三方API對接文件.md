# 商戶端三方 API 對接文件

> 最後更新：2026-07-09
> 本文件以目前 `payment-service` 線上實作為準。若與舊版 RY 文件不同，以本文件為準。

目前代收只支援以下 7 種收款方式：

| `pay_channel_id` | 說明 |
|---|---|
| `1000` | 信用卡一次付清 |
| `1001` | Apple Pay |
| `1002` | Google Pay |
| `1005` | WebATM |
| `1006` | ATM 虛擬帳號 |
| `1007` | 超商代碼 |
| `1008` | 超商條碼 |

## 路由對照

| 最新路由 | 相容路由 |
|---|---|
| `POST /api/pay_order` | `POST /api/v1/deposits` |
| `POST /api/query_transaction` | `POST /api/v1/deposits/query` |

相容路由仍可使用，但會回傳 `Deprecation: true` header。

## 1. 建立收款訂單

### Endpoint

`POST /api/pay_order`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `pay_customer_id` | string | Y | 商戶編號 |
| `pay_apply_date` | string | Y | Unix timestamp |
| `pay_order_id` | string | Y | 商戶訂單號 |
| `pay_notify_url` | string | Y | 商戶 callback URL |
| `pay_amount` | string / number | Y | 正整數 TWD 金額 |
| `pay_channel_id` | string | Y | 收款方式編碼 |
| `bank_account` | array[string] | N | 特定情境可帶入 |
| `store_number` | array[string] | N | 特定情境可帶入 |
| `pay_product_name` | string | N | 商品名稱 |
| `user_name` | string | N | 付款人姓名 |
| `bank_id` | string | N | 補充資訊 |
| `pay_currency` | string | N | 補充資訊 |
| `mobile` | string | N | 補充資訊 |
| `id_no` | string | N | 補充資訊 |
| `pay_md5_sign` | string | Y | MD5 簽名 |

### 請求範例

```json
{
  "pay_customer_id": "M10001",
  "pay_apply_date": "1783555200",
  "pay_order_id": "ORDER202607090001",
  "pay_notify_url": "https://merchant.example.com/callback",
  "pay_amount": "1000",
  "pay_channel_id": "1000",
  "bank_account": [
    "7000000000123456789"
  ],
  "pay_product_name": "Deposit",
  "user_name": "Marry Huston",
  "bank_id": "123",
  "pay_currency": "TWD",
  "mobile": "0900123456",
  "id_no": "A123456789",
  "pay_md5_sign": "UPPERCASE_MD5_SIGNATURE"
}
```

### 成功回應

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "order_id": "ORDER202607090001",
    "transaction_id": "PORDER202607090001123456",
    "view_url": "https://payment-service.example.com/api/v1/deposits/PORDER202607090001123456/redirect",
    "user_name": "Marry Huston",
    "bill_price": "1000.00000000",
    "real_price": "1000.00000000",
    "bank_no": "7000000000123456789",
    "bank_name": "Bank Transfer",
    "bank_from": "123",
    "bank_owner": "Marry Huston",
    "remark": "pay_currency=TWD; mobile=0900123456"
  }
}
```

### 回應說明

- `view_url` 為商戶應導向的付款頁入口。
- `real_price` 目前與 `bill_price` 相同。
- 以下欄位目前不會由系統回傳：`qr_url`、`expired`、`alipay_qrcode`、`rate`。

## 2. 查詢訂單

### Endpoint

`POST /api/query_transaction`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `pay_customer_id` | string | Y | 商戶編號 |
| `pay_apply_date` | string | Y | Unix timestamp |
| `pay_order_id` | string / array[string] | Y | 商戶訂單號 |
| `pay_md5_sign` | string | Y | MD5 簽名 |

### 請求範例

```json
{
  "pay_customer_id": "M10001",
  "pay_apply_date": "1783555200",
  "pay_order_id": [
    "ORDER202607090001"
  ],
  "pay_md5_sign": "UPPERCASE_MD5_SIGNATURE"
}
```

### 成功回應

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "customer_id": "M10001",
      "order_id": "ORDER202607090001",
      "transaction_id": "PORDER202607090001123456",
      "status": 0,
      "order_amount": "1000.00000000",
      "real_amount": "1000.00000000",
      "created": "2026-07-09 10:00:00",
      "notify_url": "https://merchant.example.com/callback",
      "customer_callback": "",
      "extra": {
        "user_name": "Marry Huston",
        "pay_product_name": "Deposit"
      },
      "rc_feedback": {
        "rate": null,
        "display_price": null
      },
      "pay_channel_id": "1000",
      "view_url": "https://payment-service.example.com/api/v1/deposits/PORDER202607090001123456/redirect"
    }
  ]
}
```

### 狀態碼

| `status` | 說明 |
|---|---|
| `0` | 待付款 / 處理中 |
| `2` | 已付款 |
| `5` | 付款失敗 |

## 3. 商戶 callback

當 provider 入帳通知成功後，`payment-service` 會將整理過的結果 POST 到商戶提供的 `pay_notify_url`。

商戶端需回應：

```text
OK
```

### callback payload

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `customer_id` | string | Y | 商戶編號 |
| `order_id` | string | Y | 商戶訂單號 |
| `transaction_id` | string | Y | 系統訂單號 |
| `order_amount` | number | Y | 訂單金額 |
| `real_amount` | number | Y | 實際付款金額 |
| `sign` | string | Y | callback 簽名 |
| `status` | string | Y | 訂單狀態 |
| `message` | string | Y | 狀態說明 |
| `payer_info` | string | N | 銀行帳號或繳費代碼 |
| `extra.user_name` | string | N | 付款人姓名 |
| `extra.pay_product_name` | string | N | 商品名稱 |

### callback 狀態

| `status` | 說明 |
|---|---|
| `30000` | 訂單成功 |
| `50000` | 訂單失敗 |
| `10000` | 訂單處理中 |
