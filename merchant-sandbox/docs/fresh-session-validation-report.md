# Fresh Session Validation Report

## 結果

**狀態：Passed（2026-07-22）。** 本驗收以全新 Sandbox 代收訂單完成，僅使用公開文件、Sandbox Credential、merchant-sandbox CLI、公開 HTTPS API 與 merchant-sandbox 自己的 callback acceptance metadata；未以 payment-service internal、DB、task 或 attempt 作為商戶端操作步驟。

## 非敏感驗收證據

| 項目 | 結果 |
| --- | --- |
| Merchant order ID | `MERCHANT-M6A-1784693068` |
| Platform order ID | `MERCHANT-M6A-1784693068` |
| Transaction ID | `PTM6A1784693068120443` |
| Amount | `100 TWD` |
| 付款前查單 | `status=0`（pending） |
| 人工 NewebPay Sandbox 付款後查單 | `status=2`（paid），金額相符 |
| Callback acceptance | `received=true`、`received_count=1`、HMAC／timestamp valid、無 nonce replay、`hmac-sha256-v1`、HTTP `200`、exact `OK` |
| 成功後觀察 | 30 秒後 `received_count=1`，未增加 |

## 範圍與限制

- 驗收證明 Sandbox 代收 Happy Path 與外部商戶可見的 callback acceptance；不代表 Production Ready。
- 不涵蓋一般代付、Manual Payout、Provider Duplicate Notify 重送或 Production 部署。
- callback-status 只在 merchant 受控的 receiver 主機／共享 volume 執行，不提供公開 HTTP 查詢 API。
