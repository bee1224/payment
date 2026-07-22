# External Merchant Validation Report

## Milestone 5：External Merchant Happy Path Verified

**狀態：Complete（2026-07-22）。** reference merchant 僅使用 Sandbox-only Credential 與公開 HTTP API，完成代收建單、pending 查單、真實 NewebPay Sandbox 付款、Provider Notify、paid 查單、Callback HMAC 驗證與精確 `OK` 回應。平台端驗證為單筆 `deposit_paid` ledger、callback task `sent`、attempt `success`，並於 worker interval 後維持一筆 attempt。

本結果不代表 Production Ready，也不涵蓋一般代付、Manual Payout 或 Provider Duplicate Notify 實際重送。後者獨立為 **Milestone 5.1：Provider Duplicate Notify Idempotency Validation**，狀態為 **Blocked by unverified Provider resend capability**。

## Milestone 6：External Merchant Onboarding Ready

**狀態：Complete（2026-07-22）。** Fresh-session Walkthrough 僅依公開文件、Sandbox Credential、公開 Payment API 與 merchant-sandbox 自己的 CLI 完成建單、pending 查單、付款 URL、Sandbox 付款、paid 查單、Callback HMAC／timestamp／signature version 驗證、精確 `OK`、以及 30 秒後 `received_count` 未增加；不需要 payment-service internal、DB 或平台人員查詢 task／attempt。

## Milestone 6A：Merchant Callback Acceptance Visibility

**狀態：Complete（2026-07-22）。** Sandbox receiver 已部署 callback-status 與 `merchant_order_id` 非敏感 persistence。以全新代收訂單驗證 `received=true`、`received_count=1`、HMAC／timestamp valid、無 nonce replay、`hmac-sha256-v1`、HTTP `200`、exact `OK`，30 秒後 count 仍為 `1`。此功能不建立公開 HTTP endpoint、不查 payment-service DB，且不保存 payload、Secret、完整 Nonce 或完整 Signature。
