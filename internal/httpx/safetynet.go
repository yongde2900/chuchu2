package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// 下游原本要寫的內容一律視為不可信（可能含底層錯誤訊息、路徑等內部細節），
// 不放進回應 body。
const ensureJSONErrorMessage = "internal server error"

// EnsureJSONError 是錯誤回應的第二道防線：狀態碼 >= 400 但 Content-Type 不是
// JSON 時，就地改寫成統一形狀。
//
// 為什麼需要它：oapi-codegen 產生的三個 error hook 預設都是
// `http.Error(w, err.Error(), code)`，會用 text/plain 把底層錯誤訊息直接回吐。
// internal/apihttp 已經把三個都接上了，但「有沒有接」是人為紀律
// （新增一條不經過 apihttp.Mount 的路由就會漏掉），不是結構保證。
//
// 刻意不緩衝回應主體：狀態碼與 Content-Type 在 WriteHeader 當下就已確定。
// 代價是不支援串流回應——本 API 全部是小型 JSON，沒有串流端點。
func EnsureJSONError(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			guard := &jsonErrorGuard{ResponseWriter: w, logger: logger, r: r}
			next.ServeHTTP(guard, r)
		})
	}
}

type jsonErrorGuard struct {
	http.ResponseWriter
	logger *slog.Logger
	r      *http.Request

	wroteHeader bool
	intercepted bool
}

// 只在第一次呼叫時判斷；重複呼叫直接吞掉，不重複記 log（標準庫同樣會忽略）。
func (g *jsonErrorGuard) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true

	contentType := g.Header().Get("Content-Type")
	if status < http.StatusBadRequest || strings.HasPrefix(contentType, "application/json") {
		// 原樣放行，連 header 都不碰——成功回應必須逐位元組不變。
		g.ResponseWriter.WriteHeader(status)
		return
	}

	g.intercepted = true
	requestID := RequestIDFrom(g.r.Context())

	g.logger.WarnContext(g.r.Context(), "攔截到未統一形狀的錯誤回應，已改寫成統一 JSON 形狀（可能是某條路由漏接了 error hook）",
		"request_id", requestID,
		"status", status,
		"content_type", contentType,
	)

	// Del 必須在底層 WriteHeader 之前：下游若是 http.Error 寫的，
	// Content-Length 是照純文字算的，跟改寫後的 JSON 對不上，
	// 不刪會讓呼叫端讀到被截斷或帶垃圾位元組的 body。
	g.Header().Del("Content-Length")
	g.Header().Set("Content-Type", "application/json; charset=utf-8")
	g.ResponseWriter.WriteHeader(status)

	body, err := json.Marshal(ErrorBody{
		Code:      string(apperr.CodeInternal),
		Message:   ensureJSONErrorMessage,
		RequestID: requestID,
	})
	if err != nil {
		// 固定結構理論上不會編碼失敗；保底輸出仍然合法的 JSON，不讓連線卡死。
		body = []byte(`{}`)
	}
	_, _ = g.ResponseWriter.Write(body)
}

// 攔截生效後必須吞掉下游後續的 Write，否則原本那段純文字會接在 JSON 後面
// 一起送出去。回報 (len(p), nil) 而非 0 或 error，避免下游誤判成寫入失敗。
func (g *jsonErrorGuard) Write(p []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.intercepted {
		return len(p), nil
	}
	return g.ResponseWriter.Write(p)
}
