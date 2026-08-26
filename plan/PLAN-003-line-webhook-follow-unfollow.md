# PLAN-003 — LINE Messaging API webhook：follow / unfollow 事件接入
Created: 2026-08-26
Status: draft
Working Directory: /Users/jimmy/repo/chuchu2
BDD Spec: /Users/jimmy/repo/chuchu2/bdd/BDD-003-line-webhook-follow-unfollow.feature
Language: Go 1.26（module `github.com/yongde2900/chuchu2`，相依 vendored 於 `vendor/`）
Build cmd: go build ./...
Test cmd:  go test -race -count=1 ./...
Lint cmd:  go vet ./...
Pass threshold: 8.0
Max iterations: 5

## Known Context

執行本計畫的人**沒有規劃階段的記憶**，只會讀到這份 plan 與 BDD spec。因此以下每一條都寫成可獨立
閱讀的完整敘述，`[[slug]]` 只是給日後追溯用的來源標記，不是「請去別處讀」的指路。知識庫入口是
`knowledge/index.md`，但那是卡住時的救援路徑，不是本文件的替代品。

chuchu2 是**包租代管**服務（台灣租賃住宅管理業）。Go 1.26，module `github.com/yongde2900/chuchu2`。

### A. 分層邊界＝套件邊界（來源：[[property-service-layering]]）

分層邊界刻意做成**套件邊界**，違規會表現成「錯的套件出現了錯的 import」，肉眼掃 import 區塊就看得到。
**這些約束目前沒有任何自動強制**（本輪的 scenario 14 是第一個例外，見 Task 2），要靠人記得驗。
既有的 property feature 是本輪要照抄的樣板：

- `internal/property/` —— 純領域＋service＋`Repository` 介面，**不 import bun 也不 import net/http**。
- `internal/property/pgrepo/` —— 唯一碰 bun 的地方，`propertyModel` 的欄位對齊 migration。
- `internal/property/httpapi/` —— 只做「產生的 api 型別 ↔ 領域型別」轉換，**連 net/http 都不 import**。
- `internal/apihttp/` —— 產生的 HTTP 層與 apperr 之間的唯一接線點，**不 import 任何 feature 套件**。
- `internal/server/` —— chi router 組裝，**不 import 任何 feature 套件**；feature 透過
  `server.Mount func(r chi.Router)` 自己把路由掛上來。
- `internal/app/` —— **唯一的組裝點**（`app.NewHandler(app.Deps) http.Handler`），手寫建構子，
  不用 DI 框架、不用全域變數、不用 `init()`。它因此會 import 一堆 feature 套件，**這是設計**，
  驗證分層時要把它排除。

三個依賴反轉點：`property.Repository`、`health.Checker`、`server.Mount`。本輪會新增第四個：
`line.Repository`。

**組裝不放在 `cmd/api`**（來源：[[in-process-integration-tests]]）：`package main` 無法被 import，
整合測試就只能開子行程，而子行程裡的 handler 設不了中斷點。`cmd/api` 只保留行程層面職責：
設定載入、開連線、signal、優雅關閉。

### B. 錯誤一律走中介層（來源：[[unified-error-middleware]]、[[response-safety-net]]）

`internal/apperr` 定義全域唯一的 `*apperr.Error`：

```go
type Code string
type FieldError struct{ Field, Reason string }
type Error struct{ Code Code; Message string; Details []FieldError; err error } // err 未匯出
func New(code Code, msg string) *Error
func Wrap(code Code, msg string, err error) *Error
func Validation(details ...FieldError) *Error
func HTTPStatus(code Code) int          // code → HTTP 狀態碼的唯一映射點；未知 code → 500
func (e *Error) WithError(err error) *Error      // 一律回傳複本
func (e *Error) WithMessage(msg string) *Error   // 一律回傳複本
func (e *Error) WithDetails(d ...FieldError) *Error // 一律回傳複本
func (e *Error) Is(target error) bool   // 只比對 Code
```

現有 sentinel：`apperr.ValidationFailed`、`NotFound`、`Duplicate`、`InvalidStatusTransition`、
`Internal`。**sentinel 被所有請求併發共用，所以 `With*` 一律回傳複本**（`clone()` 連 `Details` 的
backing array 都另外配置）——就地修改會造成跨請求汙染。`(*Error).Is` 只比對 `Code`，沒有它
`errors.Is(derived, sentinel)` 會因為 `With*` 回傳新指標而一律 false。

`internal/httpx` 提供：`httpx.WriteError(w, r, logger, err)`（**抽不出 `*apperr.Error` 就降級成
INTERNAL 500，原始訊息只進 log 不外洩**）、`httpx.ErrorBody`（錯誤回應的 wire 形狀，
**不是**產生的 `api.ErrorBody`；欄位 `code` / `message` / `request_id` / `details`）、
`httpx.WriteJSON`、`httpx.RequestID`、`httpx.Recoverer`、`httpx.EnsureJSONError`。

`server.NewRouter(opts, mounts...)` 的 middleware 順序固定
`RequestID → access log → EnsureJSONError → Recoverer`，三個理由缺一不可：RequestID 最外層才拿得到
id；Recoverer 最內層 panic 才會先變 500 讓 access log 記到；EnsureJSONError 包在 Recoverer 外層，
**任何路由**漏接錯誤處理都會被就地改寫成統一 JSON，純文字不可能外洩。
**這一點對本輪很重要：新的 webhook 路由即使不走產生的 api 層，只要透過 `server.Mount` 掛進
`NewRouter`，就自動享有這四層防護，不需要（也不該）自己再包一層。**

### C. HTTP 層是 spec-first 產生的，但本輪刻意不碰它（來源：[[spec-first-codegen-replaces-contract-test]]）

`api/openapi.yaml` → `go generate ./api/...` → `api/api.gen.go`。`api.gen.go` **絕對不得手動編輯**且
必須提交進版控；`test/codegen_test.go` 會逐位元組比對，spec 與程式碼分岔就轉紅。
**本輪的 webhook 端點刻意不進 spec**（使用者決定，見 §G），因此 `api/openapi.yaml` 與
`api/api.gen.go` 這次都不該被改動一個位元組，`codegen_test` 必須保持綠燈。

### D. Migration 用 bun/migrate（來源：[[bun-migrate-adoption]]、[[go-embed-no-parent-dir]]）

- **檔名必須是 `<14位時間戳>_<name>.tx.up.sql` / `.tx.down.sql`** —— bun 只在檔名含 `.tx.` 時才
  `BeginTx`，少了它會**無聲地**失去交易保護。
- `db.NewMigrator(bunDB)` 是唯一建構點（CLI 與 `test/` 的 `TestMain` 共用），已設
  `WithMarkAppliedOnSuccess(true)`。
