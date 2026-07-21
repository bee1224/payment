# Migration Runbook

Migration 位於 `migrations/`；服務啟動會呼叫 migration。空 DB 與既有 DB 都只能在明確目標環境執行。先確認 hostname、DSN 所屬環境、備份、目前版本、schema 相容性與 rollback；Production 另需明確授權。

已發布 migration 不得修改；修正採新增 additive migration。若 migration 失敗，停止後續部署、保存錯誤與版本狀態、從備份／相容版本評估；不能假設每一份 migration 都有安全 down 檔。Sandbox 與 Production 必須各自備份、各自 migration，不能以一方資料或 volume 補救另一方。
