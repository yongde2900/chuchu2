---
title: migration 改用 bun/migrate，接受 group 回滾語意
type: decisions
date: 2026-08-25
tags: [migration, bun, postgres, cli, transaction]
---

## 決定

自製的 `internal/migrate` **整個刪除**，改用 bun 官方的 `github.com/uptrace/bun/migrate`
（bun v1.2.18 的子套件，不需要新增相依）。`cmd/dbmigrate` 仍是我們自己的檔案，
提供六個子指令：`init` / `migrate` / `rollback` / `status` / `unlock` / `create_sql`。
**`up` / `down` 完全廢除、不保留別名**，會以非零 exit code 被拒絕。

## 三個一定要設對、否則會無聲失去保護的地方

1. **檔名必須是 `.tx.up.sql` / `.tx.down.sql`。**
   `bun@v1.2.18/migrate/migration.go` 的 `newSQLMigrationFunc` **只有在檔名以 `.tx.` 結尾時才
   `BeginTx`**，否則只取一條 `Conn` 直接執行。把 `*.up.sql` 原封搬過去會**無聲地失去交易保護**。
2. **`migrate.WithMarkAppliedOnSuccess(true)` 必須傳給 `NewMigrator`。**
   bun 預設**先** `MarkApplied` **再**執行 SQL，且那次寫入不在 migration 自己的交易裡 ——
   SQL 失敗時 `bun_migrations` 會留下一筆「已套用」的假記錄。
3. **兩個選項掛在不同建構子上，型別不同不可互換：**
   `WithMigrationsDirectory("db")` 是 `MigrationsOption`，只能給 `NewMigrations`；
   `WithMarkAppliedOnSuccess(true)` 是 `MigratorOption`，只能給 `NewMigrator`。

CLI 與 `TestMain` **必須共用同一個建構函式**（`db.NewMigrator`），否則兩邊行為會悄悄分岔。

## 接受的語意變更：rollback 只回滾最後一個 group

bun 把「同一次 `Migrate` 呼叫中套用的所有 migration」視為一個 group（`bun_migrations.group_id`），
`Rollback` 回滾的是**最後一個 group**，不是全部。這與被取代的手寫 `Down`（全部回滾）不同，
使用者知情並刻意接受，理由是「退回上一個穩定狀態」比「拆光整個 schema」安全。

**必須一併理解的後果：任何需要「完全乾淨資料庫」的測試不能再靠 `down` 清光，
只能用專屬的用完即丟容器。** 見 [[testcontainers-shared-vs-dedicated]]。

## 其他容易踩的點

- **`bun_migrations` 的 `name` 欄位只有時間戳，沒有 migration 名稱**（`Migration.Comment` 標了
  `bun:"-"`，不寫進資料庫）。斷言要查 `name = '20260819120000'`；
  寫成 `WHERE name LIKE '%create_properties%'` 會永遠查不到而讓測試假失敗。
  反過來 `status` 子指令的輸出**會**包含名稱，因為 `Migration.String()` 是 `name_comment`，
  comment 是 `Discover` 當下從檔名解析、存在記憶體中的。
- **bun 要求先 `Init()`**，不像手寫機制那樣 lazily `CREATE TABLE IF NOT EXISTS`。
- **`create_sql` 走 `CreateTxSQLMigrations`**（不是 `CreateSQLMigrations`），才會產生帶 `.tx.` 的檔名。
- **`Unlock` 要用 `defer` 寫在 `run` 裡** —— `main` 的 `os.Exit` 會跳過 defer，但 `run` 回傳時 defer 已經跑完。
- `cmd/dbmigrate` 保留標準庫 `flag`，**不引入 `urfave/cli`**（bun 官方範例用它，不要跟著引入）。
