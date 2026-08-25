---
title: 回應防護網 EnsureJSONError —— 讓「漏接 error hook」在結構上不可能外洩純文字
type: decisions
date: 2026-08-25
tags: [middleware, error-handling, httpx, security, router]
---

## 決定

`internal/httpx.EnsureJSONError` 是掛在 **router 層級**的最外層防護網：
狀態碼 `>= 400` 且 `Content-Type` 不以 `application/json` 開頭時，就地把回應改寫成
`ErrorBody{Code:"INTERNAL", Message:"internal server error", RequestID:<本次 request id>}`，
並記一筆 slog Warn（帶原本的狀態碼與 Content-Type，方便追查是哪條路由漏接）。

## 為什麼需要第二道防線

[[unified-error-middleware]] 已經把三個 hook 全部接上了，但「接線是否正確」是**人為紀律**
（例如日後新增一條不經過 `apihttp.Mount` 組裝的路由就會漏接），不是結構保證。
防護網把最壞情況從「純文字外洩」降級成「形狀統一但語意變模糊」。

掛在 `server.NewRouter` 的 chain 而不是只包住產生的那幾條路由，是為了讓
`/debug/panic` 之類非產生的路由也在保護範圍內。middleware 順序：
`RequestID → access log → EnsureJSONError → Recoverer`。

## 四個實作要點

1. **狀態碼一定要保留** —— 只換 body 與 Content-Type，400 仍是 400。
2. **攔截後必須吞掉下游後續的 `Write`**，且回報 `(len(p), nil)` 而非 0 或 error。
   不吞的話原本那段純文字會**接在 JSON 後面**一起送出去，body 變成無法解析的兩段。
3. **`Header().Del("Content-Length")` 必須在呼叫底層 `WriteHeader` 之前**做 ——
   下游若是 `http.Error` 寫的，長度是照純文字算的，跟改寫後的 JSON 對不上。
4. **必須處理「沒呼叫 `WriteHeader` 就直接 `Write`」**（隱含 200），與標準庫行為一致。

## 刻意不做的事

**不緩衝回應主體。** 狀態碼與 Content-Type 在 `WriteHeader` 當下就已確定，判斷在那一刻完成即可。
代價是不支援串流回應 —— 本 API 全部是小型 JSON，沒有任何串流端點，這個代價可以接受。

## 實證

整合閘門用 chi 自己的預設 404／405（`http.Error` 寫的純文字）當真實觸發源驗證過：
`GET /no-such-route` → `{"code":"INTERNAL",...}`、`HTTP=404`、`CT=application/json`，
`"404 page not found"` 完全沒有出現在 body 中，server 端記到對應的 WARN。

`Recoverer` 寫出的 JSON 500 會被原樣放行（它的 `WriteJSON` 先設 Content-Type 再
`WriteHeader`，防護網的 prefix 檢查因此短路）—— 順帶保證 panic 路徑也不可能外洩純文字。
