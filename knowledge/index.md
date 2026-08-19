# Knowledge Base Index

This is the map of the knowledge base. Each entry links to a self-contained
Markdown file. Files are grouped by type; scan the section you need and follow
the link. Keep this index in sync with the files — add a line when a file is
created, refresh the hook when content changes.

本知識庫的第一批內容來自 **PLAN-001（包租代管服務骨架 + 物件建檔查詢垂直切片）**
於 2026-08-20 通過整合閘門後的沉澱：實作期做出的決定、最終實際匯出的介面，
以及 Executor 踩到但計畫沒有預先寫下的 gotcha。

## Architecture
- [chuchu2 分層邊界以「套件邊界」表達](architecture/property-service-layering.md) — 三個依賴反轉點，以及為什麼違規是可 grep 的
- [PLAN-001 實際匯出的公開介面](architecture/exported-interfaces-property-service.md) — 下一份 plan 應該對著這份寫，而不是重讀計畫文件

## Decisions
- [包租／代管旗標刻意扁平化到 properties 資料表](decisions/rental-mode-flattened-on-property.md) — 領域背景、已知代價，以及為什麼不要「順手修正」
- [物件狀態機的完整轉換表與「單一強制點」設計](decisions/property-status-machine.md) — 七條合法轉換、對角線全非法、拒絕必須發生在寫入之前
- [金額一律 decimal，JSON 是固定小數位數的字串](decisions/money-as-decimal-string.md) — 為什麼不用 JSON number，以及 StringFixed(2) 的必要性
- [api/openapi.yaml 是唯一契約來源，並以契約測試強制它不說謊](decisions/openapi-contract-test.md) — 三個支柱，以及用突變測試證明防護力
- [logger 放進 server.Options 而非 NewRouter 的參數列](decisions/server-options-logger-field.md) — 加欄位安全、改參數列危險

## Conventions
- [兩層測試佈局](conventions/test-layout-two-tiers.md) — 單元測試不碰 Docker，整合測試集中在 test/ 打真實 HTTP

## Gotchas
- [viper 的 AutomaticEnv 對「只存在於環境變數的 key」不可靠](gotchas/viper-env-override-needs-bindenv.md) — 要明確 BindEnv，否則 testcontainers 注入會失效
- [共用容器 vs 專屬容器](gotchas/testcontainers-shared-vs-dedicated.md) — 哪兩類測試絕不能碰 TestMain 的共用容器，以及為什麼失敗會指向無辜的測試
- [用 go run 啟動服務做測試時必須以 process group 收屍](gotchas/go-run-orphan-process-group.md) — 否則留下佔埠的孤兒行程
- [go:embed 的 pattern 不允許 ".."](gotchas/go-embed-no-parent-dir.md) — migration SQL 的 embed 必須放在 db/ 自己的 package
- [空列表回應必須是 [] 不是 null](gotchas/nil-slice-marshals-to-null.md) — 而且反序列化後的斷言抓不到這件事，要看原始 bytes
- [TDD 之後編輯器診斷會回報早已修好的錯誤](gotchas/stale-gopls-diagnostics-during-tdd.md) — 編譯器是權威，但有兩類診斷可能是真的
