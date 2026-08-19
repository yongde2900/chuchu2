# BDD-001 — 包租代管系統：服務骨架 + 物件（房源）建檔與查詢垂直切片
# Created: 2026-08-19
# Status: approved
# Approved: 2026-08-19
# Revision note: 2026-08-19 於 Gate 2 審閱期間退回 draft，新增 AC7（API 契約文件）與其 scenario，
#   再次通過 Gate 1 後重新蓋章。
#   原因：使用者選了不含 proto 的 REST 技術棧，其他 repo 由 proto/ 擔任的 API 文件角色因此空缺，
#   而本輪「只做 API、不分入口」意謂這批 endpoint 遲早要交給前端串接。
# Working Directory: .   （repo 根目錄；所有路徑皆相對於此）
# Source: conversation（無既有 ticket）。本輪驗收條件由 Phase 0 問答確立：
#   AC1. 可執行的 Go 專案骨架：viper 設定載入、chi 路由、bun/Postgres 連線、Redis 連線、
#        版本化 migration、結構化 log、統一錯誤處理與 request id。
#   AC2. 一條端到端垂直切片打通全部分層：物件（房源）建檔與查詢。
#   AC3. 金額一律使用 shopspring/decimal，不得有浮點誤差。
#   AC4. 碰到 DB 的測試以 testcontainers-go 自帶 Postgres，`go test -race ./...` 單一指令即可全綠。
#   AC5. 區分「包租」與「代管」兩種營運模式，物件層需帶此標記。
#   AC6. 本輪只做 API，不分房東端／房客端入口。
#   AC7. 需有一份機器可讀的 API 契約（OpenAPI），且必須有機制保證它與服務實際行為一致 ——
#        因為本輪不含 proto/IDL，沒有任何東西自動扮演 API 文件的角色。
#
# Out of scope（本輪刻意不做，缺少不算疏漏）:
#   - 房東主檔、房客主檔的獨立 aggregate 與 CRUD。本輪物件以 landlord_name / landlord_phone
#     兩個內嵌欄位承載房東資訊，正規化成獨立資料表留待下一份 plan。
#   - 租約（簽訂、續約、提前終止）與租金收付帳務。物件狀態本輪由人工呼叫 API 變更，
#     尚未由租約事件驅動。
#   - 認證與授權。本輪所有 endpoint 皆為未保護狀態；Redis 連線先建立起來供日後 session 使用，
#     但沒有任何 session 讀寫行為。
#   - 房東端／房客端入口與其權限模型。
#   - 修繕報修、報表、通知、檔案（照片、合約掃描檔）上傳。
#   - 正式環境部署設定（CI/CD、容器映像、K8s manifest）。
#
# Known constraints（非行為性，不會被 harness 評分，需人工把關）:
#   - 程式碼註解與產品術語使用繁體中文，與使用者其他 repo 的慣例一致。
#   - 分層邊界必須真實存在：handler 不得直接碰 bun DB，repository 不得回傳 HTTP 型別。
#     這是本輪骨架的主要價值，但無法寫成可斷言的 Then，交由人工在 code review 確認。

