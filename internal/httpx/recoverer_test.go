package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
)

// TestRecoverer_CatchesPanicAndReturnsInternal500 驗證 Recoverer 攔截 panic 後
// 回應 500、body code 為 INTERNAL、request_id 非空，且 body 不含堆疊字串 "goroutine"。
func TestRecoverer_CatchesPanicAndReturnsInternal500(t *testing.T) {
	buf := &syncBuffer{}
	logger := logging.New("info", buf)

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	handler := RequestID(Recoverer(logger)(panicking))

	req := httptest.NewRequest(http.MethodGet, "/debug/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if strings.Contains(rec.Body.String(), "goroutine") {
		t.Fatalf("回應 body 包含 %q，不應該外洩堆疊: %s", "goroutine", rec.Body.String())
	}

	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v, body=%s", err, rec.Body.String())
	}
	if body.Code != string(apperr.CodeInternal) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeInternal)
	}
	if body.RequestID == "" {
		t.Fatalf("body.RequestID 為空，want 非空")
	}

	logLine := buf.String()
	if !strings.Contains(logLine, `"level":"ERROR"`) {
		t.Fatalf("log 輸出中沒有 ERROR level 的紀錄: %s", logLine)
	}
	if !strings.Contains(logLine, body.RequestID) {
		t.Fatalf("log 輸出中沒有包含 request_id %q: %s", body.RequestID, logLine)
	}
}

// TestRecoverer_NoPanic_PassesThrough 驗證沒有 panic 時 Recoverer 不影響正常回應。
func TestRecoverer_NoPanic_PassesThrough(t *testing.T) {
	buf := &syncBuffer{}
	logger := logging.New("info", buf)

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := Recoverer(logger)(ok)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
