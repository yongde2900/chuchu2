// 本檔對應 PLAN-002 Task 2 的 BDD scenarios：dbmigrate 的 init／migrate／
// status 子指令，以及錯誤用法的拒絕行為。up／down 子指令已隨舊的手寫
// migration 機制一起廢除，不再保留任何往返測試。
//
// init／migrate／status 的三個「乾淨資料庫」情境各自起一個專屬、用完即丟的
// Postgres 容器（testsupport.StartPostgres），不可用 TestMain 的共用容器
// ——本檔會反覆 init／migrate 同一個資料庫，若跑在共用容器上會與同 package
// 中其他依賴 properties 資料表既有狀態的測試互相干擾。
//
// 錯誤用法的 Outline 五個案例都在連線 Postgres 之前就被拒絕，因此完全不需要
// 資料庫，一律以 runDBMigrate(t, "", ...) 呼叫，不起容器。
package test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// dbmigrateRunTimeout 涵蓋 `go run` 第一次要編譯 dbmigrate 的時間，
// 給足夠寬鬆的上限。
const dbmigrateRunTimeout = 2 * time.Minute

// runDBMigrate 以 `go run ./cmd/dbmigrate` 執行 dbmigrate，工作目錄設為
// repo 根目錄（否則 config.Load 找不到 config/<name>.yaml）。dsn 非空時以
// CHUCHU_POSTGRES_DSN 覆寫設定檔中的 postgres.dsn，指向 dsn 這個測試容器；
// dsn 為空字串時完全不注入，用於不需要資料庫的錯誤用法案例。
//
// 不論成敗都回傳 exit code 與 stdout/stderr，由呼叫端自行斷言
// ——Task 3、Task 4 會直接沿用這個 helper，不要各寫一份。
//
// 一次性 CLI 用完即結束，不需要像常駐服務那樣以 process group 收屍
// （見 knowledge/gotchas/go-run-orphan-process-group.md 的「例外」一節），
// 用 exec.CommandContext + cmd.Run() 等它結束即可。
func runDBMigrate(t *testing.T, dsn string, args ...string) (exitCode int, stdout, stderr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), dbmigrateRunTimeout)
	defer cancel()

	cmdArgs := append([]string{"run", "./cmd/dbmigrate"}, args...)
	cmd := exec.CommandContext(ctx, "go", cmdArgs...)
	cmd.Dir = testsupport.RepoRoot(t)
	cmd.Env = os.Environ()
	if dsn != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("CHUCHU_POSTGRES_DSN=%s", dsn))
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if runErr == nil {
		return 0, stdout, stderr
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return exitErr.ExitCode(), stdout, stderr
	}

	t.Fatalf("執行 `go run ./cmd/dbmigrate %v` 失敗（非 exit code 錯誤）: %v\nstdout:\n%s\nstderr:\n%s",
		args, runErr, stdout, stderr)
	return -1, stdout, stderr
}

// openDBMigrateTestDB 直接連線 dsn，供測試自行檢查資料庫狀態
// （不透過 dbmigrate 本身，避免用被測物件驗證自己）。
func openDBMigrateTestDB(t *testing.T, dsn string) *bun.DB {
	t.Helper()

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	bunDB := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { bunDB.Close() })

	return bunDB
}

// tableExists 用 to_regclass 檢查資料表是否存在，不用「SELECT 看有沒有報錯」
// 這種脆弱作法。
func tableExists(t *testing.T, bunDB *bun.DB, table string) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var exists bool
	err := bunDB.QueryRowContext(ctx,
		"SELECT to_regclass('public.'||?) IS NOT NULL", table).Scan(&exists)
	if err != nil {
		t.Fatalf("查詢資料表 %s 是否存在失敗: %v", table, err)
	}
	return exists
}

// bunMigrationsNameCount 回傳 bun_migrations 中 name 為 name 的紀錄筆數。
func bunMigrationsNameCount(t *testing.T, bunDB *bun.DB, name string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	err := bunDB.QueryRowContext(ctx,
		"SELECT count(*) FROM bun_migrations WHERE name = ?", name).Scan(&count)
	if err != nil {
		t.Fatalf("查詢 bun_migrations 中 name = %q 的紀錄筆數失敗: %v", name, err)
	}
	return count
}

