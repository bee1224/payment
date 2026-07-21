# merchant-sandbox AGENTS.md

* 此專案是 Official Reference Merchant，不是 payment-service 的子模組或測試替身。
* 只能依 payment-service 已發布的外部文件，透過 HTTP 與 Sandbox API 溝通；不得 import、複製或共用 payment-service 的 `internal`、Model、Config、簽章函式、Secret 或資料。
* 只可使用 Sandbox URL、Sandbox 憑證與合成測試資料；任何 Production URL、憑證、訂單或 callback destination 都是設定錯誤，應 fail closed。
* Callback 成功回應必須為 HTTP 2xx 且 response body bytes 精確為大寫純文字 `OK`，不可含空白或換行。Nonce 重放必須拒絕；記憶體防護在重啟後會遺失，不得視為 Production 等級的全域冪等。
* Callback 僅依公開 HMAC-SHA256 Contract 與收到的原始 body 驗證；不得新增其他簽章、fallback 或將 API Key／API Request Secret 作為 Callback Signing Secret。
* WSL／Bash 為唯一標準操作環境，使用 `GOCACHE=$HOME/.cache/go-build` 與 `GOPATH=$HOME/go`。除非使用者明確要求，預設不執行 Git 操作。
* 不得把 Secret、完整 nonce、完整 signature、完整 callback payload 或個資寫入文件、輸出或驗收紀錄。
* Callback JSONL 紀錄只能保存非敏感 metadata、Callback Timestamp、Nonce／Signature 短指紋、body SHA-256、HMAC 結果與受控回應結果；不得保存 Secret、完整 Nonce、完整 Signature 或 payload。
