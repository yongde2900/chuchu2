---
title: 所有錯誤回應收斂到單一中介層，handler 只回傳 error
type: decisions
date: 2026-08-25
tags: [error-handling, apperr, oapi-codegen, middleware, api]
---

## 決定

改用 spec-first 產生的 strict handler 之後，**handler 一律只回傳 `error`，不自己寫錯誤回應**。
所有錯誤回應由 `internal/apihttp` 這個新套件統一轉譯成 `httpx.ErrorBody`。

## oapi-codegen 的三個 error hook（全部預設為純文字，必須全部接上）

產生的程式碼有三個錯誤出口，**預設值都是 `http.Error(w, err.Error(), code)`**，
也就是 `Content-Type: text/plain` 直接把底層錯誤訊息回吐給呼叫端：

| Hook | 觸發時機 | 預設 |
|---|---|---|
| `ChiServerOptions.ErrorHandlerFunc` | 路徑／查詢參數綁定失敗 | 純文字 400 |
| `StrictHTTPServerOptions.RequestErrorHandlerFunc` | request body 解析失敗 | 純文字 400 |
| `StrictHTTPServerOptions.ResponseErrorHandlerFunc` | handler 回傳的 error（apperr 落地處） | 純文字 500 |

`internal/apihttp` 分別以 `ParamErrorHandler` / `RequestErrorHandler` / `ResponseErrorHandler`
接上這三個，全部收斂到 `httpx.WriteError`。

## details[].field 的來源

五個產生的參數綁定錯誤型別 —— `*api.InvalidParamFormatError`、`*api.RequiredParamError`、
`*api.UnmarshalingParamError`、`*api.TooManyValuesForParamError`、`*api.RequiredHeaderError`
—— **每一個都有 `ParamName` 欄位**，那正是 BDD 要求的 `details[].field`（`id`、`page`）的來源。
用 `errors.AsType` 逐一比對取出。取不出時仍必須產出 `VALIDATION_FAILED` 400，field 留空。

## 兩個容易做錯的地方

- **wire 形狀繼續用 `httpx.ErrorBody`，不要改用產生的 `api.ErrorBody`。**
  後者的 `Details` 是 `*[]FieldError`（指標包 slice），只會多一層無謂的 nil 處理，
  而兩者的 JSON 形狀完全一致。產生的 `*400/404/409JSONResponse` 因此變成
  「產生了但沒有人使用」—— **這是刻意的，不是遺漏**。
  （順帶一提，`api.ErrorBody` 的欄位是 `RequestId` 而不是 `RequestID`，
  oapi-codegen 的命名規則如此；真要用到它時別把名字打錯。）
- **`internal/apihttp` 不 import 任何 feature 套件**（只認得 `api.StrictServerInterface`），
  所以它可以被單元測試直接測，而組裝點仍然只有 `cmd/api`。見 [[property-service-layering]]。

## 未分類錯誤的降級

`httpx.WriteError` 用 `errors.AsType[*apperr.Error]` 取不到應用層錯誤時，一律降級成
`INTERNAL` 500，**不外洩原始錯誤訊息**。這條路徑沒辦法從真實跑起來的服務可靠觸發，
因此以單元測試涵蓋（傳入 `errors.New("pq: connection refused on 10.0.0.7")`，
斷言 message 不含 `10.0.0.7`）。

第二道防線見 [[response-safety-net]]。
