package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
)

// syncBuffer 是一個帶鎖的 bytes.Buffer，避免多個 goroutine（middleware 內部）
// 同時寫入時被 -race 抓到。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestWriteJSON 驗證 WriteJSON 會設定狀態碼並輸出合法 JSON。
func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusTeapot, map[string]string{"foo": "bar"})

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v, body=%s", err, rec.Body.String())
	}
	if body["foo"] != "bar" {
		t.Fatalf("body[foo] = %q, want %q", body["foo"], "bar")
	}
}

// TestWriteError_AppError 驗證帶有 apperr.Error 的錯誤會依 HTTPStatus 映射出狀態碼，
// 且 body 帶有正確的 code / request_id / details。
func TestWriteError_AppError(t *testing.T) {
	buf := &syncBuffer{}
	logger := logging.New("info", buf)

	appErr := apperr.Validation(apperr.FieldError{Field: "name", Reason: "required"})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(withTestRequestID(req.Context(), "req-123"))
	rec := httptest.NewRecorder()

	WriteError(rec, req, logger, appErr)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v", err)
	}
	if body.Code != string(apperr.CodeValidationFailed) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeValidationFailed)
	}
	if body.RequestID != "req-123" {
		t.Fatalf("body.RequestID = %q, want %q", body.RequestID, "req-123")
	}
	if len(body.Details) != 1 || body.Details[0].Field != "name" {
		t.Fatalf("body.Details = %+v, want [{name required}]", body.Details)
	}
}

// TestWriteError_NonAppError 驗證非 apperr.Error 的一般錯誤會被降級為 INTERNAL 500，
// 且不會外洩原始錯誤訊息內容以外的堆疊資訊。
func TestWriteError_NonAppError(t *testing.T) {
	buf := &syncBuffer{}
	logger := logging.New("info", buf)

	plainErr := errors.New("某個底層 driver 炸掉了")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withTestRequestID(req.Context(), "req-456"))
	rec := httptest.NewRecorder()

	WriteError(rec, req, logger, plainErr)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v", err)
	}
	if body.Code != string(apperr.CodeInternal) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeInternal)
	}
	if body.RequestID != "req-456" {
		t.Fatalf("body.RequestID = %q, want %q", body.RequestID, "req-456")
	}
}
