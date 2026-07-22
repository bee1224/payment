# Milestone 6：External Merchant Onboarding Ready

**狀態：Complete（2026-07-22）。** 範圍是 Sandbox 文件與操作入口；不代表 Production Ready。

## 完成條件

- [x] Milestone 5 已封版，5.1 邊界獨立記錄。
- [x] 外部文件有唯一入口、閱讀順序、credential guide、可複製代收 Happy Path 與 troubleshooting。
- [x] Callback HMAC、replay protection、精確 `OK` 契約與 Sandbox／Production 邊界清楚。
- [x] 正常外部商戶流程的文件不要求 payment-service internal、DB 或平台內部工具。
- [x] 由 fresh session 僅帶 Credential 與本入口完成 walkthrough：建單、pending、付款 URL、人工 Sandbox 付款、paid 查單、callback-status acceptance metadata 與成功後未重送均已驗證。

## Milestone 6A：Merchant Callback Acceptance Visibility

**狀態：Complete（2026-07-22）。** merchant-sandbox 只保存公開 `order_id` 與非敏感 acceptance metadata，並以商戶受控主機上的 `callback-status --order-id <merchant-order-id>` 提供 received count、HMAC、timestamp、nonce replay、signature version、response status 與 exact-`OK` 結果。Sandbox Fresh-session 驗收已在首次成功後等待 30 秒，確認 `received_count` 維持 `1`。不提供公開 HTTP 查詢 API，不需要 payment-service DB、task 或 attempt table。

## 已知非阻擋限制

- 一般代付 Provider 端對端驗證、Manual Payout、Provider Duplicate Notify 重送不屬本 milestone。
- Sandbox Credential 仍須由平台以受控管道交付；這是必要的信任邊界，不是文件缺口。
