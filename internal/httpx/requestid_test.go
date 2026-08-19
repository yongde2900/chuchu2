package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequestID_SetsNonEmptyIDInContext 驗證 RequestID middleware 會把一個
// 非空的 request id 放進 context，之後可以用 RequestIDFrom 取出。
func TestRequestID_SetsNonEmptyIDInContext(t *testing.T) {
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	})

	handler := RequestID(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen == "" {
		t.Fatalf("RequestIDFrom(ctx) 在 handler 內為空字串，want 非空")
	}
}

// TestRequestID_UniquePerRequest 驗證每個請求拿到的 request id 不同。
func TestRequestID_UniquePerRequest(t *testing.T) {
	var ids []string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, RequestIDFrom(r.Context()))
	})

	handler := RequestID(next)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("ids = %v, want 兩個不同的 request id", ids)
	}
}

// TestRequestIDFrom_NoValue 驗證在沒有經過 RequestID middleware 的 context 上呼叫
// RequestIDFrom 回傳空字串，不會 panic。
func TestRequestIDFrom_NoValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := RequestIDFrom(req.Context()); got != "" {
		t.Fatalf("RequestIDFrom(空 context) = %q, want 空字串", got)
	}
}

// TestRequestID_SetsResponseHeader 驗證 request id 也會回寫到回應 header。
func TestRequestID_SetsResponseHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RequestID(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Fatalf("回應 header X-Request-Id 為空，want 非空")
	}
}