// createPropertiesMigrationName 是 db/ 底下唯一一個 migration 的版本號
// （檔名時間戳），對應 db/20260819120000_create_properties.tx.up.sql。
const createPropertiesMigrationName = "20260819120000"

func TestDBMigrate_Init_CreatesBunMigrationTables(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	bunDB := openDBMigrateTestDB(t, dsn)
	if !tableExists(t, bunDB, "bun_migrations") {
		t.Fatal("執行 `dbmigrate init` 後 bun_migrations 資料表應存在，實際不存在")
	}
	if !tableExists(t, bunDB, "bun_migration_locks") {
		t.Fatal("執行 `dbmigrate init` 後 bun_migration_locks 資料表應存在，實際不存在")
	}
}

func TestDBMigrate_Migrate_AppliesAllPendingMigrations(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	bunDB := openDBMigrateTestDB(t, dsn)
	if !tableExists(t, bunDB, "properties") {
		t.Fatal("執行 `dbmigrate migrate` 後 properties 資料表應存在，實際不存在")
	}
	if count := bunMigrationsNameCount(t, bunDB, createPropertiesMigrationName); count != 1 {
		t.Fatalf("bun_migrations 中 name = %q 的紀錄筆數 = %d，want 1", createPropertiesMigrationName, count)
	}
}

