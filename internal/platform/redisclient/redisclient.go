// Package redisclient 負責建立 Redis 連線用的 client。
package redisclient

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/yongde2900/chuchu2/internal/config"
)

// 只建立 client，不主動連線——是否連得上要另外呼叫 Ping 驗證。
func Open(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return client, nil
}
