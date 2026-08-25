// Package server 負責把各 feature 套件的路由組裝成一個完整的 chi router，
// 並提供啟動／優雅關閉 HTTP server 的邏輯。
//
// internal/server 不得 import 任何 feature 套件；每個 feature 透過 Mount
// 自己把路由掛上來。
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/internal/httpx"
)

// Mount 讓 feature 套件自己宣告路由。
type Mount func(r chi.Router)

type Options struct {
	// 為 true 時才掛載 GET /debug/panic（測試用、必定 panic 的路由）。
	Debug bool
	// 為 nil 時退回 slog.Default()。
	Logger *slog.Logger
}

// middleware 順序固定為 RequestID → access log → EnsureJSONError → Recoverer，
// 三個理由缺一不可：
//   - RequestID 最外層，access log 與 Recoverer 才拿得到 request id。
//   - Recoverer 最內層，panic 先轉成 500，access log 才記得到狀態碼。
//   - EnsureJSONError 包在 Recoverer 外層，任何路由（含 /debug/panic 這種
//     非產生的路由）漏接錯誤處理都會在這裡被改寫，純文字不可能外洩。
func NewRouter(opts Options, mounts ...Mount) *chi.Mux {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	r.Use(accessLog(logger))
	r.Use(httpx.EnsureJSONError(logger))
	r.Use(httpx.Recoverer(logger))

	if opts.Debug {
		r.Get("/debug/panic", func(w http.ResponseWriter, r *http.Request) {
			panic("/debug/panic：測試用的必定 panic 路由")
		})
	}

	for _, mount := range mounts {
		mount(r)
	}

	return r
}

// accessLog 記錄方法、路徑、狀態碼、耗時與 request_id。
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			logger.InfoContext(r.Context(), "access",
				"request_id", httpx.RequestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// 讓 access log 記得到實際寫出的狀態碼。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}
