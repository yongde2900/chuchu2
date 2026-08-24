# PLAN-002 — 架構調整：spec-first handler、統一錯誤中介層、bun/migrate
Created: 2026-08-24
Status: in-progress
Approved: 2026-08-24
Working Directory: .
BDD Spec: ./bdd/BDD-002-spec-first-architecture.feature
Language: Go 1.26.2（開發機 darwin/arm64；module `github.com/yongde2900/chuchu2`）
Build cmd: go build ./...
Test cmd:  go test -race ./...
Lint cmd:  go vet ./...
Generate cmd: go generate ./api/...
Pass threshold: 7.0
Max iterations: 5

## Known Context

執行者讀這份文件時**沒有規劃階段的記憶**，只會讀到這份 plan 與 BDD spec。因此以下每一條都寫成
可獨立閱讀的完整敘述，附上知識庫來源 slug 供追溯。

### A. 既有架構與分層邊界（來源：[[property-service-layering]]）

chuchu2 是「包租代管」服務，Go 1.26，module `github.com/yongde2900/chuchu2`。分層邊界刻意做成
**套件邊界**，違規會表現成「錯的套件出現了錯的 import」，肉眼掃 import 區塊即可發現：

- `internal/property/` —— 純領域＋service＋Repository 介面。**不 import bun，也不 import net/http。**
- `internal/property/pgrepo/` —— bun 實作，是 property 底下**唯一** import bun 的子套件。
- `internal/property/httpapi/` —— HTTP handler 與 DTO，是 property 底下**唯一** import chi／net-http 的子套件。
- `internal/server/` —— chi router 組裝，**不 import 任何 feature 套件**。

三個刻意保留的依賴反轉點：(1) `property.Repository` 介面；(2) `health.Checker` 介面；
(3) `server.Mount`（`type Mount func(r chi.Router)`）。依賴注入一律**手寫建構子**，組裝點只有
`cmd/api/main.go` 一處，不使用任何 DI 框架，不用全域變數，也不用 `init()`。

人工驗證方式：`grep -rn "uptrace/bun\|net/http" internal/property/*.go` 應只命中註解。

### B. PLAN-001 實際匯出的介面（來源：[[exported-interfaces-property-service]]）

下面是本輪會直接對著寫的既有介面。完整清單見該知識庫檔案；這裡列出本輪最相關的部分。

```go
package apperr // internal/apperr —— 本輪 Task 1 要擴充成 sentinel 形式
type Code string  // VALIDATION_FAILED / PROPERTY_NOT_FOUND / PROPERTY_DUPLICATE /
                  // PROPERTY_INVALID_STATUS_TRANSITION / INTERNAL
type FieldError struct{ Field, Reason string }
type Error struct{ Code Code; Message string; Details []FieldError; err error }  // err 未匯出
func New(code Code, msg string) *Error
func Wrap(code Code, msg string, err error) *Error
func Validation(details ...FieldError) *Error
func HTTPStatus(code Code) int    // code → HTTP 的唯一映射點；未知 code → 500

package httpx // internal/httpx
type ErrorBody struct{ Code, Message, RequestID string; Details []apperr.FieldError }
func RequestID(next http.Handler) http.Handler
func RequestIDFrom(ctx context.Context) string
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler
func WriteJSON(w http.ResponseWriter, status int, v any)
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error)
func DecodeJSON[T any](r *http.Request) (T, error)

package server // internal/server
type Mount func(r chi.Router)
type Options struct{ Debug bool; Logger *slog.Logger }
func NewRouter(opts Options, mounts ...Mount) *chi.Mux
func Run(ctx context.Context, addr string, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger) error

package health // internal/health
type Checker interface{ Name() string; Check(ctx context.Context) error }
type Report struct{ Status string; Checks map[string]string }
func NewService(checkers ...Checker) *Service
func (s *Service) Check(ctx context.Context) Report
func Mount(svc *Service) server.Mount   // GET /healthz；全 ok→200，任一 down→503

package property // internal/property —— 本輪完全不改
type Status string      // VACANT / OCCUPIED / RENOVATING / DELISTED；有 Valid()
type RentalMode string  // MASTER_LEASE / MANAGED；有 Valid()
type Layout string      // WHOLE_UNIT / INDEPENDENT_SUITE / SHARED_SUITE / SINGLE_ROOM
type CreateInput struct{ /* 金額以 string 進來 */ }
func (in CreateInput) Validate() []apperr.FieldError   // 回傳「所有」出錯欄位，不短路
type UpdateInput struct{ /* 全為指標；nil 代表不更動；刻意沒有 Status 欄位 */ }
type ListFilter struct{ Page, PageSize int; Status *Status; RentalMode *RentalMode; City string }
type ListResult struct{ Items []*Property; Total int }
func NewService(repo Repository) *Service
func (s *Service) Create/Get/List/Update/ChangeStatus(...)

package httpapi // internal/property/httpapi —— 本輪 Task 5 要改寫
func NewHandler(svc *property.Service, logger *slog.Logger) *Handler
func (h *Handler) Mount() server.Mount

package testsupport // internal/testsupport
func RepoRoot(t *testing.T) string
func StartPostgres(t *testing.T) (dsn string, stop func())   // stop 以 sync.Once 保護
func StartRedis(t *testing.T) (addr string, stop func())
func StartAPI(t *testing.T, configName string, env map[string]string) (baseURL string, output func() string, stop func())

package db // db/embed.go
var FS embed.FS   // //go:embed *.sql

package migrate // internal/migrate —— 本輪 Task 2 要整個刪除
func Up/Down/Applied(ctx context.Context, db *bun.DB) ...
```

兩個**刻意接受**的限制，**不要「修正」它們**：`httpapi` 的建構子吃具體的 `*property.Service`
而不是介面；時間直接取自 `time.Now()`，沒有可注入的 Clock。理由是測試策略走 testcontainers
端到端，不需要為了 mock 而多一層抽象。

### C. 金額慣例（來源：[[money-as-decimal-string]]）

金額一律 `decimal.Decimal`（`github.com/shopspring/decimal`），**端到端永不使用 float**。
JSON 往返一律是**固定兩位小數的字串**，用 `StringFixed(2)` 格式化（`25000.5` → `"25000.50"`）。
理由：`decimal.Decimal` 預設的 `MarshalJSON` 不補齊小數位數，且 JSON number 在 JavaScript
客戶端往返會損失十進位精度。**本輪不得改變這個行為** —— 既有整合測試斷言 `"25000.50"`，
若改用 `String()` 會得到 `"25000.5"` 而失敗。注意產生的 `api.Property` 的金額欄位仍然是
`string`（spec 宣告 `type: string`），所以 `StringFixed(2)` 的責任仍在我們自己手上。

### D. 狀態機（來源：[[property-status-machine]]）

七條合法轉換（VACANT↔OCCUPIED、VACANT↔RENOVATING、VACANT↔DELISTED、RENOVATING→DELISTED），
對角線（自己轉自己）全部非法，拒絕必須發生在寫入之前。`property.CanTransition(from, to)` 是唯一
權威來源。**本輪完全不改狀態機。**

### E. 測試佈局（來源：[[test-layout-two-tiers]]、[[testcontainers-shared-vs-dedicated]]）—— 每個 task 都必須遵守

- **單元測試貼著程式碼放**，檔名 `<被測檔>_test.go`，與被測套件同目錄。這一層**不得碰 Docker、
  不得起容器、不得連任何外部服務**，必須在一台沒有 Docker 的機器上也能跑過。
- **整合測試一律放在 repo 根目錄的 `test/` 目錄，`package test`**，一個 feature 面向一個檔，
  透過真實 HTTP 請求或真實跑起來的 binary 驗證，不直接呼叫內部函式。
- **⚠️ 共用容器的危害：** `test/` 用一個 `TestMain` 啟動**一組共用的** Postgres 與 Redis 容器供
  整個 package 使用，並在其上跑一次 migration。**任何需要「停掉容器」或「乾淨資料庫」的測試，都
  必須自己起一組用完即丟的專屬容器（`testsupport.StartPostgres(t)`），絕對不可以動 `TestMain` 的
  共用容器** —— 動了會讓同 package 中其後所有測試連鎖失敗，而且失敗訊息會指向無辜的測試，極難除錯。
  **本輪 Rule 3 滿滿都是需要乾淨資料庫的測試，每一個都必須起專屬容器。**
- `-count=1` 是必要的：Go 會快取測試結果，容器背後的測試從快取回綠會騙人。驗證整合層請用
  `go test -race -count=1 ./test/...`。
- 目前 `test/` 底下沒有任何測試呼叫 `t.Parallel()`，本輪新增的測試**也一律不得呼叫**
  （理由見下方 §J 關於臨時 migration 檔案的說明）。

### F. 其他既有 gotchas（知識庫）

- **`go:embed` 的 pattern 不允許 `..`**（[[go-embed-no-parent-dir]]）：所有需要讀 migration SQL 的
  程式碼都必須 import `db` 這個 package 使用它匯出的 `FS`，不可在別的套件寫 `//go:embed ../../db/*.sql`。
- **空列表回應必須是 `[]` 不是 `null`**（[[nil-slice-marshals-to-null]]）：nil slice 經
  `encoding/json` 會輸出 `null`。必須用 `make([]T, 0, n)`。**而且反序列化後的斷言抓不到這件事，
  要看原始 bytes。** 產生的 `api.PropertyList.Items` 是 `[]api.Property`，同樣有這個陷阱。
- **viper 的 `AutomaticEnv` 對「只存在於環境變數的 key」不可靠**（[[viper-env-override-needs-bindenv]]）：
  要明確 `BindEnv`。測試靠 `CHUCHU_POSTGRES_DSN` / `CHUCHU_REDIS_ADDR` 注入 testcontainers 的隨機 DSN。
- **用 `go run` 啟動服務做測試時必須以 process group 收屍**（[[go-run-orphan-process-group]]），否則會
  留下佔埠的孤兒行程。`testsupport.StartAPI` 已經處理好了。