- `db/embed.go` 用 `//go:embed *.sql` 嵌入**同目錄**的 `.sql`，因為 **`go:embed` 的 pattern 不允許
  `..`**，migration SQL 只能放在 `db/` 這個 package 自己的目錄裡。
- 既有樣板：`db/20260819120000_create_properties.tx.up.sql` / `.tx.down.sql`。新的 migration 時間戳
  必須**大於** `20260819120000`，否則排序會亂。
- 單一敘述的 migration **不需要**加 `--bun:split`。
- `rollback` 只回滾**最後一個 group**，不是全部。需要乾淨資料庫的測試只能用專屬容器。
- `test/` 的 `TestMain` 會對共用 Postgres 跑一次 `Init` + `Migrate`，所以本輪新增的 migration
  **會自動套用到共用容器**，同 package 的整合測試直接就有 `line_users` 可用。

### E. 設定用 viper（來源：[[viper-env-override-needs-bindenv]]）

`internal/config.Load(name)` 讀 `config/<name>.yaml`（相對路徑 `config/`，所以測試要 chdir），
可由 `CHUCHU_` 前綴環境變數覆寫（`.` 換 `_`，例如 `CHUCHU_POSTGRES_DSN`）。檔內有兩份清單：

- `requiredKeys` —— 缺了就回 `*config.MissingKeyError`，訊息一定包含 key 全名。**依序檢查，
  回報第一個缺少的 key。**
- `knownKeys` —— 逐一 `BindEnv`。**新增設定 key 必須同時加進 `knownKeys` 並明確 `BindEnv`**：
  viper 的 `AutomaticEnv` 對「只存在於環境變數、不存在於 yaml」的 key，`IsSet`／`Unmarshal`
  有時抓不到。

`config/test.yaml` 是測試用設定（`StartAPI`、`dbmigrate` 測試都用 `--config=test`）；
`config/broken.yaml` 是**刻意缺 `postgres.dsn`** 的設定，`test/startup_test.go` 斷言啟動失敗的
stderr 含字串 `"postgres.dsn"`。

### F. 測試佈局兩層（來源：[[test-layout-two-tiers]]、[[testcontainers-shared-vs-dedicated]]）

- **單元測試貼著程式碼放**（`<被測檔>_test.go`，同目錄同 package），**不得碰 Docker**，
  必須能在一台沒有 Docker 的機器上跑過。
- **整合測試一律在 `test/`（`package test`）**，一個 feature 面向一個檔，打真實 HTTP 驗證，
  不直接呼叫內部函式。預設用 `startInProcessAPI(t, dsn, redisAddr) (baseURL string, output func() string)`
  ——它用 `app.NewHandler(...)` 組出 handler 再包 `httptest.NewServer`，**在行程內**起服務，
  這樣 `dlv test ./test/ -- -test.run TestXxx` 的中斷點才跟得進 handler → service → repo。
  子行程（`testsupport.StartAPI`）只留給**行程層面的行為**，目前僅 `test/startup_test.go` 需要。
- ⚠️ `test/` 的 `TestMain` 起**一組共用容器**（`sharedPostgres()`、`sharedRedis()` 取得連線資訊）。
  **需要乾淨資料庫或要停容器的測試，必須自己起專屬容器（`testsupport.StartPostgres(t)`），
  絕不可動共用容器** —— 動了會讓同 package 後續測試連鎖失敗，而且錯誤訊息會指向無辜的測試。
- 本專案的測試**一律不用 `t.Parallel()`**。
- `-count=1` 是必要的：Go 會快取測試結果，容器背後的測試從快取回綠會騙人。

### G. 本輪已由使用者拍板的七個設計決定（是決定，不是建議）

1. **用官方 `github.com/line/line-bot-sdk-go/v8` 做事件解析與簽章驗證**，不手寫 HMAC。
2. **follow / unfollow 要落地到新的 `line_users` 資料表；unfollow 標記而非刪除**（保留歷史，
   領域上「曾經是好友」是有價值的資訊）。
3. **webhook 端點不進 `api/openapi.yaml`**，用獨立的 `server.Mount` 掛載。
4. **這一輪只收不發**，不呼叫 LINE 的 reply / push API。
5. **簽章驗不過回 401**（不是 SDK 預設的 400），新增 apperr code `LINE_SIGNATURE_INVALID`；
   簽章對但 body 壞掉回 400 `VALIDATION_FAILED`。理由：本專案錯誤一律走
   `apperr.Code` → 狀態碼的單一映射點，而「你不是 LINE」語意上是 401。
6. **`line_users` 要存「最後套用的事件時間戳」**，用來擋亂序重送：時間戳比記錄上更舊的事件
   **不得改變狀態**。這是 BDD scenario 10 存在的唯一理由，**不要把它優化掉**。
7. **設定新增 key `line.channel_secret`，且它是 required key。**

### H. LINE SDK 的實際 API（已對照 `line-bot-sdk-go/v8@v8.22.0` 原始碼確認，不要憑記憶改寫）

package 是 `github.com/line/line-bot-sdk-go/v8/linebot/webhook`。**這個模組沒有任何外部相依**
（它的 `go.mod` 只有 `module` 與 `go 1.25` 兩行），vendor 起來很乾淨。

```go
// 讀 r.Body（io.ReadAll + defer Close），用 raw bytes 做 HMAC-SHA256（key 為 channel secret）
// 比對 r.Header.Get("x-line-signature")（base64 解碼後 hmac.Equal）；
// 驗不過回 ErrInvalidSignature；驗過才 json.Unmarshal，失敗時回
// fmt.Errorf("failed to unmarshal request body: %w, %s", err, body) 包住的錯誤。
func ParseRequest(channelSecret string, r *http.Request) (*CallbackRequest, error)

var ErrInvalidSignature = errors.New("invalid signature")

// 測試要自己算正確簽章時可以用它驗，但產生簽章請自己用 crypto/hmac + crypto/sha256 + encoding/base64。
func ValidateSignature(channelSecret, signature string, body []byte) bool

type CallbackRequest struct {
    Destination string
    Events      []EventInterface   // 元素是介面，用 type switch 取具體型別
}

type FollowEvent struct {
    Event
    Source          SourceInterface
    Timestamp       int64            // ⚠️ 毫秒的 Unix 時間
    Mode            EventMode
    WebhookEventId  string
    DeliveryContext *DeliveryContext
    ReplyToken      string
    Follow          *FollowDetail
}

type UnfollowEvent struct {   // ⚠️ 沒有 ReplyToken
    Event
    Source          SourceInterface
    Timestamp       int64
    Mode            EventMode
    WebhookEventId  string
    DeliveryContext *DeliveryContext
}

type FollowDetail struct{ IsUnblocked bool }

type UserSource struct{ /* ... */ UserId string }   // ⚠️ UserId，不是 UserID
```

