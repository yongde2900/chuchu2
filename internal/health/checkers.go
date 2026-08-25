package health

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
)

// 逾時上限，避免斷線的相依把 /healthz 請求吊死。
const checkTimeout = 2 * time.Second

type postgresChecker struct {
	db *bun.DB
}

// Name() 固定回傳 "postgres"，那會成為 checks 物件中的 key。
func NewPostgresChecker(db *bun.DB) Checker {
	return &postgresChecker{db: db}
}

func (c *postgresChecker) Name() string { return "postgres" }

func (c *postgresChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return c.db.PingContext(ctx)
}

type redisChecker struct {
	client *redis.Client
}

// Name() 固定回傳 "redis"，那會成為 checks 物件中的 key。
func NewRedisChecker(client *redis.Client) Checker {
	return &redisChecker{client: client}
}

func (c *redisChecker) Name() string { return "redis" }

func (c *redisChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return c.client.Ping(ctx).Err()
}
