// Package postgres 負責建立與驗證 Postgres 連線（透過 bun ORM + pgdriver）。
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/yongde2900/chuchu2/internal/config"
)

// Open 依 cfg 建立一個 *bun.DB。這個階段只建立 connection pool，
// 不會主動連線；是否連得上要用 Ping 驗證。
func Open(ctx context.Context, cfg config.PostgresConfig) (*bun.DB, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(cfg.DSN)))

	if cfg.MaxOpenConns > 0 {
		sqldb.SetMaxOpenConns(cfg.MaxOpenConns)
	}

	db := bun.NewDB(sqldb, pgdialect.New())
	return db, nil
}

// Ping 對 db 發出一次連線驗證，err 為 nil 代表連線正常。
func Ping(ctx context.Context, db *bun.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres ping 失敗: %w", err)
	}
	return nil
}
