---
name: docker-diagnose
description: Diagnose payment-service Docker Compose, container, image, volume, network, health, logs, disk, and inspect issues; require approval before restart.
--- 
description: 在此聚合支付專案診斷 Docker Compose、container、image、volume、network、health check、logs、disk usage 或 inspect 問題時使用。預設採唯讀診斷；任何 restart、stop、remove、prune 或其他變更動作前必須取得使用者明確確認。
---

先確認目標環境與服務；預設僅檢查 `payment-service/compose.yaml`、相關部署設定及 Docker 的唯讀資訊。不要讀取無關專案內容，不要輸出 `.env`、容器環境變數或其他秘密。

依問題需要執行最小的唯讀檢查：Compose config、container 狀態、image、volume、network、health、近期 logs、disk usage 與 inspect。先從狀態與 health 開始，再讀取必要的近期 logs；避免未限量的 log 或全系統掃描。

區分設定、映像、掛載、網路、健康檢查、資源與應用程式錯誤，提出可驗證的根因與低風險下一步。不得執行 restart、stop、rm、prune、volume 清理、image 推送、deployment 或資料庫變更。

若 restart 可能有助於驗證，先說明確切 container／service、原因與影響，等待使用者明確同意後才執行。回報實際檢查、診斷結論及尚未驗證項目。
