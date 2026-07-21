---
name: feature-implementation
description: Implement one feature in this payment-service repository with minimal compatible changes and focused tests.
--- 
description: 在此聚合支付專案新增單一功能時使用。遵循 payment-service 的 Go 分層與 React/Vite 架構，以最小變更維持既有外部 API 相容性，並補齊必要測試。
---

先執行 `git status --short`，保留所有無關既有變更。

僅搜尋功能相關的入口與相鄰層：`cmd/api/`、對應的 `internal/{delivery,service,repository,domain,provider}/`、測試及必要 migration。不要預設掃描整個 repository；需擴大範圍時先說明原因。

依既有分層、命名與錯誤處理模式實作最小變更。外部 API、狀態與資料格式維持相容；如必須改變，先停止並要求確認。

金額維持整數最小貨幣單位。付款狀態、總帳、餘額、callback 與重送路徑必須維持冪等；敏感操作延續 audit log。schema 僅以新 migration 演進，絕不改寫既有 migration 語意。

先跑最相關測試；可行時在 `payment-service/` 設定專案內 `.gocache`、`.gopath` 後執行 `go test ./...` 與 `go build ./cmd/api`。回報修改、驗證及未驗證風險。
