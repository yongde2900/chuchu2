# PLAN-001 — 包租代管系統：服務骨架 + 物件（房源）建檔與查詢垂直切片
Created: 2026-08-19
Status: in-progress
Approved: 2026-08-19
Working Directory: .
BDD Spec: ./bdd/BDD-001-property-service-skeleton.feature
Language: Go 1.26.2（開發機 darwin/arm64；module `github.com/yongde2900/chuchu2`）
Build cmd: go build ./...
Test cmd:  go test -race ./...
Lint cmd:  go vet ./...
Pass threshold: 7.0
Max iterations: 6

## Known Context

本專案沒有知識庫（`./knowledge/` 不存在），以下全部是規劃階段與使用者確立、且對執行者具約束力的決定。
執行者讀這份文件時不會有本次對話的記憶，因此每一條都寫成可獨立閱讀的完整敘述。

- **這是全新的 greenfield repo。** repo 根目錄目前只有 `.git` 與 `.claude`，沒有任何 Go 程式碼、
  沒有 `go.mod`、沒有任何既有檔案可參考 —— 每一個檔案都是從零建立 —— 理由：使用者從空 repo 起步，
  因此 Task 1 必須先把 `go.mod` 與目錄骨架立起來，在那之前任何檔案都無法編譯。
- **Go module path 為 `github.com/yongde2900/chuchu2`。** 這個 repo **尚未設定任何 git remote**，
  module path 是依 git 使用者名稱決定的 —— 理由：需要一個穩定的 import prefix —— 約束：不要假設這個
  module 可以被 `go get` 抓到，也不要嘗試發佈或引用任何 `github.com/yongde2900/*` 的外部套件。
- **領域是「包租代管」**（台灣的租賃住宅管理業，受《租賃住宅市場發展及管理條例》規範）。系統必須在
  「物件（房源）」這一層區分兩種在法律上截然不同的營運模式：
  - **包租 / `MASTER_LEASE`** —— 業者向房東「承租」整個物件，不論是否出租、每月都要付給房東固定
    租金，再轉租給房客。此模式下存在**兩份**租約（業者↔房東、業者↔房客）。業者的收益為
    「轉租租金 − 承租租金」，**空置風險由業者承擔**。
  - **代管 / `MANAGED`** —— 業者只負責管理，租約只有**一份**（房東↔房客），業者是代理人，收取
    管理服務費。**空置風險由房東承擔**。
  這個旗標決定了下游所有東西的形狀：會產生幾份租約記錄、應收／應付款的方向、房東拿到的是固定租金
  還是扣除服務費後的淨額。本輪不做租約與帳務，但旗標現在就要存下來，讓下一輪可以在其上建構。
- **已知的模型取捨，是刻意決定的：** 嚴格來說「包租 vs 代管」是業者↔房東之間那份**委託契約**
  （委託管理／包租契約）的屬性，不是實體房屋單位的屬性 —— 同一個單位在契約換約時是可以換模式的。
  本輪把它**扁平化成 `properties` 資料表上的一個欄位**，因為「委託契約」這個 aggregate 目前還不存在。
  使用者已被完整告知這個取捨的代價（未來需要一次資料遷移，且無法保留「哪段期間套用哪種模式」的歷史），
  並選擇接受 —— 約束：**不要**自作主張用新增「委託契約資料表」的方式來「修正」這件事。
- **金錢一律使用 `github.com/shopspring/decimal`，端到端不得出現 float** —— 理由：與使用者其他 Go
  服務的慣例一致，且浮點運算會讓租金／押金金額產生誤差。Postgres 欄位型別為 `NUMERIC`；
  **JSON 表示法是「字串」**（`"25000.50"`），**絕不是 JSON number** —— 理由：JSON number 無法在
  JavaScript 客戶端往返而不損失十進位精度。BDD 斷言的是**精確的字串形式**，因此序列化時必須保留小數位數
  （`NUMERIC(12,2)` → `"25000.50"`，不是 `"25000.5"`）。注意 `decimal.Decimal` 預設的 `MarshalJSON`
  **不會**補齊小數位數，所以 response DTO 必須用 `StringFixed(2)` 這類方式明確格式化成 `string` 欄位。
- **傳輸層是 REST/JSON over chi。** 使用者的其他 repo 用的是 Connect RPC + buf/proto，但使用者針對這個
  專案**明確選擇了不含 proto 的技術棧** —— 約束：不要引入 buf、protobuf、或任何從 IDL 產生程式碼的機制。
- **API 介面在 spec gate 已經談定：** `POST /api/v1/properties`、`GET /api/v1/properties/{id}`、
  `GET /api/v1/properties`（查詢參數 `page`、`page_size`、`status`、`rental_mode`、`city`）、
  `PATCH /api/v1/properties/{id}`、`POST /api/v1/properties/{id}/status`、以及 `GET /healthz`。
  **狀態變更走自己的 endpoint 而不是走 PATCH** —— 理由：讓狀態轉換規則只有一個強制點。
- **物件狀態機：** `VACANT`（空置）、`OCCUPIED`（出租中）、`RENOVATING`（整修中）、`DELISTED`（已下架）。
  合法轉換為 VACANT↔RENOVATING、VACANT↔OCCUPIED、VACANT↔DELISTED、以及 RENOVATING→DELISTED。
  其餘一律拒絕，**包含同狀態轉換（VACANT→VACANT 也是非法的）**。特別注意
  DELISTED→OCCUPIED、DELISTED→RENOVATING、OCCUPIED→DELISTED **全部非法** —— 已下架的物件必須先回到
  VACANT 才能重新上架，出租中的物件必須先退租（回到 VACANT）才能下架。
- **物件的唯一鍵是 `(city, district, street_address, floor, room_no)`** 五個欄位的組合 —— 用來擋掉同一個
  門牌重複建檔。
- **房東資訊本輪內嵌在物件上**，就是 `landlord_name` 與 `landlord_phone` 兩個欄位。**刻意沒有房東資料表、
  沒有外鍵。** 詳見下方 Out of Scope。
- **測試基礎設施：** 用 `testcontainers-go` 在每次測試執行時啟一個用完即丟的 Postgres（以及 Redis）。
  開發機上 **Docker 可用**（server version 29.4.1）。開發機上**沒有**原生執行的 Postgres、**沒有**原生
  執行的 Redis，`redis-cli` 也沒有安裝 —— 因此測試**絕對不可以假設有任何預先啟動的服務**。
  `go test -race ./...` 必須在一台只開著 Docker 的冷機器上就能全綠。第一次執行會拉 image，這是預期行為，
  也是本計畫把 `Max iterations` 設為 6 的原因。
