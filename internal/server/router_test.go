package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestNewRouter_DebugTrue_MountsPanicRoute 驗證 Debug: true 時 GET /debug/panic 存在
// 且該路由被 Recoverer 攔截成 500，而不是讓程序真的崩潰。
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

// TestNewRouter_DebugFalse_DoesNotMountPanicRoute 驗證 Debug: false（正式環境的預設）時
// /debug/panic 不存在，回應 404，而不是誤把偵錯用路由帶到正式環境。
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

// TestNewRouter_AppliesMounts 驗證傳入的 Mount 函式真的會被套用到 router 上。
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

// TestNewRouter_NilLogger_DoesNotPanic 驗證 Options.Logger 為 nil 時
// NewRouter 會退回 slog.Default()，不會 panic。
func TestNewRouter_NilLogger_DoesNotPanic(t *testing.T) {
	r := NewRouter(Options{Debug: true})
	if r == nil {
		t.Fatalf("NewRouter 回傳 nil")
	}
}
