package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestRun_GracefulShutdownOnContextCancel 驗證 Run 在 ctx 被取消時會優雅關閉並回傳，
// 而不是掛住或回傳 error。
func TestRun_GracefulShutdownOnContextCancel(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "127.0.0.1:0", handler, time.Second, nil)
	}()

	// 給 goroutine 一點時間啟動 listener，再觸發取消。
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run 回傳 error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run 在 ctx 取消後 3 秒內沒有返回")
	}
}