- **`api/openapi.yaml` 是本專案 API 契約的唯一權威來源** —— 理由：使用者選了不含 proto/IDL 的 REST
  技術棧，其他 repo 由 `proto/` 擔任的 API 文件角色在這裡是空的，而本輪「只做 API、不分入口」意謂
  這批 endpoint 遲早要交給前端串接。手寫的 OpenAPI 若無人查核，第一次改欄位就開始說謊，因此本計畫
  以**契約測試**強制它與實作一致（Task 8）—— 約束：**任何 task 新增或修改 endpoint、請求／回應欄位、
  錯誤形狀時，都必須在同一個 task 內同步更新 `api/openapi.yaml`**，不可留給後面的 task 補。
  文件格式為 **OpenAPI 3.1**；驗證建議使用 `getkin/kin-openapi`（`openapi3` 解析 + `openapi3filter`
  驗證回應）。金額欄位在 schema 中必須宣告為 `type: string`（並以 `pattern` 約束為十進位數字形式），
  **不得宣告為 number** —— 這與「金額 JSON 表示法是字串」那條決定是同一件事的兩面。
- **`golangci-lint` 雖然裝在開發機上，但本計畫的 Lint gate 刻意是 `go vet ./...`** —— 理由：零設定、
  結果穩定，讓 Executor 迴圈不會在一個全新 repo 上為了風格意見而空轉 —— 約束：**不要**把 golangci-lint
  變成 gate，也不要為了它新增 `.golangci.yml`。

### Out of Scope & Ungraded Constraints

以下每一條都是**決定**，不是遺漏。沒有讀到這一段的 Executor 會「很有幫助地」把它們做出來，那是錯的。

- **不做**房東主檔與房客主檔這兩個獨立 aggregate 及其 CRUD；物件本輪以 `landlord_name` /
  `landlord_phone` 兩個內嵌欄位承載房東資訊，正規化成獨立資料表留待後續的 plan。
- **不做**租約（簽訂、續約、提前終止）與租金應收／應付帳務。物件狀態本輪由人工呼叫 status endpoint 變更，
  尚未由任何租約事件驅動。
- **不做**認證與授權。本輪所有 endpoint 皆為未保護狀態。Redis 連線**要**建立起來（並納入健康檢查），
  好讓後續回合可以把 session 放上去，但本輪不會有任何讀寫 session 的行為。
- **不做**房東端／房客端入口，也不做任何權限模型。本輪就是單一、未分眾的 API 介面。
- **不做**修繕報修、報表、通知，以及檔案上傳（房屋照片、合約掃描檔）。
- **不做**正式環境部署設定 —— CI/CD、容器映像檔、K8s manifest 一律不做。
- **未評分約束（UNGRADED）：** 程式碼註解與產品術語一律使用**繁體中文**，與使用者其他 repo 的慣例一致。
  沒有任何 scenario 會檢查這件事，由人工把關。
- **未評分約束（UNGRADED）：** **分層邊界必須真實存在** —— HTTP handler 絕對不可以直接碰 bun DB，
  repository 絕對不可以回傳 HTTP 形狀的型別。這是本輪骨架最主要的價值，但它寫不成可斷言的 Then，
  **harness 抓不到違規**，只能靠人工在 code review 把關。因此本計畫刻意讓分層變成**套件邊界**：
  `internal/property`（純領域＋service＋repository 介面，不 import `bun`、不 import `net/http`）、
  `internal/property/pgrepo`（唯一 import bun 的地方）、`internal/property/httpapi`（唯一 import
  chi/net/http 的地方）。違規會表現成「錯的套件出現了錯的 import」，肉眼掃 import 區塊即可發現。

## Project Conventions

- **技術棧：** chi v5 router、bun ORM over Postgres（pgdriver）、Redis client、viper 設定、
  shopspring/decimal 處理金額、log/slog 結構化日誌、testcontainers-go 供測試使用。
- **設定：** 以 viper 載入，由 `--config=<name>` flag 選擇，解析成 `config/<name>.yaml`。
  **不提交 `.env`**。必要的 key 缺少時必須在啟動時**大聲失敗，並在訊息中指名缺少的 key** ——
  BDD 斷言那個 key 的名字會出現在 stderr。設定值必須可由環境變數覆寫（例如 `CHUCHU_POSTGRES_DSN`
  覆寫 `postgres.dsn`），測試才能在不改動已提交的 `config/test.yaml` 的前提下注入 testcontainers
  隨機產生的 DSN／addr。
- **Migration：** 版本化的 `db/<timestamp>_<name>.{up,down}.sql` 成對檔案，由 `cmd/dbmigrate` 這個
  binary 套用，支援 `up` 與 `down` 兩個子指令。**每一個 up 都必須有能真正跑起來的 down** ——
  BDD 斷言 up→down 的往返。
- **錯誤：** 單一的、帶有穩定機器可讀 code 的應用層錯誤型別，在 HTTP 層的**恰好一個地方**映射成 HTTP
  狀態碼。BDD 斷言到的 code 有：`VALIDATION_FAILED`（400）、`PROPERTY_NOT_FOUND`（404）、
  `PROPERTY_DUPLICATE`（409）、`PROPERTY_INVALID_STATUS_TRANSITION`（409）、`INTERNAL`（500）。
  錯誤回應 body 帶有 `code`、`message`、`request_id`，驗證錯誤另外帶一個 `details` 陣列，
  元素形狀為 `{field, reason}`。本專案使用 Go 1.26，**優先使用 `errors.AsType[T](err)`**，
  不要用 `errors.As(&target)`。
- **Request ID：** 每一個請求都有一個 request id；它會同時出現在錯誤回應 body **與**該請求的結構化
  log 行裡。BDD 斷言這兩者**相同**。
- **Panic 處理：** 在 middleware 中被 recover，以 error level 連同 request id 記錄下來，回應為
  `INTERNAL` 500，**回應 body 中不得出現任何堆疊資訊**（BDD 斷言 body 不含字串 `goroutine`）。
- **Cancellation：** 每一個 repository 與 service 方法的**第一個參數都是 `ctx context.Context`**，
  而且必須真的把它傳下去、真的尊重它。
- **依賴注入：手寫的建構子注入，組裝點只有 `cmd/api/main.go` 一處** —— 每個元件透過
  `NewXxx(deps...)` 形式的建構子接收它的相依，由 main 依序建好再往上傳（設定 → logger →
  Postgres/Redis → repository → service → handler → router → server）。
  **不要引入任何 DI 框架**（`google/wire`、`uber-go/fx`、`uber-go/dig`、`samber/do` 等一律不用）
  —— 理由：本專案的相依圖是一條淺的線性鏈，框架帶來的產生器步驟或反射式容器會讓「這個東西是誰給的」
  變成要追程式碼產生器輸出或執行期解析才知道，遠比七行 `New...()` 難讀；而且 Provides 區塊列出的
  建構子簽章就是本計畫的架構本身，框架會把它藏起來。
  **也不要使用全域變數或 `init()` 來傳遞相依** —— 沒有套件層級的 `var db *bun.DB` 這種東西。
  相依的反轉點有三個，實作時必須保留：`property.Repository` 介面（領域層因此不認識 bun）、
  `health.Checker` 介面（可加掛新的探針而不動 health 套件）、以及 `server.Mount`
  （feature 套件自己宣告路由，`internal/server` 因此**不 import** 任何 feature 套件）。
  **已知且刻意接受的限制：** `httpapi.NewHandler` 吃的是具體的 `*property.Service` 而非介面，
  且時間直接取自 `time.Now()` 而沒有可注入的 Clock。這是本輪的決定（測試策略是走 testcontainers
  端到端，不需要這兩道縫），**不要**自作主張補上這兩個介面。
