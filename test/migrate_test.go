// 本檔對應 BDD scenario（@infra）：「Migration 可正向套用亦可回滾」。
//
// 這裡起一個專屬、用完即丟的 Postgres 容器（不可用 TestMain 的共用容器）：
// 本測試最後一步是 down，會把 properties 資料表刪掉，若跑在共用容器上會讓
// 同 package 中其後所有需要該資料表的測試（Task 5–7）連鎖失敗。專屬容器
// 剛好也是空的，語意上更貼合 BDD 的 Given「一個空的 Postgres 資料庫」。
package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// dbmigrateRunTimeout 涵蓋 `go run` 第一次要編譯 dbmigrate 的時間，
// 給足夠寬鬆的上限。
const dbmigrateRunTimeout = 2 * time.Minute

func TestMigrate_UpCreatesPropertiesTable_DownDropsIt(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	runDBMigrate(t, "up", dsn)
	if !propertiesTableExists(t, dsn) {
		t.Fatal("執行 `dbmigrate up` 後 properties 資料表應存在，實際不存在")
	}

	runDBMigrate(t, "down", dsn)
	if propertiesTableExists(t, dsn) {
		t.Fatal("執行 `dbmigrate down` 後 properties 資料表應不存在，實際仍存在")
	}
}

// runDBMigrate 以 `go run ./cmd/dbmigrate <subcommand> --config=test` 執行
// dbmigrate，並以 CHUCHU_POSTGRES_DSN 覆寫設定檔中的 postgres.dsn，指向
// dsn 這個專屬測試容器。工作目錄設為 repo 根目錄，否則 config.Load 找不到
// config/test.yaml。
func runDBMigrate(t *testing.T, subcommand, dsn string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), dbmigrateRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/dbmigrate", subcommand, "--config=test")
	cmd.Dir = testsupport.RepoRoot(t)
	cmd.Env = append(os.Environ(), fmt.Sprintf("CHUCHU_POSTGRES_DSN=%s", dsn))

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`go run ./cmd/dbmigrate %s --config=test` 失敗: %v\n輸出:\n%s",
			subcommand, err, string(output))
	}
}

// propertiesTableExists 直接連線 dsn，用
// to_regclass('public.properties') IS NOT NULL 驗證 properties 資料表是否存在
// ——不用「SELECT 看有沒有報錯」這種脆弱作法。
func propertiesTableExists(t *testing.T, dsn string) bool {
	t.Helper()

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	defer sqldb.Close()

	bunDB := bun.NewDB(sqldb, pgdialect.New())
	defer bunDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var exists bool
	err := bunDB.QueryRowContext(ctx,
		"SELECT to_regclass('public.properties') IS NOT NULL").Scan(&exists)
	if err != nil {
		t.Fatalf("查詢 properties 資料表是否存在失敗: %v", err)
	}

	return exists
}
