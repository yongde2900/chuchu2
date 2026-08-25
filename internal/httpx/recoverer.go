package httpx

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// Recoverer 攔截 panic。panic 值與堆疊只寫進 log，回應 body 一律是固定的
// INTERNAL 500 訊息——堆疊、檔案路徑這類內部細節絕不外洩給呼叫端。
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}

				requestID := RequestIDFrom(r.Context())
				logger.ErrorContext(r.Context(), "panic recovered",
					"request_id", requestID,
					"panic", fmt.Sprintf("%v", rec),
					"stack", string(debug.Stack()),
				)

				WriteJSON(w, http.StatusInternalServerError, ErrorBody{
					Code:      string(apperr.CodeInternal),
					Message:   "internal server error",
					RequestID: requestID,
				})
			}()

			next.ServeHTTP(w, r)
		})
	}
}
