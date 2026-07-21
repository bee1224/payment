# Workspace 開發規範

## 邊界與命名

每個第一層專案目錄都是獨立專案，具有自己的 README、AGENTS、依賴、設定、測試與部署資產。不得跨專案 import 私有程式碼；整合必須使用公開 API 與文件化契約。

## 本機環境

使用 WSL／Bash。Go 統一設定為：

```bash
export GOCACHE="$HOME/.cache/go-build"
export GOPATH="$HOME/go"
```

禁止專案內 Go cache 或 GOPATH。暫存資料放在各專案的 `tmp/` 或 Workspace `tmp/`；封存包放在 `archives/`；兩者均不納版控。

## Secret 與共享資源

`.env`、憑證、token、備份與本機敏感暫存不得提交或複製到其他專案。只有與領域無關、無 Secret 且確實跨專案使用的腳本與範本，才能列為 Workspace Shared Resources。