- **註解與產品術語使用繁體中文。**
- **測試佈局（兩層，每個 task 都必須遵守）：**
  - **單元測試貼著程式碼放**，檔名 `<被測檔>_test.go`，與被測套件同目錄。這一層**不得碰 Docker、
    不得起容器、不得連任何外部服務**，純函式邏輯而已：`CreateInput.Validate` 的每條規則、
    `CanTransition` 的完整轉換表（含同狀態轉換為非法）、`apperr.HTTPStatus` 的映射、
    `config.Load` 的缺 key 判斷、金額 `StringFixed(2)` 的格式化。這一層必須在**沒有 Docker**
    的機器上也能跑過。
  - **整合測試一律放在 repo 根目錄的 `test/` 目錄，`package test`**，一個 feature 面向一個檔：
    `test/startup_test.go`（啟動與設定）、`test/health_test.go`、`test/migrate_test.go`、
    `test/panic_test.go`、`test/property_create_test.go`、`test/property_query_test.go`、
    `test/property_update_test.go`、`test/contract_test.go`（OpenAPI 契約驗證，Task 8）。
    **全部 17 個 BDD scenario 的驗收證據都在這一層**，
    透過 `testsupport.StartAPI` 打真實 HTTP 請求驗證，不直接呼叫內部函式。
  - 這樣切的理由：整合測試跑的是**整支 binary**，不屬於任何單一套件；`hars-verify` 因此能用
    `go test -race -count=1 ./test/...` 一條指令重跑整個整合層；而且 `internal/` 底下不會有任何
    套件 import testcontainers，剛好讓分層邊界更乾淨。
  - **⚠️ 共用容器與斷線情境的衝突，必須照這個規則處理：** `test/` 用一個 `TestMain` 啟動**一組共用的**
    Postgres 與 Redis 容器供整個 package 使用（16 個 scenario 各起一組會慢到不可用），每個測試以
    **TRUNCATE `properties`** 而非重建容器來取得乾淨狀態。但 Task 3 的「任一相依服務斷線時健康檢查
    回報不健康」情境**必須自己起一組用完即丟的專屬容器再把它停掉**，
    **絕對不可以停掉共用容器** —— 停掉共用容器會讓同一個 package 中其後所有測試連鎖失敗，
    而且失敗訊息會指向無辜的測試，極難除錯。
  - `-count=1` 是必要的：Go 會快取測試結果，容器背後的測試從快取回綠會騙人。
    header 的 `Test cmd` 保持 `go test -race ./...`（涵蓋兩層），但驗證整合層時請用
    `go test -race -count=1 ./test/...`。
- **目錄佈局（Task 1 建立，後續 task 只往裡面填）：**
  ```
  api/                openapi.yaml —— API 契約的唯一權威來源
  cmd/api/            服務進入點
  cmd/dbmigrate/      migration CLI
  config/             test.yaml / broken.yaml 等設定檔
  db/                 版本化 migration SQL（本身是 package db，內含 embed.go）
  test/               整合測試（package test，全部端到端驗收都在這裡）
  internal/config/    設定載入與驗證
  internal/apperr/    統一錯誤型別與錯誤碼
  internal/httpx/     request id、panic recover、JSON 讀寫、錯誤→HTTP 映射
  internal/server/    chi router 組裝與 HTTP server 生命週期
  internal/platform/  logging、postgres（bun）、redisclient
  internal/health/    健康檢查
  internal/property/  物件領域模型、service、repository 介面
  internal/property/pgrepo/   bun 實作（唯一 import bun 的 property 子套件）
  internal/property/httpapi/  HTTP handler 與 DTO（唯一 import chi 的 property 子套件）
  internal/testsupport/       testcontainers 輔助
  ```

## Overview

本輪要從一個完全空的 repo 建出一支可執行的包租代管後端服務骨架，並打通一條貫穿全部分層的垂直切片：
物件（房源）的建檔、查詢、更新與狀態變更。骨架的部分包含 viper 設定載入（缺 key 就拒絕啟動並指名該 key）、
chi 路由、bun/Postgres 與 Redis 連線、版本化 migration（含可用的 down）、slog 結構化日誌、統一錯誤型別
與 request id、以及 panic 攔截。垂直切片的部分包含 `properties` 資料表、領域模型（含 `rental_mode`
包租／代管旗標與四態狀態機）、驗證、bun repository、service、以及六個 REST endpoint。所有金額端到端
使用 shopspring/decimal，JSON 以固定小數位數的字串呈現。所有碰 DB 的測試以 testcontainers-go 自帶
Postgres 與 Redis，`go test -race ./...` 一道指令在冷機器上即可全綠。

此外本輪產出一份 `api/openapi.yaml` 作為 API 契約的唯一權威來源，並以契約測試強制它與實作一致 ——
因為技術棧不含 proto/IDL，沒有任何東西會自動扮演 API 文件的角色，而一份無人查核的手寫文件會很快
開始說謊。

任務切分刻意採「先骨架、後垂直切片、最後鎖契約」：Task 1–4 由下往上把可編譯、可啟動、可觀測、
可 migrate 的地基立好，Task 5–7 再依 API 操作分組疊上去，Task 8 最後把文件與實作綁死。Task 1 必須最先做，因為 `go.mod` 不存在時**任何東西都不會編譯**，
這個相依關係在每個後續 task 的 `Depends on` 中都寫成明確的相依而非默契。分層邊界透過**套件邊界**表達
（見 Known Context 最後一條），讓「handler 碰 DB」這種違規在 import 區塊上一眼可見。

**Pass threshold 的取捨：** header 預設 7.0，並針對「一旦有細微缺陷、事後代價很高」的 task 個別調高：
Task 1（8.0，設定載入是所有東西的地基，且 module 與目錄佈局定錯會拖累全部後續 task）、
Task 2（8.0，錯誤型別與 request id 是每一個後續 task 都要編譯進去的介面）、
Task 4（8.0，migration 的 up/down 成對正確性事後很難補救，回滾出錯會毀掉資料）、
Task 5（8.5，同時涵蓋 decimal 金額處理與整個 property 分層介面，是全計畫最貴的錯誤來源）、
Task 7（7.5，狀態機的非法轉換清單容易漏掉同狀態轉換這種邊角）、
Task 8（8.0，契約測試如果只驗狀態碼而略過 schema 驗證與雙向路由比對，看起來是綠的但完全沒有防護力，
是最容易被敷衍過去的一個 task）。
**Max iterations 設為 6**（高於預設 5），唯一理由是 testcontainers 在冷機器上第一次要拉 image、
bring-up 容易需要一兩輪試誤（等待策略、DSN 組裝、容器停止的時機），不希望在這件事上被判定 stalled。

## Sub-Tasks

