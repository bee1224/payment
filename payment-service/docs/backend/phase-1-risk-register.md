# Phase 1 Closeout Risk Register

| 項目 | 狀態 | 證據／處置 |
| -- | -- | -- |
| 外部商戶 callback smoke test | Pending External Smoke Test | 需 Sandbox 真實測試商戶、callback URL 與協作方回覆完成。 |
| Production 部署／health／worker／備份 | Production Unverified | 本次不連線、不部署；依 operations runbook 執行前須取得授權。 |
| Manual callback 成功定義與一般 callback 不同 | Partial | 現行為 2xx；是否需 `OK` 屬外部契約待確認。 |
| 退休管理路由 | Code Verified | 皆 410；第二階段若要對外／管理操作，先設計 MFA RBAC surface。 |
| 單 VPS 主機層共同故障域 | Accepted Risk | Sandbox／Production 已隔離應用資源，Nginx／Docker／TLS／主機仍共用。 |
| 多 Provider、MQ、HA | Not Started | 非 Phase 1 範圍；以容量與可靠性數據決定。 |
| 敏感資料歷史文件 | Remediated in docs | 移除舊 API-key fingerprint 文件；仍需人工確認版本庫／外部副本及必要輪替。 |

第二階段候選：管理 API 完整化、external callback contract 收斂、Provider onboarding、監控／告警驗收、備份還原演練與主機層高可用評估。這些不是本次實作承諾。
