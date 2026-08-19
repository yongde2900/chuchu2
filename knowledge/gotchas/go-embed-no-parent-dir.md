---
title: go:embed 的 pattern 不允許 ".."，migration SQL 的 embed 宣告必須放在 db/ 自己的 package
type: gotchas
date: 2026-08-20
tags: [go-embed, migration, packages]
---

## 症狀

想在 `internal/migrate` 裡嵌入 repo 根目錄的 `db/*.sql`：

```go
//go:embed ../../db/*.sql   // ❌ 編譯不會過
var FS embed.FS
```

## 原因

`go:embed` 的 pattern **不允許包含 `..`** —— 只能嵌入該 `.go` 檔所在目錄或其子目錄的檔案。

## 作法

讓 `db/` 本身成為一個 Go package，embed 宣告放在它自己的 `.go` 檔裡，再由 `internal/migrate` import：

```
db/
├── embed.go                                   # package db，只放 //go:embed *.sql
├── 20260819120000_create_properties.up.sql
└── 20260819120000_create_properties.down.sql
```

```go
// db/embed.go
package db

import "embed"

//go:embed *.sql
var FS embed.FS
```

`internal/migrate` 的 `Up`／`Down`／`Applied` 從 `db.FS` 讀取 SQL。
這是唯一可行的作法，不是風格選擇。

## 附帶的 lint 誤報

`db/embed.go` 的 package 說明文字若有一行以 `go:embed` 開頭（例如中文註解
「`// go:embed 的 pattern 不允許包含 ".."……`」），**staticcheck 會誤報 SA9009
「ineffectual compiler directive due to extraneous space」** —— 它把說明文字誤認為壞掉的 directive。

真正的 directive（`//go:embed *.sql`，無空格）仍然正確、embed 也正常運作。
本專案的 Lint gate 是 `go vet ./...`，不受影響。若想消除誤報，把說明文字改寫成
不以 `go:embed` 開頭即可。
