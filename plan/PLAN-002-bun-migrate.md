# PLAN-002 — 以 bun/migrate 取代手寫的 migration 機制
Created: 2026-08-20
Status: approved
Approved: 2026-08-20
Working Directory: .
BDD Spec: ./bdd/BDD-002-bun-migrate.feature
Language: Go 1.26.2（開發機 darwin/arm64；module `github.com/yongde2900/chuchu2`）
Build cmd: go build ./...
Test cmd:  go test -race ./...
Lint cmd:  go vet ./...
Pass threshold: 8.0
Max iterations: 5

## Known Context

本專案**沒有知識庫**（`./knowledge/` 不存在）。以下每一條都是規劃階段與使用者確立、且對執行者具約束力
的決定。執行者讀這份文件時不會有規劃對話的記憶，因此每一條都寫成可獨立閱讀的完整敘述。

- **⚠️ 執行順序：PLAN-002 必須等 PLAN-001 的 `Status` 變成 `done` 之後才可以開始執行。**
  撰寫本計畫時 `plan/PLAN-001-property-service-skeleton.md` 的狀態是 `in-progress`：Task 1–6 為
  `done`、Task 7 為 `in-progress`、Task 8 為 `pending` —— 理由：兩份 plan 都會改動 `test/` 目錄與
  migration 執行路徑（PLAN-001 的 Task 7 會加 `test/property_update_test.go`、Task 8 會加
  `test/contract_test.go`，而本計畫要改寫 `test/main_test.go` 的 `TestMain` 與 `test/migrate_test.go`），
  同時進行必然衝突。使用者明確選擇了「先做完 PLAN-001，再做 PLAN-002」這個順序 ——
  約束：開始執行前先確認 PLAN-001 的 header 是 `Status: done`；若不是，回報 BLOCKED 而不是硬跑。
- **這不是 greenfield，這是「置換」。** PLAN-001 的 Task 4 已經產出一套**能正常運作**的手寫 migration
  機制（`internal/migrate` 套件 + `cmd/dbmigrate` 的 `up`／`down` 子指令 + `schema_migrations` 資料表），
  本輪要把它整個換成 bun 官方的 `github.com/uptrace/bun/migrate` 套件 —— 理由：使用者希望查到的 bun
  官方文件能直接套用在這個專案上，不必先在心裡把官方詞彙翻譯成自製詞彙。
- **bun 沒有現成的 migration CLI binary 可以直接安裝。** 這件事在規劃階段已對照官方指南確認過。
  bun 提供的是 `github.com/uptrace/bun/migrate` **套件**（`migrate.NewMigrations()`、
  `Migrations.Discover(fsys)`、`migrate.NewMigrator(db, migrations)`，以及 `Migrator` 上的
  `Init` / `Migrate` / `Rollback` / `Status`（`MigrationsWithStatus`）/ `MarkApplied` / `Lock` / `Unlock`
  等方法），外加一份用 `urfave/cli` 寫的 **starter-kit 範例** `cmd/bun/main.go`，供使用者自行複製到
  專案裡改。因此 `cmd/dbmigrate/main.go` 仍然是**我們自己的檔案**；本輪改變的是它底層委派給
  `migrate.Migrator` 而不是手寫的 `internal/migrate`，並改用 bun 的指令詞彙。
- **指令詞彙全面採用 bun 的，`up` / `down` 完全廢除、不保留別名。** 本輪之後 `cmd/dbmigrate` 只認得
  `init`、`migrate`、`rollback`、`status`、`unlock`、`create_sql` 六個子指令 —— 理由：使用者刻意選擇
  「不留別名」，好讓官方文件與本專案一一對應 —— 而且「廢除」只有在**有東西去驗證它真的被拒絕**時才算數，
  所以 BDD-002 的錯誤用法 Scenario Outline 明確斷言 `up` 與 `down` 會以**非零 exit code** 被拒絕。
- **接受 bun 的 migration group 回滾語意。** bun 把「同一次 `Migrate` 呼叫中套用的所有 migration」
  視為一個 **group**（`bun_migrations.group_id`），而 `Rollback` 回滾的是**最後一個 group**，不是全部已套用
  的 migration。這與被取代的手寫 `Down`（把所有已套用的 migration 由新到舊全部回滾）不同。使用者是在
  知情的情況下刻意接受的 —— 理由：退回上一個穩定狀態，比一次把整個 schema 拆光安全 ——
  **必須一併規劃的後果：任何需要「完全乾淨的資料庫」的測試，從此不能再靠 `down` 把東西清光**，
  只能改用自己起的用完即丟容器，或 `DROP SCHEMA public CASCADE` 後重建。本計畫對每一個這類測試
  都明確指定了取得乾淨狀態的方式（見各 task）。
- **`cmd/dbmigrate` 保留標準庫 `flag`，不得引入 `urfave/cli` 或任何其他 CLI 框架。** 現有的
  `cmd/dbmigrate/main.go` 已經用 stdlib `flag` 寫得很乾淨，並把邏輯收在 `run(args []string) int` 裡讓
  exit code 可被測試；指令數量從兩個長到六個之後，這個形狀依然值得保留 —— 約束：不要因為 bun 官方範例
  用了 `urfave/cli` 就跟著引入。
- **bun 的記帳資料表是 `bun_migrations` 與 `bun_migration_locks`**（`migrate` 套件的預設名稱），
  取代手寫機制的 `schema_migrations`。與現行程式碼**不同的是**：手寫的 `Up` 會在執行時 lazily
  `CREATE TABLE IF NOT EXISTS schema_migrations`，而 bun **要求先呼叫 `Migrator.Init()`**（也就是 `init`
  子指令）建立這兩張表，否則 `Migrate`／`Status`／`Rollback` 都會因為查不到 `bun_migrations` 而失敗。
  **每一條測試佈署路徑與 `test/main_test.go` 的 `TestMain` 都必須先 `Init`。**
