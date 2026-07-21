# 管理端

**驗證：Code Verified；環境實際登入與 Production 部署為尚未驗證。** React/Vite 管理端路由為 `/admin/login`、`/admin`、`/admin/payouts`、`/admin/payouts/:payoutNo`、`/admin/collections`。未登入路由由 `ProtectedRoute` 保護。

登入／登出／`me` 使用 cookie Session；登入或 `me` 回傳的 CSRF token 僅保存在記憶體，所有非 GET 請求帶 `X-CSRF-Token`。axios 使用 `withCredentials`，401 會清空 Session 與 query cache。後端僅對設定的 `ADMIN_ALLOWED_ORIGINS` 回 CORS credential headers。

已實作：代收列表、代付列表／明細、開始處理、收據上傳與下載、確認成功、失敗／取消、callback attempt 檢視與 retry。後端有 MFA enrollment／confirm 路由；前端是否有完整 MFA enrollment UI、細緻 RBAC 畫面與所有 audit 檢視，尚未驗證。前端建置：`npm run test`、`npm run build`；`VITE_API_BASE_URL` 必須分別指向 sandbox 或 production API，不能跨環境。
