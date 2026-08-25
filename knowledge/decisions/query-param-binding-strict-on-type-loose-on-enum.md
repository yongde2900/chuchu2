---
title: 查詢參數：型別錯誤回 400，enum 值錯誤仍然寬鬆
type: decisions
date: 2026-08-25
tags: [api, validation, query-params, oapi-codegen]
---

## 決定

改用產生的參數綁定之後，`GET /api/v1/properties` 的查詢參數行為**刻意分成兩種**：

| 情況 | 行為 | 理由 |
|---|---|---|
| **型別**錯誤（`?page=abc`） | **400 `VALIDATION_FAILED`**，`details[].field == "page"` | 產生的綁定做型別轉換，轉不動就是錯 |
| **enum 成員**錯誤（`?status=BOGUS`） | **不篩選該欄位，回 200** | 產生的綁定只做型別轉換，**不驗證 enum 成員** |

這推翻了 PLAN-001「查詢參數一律寬鬆、無效值視為不篩選」當中關於**型別錯誤**的部分，
但保留了關於 **enum 值**的部分。

## 為什麼是這個組合

不是設計潔癖，是**產生的程式碼實際做什麼**決定的：oapi-codegen 會把 enum 產生成
`type ListPropertiesParamsStatus string` 這種具名型別加上常數，**另有 `Valid()` 方法，
但產生的程式碼中沒有任何地方呼叫它**（已 grep 確認）。所以 `status=BOGUS` 會原封不動綁進
`*ListPropertiesParamsStatus`，不會報錯。

實作端因此要**自己**維持寬鬆語意：轉成 `property.Status` / `property.RentalMode` 後
**只有 `Valid()` 為真才設定篩選條件**，非法值視為「不篩選該欄位」。

## 連帶要做的事

**spec 必須補上 `listProperties` 的 `400` 回應**（`$ref: ErrorBody`），否則文件會說謊 ——
`?page=abc` 現在真的會回 400。這是 PLAN-002 對 `api/openapi.yaml` 唯二的改動之一。

## 其他綁定細節

- `Page` / `PageSize` 是 `*int`，nil 時傳 0 讓 `property.ListFilter.normalize` 補預設值。
- `City` 是 `*string`，nil 時傳空字串。
- **`req.Body` 是指標，可能為 nil**（雖然 spec 標了 required，產生的程式碼不會強制），
  必須擋 nil deref。
- `GetProperty` / `UpdateProperty` / `ChangePropertyStatus` 的 `req.Id` **已經是解析好的 UUID**
  （`openapi_types.UUID` 是 `uuid.UUID` 的別名），不需要也不應該再自己解析路徑參數。
- **`ChangePropertyStatus` 仍要自行驗證 status 是合法列舉值**（產生的程式碼不會驗）。