- **⚠️ 交易性退化風險 —— 這是本輪最危險的一個細節。** 現行的 `internal/migrate.applyMigration` 把
  **每一個** migration 都包在 `bunDB.RunInTx` 裡，因此每個 migration 都是全有或全無。**bun 對普通的
  `.up.sql` 不做這件事**：翻閱 `bun@v1.2.18/migrate/migration.go` 的 `newSQLMigrationFunc` 可確認，
  只有檔名以 `.tx.up.sql` / `.tx.down.sql` 結尾時 bun 才會 `BeginTx`，否則只是取一條 `Conn` 直接執行。
  把 `20260819120000_create_properties.up.sql` 原封不動搬過去，會**無聲地失去交易保護**。因此既有的
  那一對檔案**必須**改名為 `.tx.up.sql` / `.tx.down.sql`，而且本專案從今以後建立的每一個 migration
  都要用這個形式（`create_sql` 子指令因此必須產生 `.tx.` 檔名，見 Task 4）。BDD-002 有一個 scenario
  就是專門用來抓這件事的。
- **⚠️ 失敗的 migration 預設會留下記錄 —— 必須用 `WithMarkAppliedOnSuccess(true)` 關掉。**
  bun 的 `Migrator.Migrate` 預設是**先** `MarkApplied`（寫入 `bun_migrations`）**再**執行 migration 的
  SQL，而且那次寫入不在 migration 自己的交易裡。也就是說：預設設定下，一個中途失敗的 migration
  會在 `bun_migrations` 留下一筆紀錄。BDD-002 的交易性 scenario 斷言「`bun_migrations` 中不存在該
  migration 的成功紀錄」，因此建立 Migrator 時**必須**傳入 `migrate.WithMarkAppliedOnSuccess(true)`，
  讓記帳改成在 migration 成功之後才寫。這個選項必須由 CLI 與 `TestMain` **共用同一個建構函式**設定，
  否則兩邊行為會悄悄分岔（見 Task 1 的 `db.NewMigrator`）。
  **已知且接受的差異：** 即使開了這個選項，DDL 的交易與記帳的 INSERT 仍是兩個交易（bun 的設計如此），
  不像手寫機制那樣把 SQL 與版本紀錄放在同一個交易裡。這是換用官方套件的代價，使用者接受。
- **`--bun:split` 這次不需要加，已對照原始碼確認過。** bun 會把 `.sql` 檔案依 `--bun:split` 這行指示詞
  切成多段、逐段 `ExecContext`；**沒有**這行指示詞時整個檔案就是**一段**，以單次 `ExecContext` 送出 ——
  這與被取代的手寫機制（整個檔案一次 `ExecContext`）完全相同，而現行的
  `20260819120000_create_properties.up.sql`（`CREATE TABLE` + `CREATE UNIQUE INDEX` 兩個敘述）本來就
  依賴 pgdriver 的 simple query protocol 接受多敘述批次，Task 4 已驗證過可行。**所以本輪只改檔名，
  不得改動 SQL 內容，也不得加入 `--bun:split`。**（測試專用的臨時 migration 是例外，見 Task 2 ——
  那裡刻意**要**用 `--bun:split`，理由寫在該 task 內。）
- **既有檔名已經符合 bun 的 `<14 位時間戳>_<name>` 慣例。** bun 以正規表示式
  `^(\d{1,14})_([0-9a-z_\-]+)\.` 解析檔名，`20260819120000_create_properties.tx.up.sql` 會被解析成
  `Name = "20260819120000"`、`Comment = "create_properties"`。因此**不需要**重新編時間戳，只需要加上
  `.tx.` 這一段。
- **⚠️ `bun_migrations` 資料列的 `name` 欄位只有時間戳，沒有 migration 名稱。** `Migration` 結構上的
  `Comment` 欄位標了 `bun:"-"`，**不會寫進資料庫**。所以「`bun_migrations` 中存在一筆對應
  `create_properties` 的紀錄」這條斷言，實際要查的是 `name = '20260819120000'`；寫成
  `WHERE name LIKE '%create_properties%'` 會永遠查不到而讓測試假失敗。反過來說，`status` 子指令的輸出
  **會**包含 `create_properties`，因為 `Migration.String()` 是 `name_comment`，而 comment 來自
  `Discover` 當下解析出的檔名（記憶體中），不是來自資料庫。
- **PLAN-001 的 `## Project Conventions` 中「migration 支援 `up` 與 `down` 兩個子指令」那一條，
  自本輪起失效。** 那份文件不由本計畫修改（plan 檔案的修改權在 `hars-revise`），但執行者不得把 `up`／
  `down` 當成應該保留的慣例「順手補回來」。除了這一條之外，PLAN-001 的所有慣例（金錢用 decimal、
  分層邊界、手寫建構子注入、繁體中文註解、錯誤處理與 exit code 形狀、兩層測試佈局）**全部原封繼承**，
  相關部分已抄錄在下方 `## Project Conventions`。
- **本輪完全不碰領域層、API 層與 HTTP 層。** `internal/property`、`internal/property/pgrepo`、
  `internal/property/httpapi`、`internal/server`、`internal/health`、`cmd/api`、`api/openapi.yaml`
  都不應該有任何改動。
- **既有呼叫端只有兩處，已用 `grep -rn "internal/migrate" .` 確認：**
  `cmd/dbmigrate/main.go`（`migrate.Up` / `migrate.Down`）與 `test/main_test.go`（`TestMain` 的
  `migrateSharedPostgres` 呼叫 `migrate.Up`）。此外 `db/embed.go` 的註解提到 `internal/migrate`，
  屬於文字，需一併更新。`schema_migrations` 這個字串除了 `internal/migrate` 自己以外沒有別處使用。

### Out of Scope & Ungraded Constraints

以下每一條都是**決定**，不是遺漏。沒有讀到這一段的執行者會「很有幫助地」把它們做出來，那是錯的。

- **不得改動任何 migration 的 SQL 內容。** 欄位、型別、`NUMERIC` 精度、三個 CHECK 約束、唯一索引
  `properties_address_key` 一律維持現狀；本輪只換執行機制與檔名（加上 `.tx.` 這一段）。
- **不得新增任何業務用的 migration。** 本輪唯一會出現的新 `.sql` 檔案，是測試在執行期間寫進 `db/`
  再刪掉的臨時檔案，以及 `create_sql` scenario 產生後隨即清掉的那一對空白檔案。
