package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_LevelParsing(t *testing.T) {
	tests := []struct {
		level     string
		wantDebug bool
		wantInfo  bool
	}{
		{"debug", true, true},
		{"info", false, true},
		{"warn", false, false},
		{"error", false, false},
		{"", false, true},      // 未指定時預設為 info
		{"bogus", false, true}, // 無法解析時退回 info，不能 panic
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(tt.level, &buf)
			if logger == nil {
				t.Fatalf("New(%q) returned nil", tt.level)
			}

			buf.Reset()
			logger.Debug("debug message")
			if got := buf.String(); tt.wantDebug && !strings.Contains(got, "debug message") {
				t.Errorf("level %q: expected debug message to be logged, got %q", tt.level, got)
			} else if !tt.wantDebug && strings.Contains(got, "debug message") {
				t.Errorf("level %q: expected debug message to be suppressed, got %q", tt.level, got)
			}

			buf.Reset()
			logger.Info("info message")
			if got := buf.String(); tt.wantInfo && !strings.Contains(got, "info message") {
				t.Errorf("level %q: expected info message to be logged, got %q", tt.level, got)
			} else if !tt.wantInfo && strings.Contains(got, "info message") {
				t.Errorf("level %q: expected info message to be suppressed, got %q", tt.level, got)
			}
		})
	}
}

func TestNew_StructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := New("info", &buf)
	logger.Info("started", slog.String("component", "api"))

	got := buf.String()
	if !strings.Contains(got, "started") {
		t.Errorf("output %q missing message", got)
	}
	if !strings.Contains(got, "component") || !strings.Contains(got, "api") {
		t.Errorf("output %q missing structured attribute", got)
	}
}
