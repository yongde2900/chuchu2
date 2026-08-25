// dbmigrate 的進階行為：rollback 的 group 語意、migration 的交易性、
// create_sql、unlock。
//
// 每個 scenario 各起一個專屬、用完即丟的 Postgres 容器（create_sql 除外，
// 它不連資料庫）——**不可用 TestMain 的共用容器**。一律不 t.Parallel()：
// 臨時 migration 檔案在磁碟上的那段期間是全域可見的，並行會互相污染 db.FS。
//
// 輔助函式沿用 test/migrate_test.go，不重寫一份。
package test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/yongde2900/chuchu2/db"
	"github.com/yongde2900/chuchu2/internal/testsupport"
)

func migrationDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(testsupport.RepoRoot(t), "db")
}

// 把臨時 migration 寫進 db/，讓接下來的 `go run` 在重新編譯時嵌進去
// （已經跑起來的測試 binary 自己的 db.FS 不受影響，所以共用容器不會被波及）。
//
// 清理必須在寫檔**之前**註冊，測試中途失敗才一定刪得掉。
func writeTempMigration(t *testing.T, version, name, upSQL, downSQL string) {
	t.Helper()

	dir := migrationDir(t)
	upPath := filepath.Join(dir, version+"_"+name+".tx.up.sql")
	downPath := filepath.Join(dir, version+"_"+name+".tx.down.sql")

	t.Cleanup(func() {
		os.Remove(upPath)
		os.Remove(downPath)
	})

	if err := os.WriteFile(upPath, []byte(upSQL), 0o644); err != nil {
		t.Fatalf("寫入臨時 migration 檔案 %s 失敗: %v", upPath, err)
	}
	if err := os.WriteFile(downPath, []byte(downSQL), 0o644); err != nil {
		t.Fatalf("寫入臨時 migration 檔案 %s 失敗: %v", downPath, err)
	}
}

func initAndMigrate(t *testing.T, dsn string) {
	t.Helper()

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
}

// 兩次獨立的 migrate 形成兩個 group，rollback 只該回滾後者。
func TestDBMigrate_Rollback_OnlyRollsBackLastGroup(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	initAndMigrate(t, dsn)

	// 檔案必須留到 rollback 那次 `go run` 之後才刪——rollback 需要嵌入的
	// .tx.down.sql 才知道怎麼回滾。t.Cleanup 天生就是這個時序。
	writeTempMigration(t, "29990101000001", "rollback_probe",
		"CREATE TABLE rollback_probe (id INT PRIMARY KEY);\n",
		"DROP TABLE IF EXISTS rollback_probe;\n",
	)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test"); exitCode != 0 {
		t.Fatalf("套用 rollback_probe 的 `dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	bunDB := openDBMigrateTestDB(t, dsn)
	if !tableExists(t, bunDB, "rollback_probe") {
		t.Fatal("套用 rollback_probe migration 後該資料表應存在，實際不存在")
	}

	exitCode, _, stderr := runDBMigrate(t, dsn, "rollback", "--config=test")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate rollback` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	if tableExists(t, bunDB, "rollback_probe") {
		t.Fatal("`dbmigrate rollback` 後 rollback_probe 資料表應不存在，實際仍存在")
	}
	if !tableExists(t, bunDB, "properties") {
		t.Fatal("`dbmigrate rollback` 後 properties 資料表應仍存在（只回滾最後一個 group），實際不存在")
	}
	if count := bunMigrationsNameCount(t, bunDB, createPropertiesMigrationName); count != 1 {
		t.Fatalf("`dbmigrate rollback` 後 bun_migrations 中 name = %q 的紀錄筆數 = %d，want 仍為 1", createPropertiesMigrationName, count)
	}
}

func TestDBMigrate_Rollback_NothingToRollbackIsNoop(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	exitCode, stdout, stderr := runDBMigrate(t, dsn, "rollback", "--config=test")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate rollback` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("nothing to rollback")) {
		t.Fatalf("`dbmigrate rollback` 的 stdout 應包含 \"nothing to rollback\"，實際:\n%s", stdout)
	}
}

// 刻意用固定值而非 time.Now()：明顯是測試產物，且必定排在真實 migration 之後。
const txProbeVersion = "29990101000002"

// ⚠️ --bun:split 必須自成一行、前後無多餘空白，而且**不能拿掉**：
// 沒有它的話兩個敘述會被當成單一批次送出，Postgres 的 simple query protocol
// 會自動包成隱式交易——於是即使 bun 根本沒開交易，tx_probe 也不會留下來，
// 這個測試就會因為錯誤的理由通過，完全測不到 .tx. 檔名的作用。
const txProbeUpSQL = `CREATE TABLE tx_probe (id INT PRIMARY KEY);
--bun:split
INSERT INTO no_such_table VALUES (1);
`

const txProbeDownSQL = `DROP TABLE IF EXISTS tx_probe;
`