- **不得使用 bun 的 Go migration**（`CreateGoMigration` / `create_go` / `Migrations.MustRegister`）。
  本專案的 migration 一律是 SQL 檔案；Go migration 留待日後真的需要在 migration 裡跑 Go 邏輯時再說。
- **不實作 `mark_applied` 子指令。** 它的用途是把「既有正式資料庫」上已經手動套用過的 migration 標記為
  已套用，而本專案目前沒有任何長存的資料庫 —— 開發與測試都是用完即丟的 testcontainers，沒有這個需求。
- **不得改動 PLAN-001 其他 task 產出的任何行為。** API、領域模型、健康檢查、OpenAPI 契約一律不動。
- **不做 migration 的 CI 檢查**（例如「每個 `.tx.up.sql` 都必須有對應的 `.tx.down.sql`」這種靜態檢驗）。
- **未評分約束（UNGRADED）：** 程式碼註解與產品術語一律使用**繁體中文**，與 PLAN-001 及使用者其他 repo
  的慣例一致。沒有任何 scenario 會檢查這件事，由人工把關。
- **未評分約束（UNGRADED）：** 本輪結束後 `internal/migrate` 這個套件**必須完全不存在** ——
  `migrate.go`、`parse.go`、`parse_test.go` 三個檔案連同目錄一起刪除，而不是留著沒人用、或改名保留。
  沒有任何 scenario 會檢查殘留，由人工確認。
- **未評分約束（UNGRADED）：** `cmd/dbmigrate` 必須繼續使用標準庫 `flag` 解析參數，不得引入
  `urfave/cli` 或任何其他 CLI 框架。用哪個套件解析參數在行為上分不出來，harness 抓不到，
  由人工檢查 import 區塊把關。
- **未評分約束（UNGRADED）：** 必須在 `bdd/BDD-001-property-service-skeleton.feature` 中，把第 85 行
  附近的 scenario「Migration 可正向套用亦可回滾」標記為 superseded，並指向 BDD-002（建議作法：在該
  scenario 上方加一段以 `#` 開頭的註解說明它已被 BDD-002 取代，並保留原文以維持歷史可讀性；
  **不要**刪除整段）。這是文件維護動作，harness 不會驗證，由人工確認。

## Project Conventions

以下慣例自 PLAN-001 原封繼承，抄錄於此讓本文件可獨立閱讀（執行者只會讀到這一份）。

- **技術棧（本輪相關部分）：** bun ORM over Postgres（`pgdriver` + `pgdialect`）、
  `github.com/uptrace/bun/migrate`（**本輪新用到的子套件，`go.mod` 已有 `github.com/uptrace/bun v1.2.18`，
  不需要新增相依**）、viper 設定、testcontainers-go 供測試使用。
- **設定：** 以 viper 載入，由 `--config=<name>` flag 選擇 `config/<name>.yaml`。設定值可由 `CHUCHU_`
  前綴的環境變數覆寫（`.` 換成 `_`），例如 `CHUCHU_POSTGRES_DSN` 覆寫 `postgres.dsn` —— 測試就是靠這個
  把 CLI 指向 testcontainers 隨機產生的 DSN，不需要改動已提交的 `config/test.yaml`。
- **CLI 形狀：** `main()` 只有 `os.Exit(run(os.Args[1:]))` 一行；實際邏輯放在 `run(args []string) int`，
  回傳 process exit code，讓退出碼可被測試、也讓 `defer` 一定會執行（`os.Exit` 會跳過 defer，
  所以清理動作必須寫在 `run` 裡）。錯誤訊息一律寫到 **stderr**，成功訊息寫到 **stdout**。
- **錯誤處理：** 本專案使用 Go 1.26，**優先使用 `errors.AsType[T](err)`**，不要用 `errors.As(&target)`。
  應用層錯誤型別 `internal/apperr` 本輪用不到（CLI 不走 HTTP 錯誤映射）。
- **Cancellation：** 每一個會碰 I/O 的函式第一個參數都是 `ctx context.Context`，而且必須真的傳下去。
  `cmd/dbmigrate` 已經用 `signal.NotifyContext` 綁 SIGINT/SIGTERM，保留這個作法。
- **註解與產品術語使用繁體中文。**
- **測試佈局（兩層，每個 task 都必須遵守）：**
  - **單元測試貼著程式碼放**，檔名 `<被測檔>_test.go`，與被測套件同目錄。這一層**不得碰 Docker、
    不得起容器、不得連任何外部服務**，必須在沒有 Docker 的機器上也能跑過。
  - **整合測試一律放在 repo 根目錄的 `test/` 目錄，`package test`**，一個 feature 面向一個檔。
    全部驗收證據都在這一層，透過真實 HTTP 請求或**真實跑起來的 binary**（`go run ./cmd/dbmigrate ...`）
    驗證，不直接呼叫內部函式。本輪 BDD 的每一個 Then 都是對 CLI 的 exit code / stdout / stderr /
    資料庫實際狀態做斷言。
  - **⚠️ 共用容器的危害（本輪特別容易踩到）：** `test/` 用一個 `TestMain` 啟動**一組共用的**
    Postgres 與 Redis 容器供整個 package 使用，並在其上跑一次 migration，讓需要 `properties` 資料表的
    測試有表可用。**任何需要「停掉容器」或「乾淨資料庫」的測試，都必須自己起一組用完即丟的專屬容器
    （`testsupport.StartPostgres(t)`），絕對不可以動 `TestMain` 的共用容器** —— 動了會讓同 package
    中其後所有測試連鎖失敗，而且失敗訊息會指向無辜的測試，極難除錯。
    本輪滿滿都是需要乾淨資料庫的測試，**每一個 task 都已明確指定它怎麼取得乾淨狀態**，照著做。
  - 目前 `test/` 底下沒有任何測試呼叫 `t.Parallel()`（已確認），本輪新增的測試**也不得呼叫**——
    理由見 Task 2 關於臨時 migration 檔案的說明。
  - `-count=1` 是必要的：Go 會快取測試結果，容器背後的測試從快取回綠會騙人。
    header 的 `Test cmd` 保持 `go test -race ./...`（涵蓋兩層），但驗證整合層時請用
    `go test -race -count=1 ./test/...`。
