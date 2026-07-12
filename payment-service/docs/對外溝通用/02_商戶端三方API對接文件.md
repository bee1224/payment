# 商戶端代收 API 對接文件

> 文件版本：2026-07-11  
> 適用範圍：商戶端代收接口對接

本文件說明商戶如何串接 `RIG001 Gateway` 之代收能力。本文件內容即為正式對接依據。

## 主要接口一覽

為利合作方技術人員快速閱讀與對照，代收相關主要接口整理如下：

| 功能 | 方法 | 路徑 | 說明 |
|---|---|---|---|
| 建立收款訂單 | `POST` | `/api/pay_order` | 建立代收訂單並取得付款導頁資訊 |
| 查詢收款訂單 | `POST` | `/api/query_transaction` | 查詢商戶訂單目前狀態 |
| 商戶結果通知 | `POST` | 商戶自填 `pay_notify_url` | 我方於訂單狀態更新後主動回調商戶 |

如需正式網址，請使用以下網域組合：

- `POST https://api.nnviopp.com/api/pay_order`
- `POST https://api.nnviopp.com/api/query_transaction`

商戶回調網址 `pay_notify_url` 由商戶端提供，我方將於代收狀態變更後主動發送通知。

## 1. 支援之代收方式

目前支援以下 7 種收款方式：

| `pay_channel_id` | 說明 |
|---|---|
| `1000` | 信用卡一次付清 |
| `1001` | Apple Pay |
| `1002` | Google Pay |
| `1005` | WebATM |
| `1006` | ATM 虛擬帳號 |
| `1007` | 超商代碼 |
| `1008` | 超商條碼 |

## 2. 路由說明

| 功能 | 路由 |
|---|---|
| 建立收款訂單 | `POST /api/pay_order` |
| 查詢收款訂單 | `POST /api/query_transaction` |

請合作方以本文件列示之路由作為正式對接路徑。

## 3. 建立收款訂單

### Endpoint

`POST /api/pay_order`

### 請求欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `pay_customer_id` | string | Y | 商戶編號 |
| `pay_apply_date` | string | Y | Unix timestamp |
| `pay_order_id` | string | Y | 商戶訂單號 |
| `pay_notify_url` | string | Y | 商戶回調網址 |
| `pay_amount` | string / number | Y | 正整數 TWD 金額 |
| `pay_channel_id` | string | Y | 收款方式編碼 |
| `bank_account` | array[string] | N | 特定情境可傳入之帳號資訊 |
| `store_number` | array[string] | N | 特定情境可傳入之門市資訊 |
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
  "pay_customer_id": "RIG001",
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
    "view_url": "https://api.nnviopp.com/api/v1/deposits/PORDER202607090001123456/redirect",
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

- `view_url` 為付款頁面入口，商戶應引導使用者前往該網址完成付款。
- `real_price` 目前與 `bill_price` 相同。
- 以下欄位目前不由系統回傳：`qr_url`、`expired`、`alipay_qrcode`、`rate`。

## 4. 查詢訂單

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
  "pay_customer_id": "RIG001",
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
      "customer_id": "RIG001",
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
      "view_url": "https://api.nnviopp.com/api/v1/deposits/PORDER202607090001123456/redirect"
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

## 5. 商戶回調通知

當上游支付通知完成且系統確認狀態後，`RIG001 Gateway` 會主動將整理後的結果 POST 至商戶提供之 `pay_notify_url`。

商戶端收到通知後，請回應：

```text
OK
```

### 回調欄位

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `customer_id` | string | Y | 商戶編號 |
| `order_id` | string | Y | 商戶訂單號 |
| `transaction_id` | string | Y | 系統交易號 |
| `order_amount` | number | Y | 訂單金額 |
| `real_amount` | number | Y | 實際付款金額 |
| `sign` | string | Y | 回調簽名 |
| `status` | string | Y | 訂單狀態 |
| `message` | string | Y | 狀態說明 |
| `payer_info` | string | N | 銀行帳號或繳費資訊 |
| `extra.user_name` | string | N | 付款人姓名 |
| `extra.pay_product_name` | string | N | 商品名稱 |

### 回調狀態碼

| `status` | 說明 |
|---|---|
| `30000` | 訂單成功 |
| `50000` | 訂單失敗 |
| `10000` | 訂單處理中 |
