// Package httpx 提供服務共用的 HTTP middleware 與 JSON 讀寫輔助函式：
// request id 產生與傳遞、panic 攔截、以及統一的錯誤回應格式。
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// contextKey 是未匯出的型別，避免 request id 存進 context 時與其他套件的
// string key 意外碰撞。
type contextKey int

// requestIDKey 是 request id 在 context 中的 key。
const requestIDKey contextKey = iota

// RequestID 是最外層的 middleware：替每個請求產生一個非空、彼此不重複的
// request id，放進 context（供 RequestIDFrom 取出）並回寫到回應 header
// X-Request-Id。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)

		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom 從 context 取出 RequestID middleware 放入的 request id。
// 若 context 中沒有（例如未經過該 middleware），回傳空字串，不會 panic。
func RequestIDFrom(ctx context.Context) string {
	id, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		return ""
	}
	return id
}

// newRequestID 產生一個隨機的、非空的 request id。
func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 極端情況下（系統亂數來源不可用）仍要保證回傳非空字串，
		// 用時間戳當降級方案。
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
