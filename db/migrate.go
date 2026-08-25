package db

import (
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// WithMigrationsDirectory("db") 是 CreateTxSQLMigrations 產生新檔案的落點，
// 不設的話會寫到呼叫端原始碼所在的目錄。
// 注意 MigrationsOption 只能給 NewMigrations，不能給 NewMigrator。
func Migrations() (*migrate.Migrations, error) {
	migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory("db"))
	if err := migrations.Discover(FS); err != nil {
		return nil, fmt.Errorf("探索 db/ 底下的 migration 檔案失敗: %w", err)
	}
	return migrations, nil
}

// NewMigrator 是唯一的 Migrator 建構點，CLI 與測試共用，避免兩邊設定分岔。
//
// WithMarkAppliedOnSuccess(true) 不可省略：bun 預設在執行 migration SQL
// 「之前」就標記為已套用，SQL 失敗時 bun_migrations 會留下假記錄。
// 注意 MigratorOption 只能給 NewMigrator，不能給 NewMigrations。
func NewMigrator(bunDB *bun.DB) (*migrate.Migrator, error) {
	migrations, err := Migrations()
	if err != nil {
		return nil, err
	}
	return migrate.NewMigrator(bunDB, migrations, migrate.WithMarkAppliedOnSuccess(true)), nil
}