- **TDD 之後編輯器診斷會回報早已修好的錯誤**（[[stale-gopls-diagnostics-during-tdd]]）：**編譯器是權威**，
  `go build ./...` 說了算，不要相信 gopls 的殘留錯誤。產生 `api/api.gen.go` 之後尤其明顯。
- **`logger` 放進 `server.Options` 而非 `NewRouter` 的參數列**（[[server-options-logger-field]]）：
  加欄位安全，改參數列危險。本輪若要讓 router 多知道一件事，**加 `Options` 欄位**，不要改
  `NewRouter` 的簽章形狀。

### G. 本輪推翻的既有決定

[[openapi-contract-test]] 記載「`api/openapi.yaml` 是唯一契約來源，並以契約測試強制它不說謊」，
三個支柱是「文件本身合法／回應 body 對 schema／雙向路由比對」。**本輪使用者明確決定刪除
`test/contract_test.go` 整個檔案**，理由是改用 spec-first 產生程式碼後，路由集合與成功回應形狀
改由**編譯期**保證（`var _ api.StrictServerInterface = (*apiServer)(nil)` 加上產生的路由表），
不需要再用執行期測試事後追認。

### H. Phase 0 實地驗證的事實（規劃階段親自跑過，不是推測）

**環境基準線：** `go build ./...`、`go vet ./...`、`go test -race ./internal/... ./db/...` 目前
**全綠**。Docker 可用（Server 29.4.1）。`vendor/` 原本與 `go.mod` 不同步導致建置失敗，已用
`go mod vendor` 修好。

**oapi-codegen v2.8.0 已對本專案的 spec 實跑成功。** `api/openapi.yaml` 是 `openapi: 3.1.0`，
用使用者指定的設定（models + chi-server + embedded-spec + strict-server）產生出約 52KB 的檔案，
無錯誤。**以下名稱全部是從實際產出的檔案抄錄的，不要憑記憶重寫：**

```go
// 單一介面涵蓋全部 6 個 operation：
type StrictServerInterface interface {
    ListProperties(ctx context.Context, request ListPropertiesRequestObject) (ListPropertiesResponseObject, error)
    CreateProperty(ctx context.Context, request CreatePropertyRequestObject) (CreatePropertyResponseObject, error)
    GetProperty(ctx context.Context, request GetPropertyRequestObject) (GetPropertyResponseObject, error)
    UpdateProperty(ctx context.Context, request UpdatePropertyRequestObject) (UpdatePropertyResponseObject, error)
    ChangePropertyStatus(ctx context.Context, request ChangePropertyStatusRequestObject) (ChangePropertyStatusResponseObject, error)
    GetHealthz(ctx context.Context, request GetHealthzRequestObject) (GetHealthzResponseObject, error)
}

// request object（注意 Body 是指標，Id 是 openapi_types.UUID，也就是 uuid.UUID 的別名）
type ListPropertiesRequestObject struct{ Params ListPropertiesParams }
type CreatePropertyRequestObject struct{ Body *CreatePropertyJSONRequestBody }
type GetPropertyRequestObject struct{ Id openapi_types.UUID `json:"id"` }
type UpdatePropertyRequestObject struct{ Id openapi_types.UUID; Body *UpdatePropertyJSONRequestBody }
type ChangePropertyStatusRequestObject struct{ Id openapi_types.UUID; Body *ChangePropertyStatusJSONRequestBody }
type GetHealthzRequestObject struct{}

// 成功回應型別（本輪唯一會用到的那幾個）
type ListProperties200JSONResponse PropertyList
type CreateProperty201JSONResponse Property
type GetProperty200JSONResponse Property
type UpdateProperty200JSONResponse Property
type ChangePropertyStatus200JSONResponse Property
type GetHealthz200JSONResponse HealthReport
type GetHealthz503JSONResponse HealthReport
// ⚠️ 另外還會產生 CreateProperty400/409、GetProperty400/404、UpdateProperty400/404、
//    ChangePropertyStatus400/404/409 這些 *JSONResponse（底層型別是 ErrorBody）。
//    本輪改用中介層之後，這些錯誤回應型別會變成「產生了但沒有任何人使用」，這是刻意的，不是遺漏。

type ListPropertiesParams struct {
    Page *int; PageSize *int; Status *ListPropertiesParamsStatus
    RentalMode *ListPropertiesParamsRentalMode; City *string
}

type HealthReport struct {
    Checks map[string]string `json:"checks"`
    Status HealthReportStatus `json:"status"`   // HealthReportStatus 是 string 的具名型別
}

type Property struct {
    AreaPing string; City string; CreatedAt time.Time; DepositMonths int; District string
    Floor string; Id openapi_types.UUID; LandlordName string; LandlordPhone string
    Layout PropertyLayout; ManagementFee string; MonthlyRent string; RentalMode PropertyRentalMode
    RoomNo string; Status PropertyStatus; StreetAddress string; UpdatedAt time.Time
}
// ⚠️ CreatedAt/UpdatedAt 是 time.Time（marshal 成 RFC3339Nano）。既有手寫 DTO 用的
//    timeFormat = "2006-01-02T15:04:05.999999999Z07:00" 正是 RFC3339Nano，兩者相容。
// ⚠️ 金額仍是 string，StringFixed(2) 的責任仍在我們身上。

type ErrorBody struct {
    Code      string        `json:"code"`
    Details   *[]FieldError `json:"details,omitempty"`   // ⚠️ 指標包 slice
    Message   string        `json:"message"`
    RequestId string        `json:"request_id"`          // ⚠️ RequestId，不是 RequestID
}
```

產生的檔案會 import：`github.com/getkin/kin-openapi/openapi3`（embedded-spec 的 `GetSwagger()` 用）、
`github.com/go-chi/chi/v5`、**`github.com/oapi-codegen/runtime`（新相依）**、
`openapi_types "github.com/oapi-codegen/runtime/types"`。**注意 `kin-openapi` 不會因為刪掉契約測試
就變成無用相依 —— 產生的程式碼自己就 import 它。**

**⚠️ 三個 error hook，全部預設為純文字（已從 v2.8.0 產出的程式碼確認）：**

1. `ChiServerOptions.ErrorHandlerFunc func(w, r, err)` —— **路徑／查詢參數綁定失敗**。err 的具體
   型別是 `*api.InvalidParamFormatError{ParamName, Err}`、`*api.RequiredParamError{ParamName}`、
   `*api.UnmarshalingParamError{ParamName, Err}`、`*api.TooManyValuesForParamError{ParamName, Count}`、
   `*api.RequiredHeaderError{ParamName}`。**這些型別都有 `ParamName` 欄位，正是 BDD 要求的
   `details[].field` 來源**（`id`、`page`）。預設值是 `http.Error(w, err.Error(), http.StatusBadRequest)`。
2. `StrictHTTPServerOptions.RequestErrorHandlerFunc func(w, r, err)` —— **request body 解析失敗**
   （err 為 `fmt.Errorf("can't decode JSON body: %w", err)`）。預設值是
   `http.Error(w, err.Error(), http.StatusBadRequest)`。
3. `StrictHTTPServerOptions.ResponseErrorHandlerFunc func(w, r, err)` —— **handler 回傳的 error
   （也就是 apperr 落地的地方）**，以及回應寫出失敗、回應型別不符。預設值是
   `http.Error(w, err.Error(), http.StatusInternalServerError)`。

建構函式（實際簽章）：

```go
func NewStrictHandlerWithOptions(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc, options StrictHTTPServerOptions) ServerInterface
func HandlerWithOptions(si ServerInterface, options ChiServerOptions) http.Handler
type ChiServerOptions struct {
    BaseURL          string
    BaseRouter       chi.Router      // 給定時就把路由掛在這個既有的 chi router 上
    Middlewares      []MiddlewareFunc
    ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}
```

`NewStrictHandler`（不帶 options）會套用上述純文字預設值 —— 這正是 BDD「漏接的 error hook」
scenario 要重現的情境。

**enum 產生成 `type PropertyStatus string` 之類的具名型別加上常數，另有 `Valid()` 方法，但產生的
程式碼中沒有任何地方呼叫它**（已 grep 確認）。所以 enum 值不受任何自動強制 —— `status=BOGUS`
會原封不動綁進 `*ListPropertiesParamsStatus`，不會報錯。

**產生器呼叫方式已驗證可用（`vendor/` 存在時也正常）：**
`go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0`。
**產生器本身刻意不加進 `go.mod` 的 tool 區塊** —— 加了就得連整個產生器一起 vendor。
只有 `github.com/oapi-codegen/runtime` 需要進 `go.mod`。

### I. 使用者在規劃階段做出的四個決定（都是決定，不是建議）

1. **介面組裝用「嵌入式組合」。** 新增一個薄的聚合結構，用 Go embedding 把各 feature 的實作組起來，
   每個 feature 仍自己實作自己的 operation 方法，`internal/server` 依然不 import 任何 feature 套件。
   形狀大致是：
   ```go
   type apiServer struct { *health.API; *httpapi.API }
   var _ api.StrictServerInterface = (*apiServer)(nil)   // 編譯期保證
   ```
   `health` 負責 `GetHealthz`，`httpapi` 負責其餘 5 個 property operation。因為這個結構同時
   import 了兩個 feature 套件，它必須放在**組裝點**，也就是 `cmd/api`（`package main`）底下。
2. **接受查詢參數綁定變嚴格。** `GET /api/v1/properties?page=abc` 從「靜默視為預設值並回 200」
   改為回 **400 VALIDATION_FAILED**，且 `details[].field` 為 `page`。**因此 `api/openapi.yaml` 的
   `listProperties` operation 必須補上 `400` 回應**（`$ref: ErrorBody`）。這是刻意的行為變更，
   推翻 PLAN-001 當時「查詢參數一律寬鬆、無效值視為不篩選」這條決定中關於**型別錯誤**的部分。
   注意：**enum 值錯誤（如 `status=BOGUS`）仍然寬鬆**，因為產生的綁定只做型別轉換不驗證 enum
   成員，維持「不篩選該欄位」的既有行為。