### Task 1: 專案骨架、設定載入與結構化日誌
Status: done
Directory: .
Depends on: none
Pass threshold: 8.0
Provides (public interface):
```go
// go.mod: module github.com/yongde2900/chuchu2 ; go 1.26

package config // internal/config

type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    Postgres PostgresConfig `mapstructure:"postgres"`
    Redis    RedisConfig    `mapstructure:"redis"`
    Log      LogConfig      `mapstructure:"log"`
}
type ServerConfig struct {
    Port            int           `mapstructure:"port"`
    Debug           bool          `mapstructure:"debug"`            // 為 true 時才掛載 /debug/panic
    ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}
type PostgresConfig struct {
    DSN          string `mapstructure:"dsn"`
    MaxOpenConns int    `mapstructure:"max_open_conns"`
}
type RedisConfig struct {
    Addr     string `mapstructure:"addr"`
    Password string `mapstructure:"password"`
    DB       int    `mapstructure:"db"`
}
type LogConfig struct {
    Level string `mapstructure:"level"`
}

// Load 讀取 config/<name>.yaml，套用 CHUCHU_ 前綴的環境變數覆寫（"." 換成 "_"），
// 並檢查必要 key。缺 key 時回傳 *MissingKeyError。
func Load(name string) (*Config, error)

// MissingKeyError 的 Error() 訊息中必須包含缺少的 key 全名（例如 "postgres.dsn"）。
type MissingKeyError struct{ Key string }
func (e *MissingKeyError) Error() string

package logging // internal/platform/logging
func New(level string, w io.Writer) *slog.Logger
```
- 必要 key 清單至少為 `server.port`、`postgres.dsn`、`redis.addr`。
- `cmd/api/main.go` 本 task 先建立最小版本：解析 `--config` flag、呼叫 `config.Load`、
  失敗時把訊息寫到 **stderr** 並 `os.Exit(1)`（必須在 5 秒內完成，不得在失敗前先去連任何外部服務）。
  HTTP server 的啟動由 Task 3 補上。
- 建立 `config/test.yaml`（填妥 server.port / postgres.dsn / redis.addr，`server.debug: true`）
  與 `config/broken.yaml`（**刻意不含 `postgres.dsn` 這個 key**）。兩者都要提交進 repo，
  它們是 BDD 的 fixture。
- 建立 Project Conventions 中列出的完整目錄骨架（可用 `.gitkeep` 佔位），讓後續 task 有位置可放。
- 提供 `internal/testsupport` 中的 `func RepoRoot(t *testing.T) string`，讓測試能以 repo 根目錄為
  工作目錄執行 `go run ./cmd/api`。
Expected Goals (from BDD scenarios):
- [x] Scenario: 設定檔缺少必要欄位時拒絕啟動

### Task 2: 統一錯誤型別、request id、panic 攔截與 chi router 組裝
Status: done
Directory: internal/apperr, internal/httpx, internal/server
Depends on: Task 1（config.Config、logging.New、目錄骨架、cmd/api 進入點）
Pass threshold: 8.0
Provides (public interface):
```go
package apperr // internal/apperr

type Code string
const (
    CodeValidationFailed          Code = "VALIDATION_FAILED"
    CodePropertyNotFound          Code = "PROPERTY_NOT_FOUND"
    CodePropertyDuplicate         Code = "PROPERTY_DUPLICATE"
    CodeInvalidStatusTransition   Code = "PROPERTY_INVALID_STATUS_TRANSITION"
    CodeInternal                  Code = "INTERNAL"
)

// FieldError 是驗證失敗時 details 陣列的元素。
type FieldError struct {
    Field  string `json:"field"`
    Reason string `json:"reason"`
}

type Error struct {
    Code    Code
    Message string
    Details []FieldError
    err     error
}
func (e *Error) Error() string
func (e *Error) Unwrap() error

func New(code Code, msg string) *Error
func Wrap(code Code, msg string, err error) *Error
func Validation(details ...FieldError) *Error

// HTTPStatus 是 code → HTTP 狀態碼的唯一映射點。
func HTTPStatus(code Code) int

package httpx // internal/httpx

// ErrorBody 是所有錯誤回應的統一形狀。
type ErrorBody struct {
    Code      string              `json:"code"`
    Message   string              `json:"message"`
    RequestID string              `json:"request_id"`
    Details   []apperr.FieldError `json:"details,omitempty"`
}

func RequestID(next http.Handler) http.Handler
func RequestIDFrom(ctx context.Context) string

// Recoverer 攔截 panic，以 error level 帶 request_id 記錄，回應 INTERNAL 500 且不外洩堆疊。
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler

func WriteJSON(w http.ResponseWriter, status int, v any)
// WriteError 使用 errors.AsType[*apperr.Error] 取出應用錯誤；取不到時視為 INTERNAL。
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error)
func DecodeJSON[T any](r *http.Request) (T, error)

package server // internal/server

// Mount 讓每個 feature 套件宣告自己的路由；只有 httpapi/health 這類套件會實作它。
type Mount func(r chi.Router)

type Options struct {
    Debug  bool // 為 true 時掛載 GET /debug/panic
}
func NewRouter(opts Options, mounts ...Mount) *chi.Mux

// Run 啟動 HTTP server 並在 ctx 取消時優雅關閉。
func Run(ctx context.Context, addr string, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger) error
```
- middleware 順序固定為：RequestID → 結構化 access log → Recoverer。log 行必須帶 `request_id`
  屬性，且與回應 body 中的 `request_id` 完全相同。
- `GET /debug/panic` 只在 `Options.Debug` 為 true 時掛載，`config/test.yaml` 已設 `server.debug: true`。
- 本 task 讓 `cmd/api` 真正起 HTTP server 並在 SIGINT/SIGTERM 時優雅關閉。
Expected Goals (from BDD scenarios):
- [x] Scenario: 未預期的 panic 被攔截為 500 且不外洩堆疊

### Task 3: Postgres／Redis 連線、健康檢查端點與 testcontainers 測試基礎設施
Status: done
Directory: internal/platform, internal/health, internal/testsupport
Depends on: Task 1（config.Config、logging）、Task 2（server.NewRouter/Mount/Run、httpx.WriteJSON）
Provides (public interface):
```go
package postgres // internal/platform/postgres
func Open(ctx context.Context, cfg config.PostgresConfig) (*bun.DB, error)
func Ping(ctx context.Context, db *bun.DB) error

package redisclient // internal/platform/redisclient
func Open(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error)

package health // internal/health

// Checker 是單一相依服務的探針；名稱即為回應 checks 物件中的 key。
type Checker interface {
    Name() string
    Check(ctx context.Context) error
}
func NewPostgresChecker(db *bun.DB) Checker
func NewRedisChecker(c *redis.Client) Checker

type Report struct {
    Status string            `json:"status"` // "ok" | "degraded"
    Checks map[string]string `json:"checks"` // 每項為 "ok" | "down"
}

type Service struct{ /* ... */ }
func NewService(checkers ...Checker) *Service
func (s *Service) Check(ctx context.Context) Report

// Mount 掛載 GET /healthz；全部 ok 回 200，任一 down 回 503。
func Mount(svc *Service) server.Mount

package testsupport // internal/testsupport
func RepoRoot(t *testing.T) string
// StartPostgres 以 testcontainers 啟一個用完即丟的 Postgres，回傳 DSN 與可主動停止容器的函式。
func StartPostgres(t *testing.T) (dsn string, stop func())
func StartRedis(t *testing.T) (addr string, stop func())
// StartAPI 以 go run ./cmd/api --config=<name> 啟動服務（工作目錄為 repo 根），
// 以 CHUCHU_ 前綴環境變數注入容器產生的 DSN/addr，回傳 base URL、取得 stderr／log 的函式與關閉函式。
func StartAPI(t *testing.T, configName string, env map[string]string) (baseURL string, output func() string, stop func())
```
- 本 task 把 `cmd/api` 補成完整組裝：載入設定 → 建 logger → 開 Postgres／Redis → 建 health service
  → `server.NewRouter` → `server.Run`。啟動時要對兩個相依服務各做一次連線驗證，但**連不上時的行為
  是記錄警告而非退出**，因為 BDD 要求服務在相依斷線時仍能回應 `/healthz` 503。
