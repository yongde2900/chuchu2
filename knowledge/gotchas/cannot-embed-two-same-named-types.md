---
title: 兩個同名的匯出型別無法一起匿名內嵌（API redeclared）
type: gotchas
date: 2026-08-25
tags: [go, embedding, struct, compile-error, cmd-api]
---

## 症狀

想用 Go embedding 把兩個 feature 的 handler 組成一個完整的介面實作：

```go
type apiServer struct {
    *health.API      // internal/health 匯出的 API
    *httpapi.API     // internal/property/httpapi 匯出的 API
}
```

**編譯直接失敗：`API redeclared`。**

## 為什麼

匿名內嵌欄位的**隱含欄位名是型別名本身（不含套件路徑）**。兩個型別都叫 `API`，
所以兩個欄位的名稱都是 `API`，在同一個 struct 中重複宣告。
套件不同**不能**用來區分 —— `pa.API` 與 `pb.API` 內嵌後都是 `API`。

最小重現：

```go
// pa/api.go: package pa; type API struct{}
// pb/api.go: package pb; type API struct{}
type apiServer struct { *pa.API; *pb.API }   // ./main.go:10:6: API redeclared
```

## 解法

改用**具名欄位 ＋ 顯式轉發方法**，語意完全等價：

```go
type apiServer struct {
    healthAPI   *health.API
    propertyAPI *httpapi.API
}

var _ api.StrictServerInterface = (*apiServer)(nil)   // 編譯期斷言仍然成立

func (s *apiServer) GetHealthz(ctx context.Context, req api.GetHealthzRequestObject) (api.GetHealthzResponseObject, error) {
    return s.healthAPI.GetHealthz(ctx, req)
}
// ...其餘五個 operation 轉給 propertyAPI
```

代價只是多寫六個一行方法，換來的是能編譯。

## 給規劃者的提醒

PLAN-002 的計畫文件**直接寫了那段無法編譯的 embedding**，Executor 照做才發現。
規劃階段若要在文件裡給出 struct 形狀，涉及多個套件同名型別時要先確認它真的能編譯 ——
或者一開始就寫成具名欄位版本。
