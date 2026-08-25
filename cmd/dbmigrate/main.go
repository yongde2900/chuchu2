// Command dbmigrate 是操作 db/ 底下 bun/migrate migration 的 CLI。
//
// 用法：
//
//	go run ./cmd/dbmigrate <subcommand> --config=<name> [args...]
//
// 子指令（只有這六個，up／down 已廢除且不保留別名）：
//
//	init        建立 bun_migrations／bun_migration_locks 兩張記錄用資料表
//	migrate     套用所有尚未套用的 migration
//	rollback    回滾最後一次 migrate 套用的整組 migration
//	status      列出所有 migration 及其套用狀態
//	unlock      釋放卡住的 migration lock（不會再包一層 Lock）
//	create_sql  以 <name> 產生一對新的 .tx.up.sql／.tx.down.sql 檔案
//
// 每個子指令都要求 --config，讓測試能用 CHUCHU_ 前綴環境變數把它指向
// testcontainers 起的臨時容器。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/uptrace/bun/migrate"

	"github.com/yongde2900/chuchu2/db"
	"github.com/yongde2900/chuchu2/internal/config"
	"github.com/yongde2900/chuchu2/internal/platform/postgres"
)

var subcommands = []string{"init", "migrate", "rollback", "status", "unlock", "create_sql"}

func main() {
	os.Exit(run(os.Args[1:]))
}

// 邏輯收在 run 裡回傳 exit code，讓 main 只剩 os.Exit——exit code 才可測，
// 而且 main 的 os.Exit 會跳過 defer。
//
// 刻意先判斷子指令合法性再處理 flag：否則 `frobnicate --config=test`
// 會先被 flag 解析吃掉 --config，「未知子指令」這條路徑永遠測不到。
func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage())
		return 1
	}

	subcommand := args[0]
	if !isValidSubcommand(subcommand) {
		fmt.Fprintf(os.Stderr, "未知的子指令 %q，只接受: %s\n", subcommand, strings.Join(subcommands, ", "))
		return 1
	}

	fs := flag.NewFlagSet("dbmigrate "+subcommand, flag.ContinueOnError)
	configName := fs.String("config", "", "設定檔名稱（對應 config/<name>.yaml，不含副檔名，必填）")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	if *configName == "" {
		fmt.Fprintln(os.Stderr, "缺少必要旗標 --config：請指定設定檔名稱（對應 config/<name>.yaml）")
		return 1
	}

	cfg, err := config.Load(*configName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bunDB, err := postgres.Open(ctx, cfg.Postgres)
	if err != nil {
		fmt.Fprintf(os.Stderr, "建立 postgres 連線池失敗: %v\n", err)
		return 1
	}
	defer bunDB.Close()

	migrator, err := db.NewMigrator(bunDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "建立 migrator 失敗: %v\n", err)
		return 1
	}

	switch subcommand {
	case "init":
		return runInit(ctx, migrator)
	case "migrate":
		return runMigrate(ctx, migrator)
	case "rollback":
		return runRollback(ctx, migrator)
	case "status":
		return runStatus(ctx, migrator)
	case "unlock":
		return runUnlock(ctx, migrator)
	case "create_sql":
		return runCreateSQL(ctx, migrator, fs.Args())
	default:
		// 不會發生：subcommand 已經過 isValidSubcommand 檢查。
		fmt.Fprintf(os.Stderr, "未實作的子指令 %q\n", subcommand)
		return 1
	}
}

func usage() string {
	return fmt.Sprintf(
		"用法: dbmigrate <%s> --config=<name> [args...]",
		strings.Join(subcommands, "|"),
	)
}

func isValidSubcommand(name string) bool {
	for _, s := range subcommands {
		if s == name {
			return true
		}
	}
	return false
}

// 建立 bun_migrations／bun_migration_locks 兩張記錄用資料表；可重入。
func runInit(ctx context.Context, migrator *migrate.Migrator) int {
	if err := migrator.Init(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "init 失敗: %v\n", err)
		return 1
	}
	fmt.Println("init 完成")
	return 0
}

// 沒有新 migration 時視為成功。輸出含固定字串 "no new migrations" 供測試斷言。
//
// Lock 失敗時直接回傳、不註冊 defer Unlock，避免釋放一把不屬於自己的 lock。
func runMigrate(ctx context.Context, migrator *migrate.Migrator) int {
	if err := migrator.Lock(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "取得 migration lock 失敗: %v\n", err)
		return 1
	}
	defer func() {
		if err := migrator.Unlock(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "釋放 migration lock 失敗: %v\n", err)
		}
	}()

	group, err := migrator.Migrate(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate 失敗: %v\n", err)
		return 1
	}
	if group.IsZero() {
		fmt.Println("沒有新的 migration 需要套用 (no new migrations)")
		return 0
	}
	fmt.Printf("套用了 migration group #%d: %s\n", group.ID, group.Migrations)
	return 0
}

// 回滾最後一個 group（不是全部，這是 bun 的語意）。沒有可回滾的 group 時
// 視為成功，輸出含固定字串 "nothing to rollback" 供測試斷言。
func runRollback(ctx context.Context, migrator *migrate.Migrator) int {
	if err := migrator.Lock(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "取得 migration lock 失敗: %v\n", err)
		return 1
	}
	defer func() {
		if err := migrator.Unlock(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "釋放 migration lock 失敗: %v\n", err)
		}
	}()

	group, err := migrator.Rollback(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rollback 失敗: %v\n", err)
		return 1
	}
	if group.IsZero() {
		fmt.Println("沒有可回滾的 migration group (nothing to rollback)")
		return 0
	}
	fmt.Printf("回滾了 migration group #%d: %s\n", group.ID, group.Migrations)
	return 0
}

func runStatus(ctx context.Context, migrator *migrate.Migrator) int {
	migrations, err := migrator.MigrationsWithStatus(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "查詢 migration 狀態失敗: %v\n", err)
		return 1
	}
	for _, m := range migrations {
		status := "待套用"
		if m.IsApplied() {
			status = fmt.Sprintf("已套用於 %s", m.MigratedAt)
		}
		fmt.Printf("%s\t%s\n", m.String(), status)
	}
	return 0
}

// 刻意不包一層 Lock——unlock 本來就是給「拿到 Lock 但流程異常沒走到 Unlock」
// 時手動解卡用的。
func runUnlock(ctx context.Context, migrator *migrate.Migrator) int {
	if err := migrator.Unlock(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "unlock 失敗: %v\n", err)
		return 1
	}
	fmt.Println("unlock 完成")
	return 0
}

// 一律走 CreateTxSQLMigrations 而非 CreateSQLMigrations：只有 .tx. 檔名
// bun 才會包交易，本專案所有 migration 都必須有交易保護。
func runCreateSQL(ctx context.Context, migrator *migrate.Migrator, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "create_sql 需要一個 migration 名稱參數，例如: create_sql add_index")
		return 1
	}

	files, err := migrator.CreateTxSQLMigrations(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "create_sql 失敗: %v\n", err)
		return 1
	}
	for _, f := range files {
		fmt.Println(f.Path)
	}
	return 0
}
