# Milestone 5：External Merchant Happy Path Verified

**狀態：Complete（2026-07-22）。** 此為 Sandbox 驗收，不是 Production Ready。

## 已驗證

- merchant-sandbox 僅使用 Sandbox-only Credential，且只透過公開 HTTP API。
- 代收建單、pending 查單、真實 NewebPay Sandbox 付款與真實 Provider Notify。
- order=`paid`、provider transaction=`paid`、`deposit_paid` ledger 僅一筆。
- merchant callback HMAC 驗證成功，timestamp 存在，signature version=`hmac-sha256-v1`。
- receiver 回 HTTP 2xx 且 body 精確為 `OK`；callback task=`sent`、attempt=`success`。
- 成功後等待至少一個 worker interval 與排程緩衝，attempt 維持 `1`，確認停止重送。

## 明確排除

- Production 驗證、一般代付 Provider 端對端驗證、Manual Payout 完整驗收。
- Provider 實際重送的重複 Notify 驗證。

## 後續邊界

**Milestone 5.1：Provider Duplicate Notify Idempotency Validation**：**Blocked by unverified Provider resend capability**。此阻擋不代表 Milestone 5 失敗或未完成；不得用手工偽造 Provider Notify 取代實際 Provider 重送。

詳細證據見 [外部系統商 Sandbox 串接交付紀錄](../delivery/external-sandbox-integration-record.md)。
