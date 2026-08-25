package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ctx 取消後在時限內關完回傳 nil；啟動失敗（例如 port 被占用）或關閉逾時回傳 error。
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