- **`db/` 是一個 Go package（`package db`）**，裡面除了 `.sql` 還有 `embed.go`。不要嘗試在別的套件裡寫
  `//go:embed ../../db/*.sql` —— `go:embed` 的 pattern 不允許 `..`，那樣寫編譯不會過。所有需要讀
  migration SQL 的程式碼，都必須 import `db` 這個 package 使用它匯出的 `FS`。
- **`golangci-lint` 雖然裝在開發機上，但 Lint gate 刻意只有 `go vet ./...`** —— 不要把 golangci-lint
  變成 gate，也不要為它新增 `.golangci.yml`。

## Overview

本輪把 PLAN-001 Task 4 產出的手寫 migration 機制，整套換成 bun 官方的 `bun/migrate` 套件：刪掉
`internal/migrate`，把 `cmd/dbmigrate` 改成 `migrate.Migrator` 的薄殼（仍用標準庫 `flag`），指令詞彙
改成 bun 的 `init` / `migrate` / `rollback` / `status` / `unlock` / `create_sql`，記帳資料表從
`schema_migrations` 換成 `bun_migrations` 與 `bun_migration_locks`，並把既有那一對 migration 檔案改名為
`.tx.up.sql` / `.tx.down.sql` 以保住原本就有的交易性。SQL 內容一個字都不改。

**任務切分刻意讓每一個 task 邊界都是可編譯、測試有意義的狀態。** 這一輪是「置換」而不是「新增」，
最大的風險是出現一段「舊機制已刪、新機制未成」的空窗期，所以 **Task 1 是一次原子性的置換**：它同時
完成改名、接上 bun Migrator、刪除 `internal/migrate`、修好兩個既有呼叫端（`cmd/dbmigrate/main.go` 與
`test/main_test.go` 的 `TestMain`）、以及改寫 `test/migrate_test.go`。Task 1 結束時 `go build ./...`、
`go vet ./...`、`go test -race -count=1 ./...` 必須全綠，PLAN-001 的所有既有測試也必須維持綠。
**本計畫不存在任何預期中的中途破壞窗口**；如果執行 Task 1 的過程中出現暫時性的編譯錯誤，那是同一個
task 內部的中間狀態，不得在 task 結束時遺留。Task 2–4 之後只**往指令分派表加分支、往 `test/` 加檔案**，
不再動已完成的部分：Task 2 補交易性與 schema 回歸的證明並提供臨時 migration 的注入工具，Task 3 加
`rollback` 子指令與 group 語意的驗證，Task 4 加 `unlock` 與 `create_sql` 兩個開發者工具指令。
Task 1 之後、Task 3 之前，CLI 確實沒有任何「反向」指令可用 —— 這不影響任何既有測試（改寫後的
`test/migrate_test.go` 與 `TestMain` 都不需要回滾），是刻意接受的階段性狀態，由 Task 3 補上。

**測試專用 migration 的供應方式（本輪必須明確決定的一件事）：** BDD 有兩個 scenario 需要「一個只在測試
中存在的額外 migration」—— rollback 的 group 語意需要第二個 migration 形成第二個 group，交易性測試需要
一個會中途失敗的 migration。由於這些 scenario 都要求透過**真實的 `go run ./cmd/dbmigrate`** 驅動，而
CLI 讀的是 `db` package 的 `embed.FS`（編譯期決定，無法從外部注入），唯一可行的作法是：
**由測試在執行期間把臨時 migration 檔案寫進 `db/` 目錄，再讓 `go run` 重新編譯時把它嵌進去，測試結束
以 `t.Cleanup` 刪除。** 這個作法之所以成立，關鍵在於 `go run` 是在測試執行**當下**編譯的，而已經跑起來
的測試 binary 自己的 `db.FS` 不含這些檔案 —— 因此 `TestMain` 的共用容器完全不受影響。代價是這些檔案
在磁碟上存在的期間是全域可見的，所以本輪所有測試一律**不得 `t.Parallel()`**，且清理必須用 `t.Cleanup`
註冊在建立之前，讓測試失敗時也一定會清掉。這個工具由 Task 2 提供，Task 3 沿用。

**Pass threshold 的取捨：** header 預設刻意調高到 **8.0**（而非通用預設 7.0），因為本輪從頭到尾都是
migration —— 這正是「細微缺陷代價高、而且很晚才會被發現」的典型類別：丟掉一個交易邊界、悄悄改掉一個
欄位型別、或回滾範圍比預期大，都是要等到正式環境才會爆的錯，而且全都能在一個「綠色」的測試套件底下
藏得好好的。另外針對兩個最貴的 task 再調高：**Task 1（8.5）** 是不可逆的置換，一次做掉改名、接線、
刪除與兩個呼叫端修正，任何一半做完的狀態都會拖累後面三個 task；**Task 2（8.5）** 是交易性與 schema
等價性的唯一防線，而且是最容易「測試綠了但其實沒測到東西」的一個 —— 一個沒有 `--bun:split` 的失敗
探針測試會因為 Postgres 隱式批次交易而通過，完全測不到 `.tx.` 檔名的作用。**Task 4（7.5）** 反向調低
一點：`create_sql` 與 `unlock` 的爆炸半徑小、行為單純，但仍高於通用預設，因為它會在 repo 裡寫檔案，
清理不乾淨會污染後續每一次執行。
**Max iterations 維持預設 5。** PLAN-001 當時設 6，唯一理由是冷機器第一次要拉 testcontainers 的 image、
bring-up 需要一兩輪試誤；那些 image（`postgres:16-alpine`、`redis:7-alpine`）現在已經是熱的，
`testsupport.StartPostgres` 也已經是驗證過的既有程式碼，這個理由不再成立，因此回到預設值。

## Sub-Tasks

