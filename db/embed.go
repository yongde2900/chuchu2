// Package db 放 migration 的 .sql 檔案與其 embed.FS。
//
// embed pattern 不允許 ".."，所以無法從其他套件往上嵌入 db/——embed 宣告
// 必須放在這裡。呼叫端一律走 Migrations／NewMigrator，不直接讀 FS，
// 確保 CLI 與測試拿到的設定一致。
package db

import "embed"

//go:embed *.sql
var FS embed.FS
