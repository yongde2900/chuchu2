---
title: PLAN-001 實際匯出的公開介面（下一份 plan 的建構基礎）
type: architecture
date: 2026-08-20
tags: [interfaces, api, go, property, reference]
---

PLAN-001 完成後**實際存在於程式碼中**的簽章。下一份 plan 應直接對著這些寫，
不要憑計畫文件重新推測（計畫寫的是意圖，這裡寫的是落地結果）。

```go
package config // internal/config
type Config struct{ Server ServerConfig; Postgres PostgresConfig; Redis RedisConfig; Log LogConfig }
func Load(name string) (*Config, error)          // 讀 config/<name>.yaml，套 CHUCHU_ 環境變數覆寫
type MissingKeyError struct{ Key string }        // Error() 訊息含缺少的 key 全名

package logging // internal/platform/logging
func New(level string, w io.Writer) *slog.Logger // JSON handler

package apperr // internal/apperr
type Code string  // VALIDATION_FAILED / PROPERTY_NOT_FOUND / PROPERTY_DUPLICATE /
                  // PROPERTY_INVALID_STATUS_TRANSITION / INTERNAL
type FieldError struct{ Field, Reason string }
func New(code Code, msg string) *Error
func Wrap(code Code, msg string, err error) *Error
func Validation(details ...FieldError) *Error
func HTTPStatus(code Code) int                   // code → HTTP 的唯一映射點；未知 code → 500

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
type Options struct{ Debug bool; Logger *slog.Logger }   // Logger 為 nil 時退回 slog.Default()
func NewRouter(opts Options, mounts ...Mount) *chi.Mux
func Run(ctx context.Context, addr string, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger) error

package postgres // internal/platform/postgres
func Open(ctx context.Context, cfg config.PostgresConfig) (*bun.DB, error)  // 不 dial，只建 pool
func Ping(ctx context.Context, db *bun.DB) error

package redisclient // internal/platform/redisclient
func Open(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error)

package health // internal/health
type Checker interface{ Name() string; Check(ctx context.Context) error }
func NewPostgresChecker(db *bun.DB) Checker      // Name() == "postgres"
func NewRedisChecker(c *redis.Client) Checker    // Name() == "redis"
type Report struct{ Status string; Checks map[string]string }  // "ok" | "degraded" / "ok" | "down"
func NewService(checkers ...Checker) *Service    // 平行探測，每項 2s timeout
func (s *Service) Check(ctx context.Context) Report
func Mount(svc *Service) server.Mount            // GET /healthz；全 ok→200，任一 down→503

package db      // db/embed.go
var FS embed.FS                                  // //go:embed *.sql

package migrate // internal/migrate
func Up(ctx context.Context, db *bun.DB) error   // 可重入；SQL＋版本記錄同一交易
func Down(ctx context.Context, db *bun.DB) error
func Applied(ctx context.Context, db *bun.DB) ([]string, error)

package property // internal/property
type RentalMode string  // MASTER_LEASE 包租 / MANAGED 代管；有 Valid()
type Status string      // VACANT / OCCUPIED / RENOVATING / DELISTED；有 Valid()
type Layout string      // WHOLE_UNIT / INDEPENDENT_SUITE / SHARED_SUITE / SINGLE_ROOM；有 Valid()
type Property struct{ /* 金額皆為 decimal.Decimal */ }
type CreateInput struct{ /* 金額以 string 進來 */ }
func (in CreateInput) Validate() []apperr.FieldError   // 回傳「所有」出錯欄位，不短路
type UpdateInput struct{ /* 全為指標；nil 代表不更動；刻意沒有 Status 欄位 */ }
func (in UpdateInput) Validate() []apperr.FieldError
type ListFilter struct{ Page, PageSize int; Status *Status; RentalMode *RentalMode; City string }
type ListResult struct{ Items []*Property; Total int }   // Total 為套用篩選後、分頁前的筆數
type Repository interface {
    Create(ctx context.Context, p *Property) error        // 唯一鍵衝突 → CodePropertyDuplicate
    GetByID(ctx context.Context, id uuid.UUID) (*Property, error) // 查無 → CodePropertyNotFound
    List(ctx context.Context, f ListFilter) (ListResult, error)
    Update(ctx context.Context, p *Property) error
}
func NewService(repo Repository) *Service
func (s *Service) Create(ctx context.Context, in CreateInput) (*Property, error)
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Property, error)
func (s *Service) List(ctx context.Context, f ListFilter) (ListResult, error)
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Property, error)
func CanTransition(from, to Status) bool                 // 狀態機唯一權威來源
func (s *Service) ChangeStatus(ctx context.Context, id uuid.UUID, target Status) (*Property, error)

package pgrepo  // internal/property/pgrepo
func New(db *bun.DB) *PropertyRepository

package httpapi // internal/property/httpapi
func NewHandler(svc *property.Service, logger *slog.Logger) *Handler
func (h *Handler) Mount() server.Mount

package testsupport // internal/testsupport
func RepoRoot(t *testing.T) string
func StartPostgres(t *testing.T) (dsn string, stop func())   // stop 以 sync.Once 保護，可重複呼叫
func StartRedis(t *testing.T) (addr string, stop func())
func StartAPI(t *testing.T, configName string, env map[string]string) (baseURL string, output func() string, stop func())
```

**兩個刻意接受的限制（不要「修正」）：** `httpapi.NewHandler` 吃的是具體的 `*property.Service`
而非介面；時間直接取自 `time.Now()`，沒有可注入的 Clock。理由是測試策略走 testcontainers 端到端，
不需要這兩道縫。

相關：[[property-service-layering]]、[[property-status-machine]]、[[money-as-decimal-string]]
