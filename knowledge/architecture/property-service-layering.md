---
title: chuchu2 分層邊界以「套件邊界」表達
type: architecture
date: 2026-08-20
tags: [layering, packages, go, property, line]
---

chuchu2（包租代管服務）刻意讓分層邊界成為**套件邊界**，而不是只寫在文件裡的約定。
違規會直接表現成「錯的套件出現了錯的 import」，肉眼掃 import 區塊就能發現。

```
internal/property/           純領域＋service＋Repository 介面
                             —— 不 import bun，也不 import net/http
internal/property/pgrepo/    bun 實作 —— 唯一 import bun 的 property 子套件
internal/property/httpapi/   operation 方法 —— 唯一 import 產生的 api 套件的子套件
                             （PLAN-002 後已不再 import chi／net-http）
internal/apihttp/            產生的 HTTP 層與 apperr 之間的唯一接線點（PLAN-002 新增）
                             —— import api/apperr/httpx/server/chi，但不 import 任何 feature 套件
internal/server/             chi router 組裝 —— 不 import 任何 feature 套件
internal/app/                **組裝點** —— 唯一 import 多個 feature 套件的地方
```

**PLAN-002 之後的兩點變化：**

- `internal/property/httpapi` 與 `internal/health` 不再自己宣告路由（`Mount` 都已刪除），
  改為各自實作 `api.StrictServerInterface` 中屬於自己的 operation；路由表由產生的程式碼提供。
  `internal/health` 因此**不再 import `internal/server`**。
- 新增的 `internal/apihttp` **刻意不 import 任何 feature 套件**（只認得 `api.StrictServerInterface`
  這個介面），所以它不是組裝點，可以被單元測試直接測。真正把兩個 feature 合體的
  `apiServer` 放在 `cmd/api`，組裝點仍然只有那一處。見
  [[cannot-embed-two-same-named-types]] 說明它為什麼不能用 embedding 寫。

**四個刻意保留的依賴反轉點：**

1. `property.Repository` 介面 —— 領域層因此不認識 bun。
2. `health.Checker` 介面 —— 可加掛新探針而不動 `internal/health`。
3. `server.Mount`（`type Mount func(r chi.Router)`）—— feature 套件自己宣告路由，
   所以 `internal/server` 不需要 import `internal/health` 或 `internal/property/...`。
4. `line.Repository` 介面（PLAN-003 新增）—— 與 `property.Repository` 同一個模式，
   `internal/line` 領域層不認識 bun、net/http，也不認識 LINE SDK。

**PLAN-003（LINE webhook）延伸出的第三種子套件角色：**

```
internal/line/               純領域＋service＋Repository 介面
                             —— 不 import bun、不 import net/http、不 import LINE SDK
internal/line/pgrepo/        bun 實作 —— 唯一 import bun 的 line 子套件
internal/line/webhookhttp/   webhook handler —— 這個 feature 唯一被允許
                             同時 import net/http 與第三方 SDK 的子套件
```

`webhookhttp` 與 `property/httpapi` 是不同的角色，命名刻意不同：`httpapi` 是「產生的 api
型別 ↔ 領域型別」轉換層，連 net/http 都不 import；`webhookhttp` 相反，是整個 feature 唯一
容許出現 net/http 與外部 SDK 的地方（因為 LINE SDK 的 `webhook` package 自己也 import
net/http，任何 import 它的套件都會傳遞性帶進 net/http —— 這正是它不能待在 `internal/line`
裡的原因）。兩個 feature 各自的 `pgrepo` 子套件**同名**（都叫 `pgrepo`），`internal/app`
同時 import 兩者時必須給其中一個 import 別名，否則編譯失敗。

**為什麼重要：** 這是 PLAN-001 骨架最主要的價值，但它寫不成可斷言的 BDD `Then`，
harness 抓不到違規，只能靠 code review。把它變成套件邊界後，違規至少是可 grep 的。

驗證方式（每次 review 可重跑）。**注意一律用 `.Imports`（直接 import）而不是
`go list -deps`（遞移相依）—— 這裡的約束講的是「不直接 import」，用 -deps 會因為
間接相依而誤報**。`internal/app` 是唯一的例外，它本來就該 import 一堆 feature 套件：

```bash
grep -rn "uptrace/bun\|net/http" internal/property/*.go                      # 應只命中註解
go list -f '{{join .Imports "\n"}}' ./internal/apihttp | grep yongde2900     # 不應有 feature 套件
go list -f '{{join .Imports "\n"}}' ./internal/server  | grep yongde2900     # 只應有 internal/httpx
go list -f '{{join .Imports "\n"}}' ./internal/health  | grep yongde2900     # 不應有 internal/server
```

⚠️ **這些約束目前沒有任何自動強制。** 沒有測試、沒有 CI 會在有人違規時轉紅，
上面的指令要靠人記得跑。這與 [[middleware-wiring-needs-its-own-test]] 是同一類問題：
「靠紀律維持」與「結構上不可能出錯」之間差了一個測試。要補的話，一個讀 `go list`
輸出並斷言 import 集合的測試就能守住，大約二十行。

依賴注入一律手寫建構子，**組裝只有一份**，放在 `internal/app.NewHandler`；
不使用任何 DI 框架（wire／fx／dig／do），也不用全域變數或 `init()` 傳遞相依。

**因此 `internal/app` 會 import 一堆 feature 套件，而其他每個套件都只有寥寥幾個 ——
這是設計，不是腐化，不要「順手清理」。** 代價換來的是：想知道什麼接到什麼，只要讀一個檔。

組裝之所以不放在 `cmd/api`，是因為 `package main` 無法被 import，整合測試就只能開子行程 ——
而子行程裡的 handler 設不了中斷點。見 [[in-process-integration-tests]]。
`cmd/api` 保留行程層面的職責：設定載入、開連線、signal、優雅關閉。

**兩個刻意接受的限制，不要「順手修正」：** handler 的建構子吃的是具體的
`*property.Service` 而非介面；時間直接取自 `time.Now()`，沒有可注入的 Clock。
理由是測試策略走 testcontainers 端到端，不需要這兩道縫。

相關：[[server-options-logger-field]]、[[unified-error-middleware]]、[[response-safety-net]]、
[[in-process-integration-tests]]
