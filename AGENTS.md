# Workspace AGENTS.md

## 工作原則

* 使用繁體中文回覆、撰寫文件與交付報告；程式識別字與 API 欄位可維持英文。
* 此目錄是 Workspace，不是單一專案。先確認目標專案與其專屬 `AGENTS.md`，再做最小、可測試的變更。
* 無法由程式碼、文件或測試確認的內容，標記為「尚未驗證」，不得猜測。
* 除非使用者明確要求，預設不執行 Git 操作、不檢查工作樹，亦不以 Git 狀態作為驗收條件。

## Workspace 邊界

* `payment-service/` 是獨立的正式支付服務；其支付、帳務、部署與環境規則以該目錄的 `AGENTS.md` 為準。
* `merchant-sandbox/` 是獨立的 Official Reference Merchant，只能經公開 HTTP API 與 payment-service 溝通；不得 import 或複製 payment-service 的 `internal`、設定、Model 或簽章實作。
* 未來 SDK、Portal 或工具均為獨立專案；只有明確跨專案且不含領域邏輯、Secret 或部署設定的資產，才能放入共用目錄。
* 不得跨專案共用 DB、帳號、Secret、Provider credential、商戶資料、callback destination、Docker volume 或環境檔。

## 環境與工作產物

* WSL／Bash 是唯一標準本機操作環境。Windows PowerShell 僅可作為進入 WSL 或 SSH 的外層環境，兩者語法不得混用。
* Go 統一使用 `GOCACHE=$HOME/.cache/go-build` 與 `GOPATH=$HOME/go`；不得建立或使用專案內 `.gocache/`、`.gomodcache/`、`.gopath/`。
* `tmp/`、`archives/`、本機快取、建置產物、備份與本機敏感暫存資料不得納入版本控制；不得任意清理既有內容，除非使用者明確授權。
* 不得將 Secret、DSN、密碼、token、完整個資或 `.env` 實值寫入程式碼、文件、測試輸出、封包或回報。範例只能使用 placeholder。

## 共用主機與環境安全

* Production 與 Sandbox 即使位於同一 VPS，也只能共用主機 OS、Public IP、Docker Engine、Nginx、主機防火牆與 SSH；應用層資源必須隔離。
* 變更 Nginx、Docker daemon、防火牆、TLS、磁碟或主機服務前，必須評估兩環境影響。未經明確授權，不得部署、重啟、migration、修改 Production 設定／資料，或進行真實金流測試。

## 文件與交付

* 外部 API 或環境行為變更時，更新最貼近的專案文件並分析相容性。
* 完成回報需說明修改、測試、尚未驗證項目、Sandbox／Production 影響、migration 需求、部署前置條件及回滾方式。