3. **契約測試整檔刪除**（`test/contract_test.go`）。
4. **保留 `vendor/` 並在每次改動 `go.mod` 後重跑 `go mod vendor`。** ⚠️ 這是很容易漏掉的一步：
   忘記重跑會讓 `go build ./...` 直接失敗並抱怨 `modules.txt` 與 `go.mod` 不同步。

### J. bun/migrate 的完整研究成果（全部已對照 bun v1.2.18 原始碼確認）

`go.mod` 已有 `github.com/uptrace/bun v1.2.18`，`migrate` 是它的子套件，**不需要新增相依**。

- **bun 沒有現成的 migration CLI binary。** 它提供的是 `github.com/uptrace/bun/migrate` 套件
  （`migrate.NewMigrations()`、`Migrations.Discover(fsys)`、`migrate.NewMigrator(db, migrations)`，
  以及 `Migrator` 上的 `Init` / `Migrate` / `Rollback` / `MigrationsWithStatus` / `MarkApplied` /
  `Lock` / `Unlock` / `CreateTxSQLMigrations` 等方法）。`cmd/dbmigrate/main.go` 仍然是我們自己的檔案。
- **⚠️ 交易性退化風險 —— 本輪最危險的細節。** 現行的 `internal/migrate.applyMigration` 把每一個
  migration 都包在 `bunDB.RunInTx` 裡。**bun 對普通的 `.up.sql` 不做這件事**：
  `bun@v1.2.18/migrate/migration.go` 的 `newSQLMigrationFunc` **只有在檔名以 `.tx.up.sql` /
  `.tx.down.sql` 結尾時才 `BeginTx`**，否則只取一條 `Conn` 直接執行。把
  `20260819120000_create_properties.up.sql` 原封搬過去會**無聲地失去交易保護**。因此既有那一對檔案
  **必須**改名為 `.tx.up.sql` / `.tx.down.sql`，且本專案從今以後每個 migration 都要用這個形式。
- **⚠️ 失敗的 migration 預設會留下記錄。** `Migrator.Migrate` 預設**先** `MarkApplied` **再**執行
  SQL，且那次寫入不在 migration 自己的交易裡。因此建立 Migrator 時**必須**傳入
  `migrate.WithMarkAppliedOnSuccess(true)`。這個選項必須由 CLI 與 `TestMain` **共用同一個建構函式**
  設定，否則兩邊行為會悄悄分岔。**已知且接受的差異：** 即使開了這個選項，DDL 的交易與記帳的
  INSERT 仍是兩個交易，不像手寫機制那樣同一個交易。
- **⚠️ 兩個選項掛在不同的建構子上，型別不同、不可互換：** `migrate.WithMigrationsDirectory("db")`
  的型別是 `MigrationsOption`，只能傳給 `migrate.NewMigrations(...)`；
  `migrate.WithMarkAppliedOnSuccess(true)` 的型別是 `MigratorOption`，只能傳給
  `migrate.NewMigrator(...)`。傳錯編譯不會過。原始碼位置：`migrate/migrations.go:15,18,33`、
  `migrate/migrator.go:23,41,96`。
- **bun 要求先 `Migrator.Init()`** 建立 `bun_migrations` 與 `bun_migration_locks` 兩張表，
  否則 `Migrate` / `Status` / `Rollback` 都會因為查不到 `bun_migrations` 而失敗。手寫機制是 lazily
  `CREATE TABLE IF NOT EXISTS`，bun 不是。**每一條測試佈署路徑與 `test/main_test.go` 的 `TestMain`
  都必須先 `Init`。**
- **⚠️ `bun_migrations` 的 `name` 欄位只有時間戳，沒有 migration 名稱。** `Migration` 結構上的
  `Comment` 欄位標了 `bun:"-"`，不會寫進資料庫。所以斷言要查 `name = '20260819120000'`；
  寫成 `WHERE name LIKE '%create_properties%'` 會永遠查不到而讓測試假失敗。反過來說，`status`
  子指令的輸出**會**包含 `create_properties`，因為 `Migration.String()` 是 `name_comment`，
  comment 來自 `Discover` 當下從檔名解析出來的（存在記憶體中）。
- **既有檔名已符合 bun 的慣例。** bun 以 `^(\d{1,14})_([0-9a-z_\-]+)\.` 解析檔名，
  `20260819120000_create_properties.tx.up.sql` 會解析成 `Name = "20260819120000"`、
  `Comment = "create_properties"`。**不需要重編時間戳，只需加上 `.tx.` 這一段**（用 `git mv` 保留歷史）。
- **`--bun:split` 這次不需要加到正式 migration。** bun 會把 `.sql` 檔依 `--bun:split` 切成多段
  逐段 `ExecContext`；沒有這行指示詞時整個檔案是一段，以單次 `ExecContext` 送出 —— 與手寫機制相同。
  現行的 `create_properties.up.sql`（`CREATE TABLE` + `CREATE UNIQUE INDEX`）本來就依賴 pgdriver 的
  simple query protocol 接受多敘述批次。**本輪只改檔名，不得改動 SQL 內容，也不得加入 `--bun:split`。**
- **⚠️ 但交易性測試的探針 migration 必須用 `--bun:split`，這是那個測試唯一有效的作法。** 探針的
  `.tx.up.sql` 應為「`CREATE TABLE tx_probe (...)`」→ 單獨一行 `--bun:split` →「一段必定失敗的 SQL」
  （建議 `INSERT INTO no_such_table VALUES (1);`，SQLSTATE 42P01）。**理由：** 沒有 `--bun:split`
  的話兩個敘述會被當成單一批次送出，Postgres 的 simple query protocol 會自動把整批包成隱式交易 ——
  於是就算檔名不是 `.tx.`、bun 根本沒開交易，`tx_probe` 一樣不會留下來，**測試會因為錯誤的理由通過，
  完全測不到 `.tx.` 的作用**。`--bun:split` 必須自成一行、前後沒有多餘空白，否則 bun 會回報
  `unknown directive`。
- **接受 bun 的 migration group 回滾語意。** bun 把「同一次 `Migrate` 呼叫中套用的所有 migration」
  視為一個 group（`bun_migrations.group_id`），`Rollback` 回滾的是**最後一個 group**，不是全部。
  這與被取代的手寫 `Down`（全部回滾）不同，使用者知情並刻意接受，理由是「退回上一個穩定狀態」
  比「拆光整個 schema」安全。**必須一併理解的後果：任何需要「完全乾淨資料庫」的測試不能再靠
  `down` 清光，只能用專屬的用完即丟容器。**
- **指令詞彙全面採用 bun 的，`up` / `down` 完全廢除、不保留別名。** 之後只認得 `init`、`migrate`、
  `rollback`、`status`、`unlock`、`create_sql` 六個子指令。BDD 明確斷言 `up` 與 `down` 會以非零
  exit code 被拒絕。
- **`create_sql` 必須走 `CreateTxSQLMigrations`**（產生 `.tx.up.sql` / `.tx.down.sql`），
  **不可用 `CreateSQLMigrations`**（那會產生不具交易性的檔名）。bun 會自行加 14 位 UTC 時間戳前綴，
  且同一次呼叫的兩個檔案共用同一個時間戳。
- **`unlock` 的測試要用 `Migrator.Lock` 製造鎖定，不要自己 INSERT 一列**：bun 的 `Unlock` 以
  `WHERE table_name = <formattedTableName>` 刪除，手寫 INSERT 很容易把 `table_name` 填成對不上的值，
  於是 `unlock` 明明成功卻刪不到東西，測試會以難以理解的方式失敗。
- **`cmd/dbmigrate` 保留標準庫 `flag`，不得引入 `urfave/cli` 或任何 CLI 框架**（bun 官方範例用
  urfave/cli，不要跟著引入）。現有 `main()` 只有 `os.Exit(run(os.Args[1:]))` 一行，邏輯收在
  `run(args []string) int`，讓 exit code 可測、也讓 `defer` 一定會執行。錯誤訊息寫 **stderr**，
  成功訊息寫 **stdout**。
- **`Unlock` 必須用 `defer` 寫在 `run` 裡**，這樣即使 migration 失敗、`run` 回傳 1，鎖也會被釋放
  （`main` 的 `os.Exit` 會跳過 defer，但 `run` 的 defer 在 `run` 回傳時就已經跑完了）。
- **測試專用 migration 的供應方式：** rollback 的 group 語意需要第二個 migration，交易性測試需要一個
  會中途失敗的 migration。由於這些 scenario 都要透過真實的 `go run ./cmd/dbmigrate` 驅動，而 CLI 讀的是
  `db` package 的 `embed.FS`（編譯期決定，無法外部注入），唯一可行的作法是：**由測試在執行期間把臨時
  migration 檔案寫進 `db/` 目錄，再讓 `go run` 重新編譯時嵌進去，測試結束以 `t.Cleanup` 刪除**。
  之所以成立，關鍵在於 `go run` 是在測試執行**當下**編譯的，而已經跑起來的測試 binary 自己的 `db.FS`
  不含這些檔案，因此 `TestMain` 的共用容器完全不受影響。代價是檔案在磁碟上的那段期間是全域可見的，
  所以**本輪所有測試一律不得 `t.Parallel()`**，且清理必須用 `t.Cleanup` 註冊在建立**之前**，
  讓測試失敗時也一定會清掉。建議 version 取 `29990101000001` 這種明顯是測試產物、且必定排序在
  `20260819120000` 之後的值，而不是 `time.Now()`。
- **`create_sql` 的測試必須清理產生的檔案，這是硬性要求。** 留下來的檔案內容是 bun 的樣板
  （`SET statement_timeout = 0;` + `SELECT 1;`），一旦留在 `db/` 就會被之後每次 `go build` / `go run`
  嵌入並當成真的 migration 套用，也極可能被誤 commit。作法：測試在執行指令**之前**就以 `t.Cleanup`
  註冊「以 glob 找出並刪除」的清理函式。
- **`create_sql` 不需要連上資料庫**（`postgres.Open` 只建連線池不實際連線），這個 scenario 不應該
  起任何容器。
