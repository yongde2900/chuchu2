package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/httpx"
)

// 該路由必須被 Recoverer 攔成 500，而不是讓程序真的崩潰。
func TestNewRouter_DebugTrue_MountsPanicRoute(t *testing.T) {
	r := NewRouter(Options{Debug: true})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/panic")
	if err != nil {
		t.Fatalf("GET /debug/panic failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// Debug: false 是正式環境的預設，偵錯路由不能被帶進去。
func TestNewRouter_DebugFalse_DoesNotMountPanicRoute(t *testing.T) {
	r := NewRouter(Options{Debug: false})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/panic")
	if err != nil {
		t.Fatalf("GET /debug/panic failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (debug 路由不應該被掛載)", resp.StatusCode, http.StatusNotFound)
	}
}

func TestNewRouter_AppliesMounts(t *testing.T) {
	mount := Mount(func(r chi.Router) {
		r.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hi"))
		})
	})

	r := NewRouter(Options{Debug: false}, mount)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/hello")
	if err != nil {
		t.Fatalf("GET /hello failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestNewRouter_NilLogger_DoesNotPanic(t *testing.T) {
	r := NewRouter(Options{Debug: true})
	if r == nil {
		t.Fatalf("NewRouter 回傳 nil")
	}
}

// 這個測試的職責只有一件事：證明 NewRouter 組出來的 chain 真的接上了
// EnsureJSONError。**刪掉 router.go 裡的 r.Use(httpx.EnsureJSONError(logger))
// 那一行，這個測試必須變紅。**
//
// 攔截邏輯本身是否正確由 internal/httpx/safetynet_test.go 用真正的產生
// handler 驗證，這裡只手寫一個最小的漏接路由，不重複那件事。
func TestNewRouter_MountedRouteMissesErrorHook_SafetyNetRewritesToJSON(t *testing.T) {
	const leaked = "漏接 error hook 直接寫出的純文字，不應該外洩"

	mount := Mount(func(r chi.Router) {
		r.Get("/leaky", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(leaked))
		})
	})

	r := NewRouter(Options{Debug: false}, mount)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/leaky", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d（防護網不應該改變狀態碼）", rec.Code, http.StatusBadRequest)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want 開頭為 %q（EnsureJSONError 沒有掛在 NewRouter 的 chain 上）", contentType, "application/json")
	}

	rawBody := rec.Body.Bytes()

	var body httpx.ErrorBody
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v, body=%s", err, rawBody)
	}
	if body.Code != string(apperr.CodeInternal) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeInternal)
	}
	if body.RequestID == "" {
		t.Fatalf("body.RequestID 為空字串，want 非空")
	}

	if strings.Contains(string(rawBody), leaked) {
		t.Fatalf("回應 body 包含原本漏接 hook 寫出的純文字，不應該外洩: %s", rawBody)
	}
}

// 上面那個測試的對照組：成功回應不能被防護網誤攔。
func TestNewRouter_MountedRouteSuccess_PassesThroughUnchanged(t *testing.T) {
	const wantBody = "hello"

	mount := Mount(func(r chi.Router) {
		r.Get("/ok", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(wantBody))
		})
	})

	r := NewRouter(Options{Debug: false}, mount)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != wantBody {
		t.Fatalf("body = %q, want %q（成功回應不應該被防護網改寫）", rec.Body.String(), wantBody)
	}
}
