// Package server 負責把各 feature 套件的路由組裝成一個完整的 chi router，
// 並提供啟動／優雅關閉 HTTP server 的邏輯。
//
// 為了保持依賴反轉（見專案 CLAUDE.md「相依的反轉點」），internal/server
// 不 import 任何 feature 套件（例如 internal/health、internal/property/...）；
// 每個 feature 套件透過實作 Mount 自己把路由掛上來。
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/internal/httpx"
)

// Mount 讓每個 feature 套件宣告自己的路由。只有真的擁有 HTTP 路由的套件
// （例如 httpapi、health）才需要實作它。
type Mount func(r chi.Router)

// Options 是組裝 router 時的選項。
type Options struct {
	// Debug 為 true 時才掛載 GET /debug/panic（測試用、必定 panic 的路由）。
	Debug bool
	// Logger 供 access log 與 Recoverer 使用；為 nil 時退回 slog.Default()。
	Logger *slog.Logger
}

// NewRouter 依 opts 與各 feature 套件提供的 mounts 組出完整的 chi router。
//
// middleware 順序固定為：RequestID → 結構化 access log → Recoverer。
// 這個順序確保：(1) access log 與 Recoverer 都能拿到 request id；
// (2) handler 內的 panic 會先被 Recoverer 攔截、轉成 500 回應，
// 之後 access log 才照常記一行，不會讓 panic 逃出 middleware chain。
func NewRouter(opts Options, mounts ...Mount) *chi.Mux {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	r.Use(accessLog(logger))
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

// accessLog 是結構化的 access log middleware，記錄方法、路徑、狀態碼、
// 耗時與 request_id。
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

// statusRecorder 包住 http.ResponseWriter，讓 access log 記得到實際寫出的狀態碼。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}
