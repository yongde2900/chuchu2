---
title: TDD 的 Red 階段之後，編輯器診斷會持續回報早已修好的錯誤
type: gotchas
date: 2026-08-20
tags: [tooling, gopls, tdd, workflow]
---

## 症狀

TDD 流程跑完（測試先紅、實作後綠）之後，編輯器／IDE 仍大量回報：

```
undefined: Load           undefined: CanTransition
undefined: UpdateInput    missing go.sum entry for module ...
pattern *.sql: no matching files found
```

但 `go build ./...` 與 `go vet ./...` 都是乾淨的，測試也全綠。

## 原因

gopls 的快照停在 Red 階段（或 `go get` 執行到一半）的那個瞬間。
在 PLAN-001 的八個 task 中，**每一個 task 結束時都出現過這種誤報**，無一例外。

## 判準

**編譯器是權威，編輯器診斷不是。** 判斷方式很簡單：

```bash
go build ./...     # 這個說了算
go vet ./...
go test -race -count=1 ./...
```

三者皆綠就是綠，不論編輯器顯示什麼。

## 但不要一律無視 —— 有兩類診斷可能是真的

1. **`missing go.sum entry`** 這類相依性錯誤，在 `go get` 真的沒跑完時是**真實**的。
   仍然用 `go build ./...` 判斷，不要因為「上次是誤報」就跳過驗證。
2. **`declared and not used` / 語法錯誤**，若指向你沒動過的檔案，可能是**別的 agent 留下的殘骸**。
   PLAN-001 期間就發生過一次：一個被 session 額度中斷的 evaluator agent 在
   `test/contract_test.go` 留下三行注入的程式碼（含一行語法錯誤的
   `routesFromImplementation(t,)`），導致整個 package 編譯失敗。
   那次診斷是**真的**，`go build` 也確實失敗。

## 結論

看到診斷先跑 `go build ./...`：
- build 綠 → 診斷是陳舊快照，忽略。
- build 紅 → 診斷是真的，去看它指的位置，並考慮是否為中斷的 agent 留下的殘骸。
