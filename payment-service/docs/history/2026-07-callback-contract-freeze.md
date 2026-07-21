# 歷史紀錄：Callback Contract Freeze 與舊文件清理

> 歷史紀錄，已失效，不可作為現行串接依據。

## 事件

2026-07，payment-service 完整移除 MD5／legacy callback sign，代收與代付 Callback 收斂為唯一的 `hmac-sha256-v1` Header 契約。現行規格以 `docs/external/回調通知規格.md` 及 `docs/external/雜湊訊息驗證碼簽章規格.md` 為準。

## 清理項目

移除下列已淘汰的文件區：

- `docs/backend/給我自己看得/`：其中最後兩份 Postman 集合仍包含舊 MD5 gateway 測試。
- `docs/backend/對外溝通用/`：已無有效文件。

這些內容不再代表 payment-service 的 API 或 Callback 契約。API Request HMAC 與 Callback HMAC 的現行 Secret 邊界，均以現行外部文件為準。
