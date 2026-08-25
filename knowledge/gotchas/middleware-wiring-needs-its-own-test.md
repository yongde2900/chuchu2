---
title: middleware 掛在哪裡，需要它自己的測試來守
type: gotchas
date: 2026-08-25
tags: [testing, middleware, router, regression, mutation-testing]
---

## 陷阱

`internal/httpx.EnsureJSONError` 的單元測試很完整：攔截純文字、保留狀態碼、吞掉後續 Write、
成功回應逐位元組不變 —— 全部都測了，而且是用真的產生的 handler 驅動的。
整合測試也有，走真實跑起來的服務。

**但把 `r.Use(httpx.EnsureJSONError(logger))` 整行從 `server.NewRouter` 刪掉，
專案照樣建置成功，整個測試套件全綠。**

## 為什麼

- 單元測試在測試裡**手工組合** chain（`RequestID(EnsureJSONError(logger)(handler))`），
  從來沒有經過 `server.NewRouter`。
- 整合測試雖然走真實組裝好的 router，卻只驗**成功路徑** ——
  而成功路徑掛不掛防護網**行為完全相同**，因此也抓不到。

於是「防護網有沒有真的掛上」這件事，沒有任何測試在守。
對一個存在目的就是「讓漏接在結構上不可能」的元件來說，這格外諷刺：
它自己的接線正是唯一沒有回歸保護的部分。

## 解法

補一個 **router 層級**的測試：透過 `server.NewRouter` 本身掛一條會寫出
`Content-Type: text/plain` ＋ 400 的路由（模擬某條路由漏接了 hook），斷言回應被改寫成 JSON。

```go
func TestNewRouter_MountedRouteMissesErrorHook_SafetyNetRewritesToJSON(t *testing.T) {
    r := server.NewRouter(server.Options{Logger: logger}, func(r chi.Router) {
        r.Get("/leaky", func(w http.ResponseWriter, _ *http.Request) {
            w.Header().Set("Content-Type", "text/plain; charset=utf-8")
            w.WriteHeader(http.StatusBadRequest)
            _, _ = w.Write([]byte("這段純文字絕對不能出現在回應 body 中"))
        })
    })
    // 斷言：400 仍是 400、Content-Type 為 JSON、code == INTERNAL、request_id 非空、純文字不出現
}
```

這個測試在「刪掉 `r.Use` 那一行」的突變下會轉紅，而且會讓**整個專案的** `go test ./...` 轉紅。

## 通則

**測 middleware 的行為，跟測 middleware 有沒有被掛上，是兩件不同的事。**
前者可以在單元層手工組合，後者必須經過真正的組裝函式。
任何「掛上去才生效」的東西（middleware、interceptor、hook、事件訂閱）都適用 ——
判斷方式很簡單：把那一行接線刪掉，如果測試全綠，就是缺一個接線測試。

相關：[[response-safety-net]]。
