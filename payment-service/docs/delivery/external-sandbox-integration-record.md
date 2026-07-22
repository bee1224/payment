# 外部系統商 Sandbox 串接交付紀錄

## 目標

讓目前外部系統商只依 `docs/external/` 與 Sandbox 環境完成代收、代付、HMAC 與 Callback Smoke Test。

## 已完成

- 2026-07-22：Milestone 6A **Merchant Callback Acceptance Visibility** 完成。merchant-sandbox Sandbox receiver 已部署 `callback-status`，僅保存公開 merchant order ID 與非敏感 acceptance metadata。全新 Fresh-session 訂單已完成建單、pending、真實 NewebPay Sandbox 付款、paid 查單、HMAC／timestamp／signature version、HTTP `200`＋精確 `OK`，並於 30 秒後確認 `received_count=1` 未增加；外部商戶不需要 payment-service internal、DB、task 或 attempt table。此結果不代表 Production Ready。
- 2026-07-22：Milestone 6 **External Merchant Onboarding Ready** 完成。外部商戶可僅依公開文件與 Sandbox Credential 完成代收 Happy Path；一般代付、Manual Payout、Provider Duplicate Notify 與 Production 不在本驗收範圍。
- 2026-07-22：Milestone 5 **External Merchant Happy Path Verified** 完成。merchant-sandbox 以 Sandbox-only Credential 與公開 HTTP API 完成代收建單、pending 查單、真實 NewebPay Sandbox 付款、Provider Notify、order／provider transaction=`paid`、單筆 `deposit_paid` ledger、Callback HMAC valid、signature version=`hmac-sha256-v1`、HTTP 2xx＋精確 `OK`、task=`sent`、attempt=`success`；等待 worker interval 與排程緩衝後 attempt 維持 `1`。不代表 Production Ready。
- 2026-07-22：Milestone 5.1 **Provider Duplicate Notify Idempotency Validation** 記為 **Blocked by unverified Provider resend capability**。不得以手工 HTTP 或偽造 Provider Notify 取代實際 Provider 重送；此項不影響 Milestone 5 完成判定。
- 2026-07-22：Milestone 6 **External Merchant Onboarding Ready** 文件入口、Credential Guide、可複製代收 Happy Path、Callback 契約與 troubleshooting 已收斂至 `docs/external/README.md`；外部商戶正常流程不需要 payment-service internal、DB 或內部工具。
- 2026-07-22：Milestone 5A Contract Closure 將代收、一般代付與 Manual Payout 的 Callback 成功條件收斂為 HTTP 2xx 且 response body 原始 bytes 精確為 ASCII `OK`；前後空白、換行、大小寫差異與其他 body 均進入失敗／重送路徑。此為程式與文件契約修正，不是新的代付或 Production 驗收。
- 2026-07-21：修正代收文件的 `pay_apply_date` 為 Unix timestamp（秒）契約。
- 2026-07-21：補齊對外 Sandbox 串接、callback smoke test、錯誤碼與重試處理文件。
- 2026-07-21：加入終態代收訂單缺少 callback task 時的 recovery；同一 event key 會冪等補建 outbox，避免 post-commit task 寫入失敗造成永久漏送。
- 2026-07-21：修正 JSON number 型別的 `pay_amount` 被錯誤拒絕問題；對外文件明確接受 JSON number 與數字字串。
- 2026-07-21：確認公開 Sandbox `GET /health` 回應 HTTP 200。
- 2026-07-21：執行 `go test ./internal/app ./internal/delivery/http ./internal/service ./internal/repository` 與 `go build ./cmd/api`，結果通過。
- 2026-07-21：以暫時、Sandbox-only HMAC 診斷核對 Handler 實際使用的 `customer_id`、Timestamp、Nonce、HTTP method、request path 與 raw body SHA-256；診斷只輸出簽章短指紋，未輸出 Secret、完整簽章或 request body。診斷完成後已關閉，並以失敗簽章確認正常執行不再輸出診斷資料。
- 2026-07-21：以真實 Sandbox HMAC `POST /api/pay_order` 建單成功（HTTP 200、`code=0`），並取得付款 redirect URL；Golden Integration Case 已記錄於外部 HMAC 文件。
- 2026-07-21：確認付款 redirect URL 從 Sandbox VPS 可取得 HTTP 200。
- 2026-07-21：將本機唯一標準操作切換為 WSL／Bash；新增 WSL 工具預檢、Bash Sandbox 控制與 drill 腳本，對外 Sandbox 文件、Callback Smoke Test Runbook 與此交付紀錄同步改用 Bash／curl。
- 2026-07-21：以程式碼確認代收 `pay_notify_url` 逐筆保存於 provider transaction request payload，Provider Notify／expiry／recovery 載入訂單時會恢復該值，outbox task 取用恢復後的 URL；`MERCHANT_CALLBACK_URL` 非新建代收訂單 callback 來源。
- 2026-07-21：修正 WSL 執行 migration URL 測試時無法辨識 Windows drive path 的跨平台相容性；補齊 Windows／Linux、空白與非 ASCII 路徑測試，並完成完整 Go 測試與 build。
- 2026-07-21：PowerShell → WSL 開發環境切換已驗收完成並結案；後續不再重複處理此階段。
- 2026-07-21：再次 Code Verified 代收 `pay_notify_url` 逐筆保存與正式 outbox callback 路徑；補齊 NewebPay 人工付款、付款後立即取證與停止重送的 Runbook。
- 2026-07-21：部署評估：最新 migration URL 相容性修正應納入下一次 Sandbox release，但不改變已運行服務的 callback 路徑，非本次真實 Callback Smoke Test 的必要前置。Sandbox SSH 唯讀連線因本機網路不可達，現行 VPS source checksum 尚未驗證；未部署、未觸及 Production。
- 2026-07-21：完成 Milestone 4 真實 Sandbox Callback Smoke Test。成功鏈路 `M4S5-20260721185303`／`P20260721185303185320` 已驗證真實付款、Provider Notify、單筆 `deposit_paid` ledger、Callback HMAC valid、`200 OK`、task `sent` 且停止重送；重複 Provider Notify 未重複入帳或建立多餘 task。
- 2026-07-21：完成 Retry 鏈路 `M4R2-20260721190704`／`P20260721190704190719`。前兩次 callback 回 `503` 並建立 failed attempts，第三次 retry 在 Receiver 切回 `success` 後回 `200 OK`；body hash 相同，timestamp／nonce 指紋／signature 指紋均更新，task 最終 `sent` 且停止重送。
- 2026-07-21：Sandbox-only 修正 callback worker lease 使用 UTC 比較、Callback Signing Key resolver SQL alias、Reference Merchant JSONL 安全指紋紀錄與部署 port 設定；未修改、重啟或部署 Production。

