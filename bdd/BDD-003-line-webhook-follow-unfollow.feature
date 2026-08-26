# BDD-003 — LINE Messaging API webhook：follow / unfollow 事件接入
# Created: 2026-08-26
# Status: approved
# Working Directory: /Users/jimmy/repo/chuchu2
# Source: conversation（2026-08-26 與使用者的四項決定）
#   1. 事件解析與簽章驗證引入官方 github.com/line/line-bot-sdk-go/v8
#   2. follow / unfollow 要落地：新增 line_users 表，unfollow 標記而非刪除
#   3. webhook 端點不進 api/openapi.yaml，以獨立 server.Mount 掛載
#   4. 這一輪只收不發：不呼叫 LINE 的 reply / push API
# Out of scope:
#   - 送出任何訊息給 LINE（reply message、push message、channel access token、outbound HTTP client）
#   - follow / unfollow 以外的事件型別（message、postback、join、leave、beacon…）的商業邏輯
#   - 把 webhook 端點寫進 api/openapi.yaml（本輪刻意不進 spec-first 契約）
#   - 呼叫 LINE 的 Get profile API 取得 displayName、頭像等使用者資料
#   - 事件的非同步處理／重送佇列（本輪同步處理完才回應）
#   - LINE 官方帳號的 rich menu、LIFF、audience 等其他 Messaging API 功能

Feature: 接收 LINE Messaging API 的 follow / unfollow webhook
  As a 包租代管業者
  I want 服務能接收 LINE 官方帳號的加好友與封鎖事件並記錄下來
  So that 我知道目前有哪些 LINE 使用者可以被聯繫，而歷史加好友記錄不會消失

  Background:
    Given 服務已設定 line.channel_secret 為 "test-channel-secret"
    And 服務已啟動，webhook 端點為 POST /webhooks/line

  # ---------- 簽章驗證 ----------

  @security
  Scenario: 簽章正確的 follow 事件被接受
    Given 一個 follow 事件的 webhook body，其 source.userId 為 "U0000000000000000000000000000001"
    And 請求帶有以 "test-channel-secret" 對該 body 算出的正確 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 中 "U0000000000000000000000000000001" 的記錄存在且狀態為 FOLLOWING

  @security @error
  Scenario: 簽章錯誤的請求被拒絕且不產生任何資料
    Given 一個 follow 事件的 webhook body，其 source.userId 為 "U0000000000000000000000000000002"
    And 請求帶有的 x-line-signature 是以另一組 channel secret 算出來的
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 401
    And 回應 Content-Type 開頭為 "application/json"
    And 回應 body 的 code 欄位為 "LINE_SIGNATURE_INVALID"
    And line_users 中查不到 "U0000000000000000000000000000002" 的記錄

  @security @error
  Scenario: 缺少 x-line-signature header 的請求被拒絕
    Given 一個 follow 事件的 webhook body，其 source.userId 為 "U0000000000000000000000000000003"
    And 請求完全沒有帶 x-line-signature header
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 401
    And 回應 body 的 code 欄位為 "LINE_SIGNATURE_INVALID"
    And line_users 中查不到 "U0000000000000000000000000000003" 的記錄

  @security @error
  Scenario: 簽章正確但 body 不是合法 JSON 時回 400
    Given 一段不是合法 JSON 的 body "{"
    And 請求帶有以 "test-channel-secret" 對該 body 算出的正確 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 400
    And 回應 body 的 code 欄位為 "VALIDATION_FAILED"

  # ---------- LINE 平台的連線確認 ----------

  Scenario: LINE 主控台的連線確認請求（events 為空陣列）回 200
    Given 一個 events 為空陣列 [] 的 webhook body
    And 請求帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 的總筆數與請求前相同

  # ---------- follow / unfollow 落地 ----------

  Scenario: unfollow 事件把好友標記為封鎖而不是刪除記錄
    Given "U0000000000000000000000000000010" 已經是狀態 FOLLOWING 的好友
    And 一個該 userId 的 unfollow 事件，帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 中 "U0000000000000000000000000000010" 的記錄仍然存在
    And 該記錄的狀態為 BLOCKED

  Scenario: 封鎖後重新加好友會讓同一筆記錄回到 FOLLOWING，不會產生第二筆
    Given "U0000000000000000000000000000011" 的記錄狀態為 BLOCKED
    And 一個該 userId、時間戳比封鎖事件更新的 follow 事件，帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 中 userId 為 "U0000000000000000000000000000011" 的記錄恰好有 1 筆
    And 該記錄的狀態為 FOLLOWING

  Scenario: 一次 webhook 帶多個事件時全部都會被處理
    Given 一個 webhook body，events 依序為 "U0000000000000000000000000000020" 的 follow 與 "U0000000000000000000000000000021" 的 follow
    And 請求帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 中 "U0000000000000000000000000000020" 的狀態為 FOLLOWING
    And line_users 中 "U0000000000000000000000000000021" 的狀態為 FOLLOWING

  # ---------- 重送與亂序 ----------

  Scenario: 同一個事件被重送兩次仍然只有一筆記錄
    Given 一個 follow 事件的請求，其 source.userId 為 "U0000000000000000000000000000030"，帶有正確的 x-line-signature
    When LINE 平台把完全相同的請求連續 POST 到 /webhooks/line 兩次
    Then 兩次回應狀態碼都是 200
    And line_users 中 userId 為 "U0000000000000000000000000000030" 的記錄恰好有 1 筆
    And 該記錄的狀態為 FOLLOWING

  Scenario: 亂序抵達的舊 unfollow 事件不會覆蓋較新的 follow 狀態
    Given "U0000000000000000000000000000031" 的記錄狀態為 FOLLOWING，且最後套用的事件時間戳為 2000
    And 一個該 userId、時間戳為 1000 的 unfollow 事件，帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 中 "U0000000000000000000000000000031" 的狀態仍然為 FOLLOWING

  # ---------- 其他事件型別 ----------

  Scenario Outline: 非 follow／unfollow 的事件被忽略但仍回 200
    Given 一個 events 只包含一個 "<事件型別>" 事件的 webhook body
    And 請求帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 200
    And line_users 的總筆數與請求前相同

    Examples:
      | 事件型別 |
      | message  |
      | postback |
      | join     |

  # ---------- 失敗路徑 ----------

  @error
  Scenario: 資料庫寫入失敗時回 500，讓 LINE 有機會重送
    Given webhook 端點背後的 line_users 儲存層在寫入時會回傳錯誤
    And 一個 follow 事件的請求，帶有正確的 x-line-signature
    When LINE 平台 POST 該請求到 /webhooks/line
    Then 回應狀態碼為 500
    And 回應 Content-Type 開頭為 "application/json"
    And 回應 body 的 code 欄位為 "INTERNAL"

  # ---------- 設定 ----------

  @config
  Scenario: 缺少 line.channel_secret 時服務啟動失敗並指出缺少的 key
    Given 一份沒有 line.channel_secret 的設定，且環境變數也沒有提供
    When 載入該設定
    Then 載入失敗並回傳 MissingKeyError
    And 錯誤訊息包含字串 "line.channel_secret"

  # ---------- 分層邊界 ----------

  @architecture
  Scenario: LINE 領域層不認得 bun、net/http 與 LINE SDK
    Given 專案已完成 LINE webhook 的接入
    When 檢查 internal/line 這個套件（不含其子套件）的 import 清單
    Then 清單中不含 "github.com/uptrace/bun"
    And 清單中不含 "net/http"
    And 清單中不含任何 "github.com/line/line-bot-sdk-go" 開頭的套件
