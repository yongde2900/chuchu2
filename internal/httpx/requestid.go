// Package httpx 提供服務共用的 HTTP middleware 與 JSON 輔助函式。
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// 未匯出的型別，避免與其他套件的 context key 碰撞。
type contextKey int

const requestIDKey contextKey = iota

// RequestID 產生非空且不重複的 request id，放進 context 並回寫到
// 回應 header X-Request-Id。必須掛在最外層，後面的 middleware 才拿得到。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)

		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// 未經過 RequestID middleware 時回傳空字串，不會 panic。
func RequestIDFrom(ctx context.Context) string {
	id, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		return ""
	}
	return id
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 亂數來源不可用時仍必須回傳非空字串，用時間戳降級。
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
