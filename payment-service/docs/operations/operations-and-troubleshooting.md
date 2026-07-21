# 維運與排錯

先確認環境、hostname、Compose project、container、network，並以 `/health`、container health、唯讀 logs／DB 查詢確認。共用 Nginx、TLS、Docker daemon、防火牆任何操作都同時評估 Sandbox 與 Production。

| 症狀 | 檢查／處置 |
| -- | -- |
| `signature verification failed` | method/path/raw body、customer ID、timestamp、nonce 與 current/previous HMAC；不得放寬。 |
| timestamp expired／nonce replay | 時鐘、skew、nonce DB；重送需新 nonce。 |
| allowlist／trusted proxy 拒絕 | RemoteAddr、受控 proxy CIDR、Nginx forwarding headers；不可信任任意 XFF。 |
| callback 非 `OK`／timeout／exhausted | task、attempt、DNS／TLS、callback body，依 retry／alert；Manual callback 只看 2xx。 |
| DB bad connection／migration 失敗 | 停止變更、檢查 DB health／備份／版本；不要重跑不明 migration。 |
| Provider TradeNo 缺失／redirect 過期 | Provider Notify trace、訂單狀態、expiry，避免人工改 paid。 |
| receipt storage error | mount、權限、容量、magic type、10 MB 上限。 |
| worker 未執行 | worker lease、loop log、callback due task、DB connectivity。 |
| `GATEWAY_BASE_URL` 回自身 | 立刻修正為實際上游 Provider；不可用公開 API 自迴圈。 |

備份／還原：正式操作需變更窗口與授權，備份 DB 及 receipt volume，還原先在隔離目標驗證；本次未執行任何 drill，故實測 RTO/RPO 尚未驗證。
