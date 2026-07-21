# HMAC、來源驗證與 Secret Rotation

**驗證：Code Verified。** Headers：`X-Customer-Id`、`X-Timestamp`（Unix seconds）、`X-Nonce`、`X-Signature`。canonical string 以 LF 串接：`customer_id`、timestamp、nonce、uppercase HTTP method、request path、`SHA-256(raw body)` 的十六進位值；以 HMAC-SHA256 計算，signature 是十六進位字串。

Timestamp 預設容許視窗為 `GATEWAY_MAX_SKEW_SECONDS`（範例 300 秒）；nonce 以 `(gateway_request:customer_id, nonce)` 寫入 DB-backed `replay_nonces`，唯一鍵拒絕重送。缺 header、時間超窗、簽章不符、Customer ID 不符、nonce 已用過都 fail closed。若沒有 DB，程式 fallback 是記憶體 store；Production／Sandbox 的 DB-backed 實際狀態需由部署驗證。

主密鑰是 `GATEWAY_HMAC_SECRET`；輪替期間才設定 `GATEWAY_PREVIOUS_HMAC_SECRET`，驗證依序接受兩者但不得記錄值。輪替程序：受控管道發新 key → 先以 previous＋current 雙驗證短期部署 → 所有商戶切換並完成 smoke test → 移除 previous → 稽核。不要把 key、DSN、HashKey、HashIV、password 或 token 放進文件／issue／log。

Provider Notify 另以各 Provider payload 驗證及 `GATEWAY_DEPOSIT_CALLBACK_ALLOWLIST`／`GATEWAY_PAYOUT_CALLBACK_ALLOWLIST` 保護。`X-Forwarded-For`／`X-Real-IP` 只在 remote peer 屬於 `APP_TRUSTED_PROXY_CIDRS` 時信任；未列受控 Nginx／proxy 時只採 RemoteAddr。allowlist、proxy 設錯一律拒絕或呈現來源錯誤，不可降級放行。所有商戶 Callback 只使用文件化的 HMAC-SHA256 Header Contract。