- **建立 `api/openapi.yaml` 的骨架**（`openapi: 3.1.0`、`info`、`servers`、共用的 `ErrorBody` 與
  `FieldError` schema），並寫入本 task 的 `GET /healthz`（含 200 與 503 兩種回應）。
  後續 task 只往這份文件裡加自己的 endpoint。
- 健康檢查的每一次探測都要帶 timeout（建議 2 秒），避免斷線的相依把請求吊死。
- **本 task 建立 `test/` 這個整合測試 package 及其 `TestMain`**（見 Project Conventions 的測試佈局）：
  `TestMain` 啟動一組供整個 package 共用的 Postgres 與 Redis，跑完 `migrate.Up`（Task 4 完成後），
  結束時收掉。後續 Task 5–7 只往 `test/` 裡加檔案，不再改 `TestMain`。
- `stop()` 用於「斷線」情境：測試先啟服務，再停掉 Postgres 或 Redis 容器，然後打 `/healthz`。
  **這個情境必須用自己起的專屬容器**，不得停掉 `TestMain` 的共用容器 —— 否則同 package 後續測試全部連鎖失敗。
- 由於冷機器第一次會拉 image，測試需設定足夠的等待時間；`go test -race ./...` 必須在只有 Docker
  執行的機器上全綠，**不得假設有原生 Postgres 或 Redis**。
Expected Goals (from BDD scenarios):
- [x] Scenario: 以有效設定檔啟動服務
- [x] Scenario: 相依服務皆可連線時健康檢查回報健康
- [x] Scenario Outline: 任一相依服務斷線時健康檢查回報不健康

### Task 4: properties 資料表 migration 與 cmd/dbmigrate
Status: in-progress
Directory: db, internal/migrate, cmd/dbmigrate
Depends on: Task 1（config.Load、logging）、Task 3（postgres.Open、testsupport.StartPostgres）
Pass threshold: 8.0
Provides (public interface):
```go
package db // db/embed.go —— migration SQL 與其 embed.FS 同層，這是唯一可行的作法

// go:embed 的 pattern 不允許包含 ".."，無法從 internal/migrate 往上嵌入 db/。
// 因此 embed 宣告必須放在 db/ 目錄自己的 .go 檔裡，再由 internal/migrate import。
//go:embed *.sql
var FS embed.FS

package migrate // internal/migrate

// Up 依檔名時間戳由舊到新套用所有 .up.sql；Down 由新到舊套用 .down.sql。
// 兩者都從 db.FS 讀取 SQL。
// 兩者都以 schema_migrations 資料表記錄已套用的版本，並且必須可重入（已套用者跳過）。
func Up(ctx context.Context, db *bun.DB) error
func Down(ctx context.Context, db *bun.DB) error
func Applied(ctx context.Context, db *bun.DB) ([]string, error)
```
- `cmd/dbmigrate` 支援 `up` 與 `down` 兩個子指令，共用 `--config=<name>` flag。
- migration 檔案成對：`db/<timestamp>_create_properties.up.sql` 與 `.down.sql`。
- **`db/` 是一個 Go package（`package db`），裡面除了 `.sql` 還有一個只放 `//go:embed *.sql` 的
  `embed.go`。** 不要嘗試在 `internal/migrate` 裡寫 `//go:embed ../../db/*.sql` —— go:embed 的 pattern
  不允許 `..`，那樣寫編譯不會過。
- `properties` 資料表欄位（欄位名即 JSON 欄位名的 snake_case 對應）：
  `id UUID PRIMARY KEY`、`city TEXT NOT NULL`、`district TEXT NOT NULL`、`street_address TEXT NOT NULL`、
  `floor TEXT NOT NULL`、`room_no TEXT NOT NULL`、`layout TEXT NOT NULL`、
  `area_ping NUMERIC(10,2) NOT NULL`、`monthly_rent NUMERIC(12,2) NOT NULL`、
  `management_fee NUMERIC(12,2) NOT NULL DEFAULT 0`、`deposit_months INT NOT NULL`、
  `rental_mode TEXT NOT NULL`、`status TEXT NOT NULL DEFAULT 'VACANT'`、
  `landlord_name TEXT NOT NULL`、`landlord_phone TEXT NOT NULL`、
  `created_at TIMESTAMPTZ NOT NULL`、`updated_at TIMESTAMPTZ NOT NULL`。
- 唯一索引：`UNIQUE (city, district, street_address, floor, room_no)`，索引名稱要穩定
  （例如 `properties_address_key`），因為 Task 5 要靠它辨識重複建檔。
- 建議對 `rental_mode`／`status`／`layout` 加 CHECK 約束，但**列舉值的權威定義在 Task 5 的領域層**，
  兩邊必須一致。
- `down` 必須把資料表與索引乾淨地移除，讓 up→down→up 可重複執行。
Expected Goals (from BDD scenarios):
- [ ] Scenario: Migration 可正向套用亦可回滾