func TestDBMigrate_Migrate_RerunIsNoopAndDoesNotDuplicate(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	exitCode, stdout, stderr := runDBMigrate(t, dsn, "migrate", "--config=test")
	if exitCode != 0 {
		t.Fatalf("重複執行 `dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("no new migrations")) {
		t.Fatalf("重複執行 `dbmigrate migrate` 的 stdout 應包含 \"no new migrations\"，實際:\n%s", stdout)
	}

	bunDB := openDBMigrateTestDB(t, dsn)
	if count := bunMigrationsNameCount(t, bunDB, createPropertiesMigrationName); count != 1 {
		t.Fatalf("重複執行後 bun_migrations 中 name = %q 的紀錄筆數 = %d，want 仍為 1", createPropertiesMigrationName, count)
	}
}

func TestDBMigrate_Status_ListsAppliedAndPendingMigrations(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	exitCode, stdout, stderr := runDBMigrate(t, dsn, "status", "--config=test")
	if exitCode != 0 {
		t.Fatalf("`dbmigrate status` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("create_properties")) {
		t.Fatalf("`dbmigrate status` 的 stdout 應包含 \"create_properties\"，實際:\n%s", stdout)
	}
}

// expectedPropertiesColumns 是換掉手寫 migration 機制之前，properties 資料表
// 的每一欄型別與可空性，直接抄自 db/20260819120000_create_properties.tx.up.sql
// 的 DDL（該檔內容本身在本 task 完全未變更，只是重新命名並加上 .tx.）。
// data_type 用詞刻意用 information_schema.columns 回報的用詞，而不是 DDL
// 用詞：timestamptz -> "timestamp with time zone"、INT -> "integer"、
// NUMERIC(n,m) -> "numeric"、UUID -> "uuid"、TEXT -> "text"。
var expectedPropertiesColumns = map[string]struct {
	dataType   string
	isNullable string
}{
	"id":             {"uuid", "NO"},
	"city":           {"text", "NO"},
	"district":       {"text", "NO"},
	"street_address": {"text", "NO"},
	"floor":          {"text", "NO"},
	"room_no":        {"text", "NO"},
	"layout":         {"text", "NO"},
	"area_ping":      {"numeric", "NO"},
	"monthly_rent":   {"numeric", "NO"},
	"management_fee": {"numeric", "NO"},
	"deposit_months": {"integer", "NO"},
	"rental_mode":    {"text", "NO"},
	"status":         {"text", "NO"},
	"landlord_name":  {"text", "NO"},
	"landlord_phone": {"text", "NO"},
	"created_at":     {"timestamp with time zone", "NO"},
	"updated_at":     {"timestamp with time zone", "NO"},
}

// TestDBMigrate_PropertiesTableSchema_MatchesPreviousMechanism 對應「換掉
// migration 機制之後 properties 資料表的結構完全相同」：逐欄比對型別與
// 可空性，並確認 (city, district, street_address, floor, room_no) 上仍有
// 唯一索引 properties_address_key。
func TestDBMigrate_PropertiesTableSchema_MatchesPreviousMechanism(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	if exitCode, _, stderr := runDBMigrate(t, dsn, "init", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate init` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}
	if exitCode, _, stderr := runDBMigrate(t, dsn, "migrate", "--config=test"); exitCode != 0 {
		t.Fatalf("前置 `dbmigrate migrate` exit code = %d，want 0\nstderr:\n%s", exitCode, stderr)
	}

	bunDB := openDBMigrateTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := bunDB.QueryContext(ctx,
		"SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'properties'")
	if err != nil {
		t.Fatalf("查詢 properties 的 information_schema.columns 失敗: %v", err)
	}
	defer rows.Close()

	actual := make(map[string]struct{ dataType, isNullable string })
	for rows.Next() {
		var colName, dataType, isNullable string
		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			t.Fatalf("讀取 information_schema.columns 資料列失敗: %v", err)
		}
		actual[colName] = struct{ dataType, isNullable string }{dataType, isNullable}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("讀取 information_schema.columns 失敗: %v", err)
	}

	for col, want := range expectedPropertiesColumns {
		got, ok := actual[col]
		if !ok {
			t.Errorf("欄位 %q 不存在於 properties 資料表", col)
			continue
		}
		if got.dataType != want.dataType {
			t.Errorf("欄位 %q 的 data_type = %q，want %q", col, got.dataType, want.dataType)
		}
		if got.isNullable != want.isNullable {
			t.Errorf("欄位 %q 的 is_nullable = %q，want %q", col, got.isNullable, want.isNullable)
		}
	}
	for col := range actual {
		if _, ok := expectedPropertiesColumns[col]; !ok {
			t.Errorf("properties 資料表出現未預期的額外欄位 %q", col)
		}
	}

	var indexDef string
	err = bunDB.QueryRowContext(ctx,
		"SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'properties' AND indexname = 'properties_address_key'").
		Scan(&indexDef)
	if err != nil {
		t.Fatalf("查詢唯一索引 properties_address_key 失敗（可能不存在）: %v", err)
	}
	if !strings.Contains(indexDef, "UNIQUE") {
		t.Errorf("properties_address_key 的定義應該是唯一索引，實際: %s", indexDef)
	}
	for _, col := range []string{"city", "district", "street_address", "floor", "room_no"} {
		if !strings.Contains(indexDef, col) {
			t.Errorf("properties_address_key 的定義應涵蓋欄位 %q，實際: %s", col, indexDef)
		}
	}
}

// TestDBMigrate_InvalidUsage_RejectsWithNonZeroExitCode 對應「錯誤的用法會以
// 非零 exit code 拒絕並說明原因」的 Scenario Outline，五個案例都不需要資料庫，
// 一律以 dsn = "" 呼叫，全部在連線 Postgres 之前就被拒絕。
func TestDBMigrate_InvalidUsage_RejectsWithNonZeroExitCode(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "完全不給參數",
			args:       nil,
			wantStderr: "用法",
		},
		{
			name:       "migrate 不給 --config",
			args:       []string{"migrate"},
			wantStderr: "config",
		},
		{
			name:       "未知子指令 frobnicate",
			args:       []string{"frobnicate", "--config=test"},
			wantStderr: "frobnicate",
		},
		{
			name:       "up 已廢除",
			args:       []string{"up", "--config=test"},
			wantStderr: "up",
		},
		{
			name:       "down 已廢除",
			args:       []string{"down", "--config=test"},
			wantStderr: "down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exitCode, _, stderr := runDBMigrate(t, "", tt.args...)
			if exitCode == 0 {
				t.Fatalf("exit code = 0，want 非零\nstderr:\n%s", stderr)
			}
			if !bytes.Contains([]byte(stderr), []byte(tt.wantStderr)) {
				t.Fatalf("stderr 應包含 %q，實際:\n%s", tt.wantStderr, stderr)
			}
		})
	}
}
