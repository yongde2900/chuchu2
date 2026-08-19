// Package db 只放 migration 用的 .sql 檔案與其 embed.FS。
//
// go:embed 的 pattern 不允許包含 ".."，因此無法從 internal/migrate 往上
// 嵌入 db/ 目錄；embed 宣告必須放在 db/ 目錄自己的 .go 檔裡，
// 再由 internal/migrate import 這個 package 使用 FS。
package db

import "embed"

// FS 內含 db/ 目錄下所有 *.sql 檔案（成對的 <timestamp>_<name>.up.sql／
// .down.sql），由 internal/migrate 讀取並依檔名排序套用。
//
//go:embed *.sql
var FS embed.FS