### Task 5: 物件領域模型、驗證與建檔（POST /api/v1/properties）
Status: pending
Directory: internal/property
Depends on: Task 1（config）、Task 2（apperr、httpx、server.Mount）、Task 3（postgres.Open、testsupport）、Task 4（properties 資料表）
Pass threshold: 8.5
Provides (public interface):
```go
package property // internal/property —— 不得 import bun，也不得 import net/http

type RentalMode string
const (
    RentalModeMasterLease RentalMode = "MASTER_LEASE" // 包租
    RentalModeManaged     RentalMode = "MANAGED"      // 代管
)
func (m RentalMode) Valid() bool

type Status string
const (
    StatusVacant     Status = "VACANT"
    StatusOccupied   Status = "OCCUPIED"
    StatusRenovating Status = "RENOVATING"
    StatusDelisted   Status = "DELISTED"
)
func (s Status) Valid() bool

type Layout string
const (
    LayoutWholeUnit        Layout = "WHOLE_UNIT"         // 整層住家
    LayoutIndependentSuite Layout = "INDEPENDENT_SUITE"  // 獨立套房
    LayoutSharedSuite      Layout = "SHARED_SUITE"       // 分租套房
    LayoutSingleRoom       Layout = "SINGLE_ROOM"        // 雅房
)
func (l Layout) Valid() bool

type Property struct {
    ID            uuid.UUID
    City          string
    District      string
    StreetAddress string
    Floor         string
    RoomNo        string
    Layout        Layout
    AreaPing      decimal.Decimal
    MonthlyRent   decimal.Decimal
    ManagementFee decimal.Decimal
    DepositMonths int
    RentalMode    RentalMode
    Status        Status
    LandlordName  string
    LandlordPhone string
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

// CreateInput 的金額欄位以字串進來，由 Validate 負責解析成 decimal 並回報是哪個欄位出錯。
type CreateInput struct {
    City, District, StreetAddress, Floor, RoomNo string
    Layout                                       string
    AreaPing, MonthlyRent, ManagementFee         string
    DepositMonths                                int
    RentalMode                                   string
    LandlordName, LandlordPhone                  string
}
// Validate 回傳所有出錯欄位；無錯時回傳 nil。
func (in CreateInput) Validate() []apperr.FieldError

// Repository 是領域層對持久化的唯一出口；實作在 pgrepo，介面在此。
// Task 6 會擴充 GetByID/List，Task 7 會擴充 Update。
type Repository interface {
    Create(ctx context.Context, p *Property) error // 命中唯一鍵時回傳 apperr.CodePropertyDuplicate
}

type Service struct{ /* ... */ }
func NewService(repo Repository) *Service
func (s *Service) Create(ctx context.Context, in CreateInput) (*Property, error)

package pgrepo // internal/property/pgrepo —— 唯一 import bun 的 property 子套件
type PropertyRepository struct{ /* ... */ }
func New(db *bun.DB) *PropertyRepository
// 編譯期斷言：var _ property.Repository = (*PropertyRepository)(nil)

package httpapi // internal/property/httpapi —— 唯一 import chi/net/http 的 property 子套件
type Handler struct{ /* ... */ }
func NewHandler(svc *property.Service, logger *slog.Logger) *Handler
func (h *Handler) Mount() server.Mount // /api/v1/properties 相關路由
```
- **驗證規則（由 BDD 的 Examples 直接推出）：** `city` 不可為空、`street_address` 不可為空、
  `monthly_rent` 必須是可解析的十進位數且**嚴格大於 0**（"0"、"-1"、"abc" 都要被拒）、
  `area_ping` 必須大於 0、`deposit_months` 不可為負、`rental_mode` 必須是 MASTER_LEASE 或 MANAGED、
  `layout` 必須是四個合法值之一。每一個錯誤在 `details` 中都要有一項，其 `field` **等於 JSON 欄位名**
  （snake_case，例如 `street_address`、`monthly_rent`）。
- 新建物件的 `Status` 一律為 `VACANT`，`ID` 由服務端產生 UUID。
- **重複建檔：** repository 必須辨識 Postgres 唯一鍵違反（SQLSTATE 23505）並轉成
  `apperr.New(apperr.CodePropertyDuplicate, ...)`，由 `httpx.WriteError` 映射成 409。
  不得先 SELECT 再 INSERT 來判斷重複（有 race）。
- **金額序列化：** response DTO 的 `monthly_rent`、`management_fee`、`area_ping` 都是 **JSON 字串**，
  以 `StringFixed(2)` 產生，`25000.50` 必須輸出 `"25000.50"` 而不是 `"25000.5"`。
  資料庫中該筆的 `monthly_rent` 數值必須精確等於 25000.50。
- **分層驗證：** `internal/property` 套件的 import 區塊中不得出現 `uptrace/bun` 或 `net/http`；
  這是本輪骨架的核心價值，請在實作時保持乾淨。
- **同步更新 `api/openapi.yaml`：** 加入 `POST /api/v1/properties`，含 201、400（VALIDATION_FAILED，
  帶 `details`）、409（PROPERTY_DUPLICATE）三種回應，以及 `Property` 與 `CreatePropertyRequest`
  兩個 schema。金額欄位宣告為 `type: string`，不是 number。
Expected Goals (from BDD scenarios):
- [ ] Scenario: 建立一筆包租物件
- [ ] Scenario: 相同門牌重複建檔被拒絕
- [ ] Scenario Outline: 欄位驗證失敗時回報是哪個欄位出錯

### Task 6: 物件查詢 —— 單筆查詢、分頁列表與條件篩選
Status: pending
Directory: internal/property
Depends on: Task 5（Property、Repository、Service、pgrepo、httpapi 全部既有結構）
Provides (public interface):
```go
package property

// Repository 擴充（沿用 Task 5 的同一個介面）：
//   GetByID(ctx context.Context, id uuid.UUID) (*Property, error) // 找不到時回 apperr.CodePropertyNotFound
//   List(ctx context.Context, f ListFilter) (ListResult, error)

// ListFilter 的 nil 指標代表「不篩選該欄位」。
type ListFilter struct {
    Page       int // 1-based，預設 1
    PageSize   int // 預設 20，上限 100
    Status     *Status
    RentalMode *RentalMode
    City       string // 空字串代表不篩選
}
type ListResult struct {
    Items []*Property
    Total int // 套用篩選後、分頁前的總筆數
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Property, error)
func (s *Service) List(ctx context.Context, f ListFilter) (ListResult, error)
```
- 路由：`GET /api/v1/properties/{id}`、`GET /api/v1/properties`。
- 列表排序固定為 **`created_at` 由新到舊**（同時間以 `id` 作為穩定的次要排序鍵，避免分頁跳號）。
- 查詢參數 `page`、`page_size`、`status`、`rental_mode`、`city`。`total` 是套用篩選後的總筆數，
  與回傳的 `items` 長度無關；`status=DELISTED` 且無資料時 `total` 為 0、`items` 為**空陣列**
  （不得是 JSON `null`）。
- 路徑上的 `{id}` 不是合法 UUID 時回 400 `VALIDATION_FAILED`；是合法 UUID 但查無資料時回 404
  `PROPERTY_NOT_FOUND`。
- 單筆回應必須包含 RFC3339 格式的 `created_at` 與 `updated_at`，金額欄位同樣是固定小數位數的字串。
- **同步更新 `api/openapi.yaml`：** 加入 `GET /api/v1/properties/{id}`（200、400、404）與
  `GET /api/v1/properties`（200，含五個查詢參數的宣告），以及 `PropertyList` schema
  （`items` 陣列 + `total` 整數）。
Expected Goals (from BDD scenarios):
- [ ] Scenario: 以 id 查詢單一物件
- [ ] Scenario: 查詢不存在的物件
- [ ] Scenario: 分頁列出物件並依建檔時間由新到舊排序
- [ ] Scenario Outline: 依條件篩選物件列表

