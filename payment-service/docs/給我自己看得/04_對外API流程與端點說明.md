# 對外 API 流程與端點說明

> 最後更新：2026-07-11

## 契約分層

### 收款正式契約

- `POST /api/pay_order`
- `POST /api/query_transaction`

### 代付正式契約

- `POST /api/payouts`
- `POST /api/payouts/query`
- `GET /api/payouts/{payout_no}`

### Provider 相容代理層

- `POST /api/payments/pay_order`
- `POST /api/payments/query_transaction`
- `POST /api/payments/balance`
- `POST /api/payments/callback`

## 目前對外說法

- 對商戶：用 `RIG001 Gateway`
- 對上游：我方內部仍可能保留 provider 相容欄位
- 對自己：不要把 provider 相容代理層誤當成商戶正式 workflow API

## 收款流程

1. 商戶呼叫 `POST /api/pay_order`
2. 服務驗 `pay_customer_id`、`pay_apply_date`、`pay_md5_sign`
3. 系統建立本地訂單並映射收款渠道
4. 服務返回 `view_url`
5. provider 完成收款後回調我方
6. 我方更新訂單狀態並 callback 商戶 `pay_notify_url`

## 代付流程

1. 商戶呼叫 `POST /api/payouts`
2. 服務驗商戶與 API Key、資料格式、銀行代碼、餘額與冪等
3. 建立本地 payout order
4. 審核通過後由 `/approve` 送往上游
5. provider callback 或 query 補查更新狀態
6. 我方 callback 商戶

## 目前部署前你要記住的事

- 正式商戶號：`RIG001`
- 三方密鑰：我方發放 API Key
- 藍新 `HashKey` / `HashIV` 只屬於我方對上游，不屬於商戶對接憑證
