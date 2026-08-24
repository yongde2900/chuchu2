# BDD-002 — 架構調整：spec-first handler、統一錯誤中介層、bun/migrate
# Created: 2026-08-24
# Status: approved
# Working Directory: .
# Source: conversation（使用者提出的四項需求）
#   1. 本專案是 spec-first，改用 oapi-codegen 由 api/openapi.yaml 產生 handler，
#      設定為 models + chi-server + embedded-spec + strict-server，輸出 api.gen.go。
#   2. 錯誤統一由 middleware 處理：handler 不再回傳
#      api.GetPetById404JSONResponse{...}, nil，改為 return nil, apperr.NotFound.WithError(err)。
#   3. migration 改用 bun migrate。
#   4. 移除契約測試（test/contract_test.go 整檔刪除）。
#   5. 任何一個 error hook 忘了接上時，必須回傳統一形狀的預設錯誤，而不是純文字。
#      → 因此本輪要求一層防護網，讓「漏接」在結構上不可能外洩純文字，
#        而不是只用測試在事後抓。
#
# Out of scope（以下每一條都是決定，不是遺漏；執行者不得「順手補上」）：
#   - 防護網刻意「不」緩衝回應主體：狀態碼與 Content-Type 在 WriteHeader 當下就已確定，
#     判斷在那一刻完成即可。代價是不支援串流回應——本 API 全部是小型 JSON，沒有串流端點。
#   - 不引入 oapi-codegen 的請求／回應驗證 middleware（nethttp-middleware /
#     OapiRequestValidator）。這一輪只用產生的型別與路由，不做執行期 schema 驗證。
#   - 不改動領域層行為：property.Service、狀態機轉換表、金額語意、驗證規則一律不動。
#   - 不改動資料庫 schema：migration 的 SQL 內容一個字元都不得更動，只換執行機制與檔名。
#   - 不新增任何業務 endpoint，也不新增業務用的 migration。
#   - 不引入 DI 框架（wire／fx／dig／do），維持手寫建構子、組裝點只有 cmd/api/main.go。
#   - 不實作 bun 的 mark_applied 子指令，不使用 bun 的 Go migration（CreateGoMigration）。
#   - 不把 golangci-lint 變成 gate；Lint 仍只有 go vet ./...。
#
# 未評分約束（UNGRADED，harness 不會檢查，由人工把關）：
#   - 刪掉 test/contract_test.go 之後，下列 spec 約束將不再有任何自動強制：
#     金額欄位的 pattern ^\d+\.\d{2}$、enum 成員值（產生的 Valid() 沒有任何呼叫點）、
#     page/page_size 的 minimum/maximum。使用者已在知情下選擇接受。
#     註：金額固定兩位小數仍間接受既有整合測試保護（斷言 "25000.50"），
#     但那是針對單一數值的斷言，不是 schema 級強制。
#   - knowledge/decisions/openapi-contract-test.md 已被本輪推翻，需標記為 superseded。
#   - 程式碼註解與產品術語一律使用繁體中文。

