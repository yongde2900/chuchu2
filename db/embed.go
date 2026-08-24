// Package db 只放 migration 用的 .sql 檔案與其 embed.FS，以及「如何探索與
// 套用它們」的唯一權威來源（見 migrate.go 的 Migrations／NewMigrator）。
//
// embed 指令的 pattern 不允許包含 ".."，因此無法從其他套件往上嵌入 db/
// 目錄；embed 宣告必須放在 db/ 目錄自己的 .go 檔裡。cmd/dbmigrate 與
// test/main_test.go 都透過本套件匯出的 Migrations／NewMigrator 取得
// migrate.Migrations／*migrate.Migrator，不直接讀 FS，確保兩邊設定一致。
package db

import "embed"

// FS 內含 db/ 目錄下所有 *.sql 檔案（成對的 <timestamp>_<name>.tx.up.sql／
// .tx.down.sql），由 Migrations 探索並依檔名時間戳排序套用。
//
//go:embed *.sql
var FS embed.FS
