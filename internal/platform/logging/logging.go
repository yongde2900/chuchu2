// Package logging 提供整個服務共用的結構化日誌建構子。
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// level 接受 debug／info／warn／error（不分大小寫）；
// 空字串或無法辨識的值一律退回 info，不會 panic。
func New(level string, w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		// 包含 "info"、空字串、以及任何無法辨識的值。
		return slog.LevelInfo
	}
}