### Task 1: 以 bun Migrator 置換手寫機制 —— 改名、接線、刪除、指令詞彙
Status: pending
Directory: db, cmd/dbmigrate, internal/migrate, test
Depends on: none（本 task 之前的所有相依都是 PLAN-001 已完成的既有程式碼）
Pass threshold: 8.5
Provides (public interface):
```go
package db // db/migrations.go —— 與 migration SQL 同一個 package，理由見 Project Conventions

// Migrations 以 bun 的 Discover 掃描本 package 內嵌的 FS，取得所有
// <timestamp>_<name>.tx.up.sql / .tx.down.sql 組成的 migration 集合。
//
// ⚠️ 兩個選項掛在「不同」的建構子上，型別不同、不可互換：
//   migrate.WithMigrationsDirectory("db") 的型別是 MigrationsOption，
//   只能傳給 migrate.NewMigrations(...)。傳給 NewMigrator 編譯不會過。
func Migrations() (*migrate.Migrations, error)
//   實作形狀：
//     ms := migrate.NewMigrations(migrate.WithMigrationsDirectory("db"))
//     if err := ms.Discover(FS); err != nil { return nil, err }
//     return ms, nil

// NewMigrator 建立本專案唯一的 Migrator 建構點。CLI 與測試都必須走這裡，
// 否則兩邊的選項會悄悄分岔。
//
// ⚠️ migrate.WithMarkAppliedOnSuccess(true) 的型別是 MigratorOption，
//   只能傳給 migrate.NewMigrator(...)。它讓失敗的 migration 不留下紀錄。
func NewMigrator(bunDB *bun.DB) (*migrate.Migrator, error)
//   實作形狀：
//     ms, err := Migrations()
//     if err != nil { return nil, err }
//     return migrate.NewMigrator(bunDB, ms, migrate.WithMarkAppliedOnSuccess(true)), nil
//
// 已對照 bun@v1.2.18 原始碼確認：
//   migrate/migrations.go:15  type MigrationsOption func(m *Migrations)
//   migrate/migrations.go:18  func WithMigrationsDirectory(directory string) MigrationsOption
//   migrate/migrations.go:33  func NewMigrations(opts ...MigrationsOption) *Migrations
//   migrate/migrator.go:23    type MigratorOption func(m *Migrator)
//   migrate/migrator.go:41    func WithMarkAppliedOnSuccess(enabled bool) MigratorOption
//   migrate/migrator.go:96    func NewMigrator(db *bun.DB, migrations *Migrations, opts ...MigratorOption) *Migrator

// 上述 migrate 為 "github.com/uptrace/bun/migrate"（bun v1.2.18，go.mod 中已有）。

package main // cmd/dbmigrate/main.go

// run 依 args[0] 分派子指令，回傳 process exit code。
// 本 task 實作 init / migrate / status 三個分支；rollback 由 Task 3 加入，
// unlock 與 create_sql 由 Task 4 加入。未知的子指令（含已廢除的 up / down）
// 一律以非零 exit code 拒絕。
func run(args []string) int

package test // test/migrate_test.go —— 供 Task 2/3/4 的測試共用的驅動輔助

// runDBMigrate 以 `go run ./cmd/dbmigrate <args...>` 執行 CLI，工作目錄為 repo 根目錄。
// dsn 非空時以 CHUCHU_POSTGRES_DSN 注入；dsn 為空時不注入（用於不需要資料庫的情境）。
// 不論成功失敗都回傳，由呼叫端自行斷言 exit code —— 因此可用於錯誤情境。
func runDBMigrate(t *testing.T, dsn string, args ...string) (exitCode int, stdout, stderr string)
```
實作要求：

- **改名（不改內容）：** `db/20260819120000_create_properties.up.sql` →
  `.tx.up.sql`，`.down.sql` → `.tx.down.sql`。**SQL 內容一個字元都不得更動**，不要加 `--bun:split`。
  用 `git mv` 保留歷史。同步更新 `db/embed.go` 的註解（目前提到 `internal/migrate`，該套件即將不存在）。
  `//go:embed *.sql` 這個 pattern 不需要改，`.tx.up.sql` 仍然符合。
- **刪除 `internal/migrate` 整個目錄**（`migrate.go`、`parse.go`、`parse_test.go`）。
- **`cmd/dbmigrate/main.go` 改寫：** 保留現有的整體形狀（`main` 只呼叫 `run`、stdlib `flag`、
  `signal.NotifyContext`、`postgres.Open`、錯誤寫 stderr）。差異：
  - 子指令分派改成 `init` / `migrate` / `status`（其餘由 Task 3/4 補），未知子指令走 default 分支，
    **錯誤訊息中必須原樣帶出使用者輸入的那個子指令字串**（例如 `未知的子指令 "up"`），
    因為 BDD 斷言 stderr 含有 `"up"` / `"down"` / `"frobnicate"` 這些片段。
  - **完全沒有參數時**（`args` 為空）印出用法說明並回傳非零；用法說明**必須包含「用法」兩個字**，
    並列出六個子指令（含尚未實作的 `rollback` / `unlock` / `create_sql` 也可以列，或於 Task 3/4 補上，
    兩者皆可，但「用法」二字與非零 exit code 是硬性要求）。
  - `--config` **不給預設值**：未指定時印出含有 `config` 字樣的錯誤訊息到 stderr 並回傳非零
    （BDD 的 `migrate`（無 `--config`）這一列斷言 stderr 含 `"config"`）。
  - `init` → `Migrator.Init(ctx)`；`status` → `Migrator.MigrationsWithStatus(ctx)` 並把結果印到
    **stdout**（`MigrationSlice` 的 `String()` 會產出 `20260819120000_create_properties` 這種字樣，
    BDD 斷言 stdout 含 `create_properties`；建議同時印出已套用／未套用的區分，例如
    `migrations: <sorted>` 與 `unapplied migrations: <unapplied>` 兩行）。
  - `migrate` → 先 `Migrator.Lock(ctx)`、`defer Migrator.Unlock(ctx)`，再 `Migrator.Migrate(ctx)`。
    回傳的 `*MigrationGroup` 若 `IsZero()`，印出一行**包含字串 `no new migrations`** 的訊息到 stdout
    （建議：`there are no new migrations to run (database is up to date)`）；否則印出套用了哪個 group。
    **`Unlock` 必須用 `defer` 寫在 `run` 裡**，這樣即使 migration 失敗、`run` 回傳 1，鎖也會被釋放
    （`main` 的 `os.Exit` 會跳過 defer，但 `run` 的 defer 在 `run` 回傳時就跑完了）。
