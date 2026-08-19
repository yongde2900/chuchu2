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

```
test/startup_test.go         啟動與設定
test/health_test.go          健康檢查與斷線
test/migrate_test.go         migration up/down 往返
test/panic_test.go           panic 攔截
test/property_create_test.go
test/property_query_test.go
test/property_update_test.go
test/contract_test.go        OpenAPI 契約驗證
```

**全部 BDD scenario 的驗收證據都在這一層**，透過 `testsupport.StartAPI` 打**真實 HTTP 請求**驗證，
不直接呼叫內部函式。

## 為什麼這樣切

- 整合測試跑的是**整支 binary**，不屬於任何單一套件。
- 可以用 `go test -race -count=1 ./test/...` 一條指令重跑整個整合層。
- `internal/` 底下不會有任何套件 import testcontainers，分層邊界更乾淨。

## 兩個必要條件

- **`-count=1` 是必要的** —— Go 會快取測試結果，容器背後的測試從快取回綠會騙人。
- 每個整合測試以 **`TRUNCATE properties`** 取得乾淨狀態，不重建容器
  （例外見 [[testcontainers-shared-vs-dedicated]]）。

## 一個實務上的張力

規則說「所有 BDD 驗收證據透過 `StartAPI` 驅動」，但 `StartAPI` 本身是 Task 3 的產出，
而 panic 攔截的 scenario 屬於 Task 2（相依順序上更早）。
當時的處置是：仍把測試放在 `test/panic_test.go`，但改以**同行程** `httptest.NewServer`
套用真實 router 與完整 middleware chain 驅動 —— 五條 `Then` 全部照驗，
而且 log／request_id 關聯在同行程下反而比對子行程 stderr 更容易斷言，也不需要 Docker。

教訓：這條約定的**意圖**是「不要用 mock 假裝驗過端到端」，不是「一定要開子行程」。
遇到相依順序衝突時，保留意圖、調整手段，並把處置寫進 plan 的 Iteration Log。

相關：[[testcontainers-shared-vs-dedicated]]、[[go-run-orphan-process-group]]
