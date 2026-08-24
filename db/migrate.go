package db

import (
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// Migrations 探索 FS（也就是 db/ 目錄本身）底下所有 migration 檔案，回傳
// bun 用來套用／回滾的 *migrate.Migrations。
//
// 必須以 migrate.WithMigrationsDirectory("db") 建立：這個目錄設定是
// Migrator.CreateTxSQLMigrations（create_sql 子指令）產生新檔案時的落點，
// 不設定的話新檔案會寫到呼叫端原始碼所在的目錄，而不是 db/。
// MigrationsOption 只能傳給 NewMigrations，不能傳給 NewMigrator。
func Migrations() (*migrate.Migrations, error) {
	migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory("db"))
	if err := migrations.Discover(FS); err != nil {
		return nil, fmt.Errorf("探索 db/ 底下的 migration 檔案失敗: %w", err)
	}
	return migrations, nil
}

// NewMigrator 是 cmd/dbmigrate 與 test/main_test.go 共用的唯一
// *migrate.Migrator 建構點，確保兩邊拿到的 Migrator 設定完全一致。
//
// 必須傳入 migrate.WithMarkAppliedOnSuccess(true)：bun 的 Migrator.Migrate
// 預設會在執行 migration 的 SQL「之前」就先把它標記為已套用，一旦 SQL
// 執行失敗，bun_migrations 會留下一筆「已套用」的假記錄。
// MigratorOption 只能傳給 NewMigrator，不能傳給 NewMigrations。
func NewMigrator(bunDB *bun.DB) (*migrate.Migrator, error) {
	migrations, err := Migrations()
	if err != nil {
		return nil, err
	}
	return migrate.NewMigrator(bunDB, migrations, migrate.WithMarkAppliedOnSuccess(true)), nil
}