- **schema 等價性斷言的用詞陷阱：** Postgres 的 `information_schema.columns.data_type` 用詞與 DDL
  不同 —— `TIMESTAMPTZ` 是 `timestamp with time zone`、`INT` 是 `integer`、`NUMERIC(12,2)` 是
  `numeric`、`UUID` 是 `uuid`、`TEXT` 是 `text`；可空性是 `is_nullable = 'NO'`。斷言必須逐欄進行，
  並在失敗時指出是哪一欄。唯一索引名稱應仍為 `properties_address_key`。
- **既有呼叫端只有兩處**（已 grep 確認）：`cmd/dbmigrate/main.go` 與 `test/main_test.go` 的 `TestMain`
  （`migrateSharedPostgres` 呼叫 `migrate.Up`）。此外 `db/embed.go` 的套件註解提到 `internal/migrate`，
  需一併更新。
- **`go run` 在子程式非零退出時會另外印一行 `exit status N` 到 stderr**，這不影響斷言；斷言只要求
  exit code **非零**，不要斷言等於 1。

### Out of Scope & Ungraded Constraints

以下每一條都是**決定**，不是遺漏。沒有讀到這一段的執行者會「很有幫助地」把它們做出來，那是錯的。

- **防護網刻意不緩衝回應主體。** 狀態碼與 Content-Type 在 `WriteHeader` 當下就已經確定，判斷在那
  一刻完成即可 —— 因為代價是不支援串流回應，而本 API 全部是小型 JSON，沒有任何串流端點。
- **不引入 oapi-codegen 的請求／回應驗證 middleware**（`nethttp-middleware` /
  `OapiRequestValidator`）。本輪只用產生的型別與路由，不做執行期 schema 驗證。
- **不改動領域層行為。** `property.Service`、狀態機轉換表、金額語意、驗證規則一律不動。
- **不改動資料庫 schema。** migration 的 SQL 內容一個字元都不得更動，只換執行機制與檔名。
- **不新增任何業務 endpoint，也不新增業務用的 migration。** 本輪唯一會出現的新 `.sql` 檔案，是測試在
  執行期間寫進 `db/` 再刪掉的臨時檔案，以及 `create_sql` scenario 產生後隨即清掉的那一對。
- **不引入 DI 框架**（wire／fx／dig／do），維持手寫建構子，組裝點只有 `cmd/api/main.go`。
- **不實作 bun 的 `mark_applied` 子指令。** 本專案沒有長存資料庫，開發與測試都是用完即丟的 testcontainers。
- **不使用 bun 的 Go migration**（`CreateGoMigration` / `create_go` / `Migrations.MustRegister`）。
  本專案的 migration 一律是 SQL 檔案。
- **不把 golangci-lint 變成 gate。** Lint 仍只有 `go vet ./...`，也不要新增 `.golangci.yml`。
- **未評分（UNGRADED）：** 刪掉 `test/contract_test.go` 之後，spec 中的金額 `pattern ^\d+\.\d{2}$`、
  enum 成員值、`page`/`page_size` 的 `minimum`/`maximum` 將不再有任何自動強制。使用者已在知情下接受。
  金額固定兩位小數仍間接受既有整合測試斷言 `"25000.50"` 保護，但那是單一數值斷言，不是 schema 級強制。
- **未評分（UNGRADED）：** 本輪結束後 `internal/migrate` 這個套件**必須完全不存在**
  （`migrate.go`、`parse.go`、`parse_test.go` 連同目錄一起刪除），而不是留著沒人用、或改名保留。
- **未評分（UNGRADED）：** `test/contract_test.go` 必須整檔刪除，且不得以任何形式改寫保留。
- **未評分（UNGRADED）：** `knowledge/decisions/openapi-contract-test.md` 需標記為 superseded ——
  建議在檔案開頭加一段說明它已被本輪推翻，並**保留原文**以維持歷史可讀性，**不要**刪除整個檔案。
- **未評分（UNGRADED）：** `cmd/dbmigrate` 必須繼續使用標準庫 `flag`，不得引入 `urfave/cli`。
  用哪個套件解析參數在行為上分不出來，harness 抓不到，由人工檢查 import 區塊把關。
- **未評分（UNGRADED）：** 程式碼註解與產品術語一律使用**繁體中文**。
- **未評分（UNGRADED）：** `api/api.gen.go` 是產生的檔案，**絕對不得手動編輯**，且必須提交進版控。

## Project Conventions

- **錯誤處理：** Go 1.26，**優先使用 `errors.AsType[T](err)`**，不要用 `errors.As(&target)`。
- **Cancellation：** 每一個碰 I/O 的函式第一個參數都是 `ctx context.Context`，而且必須真的傳下去。
- **金錢一律 `github.com/shopspring/decimal`**，永不用 float 做餘額／租金運算。
- **設定：** viper 載入，`--config=<name>` 選擇 `config/<name>.yaml`，可由 `CHUCHU_` 前綴環境變數
  覆寫（`.` 換成 `_`）。
- **CLI 形狀：** `main()` 只有 `os.Exit(run(...))` 一行，邏輯在 `run` 裡回傳 exit code。
  錯誤訊息寫 stderr，成功訊息寫 stdout。
- **依賴注入一律手寫建構子**，組裝點只有 `cmd/api/main.go`。
- **註解與產品術語使用繁體中文。**
- **`db/` 是一個 Go package（`package db`）**，裡面除了 `.sql` 還有 `embed.go`。
- **⚠️ 任何改動 `go.mod` 的 task 都必須接著跑 `go mod vendor`**，否則 `go build ./...` 會失敗。

## Overview

本輪把 chuchu2 的 HTTP 層從「手寫 chi handler ＋ 手寫 DTO ＋ 契約測試事後追認」換成
「由 `api/openapi.yaml` 用 oapi-codegen 產生型別與路由，錯誤全部收斂到單一中介層」，
同時把 migration 從自製的 `internal/migrate` 換成 bun 官方的 `github.com/uptrace/bun/migrate`。
六個 sub-task 依序是：`apperr` sentinel 化 → bun/migrate 機制原子置換 → migration 進階行為
（rollback group／交易性／`create_sql`／`unlock`）→ 產生 `api/api.gen.go` → spec-first handler
與三個 error hook 接線 → 回應防護網。

**為什麼把 Rule 3（migration，Task 2–3）排在 Rule 1／2（HTTP，Task 4–6）之前：** 兩邊都會動到
`test/` 這個 package，而唯一真正的碰撞點是 `test/main_test.go` 的 `TestMain`（它目前呼叫
`migrate.Up`）。先做 migration，可以在 `test/` 還處於 PLAN-001 已知良好狀態時，把「刪掉舊機制、
換上新機制」這個最危險的原子置換做完並收斂；之後 HTTP 的三個 task 完全不需要碰 `TestMain`
（它們只會新增檔案、刪掉 `contract_test.go`）。反過來排的話，migration task 會落在一個剛被大幅
改動過的 `test/` package 上，同時要動 `TestMain` 又要面對剛換過的 handler，失敗來源會混在一起
難以歸因。**因此本計畫明確約束：Task 4–6 一律不得修改 `test/main_test.go`。**

**為什麼 Task 2 必須是原子的：** `up`／`down` 被完全廢除、`internal/migrate` 被整個刪除、SQL 檔名
被改成 `.tx.`、`TestMain` 改用新建構子 —— 這幾件事任何一件單獨落地都會讓專案處於「舊機制已拆、新機制
還沒通」的破碎狀態，`test/` 整個 package 會連編譯都過不了。因此 Task 2 一次做完，交付時
`go build ./...`、`go vet ./...` 與整個測試套件都必須是綠的。

**Pass threshold 的取捨（header 預設 7.0，以下為 per-task 覆寫）：** Task 2、Task 3 各設 **8.5** ——
migration 的缺陷（悄悄失去交易保護、失敗卻留下記帳、rollback 語意誤解）是典型「當下看起來會動、
出事時代價極高」的類型，而且本輪特意接受了 bun 的 group 回滾語意，錯誤理解的空間很大。Task 5
（錯誤轉譯層）與 Task 6（防護網）各設 **8.5** —— 本輪刪掉了原本負責抓 API 形狀退化的契約測試，
剩下的測試因此承擔了更多重量；錯誤路徑又天生是「平常不會走到、走到時最需要它正確」的程式碼。
Task 1 設 **8.0** —— 整個 Rule 2 都站在 sentinel 的正確性上，一個共用值被 `WithError` 汙染會造成
跨請求的資料污染。Task 4 維持 **7.0**，它是機械性的產生與接線，正確與否幾乎是二元的。

**關於 git 歷史的說明：** git 歷史中有一份同編號但不同 slug 的舊檔 `plan/PLAN-002-bun-migrate.md`，
使用者已將它刪除，且它**從未被執行過**。它的研究成果已完整吸收進本文件的 Known Context §J，
日後讀 git log 的人不需要再去翻它。

## Sub-Tasks

