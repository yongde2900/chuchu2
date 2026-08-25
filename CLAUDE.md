# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

chuchu2 是**包租代管**服務（台灣租賃住宅管理業）。Go 1.26，module `github.com/yongde2900/chuchu2`。

## 指令

```bash
go build ./...                  # 建置
go vet ./...                    # lint —— 唯一的閘門，不使用 golangci-lint
go test -race -count=1 ./...    # 全部測試
go generate ./api/...           # 由 api/openapi.yaml 產生 api/api.gen.go

# 單一測試（-count=1 必要：Go 會快取測試結果，容器背後的測試從快取回綠會騙人）
go test -race -count=1 ./internal/property/ -run TestCreateInput_Validate
go test -race -count=1 ./test/ -run TestPropertyList_Filter   # 需要 Docker

# migration CLI（六個子指令，up／down 已廢除）
go run ./cmd/dbmigrate <init|migrate|rollback|status|unlock|create_sql> --config=test
```

`./internal/...` 的測試不需要 Docker；`./test/` 的每一個都需要。

## 架構

### 組裝只有一份

`internal/app.NewHandler` 是唯一把東西接起來的地方：手寫建構子，不用 DI 框架、
不用全域變數、不用 `init()`。**它因此會 import 一堆 feature 套件，而其他套件都只有
寥寥幾個 —— 這是設計，不要「順手清理」。**

組裝**不放在 `cmd/api`**，因為 `package main` 無法被 import，整合測試就只能開子行程，
而子行程裡的 handler 設不了中斷點。`cmd/api` 保留行程層面的職責：設定載入、開連線、
signal、優雅關閉。

`internal/app` 的 `apiServer` 把 `health.API` 與 `httpapi.API` 併成完整的
`api.StrictServerInterface`。它用具名欄位＋轉發方法而非 embedding，因為兩個型別都叫 `API`，
內嵌會編譯失敗（`API redeclared`）。

### 分層邊界＝套件邊界

違規會表現成「錯的套件出現了錯的 import」。**這些約束目前沒有任何自動強制**，要靠人記得驗：

```bash
grep -rn "uptrace/bun\|net/http" internal/property/*.go                   # 應只命中註解
go list -f '{{join .Imports "\n"}}' ./internal/apihttp | grep yongde2900  # 不應有 feature 套件
go list -f '{{join .Imports "\n"}}' ./internal/server  | grep yongde2900  # 只應有 internal/httpx
```

- `internal/property/` —— 純領域＋service＋`Repository` 介面，不 import bun 也不 import net/http。
- `internal/property/pgrepo/` —— 唯一碰 bun 的地方。
- `internal/property/httpapi/` —— 只做「產生的 api 型別 ↔ 領域型別」轉換，**連 net/http 都不 import**。
- `internal/apihttp/` —— 產生的 HTTP 層與 apperr 之間的唯一接線點，**不 import 任何 feature 套件**。
- `internal/server/` —— chi router 組裝，不 import 任何 feature 套件。
- `internal/app/` —— **組裝點**，唯一 import 多個 feature 套件的地方（驗證時要排除它）。

三個依賴反轉點：`property.Repository`、`health.Checker`、`server.Mount`。

### HTTP 層是 spec-first 產生的

`api/openapi.yaml` 是契約的唯一權威來源 → `go generate ./api/...` → `api/api.gen.go`。

- **`api/api.gen.go` 絕對不得手動編輯**，且必須提交進版控。要改行為先改 spec 再重新產生。
- **任何改動 endpoint／請求回應欄位／錯誤形狀的工作，必須在同一次改動內同步更新 spec 並重跑
  `go generate`** —— `test/codegen_test.go` 會逐位元組比對，spec 與程式碼分岔就轉紅。
- 產生器本身刻意不進 `go.mod` 的 tool 區塊（進了就得把整個產生器一起 vendor）。

### 錯誤一律走中介層

**handler 只回傳 `error`，不寫回應 body、不知道狀態碼。**

```
handler 回傳 error
  → internal/apihttp 的三個 hook（參數綁定失敗／body 解析失敗／handler 錯誤）
  → httpx.WriteError → 依 apperr.Code 對映狀態碼；抽不出 *apperr.Error 就降級 INTERNAL 500
  → httpx.EnsureJSONError（router 最外層防護網）
```

`apperr` 是 sentinel 形式，`With*` **一律回傳複本** —— sentinel 被所有請求併發共用，
就地修改會造成跨請求汙染。

`EnsureJSONError` 是第二道防線：狀態碼 >= 400 但 Content-Type 不是 JSON 時就地改寫成統一形狀。
middleware 順序固定 `RequestID → access log → EnsureJSONError → Recoverer`。

