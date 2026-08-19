// 本檔是 `test` package（整合測試）的 TestMain：啟動一組供整個 package
// 共用的 Postgres 與 Redis 測試容器，package 內所有測試結束後收掉。
//
// 這裡直接呼叫 testcontainers-go 的 postgres／redis module（而不是
// internal/testsupport.StartPostgres／StartRedis），因為那兩個函式的簽章
// 需要 *testing.T，而 TestMain 只有 *testing.M 可用。internal/testsupport
// 的 StartPostgres／StartRedis 是留給個別測試起「用完即丟、可主動停止」的
// 專屬容器（例如斷線情境），語意上和這裡的「package 共用、活到 package
// 結束」不同，本來就該是兩條路。
//
// 起完共用容器後，這裡會對共用 Postgres 容器跑一次 migrate.Up，讓同 package
// 內需要 properties 資料表的測試（Task 5–7）有資料表可用。
package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/yongde2900/chuchu2/internal/migrate"
)

// sharedPostgresDSN、sharedRedisAddr 存放 TestMain 起的共用容器的連線資訊，
// 供同 package 的測試透過下方未匯出的 getter 取得。這是整合測試 package
// 的基礎設施性質全域變數（CLAUDE.md 明列的例外），不是正式程式碼的依賴注入——
// 正式程式碼（cmd/api/main.go）的組裝點仍只有一處，且不使用全域變數。
var (
	sharedPostgresDSN string
	sharedRedisAddr   string
)

// sharedPostgres 回傳 TestMain 起的共用 Postgres 測試容器的 DSN。
func sharedPostgres() string { return sharedPostgresDSN }

// sharedRedis 回傳 TestMain 起的共用 Redis 測試容器的 addr。
func sharedRedis() string { return sharedRedisAddr }

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

// runTestMain 是 TestMain 的實際邏輯：起共用容器 → 跑測試 → 收容器 → 回傳
// exit code。拆成獨立函式是為了讓 defer／os.Exit 的順序清楚、好維護。
func runTestMain(m *testing.M) int {
	startCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pgCtr, err := tcpostgres.Run(startCtx, "postgres:16-alpine",
		tcpostgres.WithDatabase("chuchu"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: 啟動共用 postgres 測試容器失敗: %v\n", err)
		return 1
	}
	defer func() {
		if err := pgCtr.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "TestMain: 停止共用 postgres 測試容器失敗: %v\n", err)
		}
	}()

	dsn, err := pgCtr.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: 取得共用 postgres 連線字串失敗: %v\n", err)
		return 1
	}
	sharedPostgresDSN = dsn

	if err := migrateSharedPostgres(startCtx, dsn); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: 對共用 postgres 測試容器套用 migration 失敗: %v\n", err)
		return 1
	}

	redisCtr, err := tcredis.Run(startCtx, "redis:7-alpine")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: 啟動共用 redis 測試容器失敗: %v\n", err)
		return 1
	}
	defer func() {
		if err := redisCtr.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "TestMain: 停止共用 redis 測試容器失敗: %v\n", err)
		}
	}()

	host, err := redisCtr.Host(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: 取得共用 redis host 失敗: %v\n", err)
		return 1
	}
	mappedPort, err := redisCtr.MappedPort(context.Background(), "6379/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: 取得共用 redis port 失敗: %v\n", err)
		return 1
	}
	sharedRedisAddr = fmt.Sprintf("%s:%s", host, mappedPort.Port())

	return m.Run()
}

// migrateSharedPostgres 對 dsn 開一個獨立的 *bun.DB 連線並套用一次
// migrate.Up，讓共用 Postgres 容器在所有測試開始前就有 properties 資料表。
// 這個連線只在套用 migration 期間短暫使用，用完即關閉，不會被存進共用狀態。
func migrateSharedPostgres(ctx context.Context, dsn string) error {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	defer sqldb.Close()

	bunDB := bun.NewDB(sqldb, pgdialect.New())
	defer bunDB.Close()

	return migrate.Up(ctx, bunDB)
}