Feature: 以 spec 為單一來源產生 HTTP 層，並把錯誤處理收斂到單一中介層

  Rule: HTTP 層由 api/openapi.yaml 產生，spec 與程式碼不得分岔

    As a 維護這個服務的開發者
    I want HTTP 的路由、請求與回應型別都由 spec 產生
    So that 文件說謊這件事在編譯期就不可能發生，不必靠測試事後追認

    Scenario: 重新產生程式碼不會產生任何差異
      Given api/openapi.yaml 與已提交的 api/api.gen.go 都在版控中
      When 重新執行專案的程式碼產生指令
      Then api/api.gen.go 的內容與執行前逐位元組相同

    Scenario: spec 宣告的每一個 endpoint 都真的路由得到
      Given 服務以有效的設定檔啟動
      When 對 spec 中宣告的每一個 path + method 各送出一次請求
      Then 沒有任何一次回應是 chi 的預設 404（body 為 "404 page not found" 的純文字）
      And 每一次回應的 Content-Type 都以 "application/json" 開頭

    Scenario: 既有的 API 行為在改用產生的 handler 之後完全不變
      Given PLAN-001 留下的整合測試（health、startup、panic、property 建檔／查詢／更新）
      When 在不修改任何既有測試斷言的前提下執行整個測試套件
      Then 全部通過
      And 建檔回應的 monthly_rent 仍為固定兩位小數的字串 "25000.50"

  Rule: 所有錯誤回應由單一中介層產生，handler 只回傳 error

    As a 這個 API 的呼叫端
    I want 不論錯誤發生在哪一層，回應形狀都一致且帶得到 request_id
    So that 我可以用同一段程式碼處理所有錯誤，並拿 request_id 回報問題

    Scenario: handler 回傳 apperr 時由中介層轉成對應的狀態碼與 body
      Given 服務已啟動且資料庫中不存在 id 為 00000000-0000-0000-0000-000000000000 的物件
      When 送出 GET /api/v1/properties/00000000-0000-0000-0000-000000000000
      Then 回應狀態碼為 404
      And 回應的 code 欄位為 "PROPERTY_NOT_FOUND"
      And 回應的 request_id 欄位不是空字串

    Scenario: 領域層的衝突錯誤同樣經由中介層轉譯
      Given 一筆狀態為 RENOVATING 的物件
      When 送出 POST /api/v1/properties/{id}/status 且 body 的 status 為 "OCCUPIED"
      Then 回應狀態碼為 409
      And 回應的 code 欄位為 "PROPERTY_INVALID_STATUS_TRANSITION"

    Scenario: 無法解析的 request body 由中介層轉成 400
      Given 服務已啟動
      When 送出 POST /api/v1/properties 且 body 是一段不合法的 JSON "{"
      Then 回應狀態碼為 400
      And 回應的 code 欄位為 "VALIDATION_FAILED"

    Scenario: 路徑參數格式錯誤由中介層轉成 400
      Given 服務已啟動
      When 送出 GET /api/v1/properties/not-a-valid-uuid
      Then 回應狀態碼為 400
      And 回應的 code 欄位為 "VALIDATION_FAILED"
      And 回應的 details 陣列中有一個元素的 field 為 "id"

    Scenario: 查詢參數型別錯誤由中介層轉成 400
      Given 服務已啟動
      When 送出 GET /api/v1/properties?page=abc
      Then 回應狀態碼為 400
      And 回應的 code 欄位為 "VALIDATION_FAILED"
      And 回應的 details 陣列中有一個元素的 field 為 "page"

    Scenario: 未分類的錯誤降級為 500 且不外洩底層訊息
      Given 一個 handler 回傳了未被 apperr 包裝的錯誤，其訊息為 "pq: connection refused on 10.0.0.7"
      When 呼叫端收到回應
      Then 回應狀態碼為 500
      And 回應的 code 欄位為 "INTERNAL"
      And 回應的 message 欄位不包含 "10.0.0.7"

    Scenario: handler 內的 panic 仍然轉成統一形狀的 500
      Given 服務以 server.debug 為 true 啟動
      When 送出 GET /debug/panic
      Then 回應狀態碼為 500
      And 回應的 code 欄位為 "INTERNAL"

    @error @integration
    Scenario Outline: 每一條錯誤路徑的回應都是統一的 JSON 形狀
      Given 服務已啟動
      When 送出 <請求>
      Then 回應的 Content-Type 以 "application/json" 開頭
      And 回應 body 可解析成含有 code、message、request_id 三個欄位的物件
      And request_id 不是空字串

      Examples: 三個錯誤 hook 各自的觸發路徑
        | 觸發的 hook                   | 請求                                                  |
        | 路徑參數綁定失敗              | GET /api/v1/properties/not-a-valid-uuid               |
        | 查詢參數綁定失敗              | GET /api/v1/properties?page=abc                       |
        | request body 解析失敗         | POST /api/v1/properties 且 body 為 "{"                |
        | handler 回傳 apperr           | GET /api/v1/properties/00000000-0000-0000-0000-000000000000 |
        | 領域層驗證失敗                | POST /api/v1/properties 且 monthly_rent 為 "0"        |

    Scenario: 漏接的 error hook 不會讓純文字外洩給呼叫端
      Given 一個刻意未接上任何 error hook 的產生 handler（等同 oapi-codegen 的預設值）
      And 該 handler 因此會以 Content-Type text/plain 寫出一個 400 回應
      When 呼叫端收到回應
      Then 回應狀態碼仍為 400
      And 回應的 Content-Type 以 "application/json" 開頭
      And 回應的 code 欄位為 "INTERNAL"
      And 回應的 request_id 欄位不是空字串
      And 原本那段純文字完全不出現在回應 body 中
      And server 端記錄一筆警告，指出有未統一形狀的錯誤回應被攔截

    Scenario: 防護網不會干擾正常的成功回應
      Given 服務已啟動
      When 送出一個會成功的 GET /api/v1/properties 請求
      Then 回應狀態碼為 200
      And 回應 body 與未加上防護網時逐位元組相同

    Scenario: apperr 的共用 sentinel 不會被 WithError 汙染
      Given 套件層級的 sentinel apperr.NotFound
      When 以兩個不同的底層錯誤各自呼叫一次 apperr.NotFound.WithError
      Then 得到兩個各自包著自己底層錯誤的獨立值
      And apperr.NotFound 本身的底層錯誤仍為 nil

  Rule: migration 由 bun/migrate 管理，手寫機制完全移除

    As a 需要演進資料庫結構的開發者
    I want migration 的詞彙與行為與 bun 官方文件一致
    So that 我查到的官方用法可以直接套用，不必先在心裡把官方詞彙翻譯成自製詞彙

    Scenario: init 建立 bun 的 migration 記錄資料表
      Given 一個乾淨的資料庫
      When 執行 dbmigrate 的 init 子指令
      Then exit code 為 0
      And 資料庫中存在 bun_migrations 與 bun_migration_locks 兩張資料表

    Scenario: migrate 套用所有尚未套用的 migration
      Given 一個已經 init 過但尚未套用任何 migration 的資料庫
      When 執行 dbmigrate 的 migrate 子指令
      Then exit code 為 0
      And 資料庫中存在 properties 資料表
      And bun_migrations 中存在一筆 name 為 "20260819120000" 的紀錄

    Scenario: 重複執行 migrate 不會出錯也不會重複套用
      Given 一個已經套用過所有 migration 的資料庫
      When 再次執行 dbmigrate 的 migrate 子指令
      Then exit code 為 0
      And 標準輸出包含 "no new migrations"
      And bun_migrations 中 name 為 "20260819120000" 的紀錄仍只有一筆

    Scenario: status 同時列出已套用與待套用的 migration
      Given 一個已經套用過所有 migration 的資料庫
      When 執行 dbmigrate 的 status 子指令
      Then exit code 為 0
      And 標準輸出包含 "create_properties"

    Scenario Outline: 錯誤的用法會以非零 exit code 拒絕並說明原因
      Given 一個已編譯得起來的 dbmigrate 執行檔
      When 執行 dbmigrate 並帶入 <參數>
      Then exit code 不為 0
      And 標準錯誤包含 <訊息片段>

      Examples:
        | 參數                    | 訊息片段        |
        | （完全不給參數）        | "用法"          |
        | migrate（不給 --config）| "config"        |
        | frobnicate --config=test| "frobnicate"    |
        | up --config=test        | "up"            |
        | down --config=test      | "down"          |

    Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響
      Given 一個資料庫先後經過兩次獨立的 migrate 呼叫，因而形成兩個 migration group
      And 第二個 group 只包含一個建立 rollback_probe 資料表的 migration
      When 執行 dbmigrate 的 rollback 子指令
      Then exit code 為 0
      And rollback_probe 資料表不存在
      And properties 資料表仍然存在
      And bun_migrations 中仍有 name 為 "20260819120000" 的紀錄

    Scenario: 沒有已套用的 group 時 rollback 安全地無動作
      Given 一個已經 init 過但沒有任何已套用 migration 的資料庫
      When 執行 dbmigrate 的 rollback 子指令
      Then exit code 為 0
      And 標準輸出包含 "nothing to rollback"

    Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema
      Given 一個 migration 會先建立 tx_probe 資料表，再執行一段必定失敗的 SQL
      And 這兩段 SQL 之間以 --bun:split 分隔，因此是兩次獨立的執行
      When 執行 dbmigrate 的 migrate 子指令
      Then exit code 不為 0
      And tx_probe 資料表不存在
      And bun_migrations 中不存在該 migration 版本的紀錄

    Scenario: 換掉 migration 機制之後 properties 資料表的結構完全相同
      Given 一個乾淨的資料庫
      When 依序執行 dbmigrate 的 init 與 migrate 子指令
      Then properties 資料表的每一個欄位的型別與可空性都與換機制之前相同
      And 存在一個涵蓋 (city, district, street_address, floor, room_no) 的唯一索引

    Scenario: create_sql 產生一對成對的空白 migration 檔案
      Given migration 目錄 db/ 中尚不存在任何名為 add_tenant_table 的 migration
      When 執行 dbmigrate 的 create_sql 子指令並給定名稱 add_tenant_table
      Then exit code 為 0
      And 產生一個以 "_add_tenant_table.tx.up.sql" 結尾的檔案
      And 產生一個以 "_add_tenant_table.tx.down.sql" 結尾的檔案
      And 兩個檔名共用同一個 14 位數字時間戳前綴

    Scenario: unlock 清除遺留的 migration 鎖定
      Given 一個已經 init 過、且 bun_migration_locks 中留有一筆鎖定紀錄的資料庫
      When 執行 dbmigrate 的 unlock 子指令
      Then exit code 為 0
      And bun_migration_locks 中的紀錄數為 0
