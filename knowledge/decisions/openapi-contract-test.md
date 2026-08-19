---
title: api/openapi.yaml 是唯一契約來源，並以契約測試強制它不說謊
type: decisions
date: 2026-08-20
tags: [openapi, contract-test, kin-openapi, api]
---

## 為什麼需要這個

chuchu2 的技術棧是 REST/JSON over chi，**刻意不含 proto/IDL**（與使用者其他 repo 的
Connect RPC + buf 不同）。因此沒有任何東西會自動扮演 API 文件的角色，
而一份無人查核的手寫 OpenAPI 文件，第一次改欄位就會開始說謊。

`api/openapi.yaml` 被定為 API 契約的**唯一權威來源**，並以 `test/contract_test.go` 強制它與實作一致。

**約束：任何 task 新增或修改 endpoint、請求／回應欄位、錯誤形狀時，
必須在同一個 task 內同步更新 `api/openapi.yaml`**，不可留給後面的 task 補。

## 契約測試的三個支柱（缺一就沒有防護力）

1. **文件本身合法** —— `openapi3.NewLoader().LoadFromFile(...)` 後呼叫 `doc.Validate(ctx)`，
   不是只確認載入成功。
2. **回應 body 對 schema** —— `openapi3filter.ValidateResponse` 搭配
   `Options{IncludeResponseStatus: true}`，如此未宣告的狀態碼本身也會驗證失敗。
   **只比對狀態碼是不夠的。**
3. **雙向路由比對** —— 以 `chi.Walk` 列舉實際註冊路由，與文件宣告集合互比：
   文件有而實作沒有、實作有而文件沒有，**兩個方向都必須讓測試失敗**。

## 兩個實作細節

- **必須排除 `/debug/` 開頭的路由。** `GET /debug/panic` 只在 `server.Options.Debug` 為 true 時掛載，
  而 `config/test.yaml` 設了 `server.debug: true`，所以測試環境**會**註冊它。
  它不屬於公開契約；漏了這個排除，反向比對必定失敗。
- **`servers` 必須宣告成相對路徑 `/`**，不能是 `http://localhost:8080`。
  契約測試用 `gorillamux` router 比對路由，而測試中服務監聽的是隨機埠號；
  相對 server URL 讓 `FindRoute` 只靠 method+path 比對，不受埠號影響。

取得路由表的方式：在 `test/` 內以**與 `cmd/api/main.go` 相同的 mount 清單**重建一次 router
（`server.NewRouter` 本來就回傳 `*chi.Mux`），不必為測試在正式程式碼開洞。
**但那份 mount 清單必須與 `cmd/api/main.go` 保持同步**，否則反向比對會靜默漏掉路由 ——
這是這個作法唯一的代價，測試中要加註解提醒。

## 驗收方式：突變測試

契約測試唯一能證明自己有防護力的方式是**故意弄壞文件，確認測試會失敗**。
PLAN-001 驗收時實跑過四項，全部如預期失敗：

| 突變 | 期望失敗訊息 |
|---|---|
| 加入文件有而實作沒有的假路由 | 「宣告了 X，但服務並未實際註冊」 |
| 刪除文件中真實存在的路由 | 「服務註冊了 X，但未文件化」 |
| 金額欄位 `type: string` → `number` | schema 層級：`got string, want number` |
| 移除已宣告的 `409` | `status is not supported` |

**日後改動契約測試後，請重跑這四項。** 一個永遠通過的契約測試比沒有契約測試更糟。

相關：[[money-as-decimal-string]]、[[kin-openapi-31-support]]
