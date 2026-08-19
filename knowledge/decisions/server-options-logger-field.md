---
title: logger 放進 server.Options 而非 NewRouter 的參數列
type: decisions
date: 2026-08-20
tags: [api-design, interface-stability, server]
---

## 情境

`internal/server` 的 middleware chain（RequestID → 結構化 access log → Recoverer）需要一個
`*slog.Logger`，但 PLAN-001 已把 `NewRouter` 的簽章凍結為：

```go
func NewRouter(opts Options, mounts ...Mount) *chi.Mux
```

後續 task 已經對著這個簽章規劃。

## 決定

把 logger 以**新增欄位**的方式放進 `Options`：

```go
type Options struct {
    Debug  bool
    Logger *slog.Logger   // nil 時退回 slog.Default()
}
```

## 為什麼

契約凍結的是 `NewRouter` 的**參數列**。往 `Options` struct 新增欄位對既有／已規劃的呼叫端
是**非破壞性**的（Go 的具名欄位初始化不受影響），而改參數列會直接讓所有規劃中的呼叫端失效。

`Logger` 為 nil 時退回 `slog.Default()`，讓「只想要 router、不在意 log」的呼叫端
（例如契約測試重建 router 時）不必被迫提供。

## 一般化的原則

在 harness 這類「後續 task 已對著簽章規劃」的流程裡，
**加欄位安全、改參數列危險**。遇到「需要多傳一個東西」時，先看能不能塞進既有的 options struct。

若真的必須改簽章，那是 `hars-revise` 的工作（修訂已核准的計畫），
不是執行期可以自行決定的事。

相關：[[property-service-layering]]、[[exported-interfaces-property-service]]
