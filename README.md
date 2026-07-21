# 四方聚合支付 Workspace

此 Workspace 用於管理彼此獨立、但以公開契約整合的正式專案。

## 專案

* `payment-service/`：正式支付服務，提供代收、代付及商戶 callback 等公開 API。
* `merchant-sandbox/`：Official Reference Merchant，以獨立商戶系統的角度驗證 payment-service Sandbox 公開 API、HMAC 與 callback；不 import 或共用 payment-service 程式碼。

## 邊界

各專案擁有自己的程式碼、設定、Secret、資料、Docker 資源、環境與部署流程。跨專案只允許透過公開 HTTP API 與明確文件契約整合；不得共用 `internal`、Model、簽章函式或環境檔。

## 本機標準

唯一標準操作環境為 WSL／Bash。Go 使用使用者層級快取：

```bash
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
```

不得使用或建立專案內 `.gocache/`、`.gomodcache/`、`.gopath/`。各專案的專屬規則請閱讀其 `AGENTS.md`。
