---
title: 整合測試在行程內起服務，只有行程行為留給子行程
type: decisions
date: 2026-08-26
tags: [testing, debugging, architecture, assembly]
---

## 決定

整合測試預設用 **`httptest.NewServer(app.NewHandler(...))` 在同一個行程內**起完整服務，
不再用子行程跑 `go run ./cmd/api`。只有「行程層面的行為」還用子行程驗。

為此把組裝從 `cmd/api`（`package main`）抽到 **`internal/app`**。

## 為什麼

**子行程裡的程式碼設不了中斷點。** `testsupport.StartAPI` 執行 `go run ./cmd/api`，
而 `go run` 還會再 fork 一層編譯後的 binary；debugger 附在 `go test` 的行程上，
handler 跑在孫行程裡，兩者毫無關係。要追一個「HTTP 進來、SQL 出去」的 bug，
只能靠 log 猜。

擋路的是套件邊界：組裝寫在 `package main`，`test/` 想在行程內把 router 組起來也
**import 不到**。抽成 `internal/app` 之後：

```bash
dlv test ./test/ -- -test.run TestPropertyCreate_Success
```

中斷點可以一路跟進 handler → service → repo。

**客觀證據：** 單一整合測試對 `./internal/...` 的覆蓋率是 23.1%，
逐函式看得到 `property.Service.Create` 72.2%、`httpx.EnsureJSONError` 83.3%。
覆蓋率只計**行程內**執行的敘述 —— 改之前這些全是 0。

附帶好處是快：省掉每個測試 `go run` 重新編譯的時間。

## 哪些留在子行程

**只有 `test/startup_test.go` 的兩個測試**，因為它們測的就是行程行為：

- 服務以有效設定啟動後**持續執行不退出**。
- 設定缺 `postgres.dsn` 時**在 5 秒內以非零 exit code 退出**，且 stderr 含該 key 名稱。

這兩件事行程內驗不出來，`testsupport.StartAPI` 因此保留。

## 代價

組裝點從 `cmd/api` 搬到 `internal/app`。**「組裝只有一份」這條約束沒有鬆動** ——
仍然是手寫建構子、沒有 DI 框架、沒有全域變數；只是從「不可測的 `package main`」
搬到「可 import 且可測的地方」。`cmd/api` 保留設定載入、開連線、signal 與優雅關閉。

要注意的是 `internal/app` 現在是唯一 import 多個 feature 套件的地方，
分層驗證的 grep 要把它排除在外，見 [[property-service-layering]]。

相關：[[test-layout-two-tiers]]、[[go-run-orphan-process-group]]