### Task 7: 物件欄位更新（PATCH）與狀態機轉換（POST /status）
Status: pending
Directory: internal/property
Depends on: Task 5（建檔與 Repository 介面）、Task 6（GetByID，用於「接著 GET 驗證」的斷言路徑）
Pass threshold: 7.5
Provides (public interface):
```go
package property

// Repository 再擴充：
//   Update(ctx context.Context, p *Property) error

// UpdateInput 的 nil 欄位代表「不更動」；金額同樣以字串進來，由 Validate 解析。
type UpdateInput struct {
    MonthlyRent   *string
    ManagementFee *string
    DepositMonths *int
    Layout        *string
    LandlordName  *string
    LandlordPhone *string
}
func (in UpdateInput) Validate() []apperr.FieldError

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Property, error)

// CanTransition 是狀態機的唯一權威來源。
// 合法：VACANT↔RENOVATING、VACANT↔OCCUPIED、VACANT↔DELISTED、RENOVATING→DELISTED。
// 其餘全部非法，含同狀態轉換（VACANT→VACANT 亦為非法）。
func CanTransition(from, to Status) bool

// ChangeStatus 轉換非法時回傳 apperr.CodeInvalidStatusTransition（HTTP 409），且不得寫入資料庫。
func (s *Service) ChangeStatus(ctx context.Context, id uuid.UUID, target Status) (*Property, error)
```
- 路由：`PATCH /api/v1/properties/{id}`、`POST /api/v1/properties/{id}/status`。
- **狀態變更不走 PATCH** —— PATCH 的 body 中若出現 `status` 欄位，一律忽略或回 400，
  絕不可以由 PATCH 繞過狀態機。這是「只有一個強制點」這個設計決定的實質內容。
- 任何成功的變更都要更新 `updated_at`，使其**嚴格晚於** `created_at`（BDD 斷言此不等式；
  若時鐘精度可能導致相等，寫入時取 `time.Now()` 並確保 `TIMESTAMPTZ` 精度足以區分）。
- 非法轉換被拒絕後，接著 GET 必須看到**原狀態不變**，代表拒絕發生在寫入之前。
- 建議把轉換表寫成 `map[Status]map[Status]bool` 或明確的 `switch`，並附繁體中文註解說明每條規則的
  業務理由（例如「已下架的物件必須先回到空置才能重新上架」）。
- **同步更新 `api/openapi.yaml`：** 加入 `PATCH /api/v1/properties/{id}`（200、400、404）與
  `POST /api/v1/properties/{id}/status`（200、400、404、409），以及 `UpdatePropertyRequest` 與
  `ChangeStatusRequest` 兩個 schema。本 task 完成後，文件中的 endpoint 應已涵蓋全部六個公開路由。
Expected Goals (from BDD scenarios):
- [ ] Scenario: 更新物件租金後查詢反映新值
- [ ] Scenario: 合法的物件狀態轉換被接受
- [ ] Scenario Outline: 非法的物件狀態轉換被拒絕

### Task 8: OpenAPI 契約測試 —— 強制文件與實作一致
Status: pending
Directory: api, test
Depends on: Task 3（openapi.yaml 骨架與 /healthz、testsupport.StartAPI）、Task 5、Task 6、Task 7（全部六個公開 endpoint 與其文件條目）
Pass threshold: 8.0
Provides (public interface):
```go
package test // test/contract_test.go —— 沒有對外匯出的介面，這是純驗收層

// 本 task 不新增任何 internal/ 套件。它唯一的產出是：
//   1. api/openapi.yaml 補完成一份合法、完整的 OpenAPI 3.1 文件
//   2. test/contract_test.go 這支把文件與實作綁在一起的測試
```
- 使用 `getkin/kin-openapi`：`openapi3.NewLoader().LoadFromFile("api/openapi.yaml")` 載入，
  並呼叫 `doc.Validate(ctx)` 確認文件本身合法（這就是 scenario 的第一個 Given）。
- **正向驗證：** 對文件中宣告的每一個 path + method 發出真實請求，每個 operation **至少涵蓋一種成功
  回應與一種錯誤回應**。以 `openapi3filter.ValidateResponse` 驗證回應狀態碼有被宣告、且 body 通過
  該狀態碼對應的 schema。
- **反向驗證：** 以 `chi.Walk` 列舉服務**實際註冊**的路由，與文件宣告的 path + method 集合做**雙向**
  比對 —— 文件有而實作沒有、實作有而文件沒有，兩者都必須讓測試失敗。
  **比對時排除 `/debug/` 開頭的路由**：`config/test.yaml` 設了 `server.debug: true`，
  `GET /debug/panic` 因此會被註冊，但它不屬於公開契約。漏了這個排除，這條斷言必定失敗。
- 為了讓 `chi.Walk` 拿得到路由表，本 task 可能需要讓 `internal/server` 匯出一個回傳 `*chi.Mux` 的
  組裝函式供測試呼叫，或在 `test/` 內以相同的 mount 清單重建一次 router。**兩種都可以，但不得為此
  在正式程式碼裡加任何只有測試會用到的公開端點。**
- **本 task 只補文件與測試，不得修改任何 handler 的行為。** 如果契約測試揭露了實作與文件不一致，
  優先修正 `api/openapi.yaml` 使其如實描述實作；只有當實作明顯違反前面 16 個 scenario 已確立的
  行為時，才回頭改實作 —— 那種情況代表前面的 task 有漏網之魚，要在 Iteration Log 中記下來。
Expected Goals (from BDD scenarios):
- [ ] Scenario: 服務的實際回應與路由集合皆符合 api/openapi.yaml

## Coverage Check
- Scenario: 以有效設定檔啟動服務 → Task 3
- Scenario: 設定檔缺少必要欄位時拒絕啟動 → Task 1
- Scenario: 相依服務皆可連線時健康檢查回報健康 → Task 3
- Scenario Outline: 任一相依服務斷線時健康檢查回報不健康 → Task 3
- Scenario: Migration 可正向套用亦可回滾 → Task 4
- Scenario: 未預期的 panic 被攔截為 500 且不外洩堆疊 → Task 2
- Scenario: 建立一筆包租物件 → Task 5
- Scenario: 相同門牌重複建檔被拒絕 → Task 5
- Scenario Outline: 欄位驗證失敗時回報是哪個欄位出錯 → Task 5
- Scenario: 以 id 查詢單一物件 → Task 6
- Scenario: 查詢不存在的物件 → Task 6
- Scenario: 分頁列出物件並依建檔時間由新到舊排序 → Task 6
- Scenario Outline: 依條件篩選物件列表 → Task 6
- Scenario: 更新物件租金後查詢反映新值 → Task 7
- Scenario: 合法的物件狀態轉換被接受 → Task 7
- Scenario Outline: 非法的物件狀態轉換被拒絕 → Task 7
- Scenario: 服務的實際回應與路由集合皆符合 api/openapi.yaml → Task 8

## Integration Scenarios
- Scenario: 以有效設定檔啟動服務
- Scenario: 相依服務皆可連線時健康檢查回報健康
- Scenario: 未預期的 panic 被攔截為 500 且不外洩堆疊
- Scenario: 建立一筆包租物件
- Scenario: 更新物件租金後查詢反映新值
- Scenario: 合法的物件狀態轉換被接受
- Scenario Outline: 非法的物件狀態轉換被拒絕
- Scenario: 服務的實際回應與路由集合皆符合 api/openapi.yaml

