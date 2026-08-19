package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/uptrace/bun"

	"github.com/yongde2900/chuchu2/db"
)

// createSchemaMigrationsSQL 建立記錄「已套用哪些 migration 版本」的資料表。
// 這張表由 migrate 套件自己管理，不透過 db/ 底下的 migration 檔案建立
// ——它是套用其他 migration 前必須先存在的基礎設施。
const createSchemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Up 依檔名時間戳由舊到新，套用 db.FS 底下所有尚未套用的 *.up.sql，
// 並在 schema_migrations 記錄版本。已套用過的版本會被跳過（可重入）。
func Up(ctx context.Context, bunDB *bun.DB) error {
	migrations, err := loadMigrations(ctx, bunDB)
	if err != nil {
		return err
	}

	applied, err := appliedSet(ctx, bunDB)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if applied[m.version] {
			continue
		}
		if err := applyMigration(ctx, bunDB, m, m.upFile, true); err != nil {
			return fmt.Errorf("套用 migration %s 失敗: %w", m.version, err)
		}
	}

	return nil
}

// Down 依檔名時間戳由新到舊，套用已套用版本對應的 *.down.sql，並從
// schema_migrations 移除該版本。未套用過的版本會被跳過（可重入）。
func Down(ctx context.Context, bunDB *bun.DB) error {
	migrations, err := loadMigrations(ctx, bunDB)
	if err != nil {
		return err
	}

	applied, err := appliedSet(ctx, bunDB)
	if err != nil {
		return err
	}

	for i := len(migrations) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		m := migrations[i]
		if !applied[m.version] {
			continue
		}
		if err := applyMigration(ctx, bunDB, m, m.downFile, false); err != nil {
			return fmt.Errorf("回滾 migration %s 失敗: %w", m.version, err)
		}
	}

	return nil
}

// Applied 回傳目前已套用的 migration 版本，依 version（即檔名時間戳）由舊到新排序。
func Applied(ctx context.Context, bunDB *bun.DB) ([]string, error) {
	if err := ensureSchemaMigrationsTable(ctx, bunDB); err != nil {
		return nil, err
	}

	rows, err := bunDB.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return nil, fmt.Errorf("查詢 schema_migrations 失敗: %w", err)
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("讀取 schema_migrations 資料列失敗: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("讀取 schema_migrations 失敗: %w", err)
	}

	return versions, nil
}

// ensureSchemaMigrationsTable 確保 schema_migrations 資料表存在；可重入。
func ensureSchemaMigrationsTable(ctx context.Context, bunDB *bun.DB) error {
	if _, err := bunDB.ExecContext(ctx, createSchemaMigrationsSQL); err != nil {
		return fmt.Errorf("建立 schema_migrations 資料表失敗: %w", err)
	}
	return nil
}

// loadMigrations 確保 schema_migrations 存在後，讀取 db.FS 底下所有 .sql
// 檔名並解析成依 version 由舊到新排序的 migration 清單。
func loadMigrations(ctx context.Context, bunDB *bun.DB) ([]migration, error) {
	if err := ensureSchemaMigrationsTable(ctx, bunDB); err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(db.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("讀取 migration 目錄失敗: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}

	migrations, err := parseMigrations(names)
	if err != nil {
		return nil, fmt.Errorf("解析 migration 檔名失敗: %w", err)
	}

	return migrations, nil
}

// appliedSet 回傳已套用版本的集合，供 Up/Down 判斷是否要跳過。
func appliedSet(ctx context.Context, bunDB *bun.DB) (map[string]bool, error) {
	versions, err := Applied(ctx, bunDB)
	if err != nil {
		return nil, err
	}

	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		set[v] = true
	}
	return set, nil
}

// applyMigration 在單一交易內執行 file 的 SQL 內容，並依 isUp 更新
// schema_migrations（up 寫入版本、down 刪除版本），確保 SQL 執行與版本紀錄
// 一起成功或一起回滾。
func applyMigration(ctx context.Context, bunDB *bun.DB, m migration, file string, isUp bool) error {
	sqlBytes, err := db.FS.ReadFile(file)
	if err != nil {
		return fmt.Errorf("讀取 migration 檔案 %s 失敗: %w", file, err)
	}

	return bunDB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("執行 %s 失敗: %w", file, err)
		}

		if isUp {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
				return fmt.Errorf("記錄 schema_migrations 版本 %s 失敗: %w", m.version, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				"DELETE FROM schema_migrations WHERE version = ?", m.version); err != nil {
				return fmt.Errorf("移除 schema_migrations 版本 %s 失敗: %w", m.version, err)
			}
		}

		return nil
	})
}
