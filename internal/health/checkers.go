package health

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
)

// checkTimeout 是每一次探測相依服務的逾時上限，避免斷線的相依把
// /healthz 請求吊死。
const checkTimeout = 2 * time.Second

// postgresChecker 是對 Postgres 做健康檢查的 Checker 實作。
type postgresChecker struct {
	db *bun.DB
}

// NewPostgresChecker 建立一個對 db 做 ping 的 Checker，Name() 固定回傳 "postgres"。
func NewPostgresChecker(db *bun.DB) Checker {
	return &postgresChecker{db: db}
}

func (c *postgresChecker) Name() string { return "postgres" }

func (c *postgresChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return c.db.PingContext(ctx)
}

// redisChecker 是對 Redis 做健康檢查的 Checker 實作。
type redisChecker struct {
	client *redis.Client
}

// NewRedisChecker 建立一個對 client 做 PING 的 Checker，Name() 固定回傳 "redis"。
func NewRedisChecker(client *redis.Client) Checker {
	return &redisChecker{client: client}
}

func (c *redisChecker) Name() string { return "redis" }

func (c *redisChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return c.client.Ping(ctx).Err()
}
