# 銀行與渠道編碼

**驗證：Code Verified（渠道）；銀行碼由 `internal/provider/gateway/bank_codes.go` 維護。** 對外代收只支援七種渠道，見 [Merchant API](merchant-api.md)。代付的三碼銀行碼必須由 Provider Gateway 的白名單驗證；文件不重複手工維護可能漂移的完整清單。新增或停用銀行碼須同時更新程式、測試與對外契約，不能只改文件。
