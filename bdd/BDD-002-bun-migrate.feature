# BDD-002 — 以 bun/migrate 取代手寫的 migration 機制
# Created: 2026-08-20
# Status: approved
# Approved: 2026-08-20
# Working Directory: .   （repo 根目錄；所有路徑皆相對於此）
# Source: conversation（無既有 ticket）。本輪驗收條件由 Phase 0 問答確立：
#   AC1. cmd/dbmigrate 底層改用 bun 的 `bun/migrate` 套件（Migrations / Migrator），
#        手寫的 internal/migrate 套件整個移除。
#   AC2. 採用 bun 的指令詞彙：init / migrate / rollback / status / unlock / create_sql。
#        舊的 up / down 兩個子指令**完全廢除**，不保留別名。
#   AC3. 接受 bun 的 group rollback 語意 —— rollback 只回滾「最後一個 migration group」，
#        而非拆掉全部已套用的 migration。
#   AC4. cmd/dbmigrate 保留標準庫 flag，不引入 urfave/cli。
#   AC5. 既有的 create_properties migration 必須維持交易性（bun 的 .up.sql 預設不包交易，
#        須改用 .tx.up.sql），且套用後的 schema 與現況完全相同。
#   AC6. BDD-001 的 scenario「Migration 可正向套用亦可回滾」由本份文件取代。
#
# Supersedes: BDD-001 的 scenario「Migration 可正向套用亦可回滾」——
#   該 scenario 斷言 `go run ./cmd/dbmigrate up|down --config=test`，AC2 廢除這兩個動詞後即不再成立。
#   本輪必須在 BDD-001 檔案中把該 scenario 標記為 superseded，並改寫 test/migrate_test.go。
#
# Depends on: PLAN-001 必須先執行完畢（含 Task 6/7/8）。兩份 plan 同時改動同一個 repo
#   必然衝突 —— PLAN-001 的 test/ TestMain 與 Task 8 的契約測試都會碰到 migration。
#
# Out of scope（本輪刻意不做，缺少不算疏漏）:
#   - 不改動任何 migration 的 SQL 內容。欄位、型別、唯一索引一律不變；本輪只換執行機制。
#   - 不新增任何業務用的 migration。
#   - 不使用 bun 的 Go migration（create_go / MustRegister）。本專案的 migration 一律是 SQL 檔案，
#     Go migration 留待日後真的需要在 migration 中跑 Go 邏輯時再說。
#   - 不做 mark_applied 這個移轉指令。它的用途是把既有正式資料庫的 migration 標記為已套用，
#     而本專案目前沒有任何長存的資料庫 —— 開發與測試都是用完即丟的 testcontainers。
#   - 不改動 PLAN-001 其他 task 產出的任何行為（API、領域模型、健康檢查一律不動）。
#   - 不做 migration 的 CI 檢查（例如「每個 up 都必須有 down」的靜態檢驗）。
#
# Known constraints（非行為性，不會被 harness 評分，需人工把關）:
#   - 程式碼註解與產品術語使用繁體中文。
#   - 本輪結束後 internal/migrate 套件必須不復存在，而不是留著沒人用。人工確認沒有殘留。
#   - AC4：cmd/dbmigrate 保留標準庫 flag，不得引入 urfave/cli 或任何其他 CLI 框架。
#     沒有任何 scenario 會檢查用哪個套件解析參數 —— 行為相同時 harness 分不出來，由人工檢查 import。
#   - AC6：本輪必須在 BDD-001 檔案中把 scenario「Migration 可正向套用亦可回滾」標記為 superseded，
#     並指向本份文件。這是文件維護動作，harness 不會驗證它有沒有做。