### Task 1: apperr sentinel 化 —— 共用錯誤值與不可變的 With* 衍生
Status: done
Directory: internal/apperr
Depends on: none
Pass threshold: 8.0
Provides (public interface):
```go
package apperr

// 既有的 Code 常數、FieldError、Error、HTTPStatus 全部保留不變（見 Known Context §B），
// New / Wrap / Validation 三個建構子也**繼續保留**——internal/property 與 internal/httpx
// 底下有十幾處呼叫它們，本輪不做那些呼叫端的改寫，避免製造無關的 churn。

// 套件層級的共用 sentinel。每一個 Code 一個，供呼叫端以 errors.Is 比對、
// 並以 With* 衍生出帶上下文的獨立值。
var (
    ValidationFailed         = &Error{Code: CodeValidationFailed, Message: "驗證失敗"}
    NotFound                 = &Error{Code: CodePropertyNotFound, Message: "找不到指定的物件"}
    Duplicate                = &Error{Code: CodePropertyDuplicate, Message: "相同門牌的物件已存在"}
    InvalidStatusTransition  = &Error{Code: CodeInvalidStatusTransition, Message: "不允許的狀態轉換"}
    Internal                 = &Error{Code: CodeInternal, Message: "internal server error"}
)

// With* 一律回傳「以 e 為底的**新** *Error」，絕不就地修改 e。
// 這是 sentinel 能安全共用的唯一理由：兩次 apperr.NotFound.WithError(a) 與
// apperr.NotFound.WithError(b) 必須得到兩個彼此獨立、且都不影響 apperr.NotFound 的值。
// Details 是 slice，複製時必須做一份新的 backing array（不可只複製 slice header），
// 否則兩個衍生值會共用同一段記憶體。
func (e *Error) WithError(err error) *Error
func (e *Error) WithMessage(msg string) *Error
func (e *Error) WithDetails(details ...FieldError) *Error

// Is 讓 errors.Is(someErr, apperr.NotFound) 對「衍生出來的副本」也成立——
// 沒有這個方法的話，因為 With* 回傳的是新指標，errors.Is 會全部失敗，sentinel 形同虛設。
// 判定條件是「target 也是 *Error 且 Code 相同」。
func (e *Error) Is(target error) bool
```
Expected Goals (from BDD scenarios):
- [x] Scenario: apperr 的共用 sentinel 不會被 WithError 汙染

實作要求：
- 這個 task 只動 `internal/apperr`，不動任何呼叫端；交付時 `go build ./...`、`go vet ./...`
  與整個測試套件必須維持綠燈。
- 單元測試放在 `internal/apperr/apperr_test.go`（既有檔），必須明確斷言：
  (1) 兩次 `WithError` 得到兩個不同的指標；(2) 各自 `errors.Unwrap` 拿到自己的底層錯誤；
  (3) `apperr.NotFound` 本身 `Unwrap()` 仍為 `nil`；(4) `errors.Is(derived, apperr.NotFound)` 為 true；
  (5) `WithDetails` 不會把 details 寫回 sentinel，且兩個衍生值的 Details 互不影響
  （對其中一個做 `append` 不會改到另一個）。
- 註解要寫清楚「為什麼 With* 必須回傳副本」，因為這正是本 scenario 存在的原因。

---

### Task 2: bun/migrate 機制原子置換 —— 刪除 internal/migrate、重寫 cmd/dbmigrate
Status: done
Directory: db, internal/migrate（刪除）, cmd/dbmigrate, test
Depends on: none
Pass threshold: 8.5
Provides (public interface):
```go
package db // db/ —— 既有的 embed.FS 旁邊新增「如何探索與套用」的唯一權威來源

var FS embed.FS   // 既有，不變（//go:embed *.sql）

// Migrations 以 FS 探索所有 migration 檔案。
// 必須用 migrate.NewMigrations(migrate.WithMigrationsDirectory("db")) 建立——
// 這個目錄設定是 create_sql 產生新檔案時的落點，型別是 MigrationsOption，
// 只能傳給 NewMigrations，不能傳給 NewMigrator。
func Migrations() (*migrate.Migrations, error)

// NewMigrator 是 CLI 與測試共用的唯一 Migrator 建構點。
// 必須傳入 migrate.WithMarkAppliedOnSuccess(true)（型別是 MigratorOption，
// 只能傳給 NewMigrator）——否則失敗的 migration 會留下已套用的記錄。
// 把它收在這裡，是為了讓 cmd/dbmigrate 與 test/main_test.go 不可能拿到設定不同的 Migrator。
func NewMigrator(bunDB *bun.DB) (*migrate.Migrator, error)
```
```go
// cmd/dbmigrate —— package main，對外只有 CLI 介面（沒有可被 import 的匯出符號）
//
// 用法：go run ./cmd/dbmigrate <subcommand> --config=<name> [args...]
// 子指令（只有這六個，up / down 完全廢除且不保留別名）：
//   init        建立 bun_migrations 與 bun_migration_locks
//   migrate     套用所有尚未套用的 migration
//   rollback    回滾最後一個 migration group
//   status      列出已套用與待套用的 migration
//   unlock      清除遺留的 migration 鎖定
//   create_sql <name>  以 CreateTxSQLMigrations 產生一對 .tx.up.sql / .tx.down.sql
//
// main() 只有 os.Exit(run(os.Args[1:])) 一行；run(args []string) int 回傳 exit code。
```
Expected Goals (from BDD scenarios):
- [x] Scenario: init 建立 bun 的 migration 記錄資料表
- [x] Scenario: migrate 套用所有尚未套用的 migration
- [x] Scenario: 重複執行 migrate 不會出錯也不會重複套用
- [x] Scenario: status 同時列出已套用與待套用的 migration
- [x] Scenario Outline: 錯誤的用法會以非零 exit code 拒絕並說明原因
- [x] Scenario: 換掉 migration 機制之後 properties 資料表的結構完全相同

實作要求：
- **這是一次原子置換，必須在同一個 task 內全部做完**，交付時 `go build ./...`、`go vet ./...`、
  `go test -race -count=1 ./...` 全綠：
  1. `git mv db/20260819120000_create_properties.up.sql db/20260819120000_create_properties.tx.up.sql`
     以及對應的 down 檔。**SQL 內容一個字元都不得更動，也不得加入 `--bun:split`。**
  2. 新增 `db.Migrations()` 與 `db.NewMigrator()`（可放在新檔 `db/migrate.go` 或既有 `db/embed.go`）。
     更新 `db/embed.go` 的套件註解 —— 它目前提到 `internal/migrate`，那個套件即將不存在。
  3. **完整刪除 `internal/migrate/` 目錄**（`migrate.go`、`parse.go`、`parse_test.go`）。
  4. 重寫 `cmd/dbmigrate/main.go` 為六個子指令。
  5. 改寫 `test/main_test.go` 的 `migrateSharedPostgres`：改用 `db.NewMigrator`，且**必須先
     `Init(ctx)` 再 `Migrate(ctx)`**（bun 不會 lazily 建表）。這是本 task 唯一被允許動
     `test/main_test.go` 的機會。
  6. 改寫 `test/migrate_test.go`（原本測 up→down 往返，`up`/`down` 已不存在）。
- **`--config` 必須是明確必填的**：BDD 斷言「`migrate`（不給 `--config`）→ 非零 exit code 且 stderr
  包含 `config`」。因此**不可以再給 `--config` 預設值 `"config"`**；缺少時要自己寫一句包含
  `config` 字樣的錯誤訊息到 stderr 並回傳非零。六個子指令一律要求 `--config`（`create_sql` 也要，
  即使它不連資料庫 —— `postgres.Open` 只建連線池，不會實際連線）。
- **錯誤用法的訊息要求：** 完全不給參數 → stderr 含「用法」；未知子指令 → stderr 含該子指令原文
  （用 `%q` 印出即可，例如 `未知的子指令 "frobnicate"`，斷言是「包含」而非「等於」）。
  順序上先判斷子指令是否合法，再處理 flag。
- **`Unlock` 必須用 `defer` 寫在 `run` 裡**（先 `Lock`、`defer Unlock`），這樣即使 migration 失敗、
  `run` 回傳非零，鎖也會被釋放。`main` 的 `os.Exit` 會跳過 defer，但 `run` 的 defer 在 `run` 回傳時
  就已經執行完畢。注意 `unlock` 子指令本身不要再包一層 Lock。
- **輸出字串是被斷言的：** `migrate` 在沒有新 migration 時，stdout 必須包含 `no new migrations`；
  `rollback` 在沒有可回滾的 group 時，stdout 必須包含 `nothing to rollback`（這條由 Task 3 斷言，
  但字串在本 task 就要定下來）。`status` 的 stdout 必須包含 `create_properties`
  （來自 `MigrationsWithStatus` 回傳的 `Migration.String()`，格式是 `name_comment`）。
- **整合測試放 `test/migrate_test.go`，每個 scenario 起自己的專屬容器**
  （`testsupport.StartPostgres(t)`），**絕不可以用 `TestMain` 的共用容器**，也**不得 `t.Parallel()`**。
- **「重複執行 migrate 不會重複套用」的斷言要查 `name = '20260819120000'`**，不要用
  `LIKE '%create_properties%'` —— `bun_migrations.name` 只有時間戳，沒有名稱。
- **schema 等價性斷言**（最後一個 scenario）要逐欄比對 `information_schema.columns` 的
  `data_type` 與 `is_nullable`，並在失敗訊息中指出是哪一欄。注意用詞：`timestamp with time zone`、
  `integer`、`numeric`、`uuid`、`text`；不可空是 `is_nullable = 'NO'`。唯一索引名稱應為
  `properties_address_key`，可查 `pg_indexes`。
- `go run` 在子程式非零退出時會多印一行 `exit status N` 到 stderr，這不影響斷言；只斷言 exit code
  **非零**，不要斷言等於 1。

---

