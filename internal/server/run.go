package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Run 在 addr 上啟動 h，並在 ctx 被取消時以 shutdownTimeout 為上限做優雅關閉。
//
// 正常的優雅關閉（ctx 取消後在時限內關完）回傳 nil；server 啟動失敗
// （例如 port 已被占用）或關閉逾時則回傳對應的 error。
func Run(ctx context.Context, addr string, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: h,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", addr)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	select {
	case err := <-serveErrCh:
		return err
	case <-ctx.Done():
		logger.Info("http server shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-serveErrCh
	}
}
