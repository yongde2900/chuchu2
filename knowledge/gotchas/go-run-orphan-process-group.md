---
title: 用 go run 啟動服務做測試時，必須以 process group 收屍
type: gotchas
date: 2026-08-20
tags: [go-run, testing, process, darwin]
---

## 症狀

測試用 `exec.Command("go", "run", "./cmd/api", ...)` 啟動服務，結束時 `cmd.Process.Kill()`，
但埠號仍被佔用，後續測試撞 `address already in use`。

## 原因

**`go run` 會先編譯，再 fork 出一個子行程去跑編譯後的 binary。**
殺掉 `go run` 本身**不會**連帶殺掉那個子行程，於是留下佔著埠號的孤兒行程。

## 作法（darwin 可行）

建立 process group，然後對整個 group 發訊號：

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// ...
syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)   // 注意負號 = 整個 process group
// 逾時未退再 SIGKILL
```

## 另外三件相關的事

1. **`StartAPI` 必須輪詢到服務真的接受連線才回傳。** `go run` 第一次要編譯，可能要數秒；
   否則測試會在服務還沒起來時就發請求。建議每 100ms 試一次 TCP dial，上限 60 秒。
2. **stdout/stderr 要導進帶 mutex 的 buffer。** `-race` 之下多個 goroutine 共用
   `bytes.Buffer` 會被 race detector 抓到。
3. **`cmd.Dir` 必須設為 repo 根目錄**（用 `testsupport.RepoRoot(t)`），
   否則 `config.Load` 找不到 `config/<name>.yaml`。

## 例外

一次性的 CLI（如 `go run ./cmd/dbmigrate up`）跑完就結束，用 `exec.CommandContext` + `cmd.Run()`
等它結束即可，不需要 process group 處理。但**要把 stdout/stderr 一起放進失敗訊息**，
否則 migration SQL 出錯只會看到 `exit status 1`，無從除錯。

相關：[[testcontainers-shared-vs-dedicated]]