### Task 3: migration 進階行為 —— rollback group 語意、交易性、create_sql、unlock
Status: in-progress
Directory: test
Depends on: Task 2（`db.NewMigrator`、六個子指令、`.tx.` 檔名慣例、`test/migrate_test.go` 的既有輔助函式）
Pass threshold: 8.5
Provides (public interface):
```go
// 本 task 不新增任何正式程式碼的匯出介面；它交付的是 test package 內部的測試輔助函式，
// 例如（名稱可自訂，形狀應為）：
//
//   // writeTempMigration 把一對臨時 migration 檔案寫進 repo 的 db/ 目錄，
//   // 讓接下來的 `go run ./cmd/dbmigrate` 在重新編譯時把它們嵌進去。
//   // 清理必須在寫檔**之前**就用 t.Cleanup 註冊，確保測試失敗時也一定刪得掉。
//   func writeTempMigration(t *testing.T, version, name, upSQL, downSQL string)
//
// 若在實作 Task 3 的過程中發現 Task 2 的 CLI 有缺陷（例如 rollback 的輸出字串不符、
// Unlock 的 defer 沒生效），修正它是本 task 的份內事，不要為了「不動 Task 2」而繞路。
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響
- [ ] Scenario: 沒有已套用的 group 時 rollback 安全地無動作
- [ ] Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema
- [ ] Scenario: create_sql 產生一對成對的空白 migration 檔案
- [ ] Scenario: unlock 清除遺留的 migration 鎖定

實作要求：
- **每一個 scenario 都必須起自己的專屬容器**（`testsupport.StartPostgres(t)`），
  `create_sql` 那個除外（它完全不需要資料庫，不應該起任何容器）。
  **絕不可以用 `TestMain` 的共用容器，且一律不得 `t.Parallel()`。**
- **rollback group 語意：** 先 `init`、再 `migrate`（group 1 = create_properties），
  接著寫入臨時 migration `29990101000001_rollback_probe`（up 建立 `rollback_probe` 資料表、
  down 丟棄它），再 `migrate` 一次（group 2 = rollback_probe），最後 `rollback`。
  斷言：exit code 0、`rollback_probe` 不存在、`properties` 仍存在、`bun_migrations` 仍有
  `name = '20260819120000'`。**臨時檔案必須留到 `rollback` 那次 `go run` 之後才刪**
  （rollback 需要嵌入的 `.tx.down.sql` 才知道怎麼回滾）——用 `t.Cleanup` 自然就會是這個時序。
- **交易性測試是本 task 最容易做錯的一項。** 探針的 `29990101000002_tx_probe.tx.up.sql` 內容必須是：
  `CREATE TABLE tx_probe (...)` → **獨立一行 `--bun:split`（前後不得有多餘空白）** →
  一段必定失敗的 SQL（建議 `INSERT INTO no_such_table VALUES (1);`）。
  **沒有 `--bun:split` 的話這個測試會因為錯誤的理由通過** —— Postgres 的 simple query protocol
  會把整批敘述包成隱式交易，於是即使 bun 根本沒開交易，`tx_probe` 也不會留下來，完全測不到
  `.tx.` 檔名的作用。斷言：exit code 非零、`tx_probe` 不存在、`bun_migrations` 中沒有
  `name = '29990101000002'` 的紀錄（後者正是 `WithMarkAppliedOnSuccess(true)` 在把關）。
- **`create_sql`：** 在執行指令**之前**就用 `t.Cleanup` 註冊「以 glob `db/*_add_tenant_table.tx.*.sql`
  找出並刪除」的清理函式 —— 留下來的樣板檔案會被之後每次 `go build` / `go run` 嵌入並當成真的
  migration 套用，也極可能被誤 commit。指令形狀是
  `go run ./cmd/dbmigrate create_sql --config=test add_tenant_table`（名稱是 flag 之後的位置參數）。
  斷言：exit code 0、存在一個以 `_add_tenant_table.tx.up.sql` 結尾的檔案、一個以
  `_add_tenant_table.tx.down.sql` 結尾的檔案、兩者共用同一個 14 位數字時間戳前綴。
- **`unlock`：** 用 `db.NewMigrator(bunDB).Lock(ctx)` 在測試行程內製造鎖定（需先 `Init`），
  **不要自己 INSERT 一列** —— bun 的 `Unlock` 以 `WHERE table_name = <formattedTableName>` 刪除，
  手寫 INSERT 很容易把 `table_name` 填成對不上的值，於是 `unlock` 明明成功卻刪不到東西。
  然後跑 `go run ./cmd/dbmigrate unlock --config=test`，斷言 exit code 0 且
  `bun_migration_locks` 的列數為 0。

---

### Task 4: 由 spec 產生 api/api.gen.go
Status: pending
Directory: api, test
Depends on: none
Provides (public interface):
```go
// api/oapi-codegen.yaml —— 產生器設定（使用者指定，不要改動這五行的語意）
//   package: api
//   generate: models / chi-server / embedded-spec / strict-server
//   output: api.gen.go
//
// api/generate.go —— 只有兩行有意義的內容：
//   package api
//   //go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml openapi.yaml
// go generate 以該檔所在目錄為工作目錄，因此 output: api.gen.go 會正確落在 api/api.gen.go。
//
// api/api.gen.go —— 產生的檔案，必須提交進版控，且**絕對不得手動編輯**。
// 它匯出 Known Context §H 列出的所有型別與函式：StrictServerInterface、各
// *RequestObject / *ResponseObject、models（Property、PropertyList、ErrorBody、
// HealthReport、CreatePropertyRequest、UpdatePropertyRequest、ChangeStatusRequest、
// ListPropertiesParams…）、參數綁定錯誤型別（InvalidParamFormatError、RequiredParamError、
// UnmarshalingParamError、TooManyValuesForParamError、RequiredHeaderError，
// 每個都有 ParamName 欄位）、ChiServerOptions、StrictHTTPServerOptions、
// HandlerWithOptions、NewStrictHandlerWithOptions、GetSwagger。
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: 重新產生程式碼不會產生任何差異

實作要求：
- **先改 `api/openapi.yaml`，再產生。** 必須為 `listProperties` operation 補上 `400` 回應
  （`content: application/json: schema: $ref: "#/components/schemas/ErrorBody"`），因為本輪起
  `?page=abc` 會回 400（見 Known Context §I.2），spec 不補就會說謊。順便修掉 `servers:` 區塊
  description 中對 `test/contract_test.go` 的描述 —— 那個檔案本輪會被刪除。
  **除此之外不得改動 spec 的任何語意**（路徑、schema、enum、pattern 一律不動）。
- 新增 `api/oapi-codegen.yaml` 與 `api/generate.go`，跑 `go generate ./api/...` 產生 `api/api.gen.go`。
- **`go.mod` 只加 `github.com/oapi-codegen/runtime`**（產生的程式碼會 import 它與
  `github.com/oapi-codegen/runtime/types`）。**產生器本身刻意不加進 `go.mod` 的 tool 區塊** ——
  加了就得連整個產生器一起 vendor。**接著必須跑 `go mod vendor`**，否則 `go build ./...` 會失敗。
- 整合測試放 `test/codegen_test.go`：讀下 `api/api.gen.go` 的內容 → 在 repo 根目錄跑
  `go generate ./api/...`（用 `testsupport.RepoRoot(t)` 當工作目錄，並給足夠寬鬆的 timeout，
  因為第一次會下載產生器）→ 再讀一次 → **逐位元組比對**。不相同時失敗訊息要指出第一個相異的位元組
  位置，不要只說「不同」。測試結束前必須把檔案還原成原本的內容（用 `t.Cleanup`），
  以免產生器版本漂移時汙染工作目錄。
- **本 task 交付時 `api/api.gen.go` 只是被編譯、還沒有人使用**，這是正常的：`go build ./...`、
  `go vet ./...` 與整個測試套件（此時 `test/contract_test.go` 仍在）必須全綠。
- **不得修改 `test/main_test.go`。**

---

### Task 5: spec-first handler 與統一錯誤中介層
Status: pending
Directory: internal/property/httpapi, internal/health, internal/apihttp, internal/httpx, cmd/api, test
Depends on: Task 1（`apperr` sentinel 與 `With*`）、Task 4（`api` 套件的產生型別與 `StrictServerInterface`）
Pass threshold: 8.5
Provides (public interface):
```go
package httpapi // internal/property/httpapi —— 完全改寫

// API 實作 api.StrictServerInterface 中屬於 property 的 5 個 operation。
// 不再需要 logger——handler 只回傳 error，寫回應與記錄日誌都是中介層的事。
type API struct{ /* svc *property.Service */ }
func NewAPI(svc *property.Service) *API

func (a *API) ListProperties(ctx context.Context, req api.ListPropertiesRequestObject) (api.ListPropertiesResponseObject, error)
func (a *API) CreateProperty(ctx context.Context, req api.CreatePropertyRequestObject) (api.CreatePropertyResponseObject, error)
func (a *API) GetProperty(ctx context.Context, req api.GetPropertyRequestObject) (api.GetPropertyResponseObject, error)
func (a *API) UpdateProperty(ctx context.Context, req api.UpdatePropertyRequestObject) (api.UpdatePropertyResponseObject, error)
func (a *API) ChangePropertyStatus(ctx context.Context, req api.ChangePropertyStatusRequestObject) (api.ChangePropertyStatusResponseObject, error)

// NewHandler 與 (*Handler).Mount 一併刪除——路由改由產生的程式碼提供。

package health // internal/health

// API 實作 api.StrictServerInterface 中的 GetHealthz。
type API struct{ /* svc *Service */ }
func NewAPI(svc *Service) *API
func (a *API) GetHealthz(ctx context.Context, req api.GetHealthzRequestObject) (api.GetHealthzResponseObject, error)
// 全 ok → api.GetHealthz200JSONResponse；任一 down → api.GetHealthz503JSONResponse。
// health.Mount 必須刪除——/healthz 現在由產生的路由表提供，兩者並存會讓 chi 因重複註冊而 panic。
// 刪掉 Mount 之後 internal/health 就不再 import internal/server 了。

package apihttp // internal/apihttp —— 新套件：產生的 HTTP 層與 apperr 之間的唯一接線點
                // 它 import api / apperr / httpx / server / chi，但**不 import 任何 feature 套件**，
                // 所以它不是組裝點，可以被單元測試直接測。

// Mount 把產生的路由掛到 chi router 上，並把三個 error hook 全部接上。
// 內部做的事：NewStrictHandlerWithOptions(si, nil, StrictHTTPServerOptions{
//   RequestErrorHandlerFunc: RequestErrorHandler(logger),
//   ResponseErrorHandlerFunc: ResponseErrorHandler(logger)})
// 再 api.HandlerWithOptions(strict, api.ChiServerOptions{
//   BaseRouter: r, ErrorHandlerFunc: ParamErrorHandler(logger)})
func Mount(si api.StrictServerInterface, logger *slog.Logger) server.Mount

// ParamErrorHandler 對應 ChiServerOptions.ErrorHandlerFunc（路徑／查詢參數綁定失敗）。
// 以 errors.AsType 逐一比對五個產生的參數錯誤型別，取出 ParamName 當作
// details[].field，轉成 apperr.ValidationFailed.WithDetails(...) 後交給 httpx.WriteError。
// 取不出 ParamName 時（理論上不會發生）仍必須產出 VALIDATION_FAILED 400，field 留空。
func ParamErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error)

