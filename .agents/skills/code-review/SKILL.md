---
name: code-review
description: Review a specified payment-service diff or file for readability, maintainability, best practices, technical debt, and potential bugs.
--- 
description: 在此聚合支付專案審查指定 diff、檔案或變更集時使用。聚焦可讀性、可維護性、最佳實務、技術債與潛在 Bug；以具體檔案位置與風險分級提出發現，不進行未授權修改。
---

先確認審查範圍；預設只讀指定 diff、檔案與其直接相依檔案。若未指定範圍，先讀 `git status --short` 與可用 diff；不要掃描整個 repository。

依嚴重度排序，只回報可行動且有證據的問題。每項包含位置、問題、影響與精簡修正方向；沒有發現時明確說明仍未覆蓋的範圍。

檢查分層界線、命名、錯誤處理、可測試性、重複邏輯、相容性、migration 演進與測試缺口。付款相關變更另檢查整數金額、狀態轉換、交易／總帳／餘額及 callback 冪等性。

不得把純風格偏好列為阻擋問題，不得修改程式碼、重設工作樹或輸出任何秘密值。