**必須知道的四個事實：**

1. **「簽章錯」與「JSON 壞」是兩個可區分的分支**：`errors.Is(err, webhook.ErrInvalidSignature)`
   為真就是簽章錯（→ 401），否則就是解析錯（→ 400）。這正是 scenario 2/3 與 scenario 4 的分界。
   `x-line-signature` 完全缺席時 `ValidateSignature` 對空字串 base64 解出空 bytes、比對失敗，
   一樣回 `ErrInvalidSignature`，所以缺 header 與簽章錯走同一條路（都是 401）。
2. **`events` 為空陣列時 `Events` 是長度 0 的 slice**（body 完全沒有 `events` 鍵時是 nil），
   兩種都必須當成「沒有事件」處理並回 200。
3. **source 要用 `webhook.SourceInterface` 的 type switch 取出 `*webhook.UserSource` 才有
   `UserId`**；source 也可能是 group / room（那時沒有 UserId 或 UserId 為空），這種事件必須略過，
   不可寫出 `line_user_id = ''` 的記錄。
4. **未知的事件型別不會讓 unmarshal 失敗**：`UnmarshalEvent` 的 default 分支回傳
   `UnknownEvent{Type, Raw}`。已知型別（message / postback / join）會 unmarshal 成各自的具體型別，
   而且各欄位都是「有才解析」，所以測試 body 只寫 `{"type":"message", ...}` 而省略 `message` 欄位
   也能解析成功。這三種在我們的 type switch 落到 default，被安靜略過。

**⚠️ 兩個會踩到的陷阱：**

- **`webhook` 這個 package 自己就 import 了 `net/http`**（同 package 內有 `httphandler.go`）。
  所以任何 import 它的套件都會傳遞性地帶進 net/http —— 這正是 scenario 14 要求 `internal/line`
  領域層**不得** import SDK 的原因。**SDK 型別只能出現在 transport 子套件裡。**
- SDK 也提供 `webhook.WebhookHandler`（`NewWebhookHandler` + `HandleEvents` / `HandleError`），
  但它自己寫 400 / 500 狀態碼，繞過本專案的 `httpx.WriteError` / `apperr` 映射。
  **不要用 `WebhookHandler`，直接用 `ParseRequest`**，錯誤照專案慣例轉成 `*apperr.Error`
  再交給 `httpx.WriteError`。

加相依的指令：`go get github.com/line/line-bot-sdk-go/v8@v8.22.0` 然後 `go mod tidy && go mod vendor`。
module cache 已經有 v8.22.0，網路可用。

### I. bun 的 upsert 有一個會讓 WHERE 找不到欄位的陷阱（規劃階段實際讀 vendor 原始碼確認）

亂序守門要靠 `INSERT ... ON CONFLICT ... DO UPDATE ... WHERE` 在**單一敘述**內完成。
用 bun 的 `NewInsert().Model(m).On("CONFLICT (line_user_id) DO UPDATE").Set(...).Where(...)` 時：

- `vendor/github.com/uptrace/bun/query_insert.go:211` —— pgdialect 有 `feature.InsertTableAlias`，
  **只要 `On(...)` 有設，bun 就會把 INSERT 寫成 `INSERT INTO "line_users" AS "<alias>"`**。
- `vendor/github.com/uptrace/bun/schema/table.go:86-90` —— **alias 預設是「結構型別名的
  underscore 形式」**，不是資料表名。`type lineUserModel struct{ bun.BaseModel
  \`bun:"table:line_users"\` }` 的 alias 會是 `line_user_model`。
- 於是 `Where("line_users.last_event_at <= EXCLUDED.last_event_at")` 會在執行期爆
  `missing FROM-clause entry for table "line_users"`。

**因此模型的 tag 必須明確寫成 `bun:"table:line_users,alias:line_users"`**，WHERE 才對得上；
或者整句改用 `db.NewRaw(...)` 手寫 SQL（也可接受，但要自己處理參數綁定）。
`appendOn` 會把 `where` 接在 `DO UPDATE SET ...` 之後，語意正確。

**另一個要點：守門條件不成立時 Postgres 不會報錯，只是 0 rows affected。**
`Upsert` 的實作**絕不可**把 `RowsAffected() == 0` 當成錯誤 —— 那正是「舊事件被正確略過」的樣子。

### J. 其他已沉澱的 gotcha

- **空列表／空物件回應必須是 `[]` 不是 `null`**（[[nil-slice-marshals-to-null]]）：nil slice 會
  marshal 成 `null`，而且**反序列化後的斷言抓不到，要看原始 bytes**。（本輪 webhook 回應沒有 body，
  但別破壞既有行為。）
- **middleware／路由掛在哪裡，需要它自己的測試來守**（[[middleware-wiring-needs-its-own-test]]）：
  刪掉那行接線若測試全綠，就是缺一個接線測試。本輪 webhook 路由的接線由整合測試（走
  `app.NewHandler`）守住。
- **兩個同名的匯出型別無法一起匿名內嵌**（[[cannot-embed-two-same-named-types]]）：`API redeclared`。
  本輪會出現第二個叫 `pgrepo` 的套件（`internal/line/pgrepo`），`internal/app` 同時 import 兩個
  `pgrepo` 時必須給其中一個 import 別名（建議 `linepgrepo`）。
- **改動 `go.mod` 後必須 `go mod tidy && go mod vendor`**，否則 `go build ./...` 會抱怨
  `modules.txt` 與 `go.mod` 不同步。
- **TDD 之後編輯器診斷會回報早已修好的錯誤**（[[stale-gopls-diagnostics-during-tdd]]）：
  編譯器是權威，`go build ./...` 說了算。
- 金額一律 `decimal.Decimal`（本輪用不到，但別破壞既有）。

### Out of Scope & Ungraded Constraints

以下每一條都是**決定**，不是遺漏。沒有讀到這一段的執行者會「很有幫助地」把它們做出來，那是錯的。

- **不送出任何訊息給 LINE。** reply message、push message、channel access token、對外的 HTTP
  client 全部不做 —— 這一輪只收不發，缺少發訊息能力是決定不是疏漏。
- **不處理 follow / unfollow 以外事件型別的商業邏輯。** message、postback、join、leave、beacon
  等只需要被安全略過並回 200，不得為它們新增資料表、欄位或分支邏輯。
- **不把 webhook 端點寫進 `api/openapi.yaml`。** 本輪刻意不進 spec-first 契約，
  `api/openapi.yaml` 與 `api/api.gen.go` 都不該被改動，`test/codegen_test.go` 必須保持綠燈。
- **不呼叫 LINE 的 Get profile API。** `line_users` 不存 displayName、頭像等使用者資料，
  這一輪只有 userId、狀態與時間戳。