func TestDBMigrate_Migrate_FailureRollsBackWholeMigration(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	initAndMigrate(t, dsn)

	writeTempMigration(t, txProbeVersion, "tx_probe", txProbeUpSQL, txProbeDownSQL)

	exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test")
	if exitCode == 0 {
		t.Fatalf("套用會失敗的 tx_probe migration，`dbmigrate migrate` exit code = 0，want 非零\nstderr:\n%s", stderr)
	}

	bunDB := openDBMigrateTestDB(t, dsn)
	if tableExists(t, bunDB, "tx_probe") {
		t.Fatal("migration 失敗後 tx_probe 資料表應不存在（整個 migration 應在交易內回滾），實際存在")
	}
	if count := bunMigrationsNameCount(t, bunDB, txProbeVersion); count != 0 {
		t.Fatalf("migration 失敗後 bun_migrations 中 name = %q 的紀錄筆數 = %d，want 0（WithMarkAppliedOnSuccess(true) 應阻止假記錄）", txProbeVersion, count)
	}
}

var createSQLTimestampPattern = regexp.MustCompile(`^(\d{14})_add_tenant_table\.tx\.(up|down)\.sql$`)

// create_sql 不連資料庫（postgres.Open 只建連線池），所以不起任何容器。
func TestDBMigrate_CreateSQL_GeneratesPairedBlankMigrationFiles(t *testing.T) {
	dir := migrationDir(t)

	// 清理必須在執行指令**之前**註冊：留下來的樣板檔案會被之後每次
	// go build／go run 嵌入並當成真的 migration 套用，也極可能被誤 commit。
	t.Cleanup(func() {
		matches, _ := filepath.Glob(filepath.Join(dir, "*_add_tenant_table.tx.*.sql"))
		for _, m := range matches {
			os.Remove(m)
		}
	})

	before, err := filepath.Glob(filepath.Join(dir, "*_add_tenant_table.tx.*.sql"))
	if err != nil {
		t.Fatalf("執行前列出 add_tenant_table migration 檔案失敗: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("執行前 db/ 目錄已存在 add_tenant_table 相關檔案，want 不存在: %v", before)
	}

	exitCode, _, stderr := runDBMigrate(t, "", "create_sql", "--config=test", "add_tenant_table")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate create_sql add_tenant_table` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*_add_tenant_table.tx.*.sql"))
	if err != nil {
		t.Fatalf("列出 add_tenant_table migration 檔案失敗: %v", err)
	}

	var upFile, downFile string
	timestamps := map[string]bool{}
	for _, m := range matches {
		base := filepath.Base(m)
		sub := createSQLTimestampPattern.FindStringSubmatch(base)
		if sub == nil {
			t.Fatalf("產生的檔名 %q 不符合預期格式（14 位數字時間戳 + _add_tenant_table.tx.up/down.sql）", base)
		}
		timestamps[sub[1]] = true
		switch sub[2] {
		case "up":
			upFile = base
		case "down":
			downFile = base
		}
	}

	if upFile == "" {
		t.Errorf("應產生一個以 \"_add_tenant_table.tx.up.sql\" 結尾的檔案，實際找到的檔案: %v", matches)
	}
	if downFile == "" {
		t.Errorf("應產生一個以 \"_add_tenant_table.tx.down.sql\" 結尾的檔案，實際找到的檔案: %v", matches)
	}
	if len(timestamps) != 1 {
		t.Errorf("up／down 檔名應共用同一個 14 位數字時間戳前綴，實際看到的時間戳集合: %v", timestamps)
	}
}

// 鎖定必須用 Migrator.Lock 製造，不要手寫 INSERT——bun 的 Unlock 以
// `WHERE table_name = <formattedTableName>` 刪除，手寫的 table_name 很容易
// 對不上，於是 unlock 明明成功卻刪不到東西，失敗方式極難理解。
func TestDBMigrate_Unlock_ClearsStaleMigrationLock(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	defer sqldb.Close()
	bunDB := bun.NewDB(sqldb, pgdialect.New())
	defer bunDB.Close()

	migrator, err := db.NewMigrator(bunDB)
	if err != nil {
		t.Fatalf("建立 migrator 失敗: %v", err)
	}

	lockCtx, lockCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer lockCancel()
	if err := migrator.Lock(lockCtx); err != nil {
		t.Fatalf("製造遺留鎖定（Lock）失敗: %v", err)
	}
	// 刻意不 Unlock，模擬流程異常留下的鎖。

	exitCode, _, stderr := runDBMigrate(t, dsn, "unlock", "--config=test")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate unlock` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	verifyDB := openDBMigrateTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var count int
	if err := verifyDB.QueryRowContext(ctx, "SELECT count(*) FROM bun_migration_locks").Scan(&count); err != nil {
		t.Fatalf("查詢 bun_migration_locks 紀錄筆數失敗: %v", err)
	}
	if count != 0 {
		t.Fatalf("`dbmigrate unlock` 後 bun_migration_locks 的紀錄數 = %d，want 0", count)
	}
}