// RequestErrorHandler 對應 StrictHTTPServerOptions.RequestErrorHandlerFunc（body 解析失敗）。
// 一律轉成 apperr.ValidationFailed.WithMessage("無法解析請求 JSON").WithError(err) → 400。
func RequestErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error)

// ResponseErrorHandler 對應 StrictHTTPServerOptions.ResponseErrorHandlerFunc
// （handler 回傳的 error，也就是 apperr 落地的地方）。直接交給 httpx.WriteError，
// 由它負責「抽得出 *apperr.Error 就照 code 對映狀態碼，抽不出就降級成 INTERNAL 500
// 且不外洩原始訊息」。
func ResponseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error)
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: spec 宣告的每一個 endpoint 都真的路由得到
- [ ] Scenario: 既有的 API 行為在改用產生的 handler 之後完全不變
- [ ] Scenario: handler 回傳 apperr 時由中介層轉成對應的狀態碼與 body
- [ ] Scenario: 領域層的衝突錯誤同樣經由中介層轉譯
- [ ] Scenario: 無法解析的 request body 由中介層轉成 400
- [ ] Scenario: 路徑參數格式錯誤由中介層轉成 400
- [ ] Scenario: 查詢參數型別錯誤由中介層轉成 400
- [ ] Scenario: 未分類的錯誤降級為 500 且不外洩底層訊息
- [ ] Scenario Outline: 每一條錯誤路徑的回應都是統一的 JSON 形狀

實作要求：
- **這也是一次原子置換。** 舊的 `httpapi.Handler`/`Mount` 與 `health.Mount` 一旦刪除，路由就必須
  同時由產生的程式碼接手，不能留下中間狀態。同一個 task 內完成：
  1. 改寫 `internal/property/httpapi`（刪除手寫 DTO、`parseListFilter`、`parsePathID`、
     `Mount`，改為五個 operation 方法）。
  2. 為 `internal/health` 新增 `API`，刪除 `Mount`。
  3. 新增 `internal/apihttp`。
  4. 新增 `cmd/api/server.go`：`type apiServer struct { *health.API; *httpapi.API }` 加上
     `var _ api.StrictServerInterface = (*apiServer)(nil)`，並在 `cmd/api/main.go` 改成
     `server.NewRouter(server.Options{...}, apihttp.Mount(&apiServer{...}, logger))`。
  5. **刪除 `test/contract_test.go` 整個檔案**（不得以任何形式改寫保留）。
  6. `internal/httpx.DecodeJSON` 在改寫後不再有任何呼叫端（body 解析改由產生的 strict handler
     負責），連同它的單元測試一併刪除。`httpx.ErrorBody`、`WriteJSON`、`WriteError`、`RequestID`、
     `Recoverer` 全部保留。
- **錯誤回應的 wire 形狀繼續用 `httpx.ErrorBody`，不要改用產生的 `api.ErrorBody`** ——
  後者的 `Details` 是 `*[]FieldError`（指標包 slice），用起來只會多一層無謂的 nil 處理，
  而兩者的 JSON 形狀完全一致。產生的 `*400JSONResponse`／`*404JSONResponse`／`*409JSONResponse`
  因此會變成「產生了但沒有人使用」，這是刻意的。
- **金額必須用 `StringFixed(2)`** 填進 `api.Property` 的 `AreaPing`／`MonthlyRent`／`ManagementFee`
  （都是 `string`）。既有整合測試斷言 `"25000.50"`，用 `String()` 會得到 `"25000.5"` 而失敗。
- **`ListProperties` 的 `Items` 必須用 `make([]api.Property, 0, n)`** —— nil slice 會 marshal 成
  `null` 而不是 `[]`，而且反序列化後的斷言抓不到，要看原始 bytes。
- **`ListProperties` 的查詢參數轉換：** `Params.Page`／`PageSize` 是 `*int`，nil 時傳 0 讓
  `property.ListFilter.normalize` 補預設值。`Status`／`RentalMode` 是具名 string 型別的指標，
  **必須維持既有的寬鬆語意**：轉成 `property.Status`／`property.RentalMode` 後只有 `Valid()`
  為真才設定篩選條件，非法 enum 值視為「不篩選該欄位」而不是錯誤（產生的綁定不驗證 enum 成員）。
  `City` 是 `*string`，nil 時傳空字串。
- **`ChangePropertyStatus` 仍要自行驗證 status 是合法列舉值**（產生的程式碼不會驗），
  非法時回傳 `apperr.ValidationFailed.WithDetails(apperr.FieldError{Field: "status", ...})`。
- **`GetProperty`／`UpdateProperty`／`ChangePropertyStatus` 的 `req.Id` 已經是解析好的 UUID**
  （`openapi_types.UUID` 是 `uuid.UUID` 的別名），不需要也不應該再自己解析路徑參數 ——
  格式錯誤在綁定階段就被 `ParamErrorHandler` 攔下了。
- **`req.Body` 是指標，可能為 nil**（雖然 spec 標了 required，產生的程式碼不會強制）。
  nil 時回傳 `apperr.ValidationFailed.WithMessage(...)`，不要 nil deref。
- **不得修改 `test/` 底下任何既有測試的斷言**（`health_test.go`、`startup_test.go`、`panic_test.go`、
  `property_create_test.go`、`property_query_test.go`、`property_update_test.go`），
  也**不得修改 `test/main_test.go`**。這正是 scenario「既有的 API 行為在改用產生的 handler 之後完全不變」
  在檢查的事。`internal/property/httpapi` 底下的**單元**測試（`httpapi_test.go`、`query_test.go`）
  則必須跟著改寫，因為被測的函式已經不存在 —— 注意 `query_test.go` 目前斷言 `page=abc → 0`，
  那條語意本輪被刻意推翻，該案例應改為由 `ParamErrorHandler` 的單元測試涵蓋。
- **新增的整合測試：**
  - `test/routing_test.go` —— 對 spec 宣告的六個 path+method 各送一次請求，斷言沒有任何一次
    回應是 chi 的預設 404（body 為 `"404 page not found"` 的純文字），且每一次的 `Content-Type`
    都以 `application/json` 開頭。路徑清單可以直接用 `api.GetSwagger()` 讀出來，這樣新增 endpoint
    時不會漏測。
  - `test/error_shape_test.go` —— 對應 Scenario Outline 的五個範例（路徑參數綁定失敗、查詢參數
    綁定失敗、body 解析失敗、handler 回傳 apperr、領域層驗證失敗），逐一斷言 `Content-Type` 以
    `application/json` 開頭、body 可解析成含 `code`／`message`／`request_id` 的物件，且
    `request_id` 不是空字串。
  - 404／409／400 的個別 scenario（`code` 等於 `PROPERTY_NOT_FOUND` /
    `PROPERTY_INVALID_STATUS_TRANSITION` / `VALIDATION_FAILED`，以及 `details[].field` 為
    `id`／`page`）可以放在同一個 `test/error_shape_test.go` 裡。
- **「未分類的錯誤降級為 500 且不外洩底層訊息」用單元測試涵蓋**
  （`internal/apihttp/apihttp_test.go`）：直接呼叫 `ResponseErrorHandler(logger)` 並傳入
  `errors.New("pq: connection refused on 10.0.0.7")`，斷言狀態碼 500、`code` 為 `INTERNAL`、
  且 `message` 不包含 `10.0.0.7`。這個路徑沒辦法從真實跑起來的服務可靠地觸發。

---

### Task 6: 回應防護網 —— 讓「漏接 error hook」在結構上不可能外洩純文字
Status: pending
Directory: internal/httpx, internal/server, test
Depends on: Task 5（已組裝完成的 router、產生的 handler 與三個 hook）
Pass threshold: 8.5
Provides (public interface):
```go
package httpx // internal/httpx

// EnsureJSONError 是最外層的回應防護網 middleware。
//
// 它在 WriteHeader 當下判斷：狀態碼 >= 400 且 Content-Type 不以 "application/json" 開頭時，
// 就把這次回應改寫成統一形狀的 JSON——狀態碼**保持不變**，body 換成
// ErrorBody{Code: "INTERNAL", Message: <固定訊息>, RequestID: <本次 request id>}，
// 並記一筆 slog Warn 指出攔截到了未統一形狀的錯誤回應（含原本的 Content-Type 與狀態碼）。
// 被攔截之後，下游後續的 Write 呼叫必須被吞掉（回報寫入成功，實際丟棄），
// 否則原本那段純文字會接在 JSON 後面一起送出去。
//
// 刻意**不緩衝回應主體**：狀態碼與 Content-Type 在 WriteHeader 當下就已確定，判斷在那一刻
// 完成即可。代價是不支援串流回應——本 API 全部是小型 JSON，沒有串流端點。
//
// 攔截時必須 Del("Content-Length")（下游若是 http.Error 寫的，長度會對不上）
// 並覆寫 Content-Type，再呼叫底層的 WriteHeader。
func EnsureJSONError(logger *slog.Logger) func(http.Handler) http.Handler

package server // internal/server
// NewRouter 的 middleware 順序變為：
//   RequestID → access log → EnsureJSONError → Recoverer
// 簽章不變（logger 已經在 Options 裡，不需要新增欄位，也不得改動參數列形狀）。
```
Expected Goals (from BDD scenarios):
- [ ] Scenario: handler 內的 panic 仍然轉成統一形狀的 500
- [ ] Scenario: 漏接的 error hook 不會讓純文字外洩給呼叫端
- [ ] Scenario: 防護網不會干擾正常的成功回應

實作要求：
- 防護網必須掛在 **router 層級**（`internal/server.NewRouter` 的 middleware chain），不是只包住
  產生的那幾條路由 —— 這樣 `/debug/panic` 之類非產生的路由也在保護範圍內。
  `internal/server` 已經 import `internal/httpx`，所以不會引入新的相依方向。
