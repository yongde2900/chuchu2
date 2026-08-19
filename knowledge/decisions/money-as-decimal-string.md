---
title: 金額一律 decimal，JSON 表示法是「固定小數位數的字串」
type: decisions
date: 2026-08-20
tags: [money, decimal, json, api]
---

## 決定

- Go 端一律 `github.com/shopspring/decimal`，**端到端不得出現 float**。
- Postgres 欄位型別為 `NUMERIC`（租金／管理費 `NUMERIC(12,2)`、坪數 `NUMERIC(10,2)`）。
- **JSON 表示法是「字串」**（`"25000.50"`），**絕不是 JSON number**。
- OpenAPI schema 中金額欄位宣告為 `type: string` 並以 `pattern` 約束為十進位數字形式，
  **不得宣告為 number**。

## 為什麼

浮點運算會讓租金／押金金額產生誤差；而 JSON number 無法在 JavaScript 客戶端往返
而不損失十進位精度（`JSON.parse("25000.50")` 會變成 float）。用字串傳輸把精度問題
擋在邊界之外。

## 實作上必須注意的陷阱

**`decimal.Decimal` 預設的 `MarshalJSON` 不會補齊小數位數。** 直接序列化 `25000.50`
會得到 `"25000.5"`，尾隨的零消失。

因此 response DTO **必須是 `string` 欄位**，並以 `StringFixed(2)` 明確格式化：

```go
func formatMoney(d decimal.Decimal) string { return d.StringFixed(2) }
```

BDD 斷言的是**精確的字串形式**（`"25000.50"`、`"27000.00"`），
所以少了 `StringFixed(2)` 會直接讓 scenario 失敗。

反方向（請求進來）則是把字串收進 `CreateInput`／`UpdateInput` 的 `string` 欄位，
由 `Validate()` 負責 `decimal.NewFromString` 解析並回報是哪個欄位出錯 ——
解析失敗會變成 `VALIDATION_FAILED` 的 `details[].field`，而不是 500。

驗證方式：建檔後直接查 DB 確認 `monthly_rent = 25000.50` 為 true（精確相等），
不要只信 HTTP 回應。

相關：[[exported-interfaces-property-service]]、[[openapi-contract-test]]
