---
name: security-review
description: Review a specified payment-service API flow or change set for authentication, authorization, HMAC, replay protection, idempotency, sensitive data, and audit risks.
--- 
description: 在此聚合支付專案審查指定 API、付款流程或變更集的安全性時使用。檢查 authentication、authorization、HMAC、replay protection、idempotency、SQL injection、敏感資料、audit log、secrets 與 rate limiting；只分析授權範圍且不洩漏秘密。
---

先確認審查範圍；只讀指定流程、diff、路由及其直接驗證／service／repository 相依。不要預設掃描全專案；需擴大範圍時先說明原因。

針對每個外部入口檢查認證、授權、輸入驗證、錯誤回應與速率限制。HMAC、時戳、nonce、來源 IP allowlist 與 trusted proxy 驗證必須 fail closed；不得重新啟用 `pay_md5_sign`。Gateway 僅接受 `GATEWAY_HMAC_SECRET`，輪替時才允許 previous secret。

檢查重送、並行與 callback 去重，確保狀態、總帳、餘額與通知皆冪等。商戶 callback 成功條件為 HTTP 2xx 且 body 為 `OK`；失敗不可靜默忽略。

檢查 SQL 是否參數化、權限是否最小化、敏感資料是否未出現在程式、log、測試或文件，以及 provider 通知、審核、人工調帳／沖正、API key 與餘額異動是否有 audit log。

以嚴重度、位置、攻擊／失效情境、影響與修正方向回報；不得輸出、複製或要求真實密鑰、DSN、密碼、token 或 callback URL。未獲授權不得修改程式或部署設定。