- **不做事件的非同步處理或重送佇列。** 本輪同步處理完才回應：所有事件寫完才回 200，
  任何一個寫入失敗就回 500 讓 LINE 重送。
- **不做 LINE 官方帳號的 rich menu、LIFF、audience 等其他 Messaging API 功能。**
- **未評分（UNGRADED，沒有 scenario 抓得到，靠人審查）：** 事件解析與簽章驗證**必須**用官方
  `github.com/line/line-bot-sdk-go/v8`，不得改成手寫 HMAC —— 兩者行為上無法區分，測試分不出來。
- **未評分（UNGRADED，沒有 scenario 抓得到，靠人審查）：** webhook 端點**必須**以獨立的
  `server.Mount` 掛載，不得寫進 `api/openapi.yaml` —— 端點能通就是能通，spec 裡有沒有它
  scenario 分不出來。
- **未評分（UNGRADED）：** 不得使用 SDK 的 `webhook.WebhookHandler`（它自己寫狀態碼、繞過
  `httpx.WriteError`）。行為上部分可觀測、但錯誤 body 形狀不一定被抓到，由人工檢查。
- **未評分（UNGRADED）：** 程式碼註解與產品術語一律使用**繁體中文**，且只寫 signature 表達不出來的
  東西（見 Project Conventions）。
- **未評分（UNGRADED）：** 不引入 DI 框架、不新增全域變數、不用 `init()`；組裝點仍只有
  `internal/app.NewHandler` 一處。
- **未評分（UNGRADED）：** Lint 仍只有 `go vet ./...`，不得引入 golangci-lint 或新增 `.golangci.yml`。

## Project Conventions

- **錯誤處理用 `errors.AsType[T](err)`**（Go 1.26 的泛型版本），不要用 `errors.As(&target)`。
- 每個碰 I/O 的函式**第一個參數是 `ctx context.Context`**，而且必須真的傳下去。
- **金額一律 `decimal.Decimal`**，JSON 往返固定兩位小數字串（`StringFixed(2)`）。
- **空列表回應必須是 `[]` 不是 `null`。**
- CLI 形狀：`main()` 只有 `os.Exit(run(...))`，邏輯在 `run` 裡回傳 exit code。錯誤寫 stderr，
  成功寫 stdout。（本輪應該用不到。）
- 設定用 viper，`--config=<name>` 對應 `config/<name>.yaml`，可由 `CHUCHU_` 前綴環境變數覆寫；
  **新增設定 key 必須明確 `BindEnv`**。
- **測試一律不用 `t.Parallel()`。**
- **改動 `go.mod` 後必須 `go mod tidy && go mod vendor`。**
- **註解只寫 signature 表達不出來的東西。** 判斷方式：把這段註解刪掉，讀者會不會因此做錯決定？
  不會 → 刪掉。
  - **刪**：覆述 signature 或程式碼的、列舉常數的同義反覆、指向 task／計畫文件的施工鷹架、
    已經過時的。（`// Valid 回報 s 是否為合法的 Status 列舉值` 掛在 `func (s Status) Valid() bool`
    上 —— signature 已經說完了。）
  - **留**：為什麼選這個做法、有什麼約束、會無聲失敗的陷阱、讀不出來的領域語意、
    「這是唯一來源，不要自己另外判斷」這類指路。
  - 註解與產品術語一律**繁體中文**；匯出的識別字依 Go 慣例以名稱開頭。
  - 完整理由見 `knowledge/conventions/comment-only-what-signature-cannot-say.md`。
  - **⚠️ 這一條是最容易被扣分的地方**：寫了一堆「這個函式做什麼」的註解會直接壓低評分。

## Overview

本輪替 chuchu2 接上 LINE Messaging API 的 webhook，收下 follow / unfollow 事件並落地到新的
`line_users` 資料表：加好友 → `FOLLOWING`，封鎖 → `BLOCKED`（**標記而非刪除**，保留歷史），
同一個 userId 永遠只有一筆記錄，重送等冪，亂序抵達的舊事件不得覆蓋較新的狀態。簽章驗證與事件解析
一律交給官方 SDK 的 `webhook.ParseRequest`，簽章錯回 401 `LINE_SIGNATURE_INVALID`、body 壞回 400
`VALIDATION_FAILED`、寫入失敗回 500 `INTERNAL`（讓 LINE 重送），全部走既有的
`apperr` → `httpx.WriteError` 中介層。端點是 `POST /webhooks/line`，**刻意不進 `api/openapi.yaml`**，
以獨立的 `server.Mount` 掛進 `server.NewRouter`，因此自動享有
`RequestID → access log → EnsureJSONError → Recoverer` 這四層防護。

四個 sub-task 依序是：設定新增 required key `line.channel_secret` → `internal/line` 純領域層
（型別、`Repository` 契約、`Service`）→ 垂直切片打通（`line_users` migration ＋ `internal/line/pgrepo`
＋ `internal/line/webhookhttp` transport ＋ `internal/app` 接線 ＋ apperr 新 code）→ 事件語意的端到端
驗證（unfollow 標記、重加好友、多事件、重送、亂序）。

**為什麼是這個順序：** Task 1 與 Task 2 都是葉節點（一個只動 `internal/config`，一個只動
`internal/line` 且不 import 專案內任何東西），可以在完全不影響既有測試的前提下先落地；
Task 3 是唯一同時碰資料庫、SDK 與組裝點的 task，需要前兩者的介面都定案；Task 4 只加整合測試
（必要時修 Task 3 的 SQL），把最容易無聲出錯的三件事——標記而非刪除、去重、亂序守門——放到
一次獨立的評分循環裡，而不是混在「端點能不能通」裡面一起判。

**為什麼把亂序守門放在 repository 的 SQL 而不是 domain service：** service 若要自己判斷就得先
`Get` 再 `Upsert`，兩次來回之間有競態窗口（LINE 會併發重送同一批事件）。`INSERT ... ON CONFLICT
DO UPDATE ... WHERE excluded.last_event_at >= line_users.last_event_at` 在單一敘述內完成，沒有窗口。
代價是這條規則變成 `line.Repository` 介面的**契約**而非可單元測試的純函式，因此 Task 2 必須把它
寫進介面註解（那正是「signature 表達不出來的東西」），Task 4 用整合測試守住它。

**Pass threshold 的取捨：** header 設 **8.0**（高於專案預設 7.0）——本輪同時包含**簽章驗證**
（認證路徑：錯放一個 `errors.Is` 分支就會讓「簽章錯」與「body 壞」互換狀態碼，或更糟，讓沒簽章的
請求被接受）與一支**新的 migration**（schema 一旦上線就難改），這兩類缺陷都是「當下看起來會動、
出事時代價極高」。**Task 3 再往上覆寫為 8.5**：它一個人同時扛簽章驗證的三條分支、upsert 守門的
SQL、migration 與唯一的組裝點改動，是全計畫風險最集中的一格。其餘 task 維持 8.0。
Max iterations 用預設 **5**。

