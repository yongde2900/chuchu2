---
title: 兩層測試佈局 —— 單元測試貼著程式碼，整合測試集中在 test/
type: conventions
date: 2026-08-20
tags: [testing, layout, convention]
---

## 規則

**第一層：單元測試貼著程式碼放**，檔名 `<被測檔>_test.go`，與被測套件同目錄。
這一層**不得碰 Docker、不得起容器、不得連任何外部服務**，純函式邏輯而已。
**必須在沒有 Docker 的機器上也能跑過。**

例：`CreateInput.Validate` 的每條規則、`CanTransition` 的完整 4×4 轉換表、
`apperr.HTTPStatus` 的映射、`config.Load` 的缺 key 判斷、`ListFilter.normalize` 的預設與夾限、
migration 檔名的解析與排序。

**第二層：整合測試一律放 repo 根目錄的 `test/`，`package test`**，一個 feature 面向一個檔：

**全部 BDD scenario 的驗收證據都在這一層**，打**真實 HTTP 請求**驗證，不直接呼叫內部函式。

### 整合測試的兩種起法

- **預設：行程內**（`startInProcessAPI` → `httptest.NewServer(app.NewHandler(...))`）。
  中斷點有效、快，是絕大多數測試該用的。
- **例外：子行程**（`testsupport.StartAPI` → `go run ./cmd/api`）。
  只給**行程層面的行為**用：服務會不會退出、設定壞掉時的 exit code 與 stderr。
  目前只有 `test/startup_test.go` 需要。

理由見 [[in-process-integration-tests]]。

## 為什麼這樣切

- 整合測試驗的是**組裝起來的系統**，不屬於任何單一套件。
- 可以用 `go test -race -count=1 ./test/...` 一條指令重跑整個整合層。
- `internal/` 底下不會有任何套件 import testcontainers，分層邊界更乾淨。

## 兩個必要條件

- **`-count=1` 是必要的** —— Go 會快取測試結果，容器背後的測試從快取回綠會騙人。
- 每個整合測試以 **`TRUNCATE properties`** 取得乾淨狀態，不重建容器
  （例外見 [[testcontainers-shared-vs-dedicated]]）。

## 這條約定的意圖

**「不要用 mock 假裝驗過端到端」才是重點，「開子行程」從來不是。**

這件事最早是在 PLAN-001 被逼出來的：panic 攔截的 scenario 排在 `StartAPI` 出現之前，
只好改用同行程 `httptest.NewServer` 套真實 router 驅動 —— 五條 `Then` 全部照驗，
而且 log／request_id 的關聯在同行程下比對子行程 stderr 更容易斷言。

後來證明那個「權宜之計」才是常態：2026-08-26 把組裝抽到 `internal/app` 之後，
整合測試預設就是行程內起服務，子行程只留給真正需要它的行程行為測試。

相關：[[testcontainers-shared-vs-dedicated]]、[[go-run-orphan-process-group]]、
[[in-process-integration-tests]]