- **`test/main_test.go` 的 `TestMain` 修正：** `migrateSharedPostgres` 目前呼叫 `migrate.Up`，
  改成用 `db.NewMigrator(bunDB)` 取得 Migrator，**先 `Init(ctx)` 再 `Migrate(ctx)`**。
  少了 `Init` 會讓整個 `test` package 在第一步就爆掉。
- **`test/migrate_test.go` 改寫：** 刪掉原本驗證 `up` → `down` 的 `TestMigrate_UpCreatesPropertiesTable_DownDropsIt`
  （該 scenario 已被 BDD-002 取代），改放本 task 的四個 scenario 加一個 Outline，並提供上面列出的
  `runDBMigrate` 輔助函式（Task 2/3/4 會直接沿用它，不要各自再寫一份）。
  既有的 `propertiesTableExists` 這類輔助可保留沿用。
- **每一個需要資料庫的 scenario 都用 `testsupport.StartPostgres(t)` 起自己的專屬容器**，
  包含「重複執行 migrate」那一條（它自己先 `init` + `migrate` 建立前提，不要借用共用容器的既有狀態）。
  **不得使用 `TestMain` 的共用容器，也不得對它下任何 DDL。**
- **`bun_migrations` 的斷言要查 `name = '20260819120000'`**，不要查 `create_properties`（見 Known Context）。
- 錯誤用法 Outline 的五列中，只有需要連資料庫的才需要容器 —— 實際上**五列都不需要**（都在連線之前就
  被拒絕），呼叫 `runDBMigrate(t, "", ...)` 即可，不要為它們起容器。
  注意 `go run` 在子程式非零退出時會另外印一行 `exit status N` 到 stderr，這不影響任何斷言；
  斷言只要求 exit code **非零**，不要斷言等於 1。
- **未評分但必做：** 在 `bdd/BDD-001-property-service-skeleton.feature` 中把 scenario
  「Migration 可正向套用亦可回滾」標記為 superseded 並指向 BDD-002（見 Out of Scope 的最後一條）。
Expected Goals (from BDD scenarios):
- [ ] Scenario: init 建立 bun 的 migration 記錄資料表
- [ ] Scenario: migrate 套用所有尚未套用的 migration
- [ ] Scenario: 重複執行 migrate 不會出錯也不會重複套用
- [ ] Scenario: status 同時列出已套用與待套用的 migration
- [ ] Scenario Outline: 錯誤的用法會以非零 exit code 拒絕並說明原因

### Task 2: 交易性與 schema 等價性的證明，以及臨時 migration 注入工具
Status: pending
Directory: internal/testsupport, test
Depends on: Task 1（`db.NewMigrator`、`cmd/dbmigrate` 的 `init` / `migrate` 分支、
  `test` package 的 `runDBMigrate` 輔助、以及已改名為 `.tx.` 的 migration 檔案）
Pass threshold: 8.5
Provides (public interface):
```go
package testsupport // internal/testsupport/migrations.go

// WriteTempMigration 在 repo 的 db/ 目錄寫入一對「只在本次測試存在」的 migration 檔案：
//   db/<version>_<name>.tx.up.sql   內容為 upSQL
//   db/<version>_<name>.tx.down.sql 內容為 downSQL
// 並以 t.Cleanup 註冊刪除（註冊必須發生在寫入之前，確保測試失敗時也會清掉）。
//
// version 必須是 14 位數字且排序在既有 migration 之後；name 必須符合 bun 的
// [0-9a-z_-]+ 規則。呼叫端自行決定這兩個值，讓測試完全可預期。
//
// 之所以能生效：`go run ./cmd/dbmigrate` 是在測試執行當下才編譯的，會把這對檔案
// 嵌進子行程的 db.FS；而已經跑起來的測試 binary 自己的 db.FS 不含它們，
// 因此 TestMain 的共用容器不受影響。也因為檔案在磁碟上是全域可見的，
// 使用本函式的測試一律不得 t.Parallel()。
func WriteTempMigration(t *testing.T, version, name, upSQL, downSQL string) (upPath, downPath string)
```
實作要求：

- **交易性 scenario 的探針 migration 必須使用 `--bun:split`。** 這是本 task 最關鍵的一點：
  探針的 `.tx.up.sql` 內容應為「`CREATE TABLE tx_probe (...)`」→ 單獨一行 `--bun:split` →
  「一段必定失敗的 SQL」（建議 `INSERT INTO no_such_table VALUES (1);`，會以 SQLSTATE 42P01 失敗）。
  **理由：** 沒有 `--bun:split` 的話，兩個敘述會被當成單一批次送出，Postgres 的 simple query protocol
  會自動把整批包成隱式交易 —— 於是就算檔名不是 `.tx.`、bun 沒有開交易，`tx_probe` 一樣不會留下來，
  **測試會因為錯誤的理由通過，完全測不到 `.tx.` 的作用**。加了 `--bun:split` 之後兩段是兩次獨立的
  `ExecContext`，只有 bun 因為 `.tx.` 檔名開的那個顯式交易能救得了它。
  `--bun:split` 必須自成一行、前後沒有多餘空白，否則 bun 會回報 `unknown directive`。
  探針的 `.tx.down.sql` 寫 `DROP TABLE IF EXISTS tx_probe;` 即可（本 scenario 不會用到它，
  但成對存在是本專案的慣例）。
- 建議的 version 取 `29990101000001` 這種「明顯是測試產物、且必定排序在 `20260819120000` 之後」的值，
  而不是 `time.Now()` —— 可預期、不會因為執行日期而改變行為。
- **交易性 scenario 的流程：** 起專屬容器 → `runDBMigrate(t, dsn, "init", "--config=test")` →
  `WriteTempMigration(...)` 寫入探針 → `runDBMigrate(t, dsn, "migrate", "--config=test")` →
  斷言 exit code 非零、`to_regclass('public.tx_probe') IS NULL`、
  且 `bun_migrations` 中沒有 `name = '29990101000001'` 的資料列。
  注意這次 `migrate` 會**先**成功套用 `create_properties`（時間戳較早）再在探針上失敗，
  所以 `properties` 資料表會存在、`bun_migrations` 會有 `20260819120000` 那一列，這是正確行為。