錯誤回應的 wire 形狀用 `httpx.ErrorBody`，**不是**產生的 `api.ErrorBody`。

### Migration 用 bun/migrate

- **檔名必須是 `.tx.up.sql` / `.tx.down.sql`** —— bun 只在檔名有 `.tx.` 時才開交易，
  少了它會**無聲地**失去交易保護。
- `db.NewMigrator` 是唯一建構點（CLI 與 `TestMain` 共用），已設 `WithMarkAppliedOnSuccess(true)`。
- `rollback` 只回滾**最後一個 group**，不是全部。需要乾淨資料庫的測試只能用專屬容器。

## 慣例

- **錯誤處理用 `errors.AsType[T](err)`**，不要用 `errors.As(&target)`。
- **金額一律 `decimal.Decimal`**，JSON 往返固定兩位小數字串（`StringFixed(2)`）；
  用 `String()` 會得到 `"25000.5"` 而非 `"25000.50"`，既有測試會抓到。
- **空列表回應必須是 `[]` 不是 `null`** —— nil slice 會 marshal 成 `null`，
  而且**反序列化後的斷言抓不到，要看原始 bytes**。
- 每個碰 I/O 的函式第一個參數是 `ctx context.Context`，且必須真的傳下去。
- CLI 形狀：`main()` 只有 `os.Exit(run(...))`，邏輯在 `run` 裡回傳 exit code
  （`main` 的 `os.Exit` 會跳過 defer）。錯誤寫 stderr，成功寫 stdout。
- 設定用 viper，`--config=<name>` 對應 `config/<name>.yaml`，可由 `CHUCHU_` 前綴環境變數覆寫。
  **新增設定 key 必須明確 `BindEnv`** —— `AutomaticEnv` 對「只存在於環境變數」的 key 不可靠。
- **改動 `go.mod` 後必須跑 `go mod vendor`**（含刪除相依：刪測試可能讓相依變成孤兒），
  否則 `go build ./...` 會抱怨 `modules.txt` 不同步。

## 測試佈局

- **單元測試貼著程式碼放**，不得碰 Docker，必須能在沒有 Docker 的機器上跑過。
- **整合測試一律在 `test/`（`package test`）**，打真實 HTTP 驗證，不直接呼叫內部函式。
  預設用 `startInProcessAPI`（`httptest.NewServer(app.NewHandler(...))`）**在行程內**起服務 ——
  這樣 `dlv test ./test/ -- -test.run TestXxx` 的中斷點才跟得進 handler → service → repo。
  子行程（`testsupport.StartAPI`）只留給**行程層面的行為**，目前僅 `startup_test.go` 需要。
- ⚠️ `test/` 的 `TestMain` 起**一組共用容器**。需要乾淨資料庫或要停容器的測試，
  **必須自己起專屬容器（`testsupport.StartPostgres(t)`），絕不可動共用容器** ——
  動了會讓同 package 後續測試連鎖失敗，且錯誤訊息指向無辜的測試。
- 本專案的測試**一律不用 `t.Parallel()`**。

## 註解規則

**只寫 signature 表達不出來的東西。**
判斷方式：把這段註解刪掉，讀者會不會因此做錯決定？不會 → 刪掉。

- **刪**：覆述 signature 或程式碼的、列舉常數的同義反覆、指向 task／計畫文件的施工鷹架、已經過時的。
  （`// Valid 回報 s 是否為合法的 Status 列舉值` 掛在 `func (s Status) Valid() bool` 上 —— signature 已經說完了）
- **留**：為什麼選這個做法、有什麼約束、會無聲失敗的陷阱、讀不出來的領域語意、
  「這是唯一來源，不要自己另外判斷」這類指路。

註解與產品術語一律用**繁體中文**；匯出的識別字依 Go 慣例以名稱開頭。
`api/api.gen.go` 不受本規則約束，也絕對不得手動編輯。

完整理由與取捨範例見 [knowledge/conventions/comment-only-what-signature-cannot-say.md](knowledge/conventions/comment-only-what-signature-cannot-say.md)。

## 知識庫

架構決定、踩過的坑與「為什麼是這樣」都記在 `knowledge/`，入口是
[knowledge/index.md](knowledge/index.md)。**動手前先掃一遍該檔的索引** ——
那裡記的是程式碼讀不出來的東西（決定的理由、被否決的做法、會無聲失敗的陷阱）。

介面清單**刻意不進知識庫**：`go doc ./...` 永遠正確且零維護，手抄的簽章清單只會腐爛。
