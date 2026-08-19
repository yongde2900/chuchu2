---
title: chuchu2 分層邊界以「套件邊界」表達
type: architecture
date: 2026-08-20
tags: [layering, packages, go, property]
---

chuchu2（包租代管服務）刻意讓分層邊界成為**套件邊界**，而不是只寫在文件裡的約定。
違規會直接表現成「錯的套件出現了錯的 import」，肉眼掃 import 區塊就能發現。

```
internal/property/           純領域＋service＋Repository 介面
                             —— 不 import bun，也不 import net/http
internal/property/pgrepo/    bun 實作 —— 唯一 import bun 的 property 子套件
internal/property/httpapi/   HTTP handler 與 DTO —— 唯一 import chi/net-http 的子套件
internal/server/             chi router 組裝 —— 不 import 任何 feature 套件
```

**三個刻意保留的依賴反轉點：**

1. `property.Repository` 介面 —— 領域層因此不認識 bun。
2. `health.Checker` 介面 —— 可加掛新探針而不動 `internal/health`。
3. `server.Mount`（`type Mount func(r chi.Router)`）—— feature 套件自己宣告路由，
   所以 `internal/server` 不需要 import `internal/health` 或 `internal/property/...`。

**為什麼重要：** 這是 PLAN-001 骨架最主要的價值，但它寫不成可斷言的 BDD `Then`，
harness 抓不到違規，只能靠 code review。把它變成套件邊界後，違規至少是可 grep 的。

驗證方式（每次 review 可重跑）：

```bash
grep -rn "uptrace/bun\|net/http" internal/property/*.go   # 應只命中註解
```

依賴注入一律手寫建構子，組裝點只有 `cmd/api/main.go` 一處；不使用任何 DI 框架
（wire／fx／dig／do），也不用全域變數或 `init()` 傳遞相依。

相關：[[exported-interfaces-property-service]]、[[server-options-logger-field]]