- **schema 回歸 scenario 的流程：** 起專屬容器 → `init` → `migrate` → 直接查
  `information_schema.columns`（`table_schema = 'public' AND table_name = 'properties'`）逐欄比對
  BDD 表格中的型別與可空性，再查 `pg_indexes` / `pg_index` 確認存在一個涵蓋
  `(city, district, street_address, floor, room_no)` 的**唯一**索引。
  **注意 Postgres 的 `data_type` 用詞與 BDD 表格的簡寫不同：** `timestamptz` 在
  `information_schema` 中是 `timestamp with time zone`、`INT` 是 `integer`、
  `NUMERIC(12,2)` 是 `numeric`、`UUID` 是 `uuid`、`TEXT` 是 `text`；可空性是 `is_nullable = 'NO'`。
  斷言必須逐欄進行並在失敗時指出是哪一欄，不要只比對欄位數量。
  索引名稱應仍為 `properties_address_key`（PLAN-001 Task 5 的重複建檔偵測依賴它），
  一併斷言會更有價值。
- scenario 的最後一條 Then「PLAN-001 既有的整合測試在不修改斷言的前提下全部通過」，
  由 `go test -race -count=1 ./test/...` 全綠來證明 —— 執行者**不得為了讓測試變綠而修改 PLAN-001
  留下的任何測試檔案的斷言**（`test/health_test.go`、`test/startup_test.go`、`test/panic_test.go`、
  `test/property_*_test.go`、以及 Task 7/8 產出的檔案一律不動）。唯一允許被改寫的既有測試檔案是
  `test/main_test.go` 與 `test/migrate_test.go`，而那是 Task 1 做的事。
- 兩個 scenario 各自起專屬容器（`testsupport.StartPostgres(t)`），**不得動共用容器**。
- 建議檔案佈局：`test/migrate_tx_test.go`（交易性）與 `test/migrate_schema_test.go`（schema 回歸）。
Expected Goals (from BDD scenarios):
- [ ] Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema
- [ ] Scenario: 換機制後 properties 資料表的結構與換之前完全相同

### Task 3: rollback 子指令與 migration group 語意
Status: pending
Directory: cmd/dbmigrate, test
Depends on: Task 1（`run` 的指令分派、`db.NewMigrator`、`runDBMigrate`）、
  Task 2（`testsupport.WriteTempMigration` —— 第二個 group 需要一個測試專用的 migration）
Pass threshold: 8.0
Provides (public interface):
```go
package main // cmd/dbmigrate/main.go

// 本 task 在 run 的指令分派表加入 rollback 分支（不改變 run 的簽章）：
//   rollback → Migrator.Lock / defer Unlock / Migrator.Rollback(ctx)
// 回傳的 *MigrationGroup 為 IsZero() 時，印出一行包含字串 "nothing to rollback"
// 的訊息到 stdout 並回傳 exit code 0（安全的無動作，不是錯誤）。
```
實作要求：

- `rollback` 的實作與 `migrate` 對稱：`Lock` → `defer Unlock` → `Rollback(ctx)` →
  依 `group.IsZero()` 決定印哪一句。**「沒有可回滾的 group」不是錯誤，exit code 必須是 0**，
  訊息必須包含 `nothing to rollback`（建議整句 `there is nothing to rollback`）。
- 若用法說明在 Task 1 尚未列出 `rollback`，本 task 補上。
- **group scenario 的流程（順序是這個 scenario 的全部重點，不可調換）：**
  1. `testsupport.StartPostgres(t)` 起專屬容器；
  2. `runDBMigrate(t, dsn, "init", "--config=test")`；
  3. `runDBMigrate(t, dsn, "migrate", "--config=test")` —— 此時磁碟上**只有** `create_properties`，
     所以它獨自形成 **group 1**；
  4. `testsupport.WriteTempMigration(t, "29990101000002", "rollback_probe", ...)` 寫入第二個 migration
     （up 建立一張獨立的資料表，例如 `CREATE TABLE rollback_probe (id int);`；
     down 為 `DROP TABLE IF EXISTS rollback_probe;`）；
  5. 再次 `runDBMigrate(t, dsn, "migrate", "--config=test")` —— `go run` 會重新編譯並嵌入新檔案，
     只有這一個 migration 是未套用的，因此形成 **group 2**；
  6. `runDBMigrate(t, dsn, "rollback", "--config=test")`；
  7. 斷言 exit code 為 0、`rollback_probe` 資料表**不存在**、`properties` 資料表**仍存在**、
     且 `bun_migrations` 中仍有 `name = '20260819120000'` 這一列。
  **第 3 步與第 5 步之間必須是兩次獨立的 CLI 執行**，這正是「兩個 group」的來源；
  把兩個 migration 一次套用會變成同一個 group，rollback 會把兩個都拆掉，scenario 就測不到重點了。
  第 4 步的臨時檔案在寫入前 `WriteTempMigration` 已註冊 `t.Cleanup` 刪除，不需要額外清理。
- **「沒有已套用的 group」scenario 的流程：** 起專屬容器 → `init` → 直接 `rollback` →
  斷言 exit code 為 0 且 stdout 含 `nothing to rollback`。
  注意此時資料庫已 `init` 但沒有任何已套用的 migration；`Migrator.Rollback` 內部會先呼叫
  `validate()`，磁碟上至少有 `create_properties` 這個 migration 存在，所以不會撞到
  「there are no migrations」這個錯誤 —— 但如果實作把 `rollback` 寫成需要先有已套用紀錄才動作，
  請確認走的是 bun 的 `Rollback` 而不是自製的前置檢查。
- 兩個 scenario 各自起專屬容器，**不得動共用容器**。建議檔案：`test/migrate_rollback_test.go`。
- 這兩個測試都不得呼叫 `t.Parallel()`（第一個會在 `db/` 留下短暫存在的檔案）。
Expected Goals (from BDD scenarios):
- [ ] Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響
- [ ] Scenario: 沒有已套用的 group 時 rollback 安全地無動作