- **狀態碼一定要保留**：BDD 斷言「回應狀態碼**仍為** 400」。防護網只換 body 與 Content-Type，
  不改狀態碼。
- **成功回應必須逐位元組不變**：`status < 400` 一律原樣放行，連 header 都不要碰。
- 必須正確處理「handler 沒有明確呼叫 `WriteHeader` 就直接 `Write`」的情況（隱含 200）。
- **「漏接的 error hook」scenario 必須用真正未接 hook 的產生 handler 來測，不要用手寫的
  `http.Error` 假裝。** 放在 `internal/httpx/safetynet_test.go`：用
  `api.NewStrictHandler(stub, nil)`（**不帶 options**，因此套用純文字預設值）＋
  `api.Handler(...)`（同樣不帶 `ErrorHandlerFunc`），外面包上 `EnsureJSONError`，
  然後請求 `GET /api/v1/properties/not-a-valid-uuid`。斷言：狀態碼 400、`Content-Type` 以
  `application/json` 開頭、`code` 為 `INTERNAL`、`request_id` 非空（測試需要一併套上
  `httpx.RequestID` middleware 才拿得到）、且原本那段純文字
  （`Invalid format for parameter id: ...`）完全不出現在 body 中，並確認 logger 收到一筆 Warn。
  `internal/httpx` 的**測試檔** import `api` 不會造成 import cycle（`api` 不 import `httpx`）。
- **「不干擾成功回應」要有兩層驗證**：單元層比對「同一個 handler 掛與不掛防護網」兩次回應的
  原始 bytes 完全相同；整合層在 `test/` 對真實跑起來的服務送一個會成功的
  `GET /api/v1/properties`，斷言 200 且 body 是合法的 `PropertyList`（含 `items` 為 `[]` 而非
  `null` —— 要看原始 bytes）。
- **panic scenario** 沿用既有的 `test/panic_test.go`，**不得修改它的斷言**；本 task 要確認
  `Recoverer` 寫出的 JSON 500 會被防護網原樣放行（Content-Type 已經是 `application/json`）。
  順序上 `EnsureJSONError` 在 `Recoverer` 外層，所以 Recoverer 的輸出會經過防護網 —— 這是刻意的，
  正好順便保證 panic 路徑也不可能外洩純文字。
- **不得修改 `test/main_test.go`。**

## Coverage Check

- Scenario: 重新產生程式碼不會產生任何差異 → Task 4
- Scenario: spec 宣告的每一個 endpoint 都真的路由得到 → Task 5
- Scenario: 既有的 API 行為在改用產生的 handler 之後完全不變 → Task 5
- Scenario: handler 回傳 apperr 時由中介層轉成對應的狀態碼與 body → Task 5
- Scenario: 領域層的衝突錯誤同樣經由中介層轉譯 → Task 5
- Scenario: 無法解析的 request body 由中介層轉成 400 → Task 5
- Scenario: 路徑參數格式錯誤由中介層轉成 400 → Task 5
- Scenario: 查詢參數型別錯誤由中介層轉成 400 → Task 5
- Scenario: 未分類的錯誤降級為 500 且不外洩底層訊息 → Task 5
- Scenario: handler 內的 panic 仍然轉成統一形狀的 500 → Task 6
- Scenario Outline: 每一條錯誤路徑的回應都是統一的 JSON 形狀 → Task 5
- Scenario: 漏接的 error hook 不會讓純文字外洩給呼叫端 → Task 6
- Scenario: 防護網不會干擾正常的成功回應 → Task 6
- Scenario: apperr 的共用 sentinel 不會被 WithError 汙染 → Task 1
- Scenario: init 建立 bun 的 migration 記錄資料表 → Task 2
- Scenario: migrate 套用所有尚未套用的 migration → Task 2
- Scenario: 重複執行 migrate 不會出錯也不會重複套用 → Task 2
- Scenario: status 同時列出已套用與待套用的 migration → Task 2
- Scenario Outline: 錯誤的用法會以非零 exit code 拒絕並說明原因 → Task 2
- Scenario: rollback 只回滾最後一個 group，先前的 group 不受影響 → Task 3
- Scenario: 沒有已套用的 group 時 rollback 安全地無動作 → Task 3
- Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema → Task 3
- Scenario: 換掉 migration 機制之後 properties 資料表的結構完全相同 → Task 2
- Scenario: create_sql 產生一對成對的空白 migration 檔案 → Task 3
- Scenario: unlock 清除遺留的 migration 鎖定 → Task 3

（共 25 列，與 BDD spec 的 25 個 scenario 一一對應，每個 scenario 恰好出現一次。）

## Integration Scenarios

以下 scenario 只有在多個 task 組裝起來之後才真正成立，`hars-verify` 會在全部 task 完成後對
已組裝的系統重跑一次。它們在上面的 Coverage Check 中仍各只計一次。

- Scenario: spec 宣告的每一個 endpoint 都真的路由得到
- Scenario: 既有的 API 行為在改用產生的 handler 之後完全不變
- Scenario: handler 回傳 apperr 時由中介層轉成對應的狀態碼與 body
- Scenario: 領域層的衝突錯誤同樣經由中介層轉譯
- Scenario Outline: 每一條錯誤路徑的回應都是統一的 JSON 形狀
- Scenario: handler 內的 panic 仍然轉成統一形狀的 500
- Scenario: 漏接的 error hook 不會讓純文字外洩給呼叫端
- Scenario: 防護網不會干擾正常的成功回應
- Scenario: migration 中途失敗時整個 migration 回滾，不留下半套 schema

## Iteration Log

### Task 1 — Iter 1 — score 8.3/10 — PASS（但留有 [error] 等級發現）
- Changed: `internal/apperr/apperr.go`（五個 sentinel、`clone()`、`WithError`/`WithMessage`/`WithDetails`、`Is`）、
  `internal/apperr/apperr_test.go`（4 個新測試）。
- 驗證：`go vet ./internal/apperr/...` 乾淨；`go test -race -count=1 ./internal/apperr/...` 13 個測試全過；
  `go build ./...` 與 `go test -race -count=1 ./internal/... ./db/...` 全綠，既有呼叫端未受影響。
- Remaining: Evaluator 實證 `TestSentinel_WithDetails_DoesNotShareBackingArray` 是偽陽性——
  把 `clone()` 的 Details 深拷貝分支整個移除後測試仍然全綠。原因：(1) `WithDetails` 無條件以
  `append(nil, details...)` 覆寫 `c.Details`，`clone()` 的拷貝邏輯在該路徑永遠不被執行；
  (2) 變參陣列 `cap == len == 1`，測試中的 `append` 必定重新配置，原理上無法觀察 aliasing。
  實作正確但該防線未受測；Task 5 會大量使用 `WithDetails`，故以 Iter 2 補強測試。

### Task 1 — Iter 2 — PASS — Task 1 完成
- Changed: 僅 `internal/apperr/apperr_test.go`（新增 `TestSentinel_Clone_DoesNotAliasDetailsBackingArray`
  與 `TestSentinel_Clone_DerivedFromDerived_DoesNotAliasDetails`）。`apperr.go` 最終狀態與 Iter 1
  逐位元組相同 —— 本輪只補測試，不動已經正確的實作。
- 關鍵作法：基底的 `Details` 刻意保留剩餘容量（`len 1, cap 8`），並改用 `WithMessage`（會經過
  `clone()` 但不重建 Details）作為衍生路徑 —— `WithDetails` 會把 Details 整個重建，正好遮住 bug。
- **Coordinator 獨立驗證（不採信 Executor 自述）：** 親手把 `clone()` 退化為 `c := *e; return &c`，
  兩個新測試如預期轉紅（其餘測試全數仍過，證明抓到的正是這條防線）；還原後與備份逐位元組相同、
  測試轉綠。`go build ./...`、`go test -race -count=1 ./internal/... ./db/...` 全綠。
- Iter 1 的 [error] 發現已關閉。

### Task 2 — Iter 1 — score 9.1/10 — PASS — Task 2 完成
- Changed: `db/*.sql` 兩個檔以 `git mv` 改名為 `.tx.up.sql` / `.tx.down.sql`（內容零變動，已由
  `git diff -M --stat -- db/` 證實）、新增 `db/migrate.go`（`Migrations()` / `NewMigrator()`）、
  更新 `db/embed.go` 註解、**刪除整個 `internal/migrate/`**（三個檔）、重寫 `cmd/dbmigrate/main.go`
  為六個子指令、`test/main_test.go` 僅改 `migrateSharedPostgres` 內部、重寫 `test/migrate_test.go`
  並提供 `runDBMigrate` 供 Task 3／4 沿用。`go.mod` / `go.sum` 未變動（bun/migrate 已在 vendor 中）。
- 驗證：`go build ./...`、`go vet ./...`、`go test -race -count=1 ./...` 全綠（`test` 套件 22.4s）。
- **Coordinator 獨立驗證：** `git status` 顯示兩個 SQL 為 `R`（純改名）且無內容 diff；
  `internal/migrate` 三檔為 `D`；`WithMarkAppliedOnSuccess(true)` 與 `WithMigrationsDirectory("db")`
  分別掛在正確的建構子上；`create_sql` 走 `CreateTxSQLMigrations`。
- Evaluator 特別查證了「`up`／`down` 短子字串斷言是否可能因錯誤理由通過」：不會 ——
  該斷言只可能由程式自己以 `%q` 回吐的「未知的子指令」訊息滿足，固定的子指令清單
  （init／migrate／rollback／status／unlock／create_sql）不含 `up` 或 `down` 子字串。
- Coordinator 後續修掉兩個小瑕疵（改完重跑 build／vet／`./test/...` 全綠）：
  1. `test/migrate_test.go` 的 `errors.As` 改為專案慣例的 `errors.AsType[*exec.ExitError]`；
  2. `db/embed.go` 一行以 `go:embed` 開頭的中文註解被 staticcheck 判為無效編譯指令（SA9009），
     改寫首字避免誤導；真正的 `//go:embed *.sql` 未受影響。
