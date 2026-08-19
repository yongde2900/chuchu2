// Command api 是包租代管服務的進入點。
//
// 組裝順序：config.Load → logging.New → 開 Postgres／Redis 連線 → 組
// health.Service → server.NewRouter（掛上 health.Mount）→ server.Run。
// 設定載入失敗時把訊息寫到 stderr 並以非 0 code 結束；成功則啟動 HTTP server，
// 收到 SIGINT/SIGTERM 時優雅關閉。
//
// 啟動時會對 Postgres／Redis 各做一次連線驗證，但連不上時只記錄警告、
// 不會讓服務啟動失敗 —— 服務仍會啟動並持續回應請求，讓 GET /healthz
// 能如實回報 503「degraded」。這是「以有效設定檔啟動服務」與「相依斷線時
// /healthz 回 503」這兩個 BDD scenario 能同時成立的關鍵。
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/yongde2900/chuchu2/internal/config"
	"github.com/yongde2900/chuchu2/internal/health"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
	"github.com/yongde2900/chuchu2/internal/platform/postgres"
	"github.com/yongde2900/chuchu2/internal/platform/redisclient"
	"github.com/yongde2900/chuchu2/internal/server"
)

func main() {
	os.Exit(run())
}

// run 是 main 的實際邏輯，回傳 process exit code，方便測試與維持 main 精簡。
func run() int {
	configName := flag.String("config", "config", "設定檔名稱（對應 config/<name>.yaml，不含副檔名）")
	flag.Parse()

	cfg, err := config.Load(*configName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	logger := logging.New(cfg.Log.Level, os.Stdout)
	logger.Info("service config loaded", "config", *configName, "server_port", cfg.Server.Port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.Postgres)
	if err != nil {
		logger.Error("建立 postgres 連線池失敗", "error", err)
		return 1
	}
	defer db.Close()

	if err := postgres.Ping(ctx, db); err != nil {
		logger.Warn("啟動時無法連線 postgres，服務仍會啟動（/healthz 會回報 degraded）", "error", err)
	}

	redisClient, err := redisclient.Open(ctx, cfg.Redis)
	if err != nil {
		logger.Error("建立 redis client 失敗", "error", err)
		return 1
	}
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Warn("啟動時無法連線 redis，服務仍會啟動（/healthz 會回報 degraded）", "error", err)
	}

	healthSvc := health.NewService(
		health.NewPostgresChecker(db),
		health.NewRedisChecker(redisClient),
	)

	router := server.NewRouter(server.Options{
		Debug:  cfg.Server.Debug,
		Logger: logger,
	}, health.Mount(healthSvc))

	addr := net.JoinHostPort("", strconv.Itoa(cfg.Server.Port))
	if err := server.Run(ctx, addr, router, cfg.Server.ShutdownTimeout, logger); err != nil {
		logger.Error("http server 非預期結束", "error", err)
		return 1
	}

	return 0
}