## Iteration Log

### Task 1 — Iter 1 — score 9.7/10 — PASS
- Changed: 建立 `go.mod`（module `github.com/yongde2900/chuchu2`, go 1.26，僅 viper 相依）、
  `internal/config`（`Config`/`ServerConfig`/`PostgresConfig`/`RedisConfig`/`LogConfig`、`Load`、
  `MissingKeyError`）、`internal/platform/logging`（`New`）、`internal/testsupport/reporoot.go`
  （`RepoRoot`）、`cmd/api/main.go`（最小版本：`--config` flag → `config.Load` → 失敗寫 stderr 並
  exit 1）、fixture `config/test.yaml` 與 `config/broken.yaml`、`test/startup_test.go`，
  以及 12 個 `.gitkeep` 目錄佔位。
- Gates（Coordinator 自 disk 驗證）：`go build ./...` clean、`go vet ./...` clean、
  `go test -race -count=1 ./...` 全綠（8 個測試／4 個 package）。
- 環境變數覆寫機制經 Evaluator 手動驗證確實生效，含「key 只存在於環境變數、不存在於 yaml」
  這個 Task 3 會依賴的關鍵情境 —— 作法是對每個已知 key 明確 `BindEnv`，而非只靠 `AutomaticEnv`。
- Remaining: 無阻塞項。三個 info 級註記：`internal/platform/logging/logging_test.go` 有 gofmt
  對齊瑕疵；`config.Load` 的 config 路徑相對於 cwd（測試與 Task 3 皆以 repo 根為工作目錄，可接受）；
  Task 1 的檔案目前在 git 中仍為 untracked（本 skill 不自行 commit）。

### Task 2 — Iter 1 — score 9.5/10 — PASS
- Changed: `internal/apperr`（`Code` 五個常數、`FieldError`、`Error`＋`Unwrap`、`New`/`Wrap`/`Validation`、
  `HTTPStatus` 單一映射點）、`internal/httpx`（`ErrorBody`、`RequestID`＋`RequestIDFrom`（未匯出 context key）、
  `Recoverer`、`WriteJSON`、`WriteError`、泛型 `DecodeJSON[T]`）、`internal/server`（`Mount`、`Options`、
  `NewRouter`、`Run`＋graceful shutdown）、`cmd/api/main.go`（真正起 HTTP server，SIGINT/SIGTERM 優雅關閉）、
  `test/panic_test.go`；go.mod 新增 `github.com/go-chi/chi/v5 v5.3.1`。
- Gates（Coordinator 自 disk 驗證）：`go build ./...`、`go vet ./...` clean，
  `go test -race -count=1 ./...` 全綠（7 個 package、30+ 個測試）。
- Evaluator 另以手動方式驗證：實跑 `go run ./cmd/api --config=test` 並 curl `/debug/panic`，
  確認 500／`code=INTERNAL`／非空 request_id／body 不含 `goroutine`，且堆疊只出現在 log 的 `stack` 欄位；
  middleware 順序 RequestID → access log → Recoverer 在 handler panic 時仍能產出 request_id。
  另確認 `internal/server` 的 intra-repo import 只有 `internal/httpx`，未 import 任何 feature 套件。
- **Coordinator 修正：** Executor 留下的 `go.mod` 把 chi 標成 `// indirect`（實為直接相依），
  已執行 `go mod tidy` 修正並重跑兩道 gate 確認仍為綠。
- **Coordinator 對計畫張力的處置（未改動計畫）：** 測試佈局約定所有 BDD 驗收證據放 `test/` 並透過
  `testsupport.StartAPI` 驅動，但 `StartAPI` 是 Task 3 的產出、相依順序上晚於 Task 2。本 task 的 panic
  scenario 因此改以**同行程** `httptest.NewServer` 套用真實 router 與完整 middleware chain 驅動 ——
  五條 Then 全部照驗，且 log／request_id 關聯在同行程下反而比對子行程 stderr 更容易斷言，亦不需 Docker。
- **契約微調（事前授權，非破壞性）：** `NewRouter` 的參數列已凍結，但 middleware chain 需要 logger，
  故 logger 以新增欄位的方式放進 `Options`（`Logger *slog.Logger`，nil 時退回 `slog.Default()`）。
  新增欄位不影響已規劃的呼叫端，改參數列則會。
- Remaining: 無阻塞項。

### Task 3 — Iter 1 — score 9.2/10 — PASS
- Changed: `internal/platform/postgres`（`Open`／`Ping`，bun + pgdriver + pgdialect）、
  `internal/platform/redisclient`（`Open`，go-redis/v9）、`internal/health`（`Checker` 介面、
  `Report`、`Service`＋平行探測、`NewPostgresChecker`／`NewRedisChecker`（`Name()` 為 `postgres`／`redis`）、
  `Mount` 掛 `GET /healthz`）、`internal/testsupport`（`StartPostgres`／`StartRedis`／`StartAPI`）、
  `cmd/api/main.go`（完整組裝；相依連不上時**記錄警告而非退出**）、`api/openapi.yaml`（3.1.0 骨架，
  含 `ErrorBody`／`FieldError`／`HealthReport` 與 `/healthz` 的 200／503）、
  `test/main_test.go`（共用容器 `TestMain`）、`test/health_test.go`、`test/startup_test.go`（新增啟動情境）。
  go.mod 新增 bun、go-redis/v9、testcontainers-go（含 modules/postgres、modules/redis）。
- Gates（Coordinator 自 disk 驗證）：`go build ./...`、`go vet ./...` clean，
  `go test -race -count=1 ./...` 全綠。整合層 `./test/...` 暖快取約 10s，冷機器含拉 image 約 36s。
- Evaluator 手動驗證三個「綠測試也可能藏住」的關鍵點，全部通過：
  (1) 斷線情境確實各起**專屬**用完即丟容器，未動到 `TestMain` 共用容器；
  (2) 每個斷線測試都**先斷言 200** 再停容器，因此能區分「探針偵測到斷線」與「從頭就沒連上」；
  (3) 實際以無法連線的 DSN 啟動 `cmd/api`，確認行程不退出且 `/healthz` 回 503 degraded。
  另確認 `internal/server` 未 import `internal/health`（唯一符合處是說明邊界的註解），
  `internal/health` 單元測試只用假 Checker、不含 Docker 相依。
- **Coordinator 對計畫張力的處置（未改動計畫）：** 計畫寫 Task 3 的 `TestMain` 要跑 `migrate.Up`，
  但 `internal/migrate` 是 Task 4 的產出。本 task 的 `TestMain` 只負責容器生命週期，
  migration 由 Task 4 接上（已在 Executor 與 Evaluator 的 prompt 中明確標示此為正確、非缺漏）。
- Remaining: 無阻塞項。一個 info：`Service.Check` 的註解寫「依序（平行）」用詞自相矛盾，行為（平行）正確。

## Amendments
