package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTempConfig 在暫時目錄下建立 config/<name>.yaml，並回傳該暫時目錄路徑。
// Load 會依 "config/<name>.yaml" 相對路徑讀檔，所以測試需要切換工作目錄到這個暫時目錄。
func writeTempConfig(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	path := filepath.Join(configDir, name+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := writeTempConfig(t, "test", `
server:
  port: 8080
  debug: true
  shutdown_timeout: 5s
postgres:
  dsn: "postgres://postgres:postgres@localhost:5432/chuchu?sslmode=disable"
  max_open_conns: 10
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
log:
  level: "info"
`)
	chdir(t, dir)

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if !cfg.Server.Debug {
		t.Errorf("Server.Debug = false, want true")
	}
	if cfg.Server.ShutdownTimeout != 5*time.Second {
		t.Errorf("Server.ShutdownTimeout = %v, want 5s", cfg.Server.ShutdownTimeout)
	}
	if cfg.Postgres.DSN != "postgres://postgres:postgres@localhost:5432/chuchu?sslmode=disable" {
		t.Errorf("Postgres.DSN = %q, unexpected", cfg.Postgres.DSN)
	}
	if cfg.Postgres.MaxOpenConns != 10 {
		t.Errorf("Postgres.MaxOpenConns = %d, want 10", cfg.Postgres.MaxOpenConns)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr = %q, want localhost:6379", cfg.Redis.Addr)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
}

func TestLoad_MissingRequiredKey(t *testing.T) {
	dir := writeTempConfig(t, "broken", `
server:
  port: 8080
redis:
  addr: "localhost:6379"
`)
	chdir(t, dir)

	_, err := Load("broken")
	if err == nil {
		t.Fatalf("Load returned nil error, want *MissingKeyError")
	}

	missingErr, ok := errors.AsType[*MissingKeyError](err)
	if !ok {
		t.Fatalf("Load returned %v (%T), want *MissingKeyError", err, err)
	}
	if missingErr.Key != "postgres.dsn" {
		t.Errorf("MissingKeyError.Key = %q, want %q", missingErr.Key, "postgres.dsn")
	}
	if got := err.Error(); got == "" {
		t.Fatalf("Error() returned empty string")
	} else if !strings.Contains(got, "postgres.dsn") {
		t.Errorf("Error() = %q, want it to contain %q", got, "postgres.dsn")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	dir := writeTempConfig(t, "test", `
server:
  port: 8080
postgres:
  dsn: "postgres://placeholder"
redis:
  addr: "localhost:6379"
`)
	chdir(t, dir)

	t.Setenv("CHUCHU_POSTGRES_DSN", "postgres://overridden:pw@host:5432/db?sslmode=disable")
	t.Setenv("CHUCHU_REDIS_ADDR", "redis-overridden:6380")

	cfg, err := Load("test")
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg.Postgres.DSN != "postgres://overridden:pw@host:5432/db?sslmode=disable" {
		t.Errorf("Postgres.DSN = %q, env override not applied", cfg.Postgres.DSN)
	}
	if cfg.Redis.Addr != "redis-overridden:6380" {
		t.Errorf("Redis.Addr = %q, env override not applied", cfg.Redis.Addr)
	}
}

func TestLoad_EnvOverrideSatisfiesMissingKey(t *testing.T) {
	// postgres.dsn 完全不在 yaml 裡，但環境變數有提供 —— Load 不應該報 MissingKeyError。
	dir := writeTempConfig(t, "broken", `
server:
  port: 8080
redis:
  addr: "localhost:6379"
`)
	chdir(t, dir)

	t.Setenv("CHUCHU_POSTGRES_DSN", "postgres://from-env@host:5432/db?sslmode=disable")

	cfg, err := Load("broken")
	if err != nil {
		t.Fatalf("Load returned unexpected error with env override: %v", err)
	}
	if cfg.Postgres.DSN != "postgres://from-env@host:5432/db?sslmode=disable" {
		t.Errorf("Postgres.DSN = %q, env override not applied", cfg.Postgres.DSN)
	}
}
