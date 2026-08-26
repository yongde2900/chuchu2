---
title: bun 的 ON CONFLICT upsert 加 WHERE 守門時，table alias 預設值會讓 WHERE 找不到欄位
type: gotchas
date: 2026-08-26
tags: [bun, postgres, upsert, migration, idempotency]
---

## 情境

PLAN-003（LINE webhook follow/unfollow）需要一個「單一 SQL 敘述完成、且能擋掉亂序重送」的
upsert：新事件的時間戳比既有記錄舊時，整筆保持不變且不算錯誤。標準寫法是
`INSERT ... ON CONFLICT (pk) DO UPDATE SET ... WHERE <既有值> <= EXCLUDED.<新值>`——
用 `<=` 而非 `<`，因為時間戳相同代表同一事件被重送，覆寫成同樣的值是等冪的。

## 陷阱

用 bun 的 query builder 寫這個 upsert（`NewInsert().Model(m).On("CONFLICT (...) DO UPDATE").Set(...).Where(...)`）時，
`Where(...)` 引用資料表名的欄位會在**執行期**炸掉：

```
missing FROM-clause entry for table "line_users"
```

即使 `Where` 裡寫的欄位名完全正確、資料表也確實叫這個名字。

## 原因（讀 vendor 原始碼確認）

- `vendor/github.com/uptrace/bun/query_insert.go`：pgdialect 有 `feature.InsertTableAlias`，
  **只要 `On(...)` 有設，bun 就會把 INSERT 寫成 `INSERT INTO "line_users" AS "<alias>"`**。
- `vendor/github.com/uptrace/bun/schema/table.go`：**alias 預設是「結構型別名的 underscore
  形式」**，不是資料表名。`type lineUserModel struct{ bun.BaseModel \`bun:"table:line_users"\` }`
  的 alias 因此會是 `line_user_model`，跟表名完全不同。
- `Where("line_users.last_event_at <= EXCLUDED.last_event_at")` 引用的是 `line_users`，
  但實際 FROM 子句裡這個表這時候叫 `line_user_model`，於是找不到。

## 正確作法

**model 的 tag 必須明確寫出 `alias:`，讓它等於表名**，`Where` 才對得上：

```go
type lineUserModel struct {
    bun.BaseModel `bun:"table:line_users,alias:line_users"`
    ...
}
```

或者整句改用 `db.NewRaw(...)` 手寫 SQL（也可行，但要自己處理參數綁定，失去 query builder 的型別檢查）。

## 另外兩個容易一起漏掉的點

1. **守門條件不成立時，Postgres 不會報錯，只是 0 rows affected** —— 那正是「舊事件被正確
   略過」的樣子。實作**絕不可**把 `RowsAffected() == 0` 當成錯誤，否則亂序抵達的舊事件會被
   誤判成失敗，回傳 500 讓上游重送，而重送的還是同一個舊事件，形成無窮迴圈。
2. `created_at` 一類「只在第一次寫入時設定」的欄位，`DO UPDATE SET` 的欄位清單裡不可以列
   進去 —— 漏掉這條不會報錯也不會轉紅，只會在資料庫裡默默把「第一次建立的時間」蓋掉。

## 怎麼驗證測試真的抓得到

亂序守門這條規則沒有編譯期或型別系統能守住，只能用整合測試打真實 Postgres。要確認測試
真的在測這件事而不是巧合綠燈，可以用突變測試：把 `WHERE` 子句拿掉再跑一次亂序 scenario 的
測試，應該要轉紅（PLAN-003 的 hars-verify 階段實際做過這個驗證）。

相關：[[bun-migrate-adoption]]（`.tx.` 檔名與交易保護是另一個 bun/migrate 的坑）、
[[bun-split-required-for-transactionality-test]]（同樣是「測試綠燈但沒測到真正保護」的模式）。