## Sub-Tasks

### Task 1: 設定新增 required key `line.channel_secret`
Status: pending
Directory: internal/config, config
Depends on: none
Provides (public interface):
```go
package config // internal/config

type LineConfig struct {
    ChannelSecret string `mapstructure:"channel_secret"`
}

type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    Postgres PostgresConfig `mapstructure:"postgres"`
    Redis    RedisConfig    `mapstructure:"redis"`
    Log      LogConfig      `mapstructure:"log"`
    Line     LineConfig     `mapstructure:"line"`   // 新增
}

// requiredKeys 追加 "line.channel_secret"；knownKeys 也追加同一個 key（BindEnv 才會生效，
// 環境變數名為 CHUCHU_LINE_CHANNEL_SECRET）。
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: 缺少 line.channel_secret 時服務啟動失敗並指出缺少的 key

實作要求：

- `config/test.yaml` 必須加上（值就是 BDD Background 指定的那個，後續整合測試會拿它算簽章）：
  ```yaml
  line:
    channel_secret: "test-channel-secret"
  ```
- **⚠️ `line.channel_secret` 必須加在 `requiredKeys` 的 `postgres.dsn` 之後。**
  `Load` 依序檢查、回報**第一個**缺少的 key；`config/broken.yaml` 同時缺 `postgres.dsn` 與
  `line.channel_secret`，而 `test/startup_test.go` 斷言 stderr 含 `"postgres.dsn"`。順序放錯會讓
  那個既有測試轉紅，而且錯誤訊息看起來完全無關。**`config/broken.yaml` 本身不要改。**
- **既有的三個會建立臨時 yaml 的 config 單元測試會因為新的 required key 而失敗，必須一併補上
  `line.channel_secret`**：`TestLoad_ValidConfig`、`TestLoad_EnvOverride`、
  `TestLoad_EnvOverrideSatisfiesMissingKey`（最後這個也可以改成用 `CHUCHU_LINE_CHANNEL_SECRET`
  環境變數提供，順便多驗一條 BindEnv 生效的路徑）。
- 新增單元測試對應本 scenario：臨時 yaml 有 `server.port` / `postgres.dsn` / `redis.addr`
  但**沒有** `line.channel_secret`、且環境變數也沒提供時，`Load` 回傳的錯誤要
  `errors.AsType[*MissingKeyError]` 取得成功、`Key == "line.channel_secret"`，且
  `err.Error()` 含字串 `"line.channel_secret"`。
- 交付時 `go build ./...`、`go vet ./...` 與整個測試套件必須維持綠燈（`cmd/api` 這時還不會用到
  新欄位，那是 Task 3 的事——多一個沒人讀的欄位不會讓任何測試失敗）。

---

### Task 2: `internal/line` 領域層 —— 型別、Repository 契約與 Service
Status: pending
Directory: internal/line
Depends on: none
Provides (public interface):
```go
package line // internal/line —— 純領域層：不 import bun、不 import net/http、不 import LINE SDK

type Status string
const (
    StatusFollowing Status = "FOLLOWING"
    StatusBlocked   Status = "BLOCKED"
)
func (s Status) Valid() bool

type EventType string
const (
    EventTypeFollow   EventType = "FOLLOW"
    EventTypeUnfollow EventType = "UNFOLLOW"
)

// Event 是 transport 層轉譯後的領域事件。OccurredAtMillis 直接沿用 LINE 的毫秒時間戳，
// 不轉成 time.Time：它在領域中的唯一用途是比大小擋亂序。
type Event struct {
    UserID           string
    Type             EventType
    OccurredAtMillis int64
}

// Status 是本事件套用後該有的狀態：FOLLOW → FOLLOWING、UNFOLLOW → BLOCKED。
func (e Event) Status() Status

type User struct {
    UserID            string
    Status            Status
    LastEventAtMillis int64
    CreatedAt         time.Time
    UpdatedAt         time.Time
}

// Repository 是領域層對持久化的唯一出口；實作在 pgrepo 子套件。
type Repository interface {
    // Upsert 以 UserID 為鍵寫入 u，實作必須同時滿足三件事：
    //  1. 沒有該 UserID 的記錄時新增一筆。
    //  2. 已有記錄且 u.LastEventAtMillis >= 既有值時，覆寫狀態與時間戳，絕不新增第二筆。
    //  3. 已有記錄且 u.LastEventAtMillis < 既有值時（亂序抵達的舊事件），整筆保持不變，
    //     且不算錯誤。
    // 第 3 點是擋住亂序重送的唯一防線，而且必須在單一 SQL 敘述內完成——先 SELECT 再 UPDATE
    // 的兩次來回之間有競態窗口，LINE 會併發重送同一批事件。
    Upsert(ctx context.Context, u *User) error
}

type Service struct{ /* repo Repository */ }
func NewService(repo Repository) *Service

// Handle 依抵達順序逐一套用 events；任一事件寫入失敗即中止並回傳該錯誤，
// 由 HTTP 層轉成 500 讓 LINE 重送整批。本輪刻意沒有「部分成功」的語意。
func (s *Service) Handle(ctx context.Context, events []Event) error
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: LINE 領域層不認得 bun、net/http 與 LINE SDK

實作要求：

- 這個 task 只新增 `internal/line/` 底下的檔案，不動任何既有檔案。建議檔案切法：
  `line.go`（型別與 Status／Event）、`service.go`（`Repository` 介面與 `Service`）。
- `Handle` 產生的 `User` 的 `CreatedAt` / `UpdatedAt` 用 `time.Now().UTC()`。
  **不要為了可測性引入 Clock 介面** —— 本專案既有的 `property.Service` 也是直接取
  `time.Now()`，測試策略走端到端，多一層抽象是負債（見 Known Context §A 的既有取捨）。
- `Handle` 收到空 slice 或 nil 時必須是安全的 no-op（一次 repo 呼叫都不發），這是
  「LINE 主控台連線確認請求」那條 scenario 的地基。
- 單元測試（`internal/line/*_test.go`，**不得碰 Docker**）用一個手寫的假 `Repository`
  （記錄收到的 `*User`、可設定回傳錯誤）覆蓋：狀態映射、`Handle` 依序呼叫 `Upsert`、
  空事件不呼叫、第一個錯誤即中止且原樣回傳（能用 `errors.Is` 比對到原錯誤）。
