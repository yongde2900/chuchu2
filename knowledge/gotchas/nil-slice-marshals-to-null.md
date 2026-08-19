---
title: 空列表回應必須是 []，不是 null —— 而且一般的測試斷言抓不到這件事
type: gotchas
date: 2026-08-20
tags: [json, go, api, testing]
---

## 症狀

`GET /api/v1/properties?status=DELISTED` 查無資料時，回應是：

```json
{"items":null,"total":0}
```

但契約要求 `items` 為**空陣列** `[]`。

## 原因

Go 的 nil slice（`var items []T`）`json.Marshal` 後是 `null`，不是 `[]`。

## 作法

在建立 slice 時就給定非 nil 的空值：

```go
items := make([]propertyResponse, 0, len(result.Items))   // 即使 len 為 0 也不是 nil
```

## 更重要的陷阱：這件事很難用測試證明

一個把回應反序列化成 `[]T` 的測試，**無法區分 `null` 與 `[]`** —— 兩者都會得到一個
長度為 0 的 slice。所以「測試通過」不等於「回的是 `[]`」。

斷言 `body.Items == nil` 會比什麼都不驗好一些（Go 的 `json.Unmarshal` 對 `null` 會留下 nil slice，
對 `[]` 會給出非 nil 的空 slice），但最可靠的方式是**直接檢查原始回應 bytes**：

```go
raw, _ := io.ReadAll(resp.Body)
// 直接對 raw 這個字串做斷言，而不是反序列化後才看
```

PLAN-001 驗收時就是以原始 bytes 確認回應為 `{"items":[],"total":0}`。

## 一般化

任何「回傳集合」的 endpoint 都適用這一條。review 列表型 API 時，
養成習慣直接看 wire 上的位元組，而不是看反序列化後的 Go 值。

相關：[[openapi-contract-test]]