Feature: 包租代管系統服務骨架與物件建檔查詢

  Rule: 服務骨架可啟動、可觀測、可演進

    As a 維運人員
    I want 服務能以設定檔驅動啟動，並在故障時明確表態
    So that 我能在部署與值班時判斷服務是否健康、問題出在哪一層

    @infra
    Scenario: 以有效設定檔啟動服務
      Given 設定檔 config/test.yaml 存在且填妥 server.port、postgres.dsn、redis.addr
      When 以 `go run ./cmd/api --config=test` 啟動服務
      Then 程序持續執行不退出
      And 對 server.port 發出的 TCP 連線可被接受

    @infra @error
    Scenario: 設定檔缺少必要欄位時拒絕啟動
      Given 設定檔 config/broken.yaml 存在但沒有 postgres.dsn 這個 key
      When 以 `go run ./cmd/api --config=broken` 啟動服務
      Then 程序在 5 秒內退出且 exit code 不為 0
      And stderr 輸出的訊息中包含字串 "postgres.dsn"

    @infra @integration
    Scenario: 相依服務皆可連線時健康檢查回報健康
      Given 服務已啟動
      And Postgres 可連線
      And Redis 可連線
      When 對 GET /healthz 發出請求
      Then 回應狀態碼為 200
      And 回應 JSON 的 status 欄位為 "ok"
      And 回應 JSON 的 checks.postgres 欄位為 "ok"
      And 回應 JSON 的 checks.redis 欄位為 "ok"

    @infra @error @integration
    Scenario Outline: 任一相依服務斷線時健康檢查回報不健康
      Given 服務已啟動
      And <斷線服務> 已停止接受連線
      When 對 GET /healthz 發出請求
      Then 回應狀態碼為 503
      And 回應 JSON 的 status 欄位為 "degraded"
      And 回應 JSON 的 <斷線檢查項> 欄位為 "down"

      Examples:
        | 斷線服務 | 斷線檢查項      |
        | Postgres | checks.postgres |
        | Redis    | checks.redis    |

    @infra
    Scenario: Migration 可正向套用亦可回滾
      Given 一個空的 Postgres 資料庫
      When 執行 `go run ./cmd/dbmigrate up --config=test`
      Then properties 資料表存在
      When 接著執行 `go run ./cmd/dbmigrate down --config=test`
      Then properties 資料表不存在

    @infra @error
    Scenario: 未預期的 panic 被攔截為 500 且不外洩堆疊
      Given 服務已啟動
      And 存在一個必定 panic 的測試用路由 GET /debug/panic
      When 對 GET /debug/panic 發出請求
      Then 回應狀態碼為 500
      And 回應 JSON 的 code 欄位為 "INTERNAL"
      And 回應 JSON 的 request_id 欄位為非空字串
      And 回應 body 不包含字串 "goroutine"
      And 服務輸出的 log 中存在一筆 level 為 error 且 request_id 與回應相同的紀錄

  Rule: 管理員可建檔與查詢物件（房源）

    As a 內部管理員
    I want 把承接的房源建檔並隨時查得到
    So that 後續的租約簽訂與租金帳務有一份可信的物件主檔可依據

    Background:
      Given 服務已啟動且 properties 資料表為空

    Scenario: 建立一筆包租物件
      When 對 POST /api/v1/properties 發出請求，body 為
        """
        {
          "city": "臺北市",
          "district": "大安區",
          "street_address": "復興南路一段 100 號",
          "floor": "5",
          "room_no": "A",
          "layout": "INDEPENDENT_SUITE",
          "area_ping": "8.50",
          "monthly_rent": "25000.50",
          "management_fee": "1200.00",
          "deposit_months": 2,
          "rental_mode": "MASTER_LEASE",
          "landlord_name": "王大明",
          "landlord_phone": "0912345678"
        }
        """
      Then 回應狀態碼為 201
      And 回應 JSON 的 id 欄位為合法 UUID
      And 回應 JSON 的 status 欄位為 "VACANT"
      And 回應 JSON 的 monthly_rent 欄位為字串 "25000.50"
      And 回應 JSON 的 rental_mode 欄位為 "MASTER_LEASE"
      And 資料庫 properties 資料表中該筆 monthly_rent 欄位的值等於 25000.50

    @error
    Scenario: 相同門牌重複建檔被拒絕
      Given 已存在一筆物件，其 city 為 "臺北市"、district 為 "大安區"、street_address 為 "復興南路一段 100 號"、floor 為 "5"、room_no 為 "A"
      When 以完全相同的 city、district、street_address、floor、room_no 對 POST /api/v1/properties 發出請求
      Then 回應狀態碼為 409
      And 回應 JSON 的 code 欄位為 "PROPERTY_DUPLICATE"
      And 資料庫 properties 資料表的資料筆數仍為 1

    @error
    Scenario Outline: 欄位驗證失敗時回報是哪個欄位出錯
      When 對 POST /api/v1/properties 發出請求，其 <欄位> 的值為 <值>，其餘欄位皆合法
      Then 回應狀態碼為 400
      And 回應 JSON 的 code 欄位為 "VALIDATION_FAILED"
      And 回應 JSON 的 details 陣列中存在一項，其 field 欄位為 <欄位>
      And 資料庫 properties 資料表的資料筆數為 0

      Examples:
        | 欄位             | 值               |
        | "city"           | ""               |
        | "street_address" | ""               |
        | "monthly_rent"   | "0"              |
        | "monthly_rent"   | "-1"             |
        | "monthly_rent"   | "abc"            |
        | "area_ping"      | "0"              |
        | "deposit_months" | -1               |
        | "rental_mode"    | "SUBLEASE"       |
        | "layout"         | "CASTLE"         |

    Scenario: 以 id 查詢單一物件
      Given 已存在一筆 monthly_rent 為 25000.50、rental_mode 為 "MANAGED" 的物件，其 id 為 P1
      When 對 GET /api/v1/properties/P1 發出請求
      Then 回應狀態碼為 200
      And 回應 JSON 的 id 欄位等於 P1
      And 回應 JSON 的 monthly_rent 欄位為字串 "25000.50"
      And 回應 JSON 的 rental_mode 欄位為 "MANAGED"
      And 回應 JSON 包含 created_at 與 updated_at 兩個 RFC3339 格式的欄位

    @error
    Scenario: 查詢不存在的物件
      When 對 GET /api/v1/properties/00000000-0000-0000-0000-000000000000 發出請求
      Then 回應狀態碼為 404
      And 回應 JSON 的 code 欄位為 "PROPERTY_NOT_FOUND"

    Scenario: 分頁列出物件並依建檔時間由新到舊排序
      Given 已依序建立 25 筆物件，建立順序為第 1 筆最早、第 25 筆最晚
      When 對 GET /api/v1/properties?page=1&page_size=10 發出請求
      Then 回應狀態碼為 200
      And 回應 JSON 的 total 欄位為 25
      And 回應 JSON 的 items 陣列長度為 10
      And items 陣列第一項是第 25 筆建立的物件
      And items 陣列最後一項是第 16 筆建立的物件
      When 接著對 GET /api/v1/properties?page=3&page_size=10 發出請求
      Then 回應 JSON 的 items 陣列長度為 5

    Scenario Outline: 依條件篩選物件列表
      Given 已存在 4 筆物件：
        | 編號 | city   | status     | rental_mode  |
        | 甲   | 臺北市 | VACANT     | MASTER_LEASE |
        | 乙   | 臺北市 | OCCUPIED   | MANAGED      |
        | 丙   | 新北市 | VACANT     | MANAGED      |
        | 丁   | 新北市 | RENOVATING | MASTER_LEASE |
      When 對 GET /api/v1/properties?<查詢字串> 發出請求
      Then 回應狀態碼為 200
      And 回應 JSON 的 total 欄位為 <筆數>
      And items 陣列恰好包含編號 <預期結果> 的物件

      Examples:
        | 查詢字串                       | 筆數 | 預期結果 |
        | status=VACANT                  | 2    | 甲、丙   |
        | rental_mode=MANAGED            | 2    | 乙、丙   |
        | city=新北市                    | 2    | 丙、丁   |
        | city=臺北市&status=VACANT      | 1    | 甲       |
        | status=DELISTED                | 0    | 無       |

    Scenario: 更新物件租金後查詢反映新值
      Given 已存在一筆 monthly_rent 為 25000.50 的物件，其 id 為 P1
      When 對 PATCH /api/v1/properties/P1 發出請求，body 為 {"monthly_rent": "27000.00"}
      Then 回應狀態碼為 200
      And 回應 JSON 的 monthly_rent 欄位為字串 "27000.00"
      When 接著對 GET /api/v1/properties/P1 發出請求
      Then 回應 JSON 的 monthly_rent 欄位為字串 "27000.00"
      And 回應 JSON 的 updated_at 欄位晚於 created_at 欄位

    Scenario: 合法的物件狀態轉換被接受
      Given 已存在一筆 status 為 "VACANT" 的物件，其 id 為 P1
      When 對 POST /api/v1/properties/P1/status 發出請求，body 為 {"status": "RENOVATING"}
      Then 回應狀態碼為 200
      And 回應 JSON 的 status 欄位為 "RENOVATING"
      When 接著對 GET /api/v1/properties/P1 發出請求
      Then 回應 JSON 的 status 欄位為 "RENOVATING"

    @error
    Scenario Outline: 非法的物件狀態轉換被拒絕
      Given 已存在一筆 status 為 <原狀態> 的物件，其 id 為 P1
      When 對 POST /api/v1/properties/P1/status 發出請求，body 為 {"status": <目標狀態>}
      Then 回應狀態碼為 409
      And 回應 JSON 的 code 欄位為 "PROPERTY_INVALID_STATUS_TRANSITION"
      When 接著對 GET /api/v1/properties/P1 發出請求
      Then 回應 JSON 的 status 欄位仍為 <原狀態>

      Examples:
        | 原狀態       | 目標狀態     |
        | "DELISTED"   | "OCCUPIED"   |
        | "DELISTED"   | "RENOVATING" |
        | "OCCUPIED"   | "DELISTED"   |
        | "VACANT"     | "VACANT"     |

  Rule: API 契約有單一權威來源，且與服務實作保持一致

    As a 需要串接這組 API 的前端或第三方開發者
    I want 一份機器可讀、且被驗證過與實作一致的 API 契約
    So that 我可以據以產生 client、寫 mock、預期錯誤形狀，而不必去讀後端的 Go 程式碼

    @contract @integration
    Scenario: 服務的實際回應與路由集合皆符合 api/openapi.yaml
      Given 檔案 api/openapi.yaml 存在且是一份合法的 OpenAPI 3.1 文件
      And 服務已啟動
      When 對 openapi.yaml 中宣告的每一個 path + method 各發出請求，且對每個 operation
        至少涵蓋一種成功回應與一種錯誤回應
      Then 每一次回應的狀態碼都出現在該 operation 宣告的 responses 之中
      And 每一次回應的 body 都通過該狀態碼所對應的 schema 驗證
      And openapi.yaml 宣告的 path + method 集合，與服務實際註冊的路由集合完全相同
        （既不得有已文件化但未實作的端點，也不得有已實作但未文件化的端點；
        比對時排除 /debug/ 開頭的除錯路由，那些只在 server.debug 為 true 時掛載，不屬於公開契約）
