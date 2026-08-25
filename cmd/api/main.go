// Command api 是包租代管服務的行程進入點：設定載入、開連線、signal 與優雅關閉。
// 實際把元件接起來的組裝在 internal/app（抽出去是為了讓整合測試能在同一個
// 行程內組出 handler，見該套件的說明）。
//
// 啟動時對 Postgres／Redis 各做一次連線驗證，但連不上時**只記錄警告、
// 不讓啟動失敗**——服務仍會起來並持續回應請求，GET /healthz 才能如實
// 回報 503「degraded」。這是刻意的：相依斷線時要能被觀測到，而不是整個服務消失。
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

	"github.com/yongde2900/chuchu2/internal/app"
	"github.com/yongde2900/chuchu2/internal/config"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
	"github.com/yongde2900/chuchu2/internal/platform/postgres"
	"github.com/yongde2900/chuchu2/internal/platform/redisclient"
	"github.com/yongde2900/chuchu2/internal/server"
)

func main() {
	os.Exit(run())
}

// 邏輯收在 run 裡回傳 exit code，讓 main 只剩 os.Exit——exit code 才可測，
// 而且 main 的 os.Exit 會跳過 defer。
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

	router := app.NewHandler(app.Deps{
		DB:     db,
		Redis:  redisClient,
		Logger: logger,
		Debug:  cfg.Server.Debug,
	})

	addr := net.JoinHostPort("", strconv.Itoa(cfg.Server.Port))
	if err := server.Run(ctx, addr, router, cfg.Server.ShutdownTimeout, logger); err != nil {
		logger.Error("http server 非預期結束", "error", err)
		return 1
	}

	return 0
}
