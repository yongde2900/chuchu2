---
title: 要測嵌進 embed.FS 的 migration，得在執行期把檔案寫進 db/
type: gotchas
date: 2026-08-25
tags: [migration, go-embed, testing, bun, cleanup]
---

## 問題

migration 的 rollback group 語意需要第二個 migration，交易性測試需要一個會中途失敗的 migration。
但這些 scenario 都要透過真實的 `go run ./cmd/dbmigrate` 驅動，而 CLI 讀的是 `db` package 的
`embed.FS` —— **編譯期決定，無法從外部注入**。

## 作法

由測試在執行期間把臨時 migration 檔案**寫進 repo 的 `db/` 目錄**，
讓接下來的 `go run` 在重新編譯時把它們嵌進去，測試結束以 `t.Cleanup` 刪除。

之所以成立，關鍵在於 **`go run` 是在測試執行「當下」編譯的**，
而已經跑起來的測試 binary 自己的 `db.FS` 不含這些檔案 ——
因此 `TestMain` 的共用容器完全不受影響。

## 四條必須遵守的規則

1. **清理必須在寫檔「之前」就用 `t.Cleanup` 註冊**，確保測試中途失敗也一定刪得掉。
2. **一律不得 `t.Parallel()`** —— 檔案在磁碟上的那段期間是全域可見的，並行會互相污染。
3. **version 取 `29990101000001` 這種明顯是測試產物、且必定排在真實 migration 之後的固定值**，
   不要用 `time.Now()`。
4. **rollback 的臨時檔案必須留到 `rollback` 那次 `go run` 之後才刪** ——
   rollback 需要嵌入的 `.tx.down.sql` 才知道怎麼回滾。用 `t.Cleanup` 自然就是這個時序。

## `create_sql` 的清理更重要

`create_sql` 產生的樣板檔案（`SET statement_timeout = 0;` + `SELECT 1;`）一旦留在 `db/`，
**會被之後每次 `go build` / `go run` 嵌入並當成真的 migration 套用**，也極可能被誤 commit。
測試必須在執行指令**之前**就以 glob 註冊清理函式。

相關：[[go-embed-no-parent-dir]]（為什麼 embed 必須放在 `db/` 自己的 package）、
[[bun-migrate-adoption]]。
