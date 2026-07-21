---
name: bug-fix
description: Diagnose and minimally fix one defect in this payment-service repository with root-cause evidence and focused regression tests.
--- 
description: 在此聚合支付專案診斷並修復單一缺陷時使用。進行根因分析，提出最小修復，驗證正常與重送或拒絕路徑，避免影響既有 API 與付款安全性。
---

先執行 `git status --short`，不要還原、覆寫或格式化無關變更。

從錯誤訊息、測試、路由或受影響入口開始，沿呼叫鏈逐步讀取必要檔案。不要預設全專案掃描；資訊不足而需擴大範圍時先說明原因。

先寫出可驗證的根因與重現／失敗條件，再以最小改動修復。不得藉由放寬驗證、吞掉錯誤、停用安全檢查或改變既有 API 來掩蓋問題。

付款、餘額、總帳與 callback 修復必須保留整數金額、交易邊界與冪等性；驗證失敗採 fail closed，並保留適用的 audit log。

補充或更新聚焦測試，至少覆蓋原始失敗與修復後成功；涉及重送或安全驗證時另覆蓋重送或拒絕路徑。先跑聚焦測試，再盡可能執行 `go test ./...` 與 `go build ./cmd/api`。回報根因、修改、證據與剩餘風險。