Feature: 以 bun/migrate 取代手寫的 migration 機制

  Rule: dbmigrate CLI 以 bun 的指令詞彙運作

    As a 維運人員
    I want migration 指令與 bun 官方文件一致
    So that 我查到的官方資料能直接套用，不必先在心裡translate成本專案的自製詞彙

    @cli
    Scenario: init 建立 bun 的 migration 記錄資料表
      Given 一個空的 Postgres 資料庫
      When 執行 `go run ./cmd/dbmigrate init --config=test`
      Then 程序的 exit code 為 0
      And bun_migrations 資料表存在

    @cli
    Scenario: migrate 套用所有尚未套用的 migration
      Given 一個空的 Postgres 資料庫
      And 已執行過 `go run ./cmd/dbmigrate init --config=test`
      When 執行 `go run ./cmd/dbmigrate migrate --config=test`
      Then 程序的 exit code 為 0
      And properties 資料表存在
      And bun_migrations 資料表中存在一筆對應 create_properties 的紀錄

    @cli
    Scenario: 重複執行 migrate 不會出錯也不會重複套用
      Given 一個已套用完所有 migration 的資料庫
      When 再次執行 `go run ./cmd/dbmigrate migrate --config=test`
      Then 程序的 exit code 為 0
      And stdout 中包含字串 "no new migrations"
      And bun_migrations 資料表的資料筆數不變

    @cli
    Scenario: status 同時列出已套用與待套用的 migration
      Given 一個已套用完所有 migration 的資料庫
      When 執行 `go run ./cmd/dbmigrate status --config=test`
      Then 程序的 exit code 為 0
      And stdout 中包含字串 "create_properties"

    @cli @error
    Scenario Outline: 錯誤的用法會以非零 exit code 拒絕並說明原因
      When 執行 `go run ./cmd/dbmigrate <參數>`
      Then 程序的 exit code 不為 0
      And stderr 中包含字串 <訊息片段>

      Examples:
        | 參數                      | 訊息片段     |
        | frobnicate --config=test  | "frobnicate" |
        | up --config=test          | "up"         |
        | down --config=test        | "down"       |
        | migrate                   | "config"     |
        |                           | "用法"       |

  Rule: 回滾採用 bun 的 migration group 語意

    As a 維運人員
    I want rollback 把資料庫退回上一個穩定狀態，而不是一次拆光
    So that 我在正式環境誤下 rollback 時，損害範圍是可預期的一組，而不是整個 schema

    @rollback
    Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響
      Given 一個空的且已 init 的資料庫
      And 存在兩個 migration：較早的 create_properties 與較晚的一個測試用 migration
      And 已先執行 migrate 只套用 create_properties，形成第一個 group
      And 接著再執行 migrate 套用該測試用 migration，形成第二個 group
      When 執行 `go run ./cmd/dbmigrate rollback --config=test`
      Then 程序的 exit code 為 0
      And 該測試用 migration 建立的資料表不存在
      And properties 資料表**仍然存在**
      And bun_migrations 中仍保有 create_properties 的紀錄

    @rollback
    Scenario: 沒有已套用的 group 時 rollback 安全地無動作
      Given 一個已 init 但尚未套用任何 migration 的資料庫
      When 執行 `go run ./cmd/dbmigrate rollback --config=test`
      Then 程序的 exit code 為 0
      And stdout 中包含字串 "nothing to rollback"

  Rule: migration 具交易性，且既有 schema 完全不變

    As a 開發者
    I want migration 失敗時不留下半套 schema，且換機制不改變任何欄位
    So that 我可以安全重試，且 PLAN-001 建立的所有查詢與測試不會因為換機制而壞掉

    @tx
    Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema
      Given 一個已 init 的資料庫
      And 存在一個測試用的 .tx.up.sql migration，它先成功建立資料表 tx_probe，接著執行一段必定失敗的 SQL
      When 執行 `go run ./cmd/dbmigrate migrate --config=test`
      Then 程序的 exit code 不為 0
      And tx_probe 資料表不存在
      And bun_migrations 中不存在該 migration 的成功紀錄

    @regression
    Scenario: 換機制後 properties 資料表的結構與換之前完全相同
      Given 一個空的且已 init 的資料庫
      When 執行 `go run ./cmd/dbmigrate migrate --config=test`
      Then properties 資料表具備下列欄位，且型別與可空性皆符合：
        | 欄位            | 型別        | 可空 |
        | id              | uuid        | 否   |
        | city            | text        | 否   |
        | district        | text        | 否   |
        | street_address  | text        | 否   |
        | floor           | text        | 否   |
        | room_no         | text        | 否   |
        | layout          | text        | 否   |
        | area_ping       | numeric     | 否   |
        | monthly_rent    | numeric     | 否   |
        | management_fee  | numeric     | 否   |
        | deposit_months  | integer     | 否   |
        | rental_mode     | text        | 否   |
        | status          | text        | 否   |
        | landlord_name   | text        | 否   |
        | landlord_phone  | text        | 否   |
        | created_at      | timestamptz | 否   |
        | updated_at      | timestamptz | 否   |
      And 存在一個涵蓋 (city, district, street_address, floor, room_no) 的唯一索引
      And PLAN-001 既有的整合測試在不修改斷言的前提下全部通過

  Rule: 開發者可用 CLI 建立新的 migration 與排除鎖定

    As a 開發者
    I want 用指令產生 migration 檔案骨架、並在流程中斷時解除鎖定
    So that 我不必手動拼時間戳檔名，也不會因為一次中斷就卡死整個 migration 流程

    @cli
    Scenario: create_sql 產生一對成對的空白 migration 檔案
      When 執行 `go run ./cmd/dbmigrate create_sql add_tenant_table --config=test`
      Then 程序的 exit code 為 0
      And db/ 目錄下新增了兩個檔案，檔名分別以 _add_tenant_table.tx.up.sql 與 _add_tenant_table.tx.down.sql 結尾
      And 這兩個檔名的時間戳前綴相同且為 14 位數字

    @cli
    Scenario: unlock 清除遺留的 migration 鎖定
      Given 一個已 init 的資料庫
      And bun_migration_locks 資料表中存在一筆遺留的鎖定紀錄
      When 執行 `go run ./cmd/dbmigrate unlock --config=test`
      Then 程序的 exit code 為 0
      And bun_migration_locks 資料表中不存在任何鎖定紀錄
