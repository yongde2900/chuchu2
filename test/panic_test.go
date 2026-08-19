// 本檔對應 BDD scenario（@infra @error）：
// 「未預期的 panic 被攔截為 500 且不外洩堆疊」。
//
// 這個測試不需要 Docker、不需要任何外部服務：它同行程組出完整的 chi router
// （含 RequestID → access log → Recoverer 這條 middleware chain），
// 用 httptest.NewServer 起一個真正的 HTTP server 來驗證行為，
// 並直接檢查寫進自訂 buffer 的結構化 log。
//
// 注意：testsupport.StartAPI（以子行程跑起整支 binary）是 Task 3 的產出，
// 這裡刻意不用它、也不建立它。
package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yongde2900/chuchu2/internal/platform/logging"
	"github.com/yongde2900/chuchu2/internal/server"
)

// syncBuffer 是一個帶鎖的 io.Writer，讓 logger 的寫入在 -race 之下是安全的
// （HTTP server 在背景 goroutine 處理請求，測試主 goroutine 之後又讀取同一個 buffer）。
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

// logRecord 是 slog JSON handler 輸出的一行 log 的最小子集。
type logRecord struct {
	Level     string `json:"level"`
	RequestID string `json:"request_id"`
}

// errorResponseBody 對應 httpx.ErrorBody 的形狀（這裡不直接 import internal/httpx 的型別，
// 用一個等價的匿名結構驗證回應「形狀」本身也符合契約）。
type errorResponseBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func TestPanic_RecoveredAsInternal500(t *testing.T) {
	logBuf := &syncBuffer{}
	logger := logging.New("info", logBuf)

	r := server.NewRouter(server.Options{Debug: true, Logger: logger})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/panic")
	if err != nil {
		t.Fatalf("GET /debug/panic 失敗: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("狀態碼 = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	if strings.Contains(string(rawBody), "goroutine") {
		t.Fatalf("回應 body 不應該包含 %q，實際 body: %s", "goroutine", rawBody)
	}

	var body errorResponseBody
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v, body=%s", err, rawBody)
	}
	if body.Code != "INTERNAL" {
		t.Fatalf("body.code = %q, want %q", body.Code, "INTERNAL")
	}
	if body.RequestID == "" {
		t.Fatalf("body.request_id 為空字串，want 非空")
	}

	if !hasErrorLogWithRequestID(t, logBuf.String(), body.RequestID) {
		t.Fatalf("log 輸出中找不到 level=ERROR 且 request_id=%q 的紀錄，log 內容:\n%s", body.RequestID, logBuf.String())
	}
}

// hasErrorLogWithRequestID 逐行解析 JSON log，找出一筆 level 為 ERROR
// 且 request_id 與 wantRequestID 相同的紀錄。
func hasErrorLogWithRequestID(t *testing.T, logOutput string, wantRequestID string) bool {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(logOutput), "\n") {
		if line == "" {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log 輸出中有一行不是合法 JSON: %v, line=%s", err, line)
		}
		if rec.Level == "ERROR" && rec.RequestID == wantRequestID {
			return true
		}
	}
	return false
}