- **scenario 14 的守門測試**：新增 `internal/line/layering_test.go`（`package line`），
  用標準庫 `go/build` 的 `build.ImportDir(".", 0)` 取得本套件的 `Imports`，斷言其中
  **不含** `github.com/uptrace/bun`、**不含** `net/http`、**也不含任何** 以
  `github.com/line/line-bot-sdk-go` 開頭的路徑（用 `strings.HasPrefix` 判斷，別只比對完整字串）。
  失敗訊息要印出違規的 import 路徑，否則除錯時只知道「有東西不對」。
  這個測試不需要 Docker，也不開子行程。
  人工驗證的等價指令（給評審用）：
  ```bash
  go list -f '{{join .Imports "\n"}}' ./internal/line | grep -E 'uptrace/bun|^net/http$|line-bot-sdk-go'
  ```
  應該沒有任何輸出。
- 交付時 `go build ./...`、`go vet ./...` 與整個測試套件必須綠燈。

---

### Task 3: 垂直切片打通 —— `line_users` migration、pgrepo、webhook transport 與組裝
Status: pending
Directory: db, internal/line/pgrepo, internal/line/webhookhttp, internal/apperr, internal/app, cmd/api, test
Depends on: Task 1, Task 2
Pass threshold: 8.5
Provides (public interface):
```go
package apperr // internal/apperr —— 新增一個 code 與一個 sentinel
const CodeLineSignatureInvalid Code = "LINE_SIGNATURE_INVALID"
var LineSignatureInvalid = &Error{Code: CodeLineSignatureInvalid, Message: "LINE 簽章驗證失敗"}
// HTTPStatus(CodeLineSignatureInvalid) == http.StatusUnauthorized (401)

package pgrepo // internal/line/pgrepo —— 唯一碰 bun 的 line 子套件
func New(db *bun.DB) *LineUserRepository
var _ line.Repository = (*LineUserRepository)(nil)

package webhookhttp // internal/line/webhookhttp —— 唯一碰 net/http 與 LINE SDK 的 line 子套件
func NewHandler(svc *line.Service, channelSecret string, logger *slog.Logger) *Handler
func (h *Handler) Mount() server.Mount   // POST /webhooks/line

package app // internal/app
type Deps struct {
    DB     *bun.DB
    Redis  *redis.Client
    Logger *slog.Logger
    Debug  bool
    LineChannelSecret string   // 新增
}
```
測試輔助函式（Task 4 會直接沿用，寫在 `test/line_webhook_test.go`，不要放進正式套件）：
```go
const lineChannelSecret = "test-channel-secret"   // 必須與 config/test.yaml 一致
func lineSignature(secret string, body []byte) string                     // HMAC-SHA256 + base64
func postLineWebhook(t *testing.T, baseURL string, body []byte, signature string) *http.Response
func followEventBody(userID string, occurredAtMillis int64) []byte
func unfollowEventBody(userID string, occurredAtMillis int64) []byte
func lineUserRow(t *testing.T, dsn, userID string) (status string, lastEventAt int64, found bool)
func lineUsersCount(t *testing.T, dsn string) int
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: 簽章正確的 follow 事件被接受
- [ ] Scenario: 簽章錯誤的請求被拒絕且不產生任何資料
- [ ] Scenario: 缺少 x-line-signature header 的請求被拒絕
- [ ] Scenario: 簽章正確但 body 不是合法 JSON 時回 400
- [ ] Scenario: LINE 主控台的連線確認請求（events 為空陣列）回 200
- [ ] Scenario Outline: 非 follow／unfollow 的事件被忽略但仍回 200
- [ ] Scenario: 資料庫寫入失敗時回 500，讓 LINE 有機會重送

實作要求：

**(1) 相依**
```bash
go get github.com/line/line-bot-sdk-go/v8@v8.22.0
go mod tidy && go mod vendor
```
忘了 `go mod vendor` 會讓 `go build ./...` 直接失敗並抱怨 `modules.txt` 不同步。

**(2) migration**（`db/20260826120000_create_line_users.tx.up.sql` / `.tx.down.sql`）

檔名的 `.tx.` **不可省略**，否則無聲失去交易保護。時間戳必須大於 `20260819120000`。單一敘述，
**不需要** `--bun:split`。建議 schema：

```sql
CREATE TABLE line_users (
    line_user_id   TEXT PRIMARY KEY,
    status         TEXT NOT NULL,
    last_event_at  BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    CONSTRAINT line_users_status_check CHECK (status IN ('FOLLOWING', 'BLOCKED'))
);
```

- `line_user_id` 直接當主鍵：「同一個 userId 恰好一筆」因此是**結構上不可能違反**的，
  不必靠應用層自律。
- `last_event_at` 是 **LINE 事件的毫秒 Unix 時間戳**（`BIGINT`），不是 `TIMESTAMPTZ`——
  它只用來比大小擋亂序，換算成時間型別只會多一個誤差來源。這個欄位存在的唯一理由要寫成註解。
- down 檔：`DROP TABLE IF EXISTS line_users;`
- 這支 migration 會被 `test/` 的 `TestMain` 自動套用到共用容器，同 package 的整合測試直接有表可用。
  既有的 `test/migrate_test.go` / `test/migrate_advanced_test.go` 只斷言
  `name = '20260819120000'` 與 `properties` 的存在，多一支 migration 不會讓它們轉紅——但交付前
  仍要實際跑過確認。

**(3) `internal/line/pgrepo`**

照抄 `internal/property/pgrepo` 的形狀（未匯出的 model 結構 + `toModel` / `toDomain`）。
**⚠️ model 的 tag 必須是 `bun:"table:line_users,alias:line_users"`** —— 理由見 Known Context §I，
少了 `alias:` 會讓 `ON CONFLICT` 的 WHERE 在執行期爆 `missing FROM-clause entry`。

守門的 upsert（語意等價於下列 SQL，用 bun 的 query builder 或 `NewRaw` 都可以）：

```sql
INSERT INTO line_users (line_user_id, status, last_event_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (line_user_id) DO UPDATE SET
    status        = EXCLUDED.status,
    last_event_at = EXCLUDED.last_event_at,
    updated_at    = EXCLUDED.updated_at
WHERE line_users.last_event_at <= EXCLUDED.last_event_at
```

- 用 `<=` 而不是 `<`：時間戳相同代表同一個事件被重送，覆寫成同樣的值是等冪的。
- **`RowsAffected() == 0` 不是錯誤**，那正是「舊事件被正確略過」的樣子。實作若把它當錯誤，
  scenario 10 會變成 500。
- `created_at` 只在 INSERT 時寫入，`DO UPDATE` 不得覆寫它（保留「第一次成為好友的時間」）。
- 其他資料庫錯誤照 `property/pgrepo` 的作法用 `fmt.Errorf("...: %w", err)` 包起來回傳——
  它不是 `*apperr.Error`，`httpx.WriteError` 會自動降級成 INTERNAL 500，這正是 scenario 12 要的。

**(4) `internal/apperr`**

新增 `CodeLineSignatureInvalid` 常數、`LineSignatureInvalid` sentinel，並在 `HTTPStatus` 的
switch 補上 → `http.StatusUnauthorized`。`internal/apperr/apperr_test.go` 的 `TestHTTPStatus`
是 table-driven，補一列即可。

**(5) `internal/line/webhookhttp`**

套件名刻意**不叫 `httpapi`**：property 的 `httpapi` 是「產生的 api 型別 ↔ 領域型別」轉換層且
連 net/http 都不 import，本套件則相反，是這個 feature 唯一被允許出現 net/http 與 LINE SDK 的地方，
名字要看得出差別。

handler 的骨架（**不要用 SDK 的 `webhook.WebhookHandler`**）：

```go
cb, err := webhook.ParseRequest(h.channelSecret, r)
if err != nil {
    if errors.Is(err, webhook.ErrInvalidSignature) {
        httpx.WriteError(w, r, h.logger, apperr.LineSignatureInvalid.WithError(err))
        return
    }
    httpx.WriteError(w, r, h.logger,
        apperr.ValidationFailed.WithMessage("無法解析 LINE webhook 請求").WithError(err))
    return
}
if err := h.svc.Handle(r.Context(), toDomainEvents(cb.Events)); err != nil {
    httpx.WriteError(w, r, h.logger, err)   // 非 *apperr.Error → 自動降級成 INTERNAL 500
    return
}
w.WriteHeader(http.StatusOK)
```

- `toDomainEvents` 用 type switch 只認 `*webhook.FollowEvent` 與 `*webhook.UnfollowEvent`，
  **其餘型別（含 `webhook.UnknownEvent`）安靜略過**；再用 `webhook.SourceInterface` 的 type switch
  取出 `*webhook.UserSource` 的 `UserId`，取不到或為空字串就略過該事件（group / room 來源沒有
  userId，寫進去會產生垃圾記錄）。
- 原始錯誤只進 `WithError`（供 log），**不得**出現在回應 body 的 `message`——`ParseRequest`
  的解析錯誤訊息裡帶著整個 request body。
- 回應成功時 body 留空（`w.WriteHeader(http.StatusOK)`）即可，BDD 只斷言狀態碼。
- 路由：`func (h *Handler) Mount() server.Mount { return func(r chi.Router) { r.Post("/webhooks/line", h.handle) } }`。
  路徑**不在** `/api/v1` 底下。
- 單元測試（不碰 Docker）可以用 `httptest.NewRecorder` + 假的 `line.Repository` 直接驅動 handler，
  覆蓋三條錯誤分支的狀態碼與 `code` 欄位。這一層測完，整合測試就只需要驗「真的接上去了」。

**(6) 組裝與接線**

- `internal/app`：`Deps` 加 `LineChannelSecret string`；`NewHandler` 內
  `line.NewService(linepgrepo.New(d.DB))` → `webhookhttp.NewHandler(...)` →
  把 `.Mount()` 當成第二個 `server.Mount` 傳給 `server.NewRouter`。
  **⚠️ `internal/line/pgrepo` 與 `internal/property/pgrepo` 套件名相同，import 時必須給別名**
  （建議 `linepgrepo`），否則編譯失敗。
- `cmd/api/main.go`：`LineChannelSecret: cfg.Line.ChannelSecret`。
- `test/inprocess_test.go` 的 `startInProcessAPI`：Deps 補上 channel secret。
  **簽章不是選配** —— 不傳的話所有 webhook 測試都會 401。用一個 package 層級常數
  `lineChannelSecret = "test-channel-secret"`，值必須與 `config/test.yaml` 一致（子行程測試走 yaml，
  行程內測試走常數，兩邊分岔會很難查）。`startInProcessAPI` 的簽章形狀維持不變。

**(7) 整合測試**（`test/line_webhook_test.go`，`package test`）

一律走 `startInProcessAPI(t, sharedPostgres(), sharedRedis())` 打真實 HTTP。BDD 各 scenario 用的
userId 彼此不同（`...0001`、`...0002`、`...0003`…），所以共用容器就夠用，**不要動 `TestMain`**。

- 「簽章正確的 follow 事件被接受」：200，且 `line_users` 查得到該 userId、狀態 `FOLLOWING`。
- 「簽章錯誤」：用另一組 secret 算簽章 → 401、`Content-Type` 開頭 `application/json`、
  body 的 `code` 為 `LINE_SIGNATURE_INVALID`、且該 userId 查不到記錄。
- 「缺少 header」：完全不帶 `x-line-signature` → 401、`code` 同上、查不到記錄。
- 「body 不是合法 JSON」：body 就是 `{`，簽章用正確 secret 對這段 bytes 算 → 400、
  `code` 為 `VALIDATION_FAILED`。
- 「events 為空陣列」：`{"destination":"...","events":[]}` → 200，且 `line_users` 總筆數與請求前
  相同（請求前後各查一次 `SELECT count(*)`）。
- 「非 follow／unfollow 的事件」：**這是 Scenario Outline，三個 Examples（message / postback / join）
  都要跑到**，建議寫成 table-driven 的子測試。每一種都要 200 且總筆數不變。
- 「資料庫寫入失敗回 500」：**必須用 `testsupport.StartPostgres(t)` 起專屬容器**（絕不可動共用容器），
  且**不對它套用 migration**（沒有 `line_users` 表，寫入必定失敗）；再用該 dsn 呼叫
  `startInProcessAPI`。斷言 500、`Content-Type` 開頭 `application/json`、`code` 為 `INTERNAL`，
  並確認 body 裡**沒有**洩漏底層 SQL 錯誤訊息。
- 簽章輔助函式自己用 `crypto/hmac` + `crypto/sha256` + `encoding/base64` 算
  （`test/` 底下 import LINE SDK 也可以，但產生簽章 SDK 沒有提供函式）。

**(8) 順手但不強制（UNGRADED）**：`CLAUDE.md` 的分層清單與驗證指令目前只列到 property，
可以補上 `internal/line/`、`internal/line/pgrepo/`、`internal/line/webhookhttp/` 三行與對應的
`go list` 驗證指令。知識庫的沉澱由 `hars-verify` 負責，這個 task 不必寫 `knowledge/`。

交付時 `go build ./...`、`go vet ./...` 與 `go test -race -count=1 ./...` 全部必須綠燈，
**包含 `test/codegen_test.go`**（本輪不得改動 `api/openapi.yaml` 與 `api/api.gen.go`）。

---

### Task 4: 事件語意端到端 —— 標記封鎖、重加好友、多事件、重送、亂序
Status: pending
Directory: test
Depends on: Task 3
Provides (public interface): 無新的匯出介面。本 task 的產出是 `test/line_event_semantics_test.go`
（`package test`），沿用 Task 3 在 `test/line_webhook_test.go` 建立的輔助函式；若過程中發現
`internal/line/pgrepo` 的 upsert SQL 或 `internal/line` 的事件套用邏輯有缺口，一併修正。
Expected Goals (from BDD scenarios):
- [ ] Scenario: unfollow 事件把好友標記為封鎖而不是刪除記錄
- [ ] Scenario: 封鎖後重新加好友會讓同一筆記錄回到 FOLLOWING，不會產生第二筆
- [ ] Scenario: 一次 webhook 帶多個事件時全部都會被處理
- [ ] Scenario: 同一個事件被重送兩次仍然只有一筆記錄
- [ ] Scenario: 亂序抵達的舊 unfollow 事件不會覆蓋較新的 follow 狀態

實作要求：

- 這五條是本 feature 最容易**無聲**做錯的地方（刪掉而不是標記、寫出第二筆、舊事件蓋掉新狀態），
  所以刻意獨立成一次評分循環。**如果 Task 3 的實作已經正確，本 task 的產出主要是測試，
  這是預期內的結果，不是偷懶** —— 但每一條都必須有一個會因為對應 bug 而轉紅的斷言。
- 全部走 `startInProcessAPI(t, sharedPostgres(), sharedRedis())` 打真實 HTTP，**用共用容器**
  （每條 scenario 的 userId 都不同，不需要乾淨資料庫），一律不 `t.Parallel()`。
- 前置狀態（「已經是 FOLLOWING 的好友」「記錄狀態為 BLOCKED」「最後套用的事件時間戳為 2000」）
  的建立方式，二選一並在測試裡說清楚選了哪一種：
  (a) 先打一次 webhook 把它做出來（比較貼近真實、也順便驗了 follow 路徑）；
  (b) 直接對共用 Postgres `INSERT` 一列。scenario 10 需要 `last_event_at = 2000` 這種特定值，
  用 (a) 送一個 `timestamp: 2000` 的 follow 事件即可，不必落到 (b)。
- 逐條的斷言重點：
  - **unfollow 標記而非刪除**：200；該 userId 的記錄**仍然存在**（`found == true`）且
    `status == 'BLOCKED'`。**只斷言狀態不夠**——「刪掉後重新插入一筆 BLOCKED」也會通過狀態斷言，
    建議一併比對 `created_at` 沒有改變。
  - **封鎖後重新加好友**：先讓該 userId 變 BLOCKED（時間戳較小），再送時間戳較大的 follow；
    200、`SELECT count(*) WHERE line_user_id = ...` **恰好為 1**、狀態回到 `FOLLOWING`。
  - **一次帶多個事件**：一個 body 內兩個 follow（`...0020`、`...0021`），200，兩筆都是 `FOLLOWING`。
  - **重送兩次**：用**完全相同的 bytes 與同一個簽章**連續 POST 兩次（不要重新產生 body，
    時間戳一變就不是同一個事件了），兩次都 200，該 userId 恰好 1 筆且為 `FOLLOWING`。
  - **亂序**：先建立 `FOLLOWING` + `last_event_at = 2000`，再送 `timestamp: 1000` 的 unfollow；
    200（**不是 4xx**——對 LINE 而言這個請求處理成功了），狀態**仍然** `FOLLOWING`，
    且 `last_event_at` 仍然是 2000。
- 交付時 `go build ./...`、`go vet ./...` 與 `go test -race -count=1 ./...` 全部綠燈。

## Coverage Check

- Scenario: 簽章正確的 follow 事件被接受 → Task 3
- Scenario: 簽章錯誤的請求被拒絕且不產生任何資料 → Task 3
- Scenario: 缺少 x-line-signature header 的請求被拒絕 → Task 3
- Scenario: 簽章正確但 body 不是合法 JSON 時回 400 → Task 3
- Scenario: LINE 主控台的連線確認請求（events 為空陣列）回 200 → Task 3
- Scenario: unfollow 事件把好友標記為封鎖而不是刪除記錄 → Task 4
- Scenario: 封鎖後重新加好友會讓同一筆記錄回到 FOLLOWING，不會產生第二筆 → Task 4
- Scenario: 一次 webhook 帶多個事件時全部都會被處理 → Task 4
- Scenario: 同一個事件被重送兩次仍然只有一筆記錄 → Task 4
- Scenario: 亂序抵達的舊 unfollow 事件不會覆蓋較新的 follow 狀態 → Task 4
- Scenario Outline: 非 follow／unfollow 的事件被忽略但仍回 200 → Task 3
- Scenario: 資料庫寫入失敗時回 500，讓 LINE 有機會重送 → Task 3
- Scenario: 缺少 line.channel_secret 時服務啟動失敗並指出缺少的 key → Task 1
- Scenario: LINE 領域層不認得 bun、net/http 與 LINE SDK → Task 2

（共 14 列，與 BDD spec 的 14 個 scenario 一一對應——`Scenario Outline` 不論有幾個 Examples
都只計一次——每個 scenario 恰好出現一次。）

## Integration Scenarios

以下 scenario 只有在多個 task 組裝起來之後才真正成立（設定 → 領域層 → 持久化 → transport →
`app.NewHandler` 的接線，任何一段沒接上都會整條垮），`hars-verify` 會在全部 task 完成後對已組裝的
系統重跑一次。它們在上面的 Coverage Check 中仍各只計一次。

- Scenario: 簽章正確的 follow 事件被接受
- Scenario: 簽章錯誤的請求被拒絕且不產生任何資料
- Scenario: 缺少 x-line-signature header 的請求被拒絕
- Scenario: 簽章正確但 body 不是合法 JSON 時回 400
- Scenario: LINE 主控台的連線確認請求（events 為空陣列）回 200
- Scenario: unfollow 事件把好友標記為封鎖而不是刪除記錄
- Scenario: 封鎖後重新加好友會讓同一筆記錄回到 FOLLOWING，不會產生第二筆
- Scenario: 一次 webhook 帶多個事件時全部都會被處理
- Scenario: 同一個事件被重送兩次仍然只有一筆記錄
- Scenario: 亂序抵達的舊 unfollow 事件不會覆蓋較新的 follow 狀態
- Scenario Outline: 非 follow／unfollow 的事件被忽略但仍回 200
- Scenario: 資料庫寫入失敗時回 500，讓 LINE 有機會重送
- Scenario: LINE 領域層不認得 bun、net/http 與 LINE SDK

（最後一條列在這裡是因為它**最容易在後續 task 回歸**：Task 3 只要有人為了省事把 SDK 型別
搬進 `internal/line`，它就會轉紅，而那時 Task 2 早就標記 done 了。）

## Iteration Log
