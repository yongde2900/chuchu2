---
title: HTTP 層改由 openapi.yaml 產生，契約測試因此整檔刪除
type: decisions
date: 2026-08-25
tags: [openapi, oapi-codegen, codegen, api, contract-test]
---

## 決定

`api/openapi.yaml` 仍是 API 契約的唯一權威來源，但**強制它不說謊的手段從「執行期契約測試」
換成「編譯期產生程式碼」**。PLAN-002 用 oapi-codegen v2.8.0 從 spec 產生 `api/api.gen.go`，
並**整檔刪除 `test/contract_test.go`**（PLAN-001 記載契約測試作法的知識庫條目也一併刪除）。

**依舊有效的約束：任何改動 endpoint、請求／回應欄位、錯誤形狀的工作，
必須在同一次改動內同步更新 `api/openapi.yaml` 並重跑 `go generate ./api/...`**，
不可留給後面補 —— 這條從 PLAN-001 沿用至今，只是強制手段換了。

## 為什麼

契約測試是「事後追認」：先手寫 handler，再用測試檢查它跟文件一致。
產生程式碼是「源頭一致」：路由集合與成功回應的形狀由 spec **編譯期**決定，
不一致根本編譯不過 —— `var _ api.StrictServerInterface = (*apiServer)(nil)`
這一行就是全部六個 operation 的存在性保證。

## 機制

- `api/oapi-codegen.yaml`：`package: api`、`generate: models / chi-server / embedded-spec / strict-server`、`output: api.gen.go`。
- `api/generate.go`：只有 `package api` 與一行 `//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml openapi.yaml`。
  `go generate` 以該檔所在目錄為工作目錄，所以 `output: api.gen.go` 會正確落在 `api/`。
- **產生器本身刻意不進 `go.mod` 的 tool 區塊** —— 進了就得連整個產生器一起 vendor。
  `go.mod` 只加 `github.com/oapi-codegen/runtime` 這一個直接相依。
- `api/api.gen.go` **必須提交進版控，且絕對不得手動編輯**。

## 用什麼守住它

`test/codegen_test.go` 讀下 `api/api.gen.go` → 重跑 `go generate ./api/...` → 再讀一次 →
**逐位元組比對**，失敗時指出第一個相異的位元組 offset。`t.Cleanup` 一律把檔案還原成執行前的
內容，避免產生器版本漂移汙染工作目錄。

突變測試證明它有防護力（兩個方向都抓得到）：手動編輯 `api.gen.go` → 轉紅；
**改了 `openapi.yaml` 卻沒重新產生 → 也轉紅**（後者才是現實中真正會發生的漂移）。

## 已知且接受的代價（UNGRADED）

刪掉契約測試之後，spec 中的金額 `pattern ^\d+\.\d{2}$`、enum 成員值、
`page`/`page_size` 的 `minimum`/`maximum` **不再有任何自動強制**。
使用者在知情下接受。金額固定兩位小數仍間接受既有整合測試斷言 `"25000.50"` 保護，
但那是單一數值斷言，不是 schema 級強制。詳見 [[money-as-decimal-string]]。
