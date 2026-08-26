# Knowledge Base Index

This is the map of the knowledge base. Each entry links to a self-contained
Markdown file. Files are grouped by type; scan the section you need and follow
the link. Keep this index in sync with the files — add a line when a file is
created, refresh the hook when content changes.

本知識庫的第一批內容來自 **PLAN-001（包租代管服務骨架 + 物件建檔查詢垂直切片）**
於 2026-08-20 通過整合閘門後的沉澱：實作期做出的決定，以及 Executor 踩到但
計畫沒有預先寫下的 gotcha。

第二批來自 **PLAN-002（spec-first handler、統一錯誤中介層、bun/migrate）**
於 2026-08-25 通過整合閘門後的沉澱。PLAN-002 推翻了 PLAN-001 的契約測試決定，
被推翻的條目已刪除 —— 要追溯當時的作法請看 git 歷史與 `plan/PLAN-002-*.md`。

第三批來自 **PLAN-003（LINE Messaging API webhook：follow/unfollow 事件接入）**
於 2026-08-26 通過整合閘門後的沉澱：第二個垂直切片套用同一套分層模式，
外加 bun upsert 與 LINE SDK 的兩個實作期才發現的坑。

## Architecture
- [chuchu2 分層邊界以「套件邊界」表達](architecture/property-service-layering.md) — 四個依賴反轉點，以及為什麼違規是可 grep 的（PLAN-003 補上 line.Repository 與 webhookhttp 這第三種子套件角色）

（介面清單刻意不進知識庫：`go doc ./...` 永遠正確且零維護，手抄的簽章清單只會腐爛。
這裡只記 signature 讀不出來的東西。）

## Decisions
- [包租／代管旗標刻意扁平化到 properties 資料表](decisions/rental-mode-flattened-on-property.md) — 領域背景、已知代價，以及為什麼不要「順手修正」
- [物件狀態機的完整轉換表與「單一強制點」設計](decisions/property-status-machine.md) — 七條合法轉換、對角線全非法、拒絕必須發生在寫入之前
- [金額一律 decimal，JSON 是固定小數位數的字串](decisions/money-as-decimal-string.md) — 為什麼不用 JSON number，以及 StringFixed(2) 的必要性
- [HTTP 層改由 openapi.yaml 產生，契約測試因此整檔刪除](decisions/spec-first-codegen-replaces-contract-test.md) — 從執行期追認換成編譯期保證，以及付出的代價
- [所有錯誤回應收斂到單一中介層](decisions/unified-error-middleware.md) — oapi-codegen 的三個 hook 預設全是純文字，details[].field 從 ParamName 來
- [回應防護網 EnsureJSONError](decisions/response-safety-net.md) — 讓「漏接 error hook」在結構上不可能外洩純文字，以及為什麼刻意不緩衝 body
- [migration 改用 bun/migrate，接受 group 回滾語意](decisions/bun-migrate-adoption.md) — 三個設錯就無聲失去保護的地方
- [查詢參數：型別錯誤回 400，enum 值錯誤仍然寬鬆](decisions/query-param-binding-strict-on-type-loose-on-enum.md) — 為什麼是這個組合，由產生的程式碼實際行為決定
- [整合測試在行程內起服務](decisions/in-process-integration-tests.md) — 子行程裡的 handler 設不了中斷點，組裝因此抽到 internal/app
- [logger 放進 server.Options 而非 NewRouter 的參數列](decisions/server-options-logger-field.md) — 加欄位安全、改參數列危險
- [LINE 若加第二個領域，改用 Content-Based Router 收斂在 internal/line 頂層](decisions/line-multi-domain-content-based-router.md) — 尚未實作的未來決定；webhookhttp 子套件屆時整個收掉，改名是真正的重構不是搬檔案

## Conventions
- [兩層測試佈局](conventions/test-layout-two-tiers.md) — 單元測試不碰 Docker，整合測試集中在 test/ 打真實 HTTP

## Gotchas
- [viper 的 AutomaticEnv 對「只存在於環境變數的 key」不可靠](gotchas/viper-env-override-needs-bindenv.md) — 要明確 BindEnv，否則 testcontainers 注入會失效
- [共用容器 vs 專屬容器](gotchas/testcontainers-shared-vs-dedicated.md) — 哪兩類測試絕不能碰 TestMain 的共用容器，以及為什麼失敗會指向無辜的測試
- [用 go run 啟動服務做測試時必須以 process group 收屍](gotchas/go-run-orphan-process-group.md) — 否則留下佔埠的孤兒行程
- [go:embed 的 pattern 不允許 ".."](gotchas/go-embed-no-parent-dir.md) — migration SQL 的 embed 必須放在 db/ 自己的 package
- [空列表回應必須是 [] 不是 null](gotchas/nil-slice-marshals-to-null.md) — 而且反序列化後的斷言抓不到這件事，要看原始 bytes
- [TDD 之後編輯器診斷會回報早已修好的錯誤](gotchas/stale-gopls-diagnostics-during-tdd.md) — 編譯器是權威，但有兩類診斷可能是真的
- [兩個同名的匯出型別無法一起匿名內嵌](gotchas/cannot-embed-two-same-named-types.md) — `API redeclared`；改用具名欄位＋顯式轉發
- [沒有 --bun:split，交易性測試會因為錯誤的理由通過](gotchas/bun-split-required-for-transactionality-test.md) — Postgres 隱式交易會遮住真正的 bug
- [middleware 掛在哪裡，需要它自己的測試來守](gotchas/middleware-wiring-needs-its-own-test.md) — 刪掉那行接線若測試全綠，就是缺一個接線測試
- [要測嵌進 embed.FS 的 migration，得在執行期把檔案寫進 db/](gotchas/runtime-written-migration-files-for-embedded-fs.md) — 四條清理規則，漏一條會汙染 repo
- [bun 的 ON CONFLICT upsert 加 WHERE 守門時，alias 預設值會讓 WHERE 找不到欄位](gotchas/bun-upsert-on-conflict-alias-trap.md) — 執行期才炸，且 RowsAffected==0 不是錯誤
- [line-bot-sdk-go v8 的 webhook 事件／來源解析回傳 value type，不是指標](gotchas/line-sdk-webhook-value-not-pointer-types.md) — type switch 寫成指標形式會讓事件安靜被吃掉
