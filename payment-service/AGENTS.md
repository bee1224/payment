# payment-service AGENTS.md

## 專案定位與環境

* Go 1.22／MariaDB 10.11 支付 API，含 React／Vite 管理後台、代收、代付、商戶驗證、總帳、callback、重試與人工代付。
* Production：`/opt/payment/payment-service`、`api.nnviopp.com`；Sandbox：`/opt/payment/payment-service-sandbox`、`sandbox-api.nnviopp.com`、`sandbox.nnviopp.com`。
* 兩環境同 VPS、同 Public IP，但 DB、帳號、Secret、Provider credential、商戶資料、callback、network、volume 與 Compose Project 必須完全隔離。AWS Lightsail 不再是部署依據。

## VPS 與遠端操作

* 執行遠端變更前，確認 VPS hostname、目標環境、部署路徑、Compose Project、container、network，以及是否觸及 Nginx、Docker daemon、防火牆、TLS、磁碟或主機重啟等共用資源。
* 標準操作路徑為 Windows PowerShell → SSH → Ubuntu Bash → Docker／MariaDB／Nginx。不要把複雜 Docker、SQL、heredoc、command substitution 或多層引號直接塞入單一 SSH 命令。
* Production 前必須先列出影響、執行動作、驗證與回滾並取得明確同意。正常發布順序：本機測試 → Sandbox 部署 → Sandbox 驗收 → Production 部署。
* 未經明確授權，不得進行 Production 部署／重建／重啟／migration／環境變數或資料修改，亦不得做真實金流測試。

## 專案結構與修改原則

* 先閱讀受影響的 Handler、Service、Repository、Migration、設定與既有測試，再做最小且聚焦的修改；不得無證據全面重寫或新增不必要抽象。
* `cmd/api/` 為入口；`internal/` 為領域與實作；`frontend/` 為管理端；`migrations/` 為 schema 演進；`docs/` 為文件；`deploy/` 為部署設定。
* 既有 migration 不得改寫已發布語意；修正必須新增 migration。
* `.tgz`、`.tar.gz`、`.gocache/`、`.gomodcache/`、`.gopath/`、`tmp/`、`output/`、執行檔及其他工作產物不得任意修改、清理或納入審查。

## 支付與帳務不可破壞規則

* 禁止以 `float32` 或 `float64` 儲存、運算或比較金額；金額型別、精度與單位須延續既有 Domain、API 與 DB 約定。
* 代收／代付狀態、總帳、餘額、人工確認與 callback task 必須冪等。重送 API、Provider Notify 或 Worker 不得造成重複入帳、扣款、分錄、訂單完成或非法狀態跳轉。
* 最終保護必須由 transaction、原子條件更新及合理 DB constraint 提供。狀態轉換集中管理；不得在 Handler、Worker 或 Repository 任意寫狀態字串，且不得於 commit 後使用失效 transaction handle。
* 人工代付 claim 與確認成功須防止併發及重複確認。已完成／入帳訂單收到相同事件時應冪等；資料不一致時 fail closed 並留下可追查紀錄。

## HMAC、Callback 與人工操作

* HMAC、timestamp、nonce、來源 IP allowlist 與 trusted proxy 驗證均 fail closed；禁止新增弱雜湊簽章、SHA-1 或未文件化 fallback。
* 所有新增對外簽章必須使用既定 HMAC-SHA256 Contract。Merchant API Key、API Request Signing Secret、Callback Signing Secret 與 Gateway HMAC Secret 不得混用；修改契約時同步更新程式、測試、Golden Case、對外文件與交付紀錄。
* 主密鑰為 `GATEWAY_HMAC_SECRET`；只有既有輪替流程才使用 `GATEWAY_PREVIOUS_HMAC_SECRET`。trusted proxy 只能是受控 Proxy 或明確 CIDR；單機記憶體 nonce 不等於全域防重放。
* 商戶 callback 成功條件固定為 HTTP 2xx 且 response body bytes 精確為大寫 `OK`（不可含空白或換行）。失敗必須進既有 retry、audit 或告警流程；payload、簽章與成功判定是外部契約，不得任意變更。
* Manual Payout 只共用 Delivery Engine，不得為外觀強行合併 Repository、Worker 或 Task Model。Provider Notify、人工代付、收據、調帳、沖正、API Key、餘額異動與 callback 人工重送均須保留 audit log。
* 收據上傳要限制大小、實際 MIME 與允許類型，防止 path traversal，且不可經可猜測公開路徑存取。

## Secret、驗證與相容性

* 不得在程式、文件、測試輸出、部署封包或回報寫入正式金鑰、DSN、密碼、token、Session secret、HashKey、HashIV、完整個資或 `.env` 實值；範例使用 placeholder。
* 在 WSL／Bash 執行驗證，使用 `GOCACHE=$HOME/.cache/go-build`、`GOPATH=$HOME/go`。先跑最相關測試，再於可行時執行 `go test ./...` 與 `go build ./cmd/api`。
* 修改 HTTP 行為時驗證成功、錯誤、重送與未授權；修改 Service、狀態、簽章、加解密、IP 或帳務時補正常、重送、拒絕測試，必要時補併發與 transaction failure。
* 修改設定欄位時同步檢查 config、環境範例、Compose、Production／Sandbox 部署設定與文件。外部 API、環境變數、簽章、callback payload 或支付狀態變更前，必須做相容性分析；不得破壞性 rename、移除路由或變更成功格式。
* `GATEWAY_BASE_URL` 必須指向實際上游出金 Provider，不得指回本服務公開 API。Docker、migration 或資料查詢一律先確認目標環境，且不得使用會清空 volume、刪 DB 或大量刪容器的指令。

## Git 與完成回報

* 除非使用者明確要求，不執行 Git 操作、工作樹檢查、commit、push、pull、fetch、checkout、switch、merge、rebase 或 reset；禁止 `git reset --hard`、`git checkout --`、大範圍還原與覆寫無關檔案。
* 回報須列出問題判定、修改檔案、核心邏輯、測試／build、尚未驗證、Sandbox／Production 影響、migration、既有 API 影響、部署前置條件與回滾。不得將 Code Complete 直接稱為 Production Ready。
