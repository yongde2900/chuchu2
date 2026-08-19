// Command dbmigrate 是套用／回滾 db/ 底下 migration 的一次性指令。
//
// 用法：
//
//	go run ./cmd/dbmigrate up   --config=<name>
//	go run ./cmd/dbmigrate down --config=<name>
//
// 兩個子指令共用 --config flag，載入方式與 cmd/api 相同（config.Load），
// 讓測試能用 CHUCHU_ 前綴環境變數把它指向 testcontainers 起的臨時容器。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yongde2900/chuchu2/internal/config"
	"github.com/yongde2900/chuchu2/internal/migrate"
	"github.com/yongde2900/chuchu2/internal/platform/postgres"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run 是 main 的實際邏輯，回傳 process exit code，方便測試與維持 main 精簡。
func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: dbmigrate <up|down> --config=<name>")
		return 1
	}

	subcommand := args[0]
	if subcommand != "up" && subcommand != "down" {
		fmt.Fprintf(os.Stderr, "未知的子指令 %q，只接受 up 或 down\n", subcommand)
		return 1
	}

	fs := flag.NewFlagSet("dbmigrate "+subcommand, flag.ContinueOnError)
	configName := fs.String("config", "config", "設定檔名稱（對應 config/<name>.yaml，不含副檔名）")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	cfg, err := config.Load(*configName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.Postgres)
	if err != nil {
		fmt.Fprintf(os.Stderr, "建立 postgres 連線池失敗: %v\n", err)
		return 1
	}
	defer db.Close()

	switch subcommand {
	case "up":
		if err := migrate.Up(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "migrate up 失敗: %v\n", err)
			return 1
		}
		fmt.Println("migrate up 完成")
	case "down":
		if err := migrate.Down(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "migrate down 失敗: %v\n", err)
			return 1
		}
		fmt.Println("migrate down 完成")
	}

	return 0
}
