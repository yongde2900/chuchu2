package testsupport

import (
	"context"
	"sync"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// 涵蓋冷機器第一次拉 image 的時間，刻意寬鬆。
const containerStartTimeout = 5 * time.Minute

// 斷線情境需要它盡快、確定地完成。
const containerStopTimeout = 30 * time.Second

// StartPostgres 啟一個用完即丟的 Postgres 容器。
//
// stop 用 sync.Once 包住，重複呼叫安全：斷線情境會先手動呼叫，
// 之後 t.Cleanup 的收尾又會再呼叫一次。
func StartPostgres(t *testing.T) (dsn string, stop func()) {
	t.Helper()

	startCtx, cancel := context.WithTimeout(context.Background(), containerStartTimeout)
	defer cancel()

	ctr, err := tcpostgres.Run(startCtx, "postgres:16-alpine",
		tcpostgres.WithDatabase("chuchu"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("啟動 postgres 測試容器失敗: %v", err)
	}

	connStr, err := ctr.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		t.Fatalf("取得 postgres 測試容器連線字串失敗: %v", err)
	}

	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), containerStopTimeout)
			defer stopCancel()
			if err := ctr.Terminate(stopCtx); err != nil {
				t.Logf("停止 postgres 測試容器失敗: %v", err)
			}
		})
	}
	t.Cleanup(stopFn)

	return connStr, stopFn
}

// StartRedis 啟一個用完即丟的 Redis 容器。stop 可重複呼叫，理由同 StartPostgres。
func StartRedis(t *testing.T) (addr string, stop func()) {
	t.Helper()

	startCtx, cancel := context.WithTimeout(context.Background(), containerStartTimeout)
	defer cancel()

	ctr, err := tcredis.Run(startCtx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("啟動 redis 測試容器失敗: %v", err)
	}

	host, err := ctr.Host(context.Background())
	if err != nil {
		t.Fatalf("取得 redis 測試容器 host 失敗: %v", err)
	}
	mappedPort, err := ctr.MappedPort(context.Background(), "6379/tcp")
	if err != nil {
		t.Fatalf("取得 redis 測試容器對應 port 失敗: %v", err)
	}

	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), containerStopTimeout)
			defer stopCancel()
			if err := ctr.Terminate(stopCtx); err != nil {
				t.Logf("停止 redis 測試容器失敗: %v", err)
			}
		})
	}
	t.Cleanup(stopFn)

	return host + ":" + mappedPort.Port(), stopFn
}