## 尚待完成／驗收依賴

- 平台側：設定可用的 Sandbox 上游代付 `GATEWAY_BASE_URL`；本機 `.env.sandbox` 目前仍為必要值 placeholder，不能據此驗證上游代付。
- Production 部署與 Production smoke test 尚未執行；Sandbox Verified 不等於 Production Ready。

## 驗收證據欄位

| 項目 | 平台紀錄 | 系統商紀錄 | 結果 |
| --- | --- | --- | --- |
| 代收建單與付款 | `M4S5-20260721185303`／`P20260721185303185320` | Reference Merchant JSONL | 真實 Sandbox 付款與成功 Callback 通過 |
| 代收 callback | task id `40`、attempt no `1` success、HTTP `200`、task `sent` | body hash `c5899025bcf61daff17818a3907cb42c9628a7afe8cf2ab6761bcec5fd0fd6fa`、HMAC valid=`true` | 通過 |
| callback 重試 | `M4R2-20260721190704`／task id `42`；attempt no `1`、`2` failed 503，no `3` success 200 | body hash `8e14c655f1f07e494eb7f925bed34b3cdae83ddf07c6e404eee436d77b5ba684`；timestamp／nonce／signature 指紋均更新 | 通過 |
| 代付建單與查詢 | payout 編號／狀態 | 商戶代付編號 | 待執行 |

## 最終 Delivery Checklist

- [x] 外部系統商可取得 Sandbox 代收、代付、HMAC、callback 與錯誤／重試文件。
- [x] Golden Integration Case 已有 Bash 腳本，必須帶入本次公開 HTTPS `pay_notify_url` 才能建單。
- [x] 真實 callback 成功、失敗重送、成功後停止的雙方證據格式與平台唯讀觀測命令已備妥。
- [x] 逐筆 `pay_notify_url` 的保存與正式 callback outbox 讀取路徑已 Code Verified。
- [x] WSL 工具鏈、跨平台 migration URL 修正與本機 Go 測試／build 驗收完成；環境切換階段結案。
- [x] 新的未過期 Sandbox 訂單已以外部系統商 URL 建立。
- [x] NewebPay Sandbox 真實付款、Provider Notify、單筆 ledger、task／attempt 與 callback 成功已驗收。
- [x] 失敗重送與成功後停止已驗收。