### Task 4: create_sql 與 unlock —— 開發者工具指令
Status: pending
Directory: cmd/dbmigrate, test
Depends on: Task 1（`run` 的指令分派、`db.NewMigrator` 的 `WithMigrationsDirectory("db")` 選項、
  `runDBMigrate`）
Pass threshold: 7.5
Provides (public interface):
```go
package main // cmd/dbmigrate/main.go

// 本 task 在 run 的指令分派表加入最後兩個分支（不改變 run 的簽章）：
//   create_sql <name> --config=<name>
//       → Migrator.CreateTxSQLMigrations(ctx, name)
//         ⚠️ 必須用 CreateTxSQLMigrations（產生 .tx.up.sql / .tx.down.sql），
//            不可用 CreateSQLMigrations（產生不具交易性的 .up.sql / .down.sql）。
//   unlock --config=<name>
//       → Migrator.Unlock(ctx)
```
實作要求：

- **`create_sql` 必須產生 `.tx.` 檔名。** bun 有兩個很像的方法：`CreateSQLMigrations` 產生
  `.up.sql` / `.down.sql`（**不具交易性**），`CreateTxSQLMigrations` 產生 `.tx.up.sql` / `.tx.down.sql`。
  本專案的所有 migration 都必須是交易式的（見 Known Context 的交易性風險那一條），
  所以 `create_sql` 這個**指令名稱**雖然沿用 bun 的詞彙，底層走的是 `CreateTxSQLMigrations`。
  BDD 明確斷言產生的兩個檔名以 `_add_tenant_table.tx.up.sql` 與 `_add_tenant_table.tx.down.sql` 結尾。
- **檔案寫到哪裡：** bun 的 `createSQL` 寫進 `Migrations` 的 directory。Task 1 的 `db.Migrations()`
  已固定傳入 `migrate.WithMigrationsDirectory("db")`（注意是掛在 `NewMigrations` 上，不是
  `NewMigrator` 上 —— 型別分別是 `MigrationsOption` 與 `MigratorOption`），那是相對於行程工作目錄的路徑，而 CLI 一律
  從 repo 根目錄執行（測試也用 `cmd.Dir = testsupport.RepoRoot(t)`），因此會正確落在 `db/`。
  **不要**依賴 bun 在沒有明確設定時用 `runtime.Callers` 推出來的 implicit directory，那個值取決於
  編譯機器上的原始碼路徑，脆弱且不可預期。
- `create_sql` 的 migration 名稱由 `args[1]` 提供（例如 `create_sql add_tenant_table --config=test`）。
  名稱缺漏時以非零 exit code 拒絕並說明。bun 會自行加上 14 位 UTC 時間戳前綴，
  且**同一次呼叫產生的兩個檔案共用同一個時間戳**（BDD 斷言這一點）。
  `create_sql` **不需要連上資料庫**就能完成 —— `postgres.Open` 只建立連線池不會實際連線，
  所以這個 scenario 不需要起任何容器，測試也不應該為它起容器。
- **⚠️ `create_sql` 的測試必須清理產生的檔案，這是硬性要求。** 留下來的檔案內容是 bun 的樣板
  （`SET statement_timeout = 0;` + `SELECT 1;`），一旦留在 `db/` 就會被之後每一次 `go build` /
  `go run` 嵌入並當成真的 migration 套用，也極可能被誤 commit 進 repo。作法：測試在執行指令**之前**
  就以 `t.Cleanup` 註冊「以 glob `db/*_add_tenant_table.tx.*.sql` 找出並刪除」的清理函式，
  確保指令成功、失敗、或測試中途 `t.Fatal` 都會清乾淨。
- **`unlock` scenario 的流程：** 起專屬容器 → `runDBMigrate(t, dsn, "init", "--config=test")` →
  **在行程內**用 `db.NewMigrator(bunDB)` 取得 Migrator 並呼叫 `Lock(ctx)` 製造一筆遺留的鎖定紀錄
  → `runDBMigrate(t, dsn, "unlock", "--config=test")` → 斷言 exit code 為 0 且
  `SELECT count(*) FROM bun_migration_locks` 為 0。
  **用 `Migrator.Lock` 而不是自己 `INSERT` 一列**：bun 的 `Unlock` 是以
  `WHERE table_name = <formattedTableName>` 刪除的，手寫 INSERT 很容易把 `table_name` 填成對不上的值，
  於是 `unlock` 明明成功卻刪不到東西，測試會以難以理解的方式失敗。
- 需要資料庫的 scenario 用 `testsupport.StartPostgres(t)` 起專屬容器，**不得動共用容器**。
  建議檔案：`test/migrate_tooling_test.go`。
- 兩個測試都不得呼叫 `t.Parallel()`（`create_sql` 會在 `db/` 產生短暫存在的檔案）。
- 若用法說明在 Task 1 尚未列出 `unlock` / `create_sql`，本 task 補上，並確認 Task 1 的
  「未知子指令」Outline 仍然全綠（`frobnicate` / `up` / `down` 依然要被拒絕）。
Expected Goals (from BDD scenarios):
- [ ] Scenario: create_sql 產生一對成對的空白 migration 檔案
- [ ] Scenario: unlock 清除遺留的 migration 鎖定

## Coverage Check
- Scenario: init 建立 bun 的 migration 記錄資料表 → Task 1
- Scenario: migrate 套用所有尚未套用的 migration → Task 1
- Scenario: 重複執行 migrate 不會出錯也不會重複套用 → Task 1
- Scenario: status 同時列出已套用與待套用的 migration → Task 1
- Scenario Outline: 錯誤的用法會以非零 exit code 拒絕並說明原因 → Task 1
- Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響 → Task 3
- Scenario: 沒有已套用的 group 時 rollback 安全地無動作 → Task 3
- Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema → Task 2
- Scenario: 換機制後 properties 資料表的結構與換之前完全相同 → Task 2
- Scenario: create_sql 產生一對成對的空白 migration 檔案 → Task 4
- Scenario: unlock 清除遺留的 migration 鎖定 → Task 4

## Integration Scenarios
- Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema
- Scenario: 換機制後 properties 資料表的結構與換之前完全相同
- Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響

## Iteration Log

## Amendments
