# 環境晉級

`Local → Sandbox → External Integration Verification → Production Candidate → Production`

| 階段 | 最低門檻 |
| -- | -- |
| Local | 聚焦測試、`go test ./...`、`go build ./cmd/api`；設定不含真實 Secret。 |
| Sandbox | 隔離 env／DB／volume／credential，health、管理端與內部 smoke test。 |
| External Integration Verification | 外部商戶 callback URL、HMAC、HTTP／body 成功、retry、task／attempt／audit 受控驗收。 |
| Production Candidate | Sandbox 結果、差異設定、備份、migration 相容性、回滾與 Provider／callback 端點都經審查。 |
| Production | 使用者明確授權後部署與非真實付款 smoke test。 |

目前：Code／Test closeout 可由程式與測試核對；Sandbox 部署存在但本次未連線複驗；External callback smoke test Pending；因此不是 Production Ready。
