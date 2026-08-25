---
title: 沒有 --bun:split，交易性測試會因為錯誤的理由通過
type: gotchas
date: 2026-08-25
tags: [migration, bun, postgres, testing, false-positive]
---

## 陷阱

要驗證「migration 中途失敗時整個 migration 回滾」，直覺的探針是一個 `.tx.up.sql`：

```sql
CREATE TABLE tx_probe (id INT PRIMARY KEY);
INSERT INTO no_such_table VALUES (1);   -- 必定失敗
```

跑完之後 `tx_probe` 確實不存在，測試轉綠 —— **但這個綠是假的**。

## 為什麼

兩個敘述沒有分隔指示詞時，bun 會把整個檔案當成**一段**，以單次 `ExecContext` 送出。
**Postgres 的 simple query protocol 會自動把整批敘述包成隱式交易** ——
所以就算檔名不是 `.tx.`、bun 根本沒有 `BeginTx`，`tx_probe` 一樣不會留下來。
測試完全沒有測到 `.tx.` 檔名的作用。

## 正確作法

兩段 SQL 之間放**獨立一行 `--bun:split`**（前後不得有多餘空白，否則 bun 會回報
`unknown directive`）：

```sql
CREATE TABLE tx_probe (id INT PRIMARY KEY);
--bun:split
INSERT INTO no_such_table VALUES (1);
```

這樣才是兩次獨立的 `ExecContext`，隱式交易不再涵蓋兩者，真正的保護只剩 `.tx.` 帶來的 `BeginTx`。

## 怎麼證明測試真的有效

用突變測試雙向確認（PLAN-002 實測結果）：

- 把檔名的 `.tx.` 拿掉、保留 `--bun:split` → **測試轉紅**（`tx_probe` 留了下來）。
- **在上一步之上再把 `--bun:split` 拿掉 → 測試又轉綠**，儘管此時完全沒有交易保護。

第二步正是「因為錯誤的理由通過」的實證。任何交易性測試都值得這樣驗一次。

相關：[[bun-migrate-adoption]] 說明 `.tx.` 檔名與 `WithMarkAppliedOnSuccess` 的作用。
