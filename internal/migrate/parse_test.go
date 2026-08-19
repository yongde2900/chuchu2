package migrate

import "testing"

// TestParseMigrations_OrdersByTimestampAscending 驗證 parseMigrations 回傳的
// 順序依檔名時間戳由舊到新排序，且能正確配對 up/down 檔案。
func TestParseMigrations_OrdersByTimestampAscending(t *testing.T) {
	names := []string{
		"20260819120000_create_properties.down.sql",
		"20260101000000_create_foo.up.sql",
		"20260819120000_create_properties.up.sql",
		"20260101000000_create_foo.down.sql",
		"20260601000000_create_bar.up.sql",
		"20260601000000_create_bar.down.sql",
	}

	got, err := parseMigrations(names)
	if err != nil {
		t.Fatalf("parseMigrations() error = %v, want nil", err)
	}

	wantOrder := []string{
		"20260101000000_create_foo",
		"20260601000000_create_bar",
		"20260819120000_create_properties",
	}

	if len(got) != len(wantOrder) {
		t.Fatalf("len(got) = %d, want %d (%+v)", len(got), len(wantOrder), got)
	}
	for i, wantVersion := range wantOrder {
		if got[i].version != wantVersion {
			t.Fatalf("got[%d].version = %q, want %q", i, got[i].version, wantVersion)
		}
	}

	first := got[0]
	if first.upFile != "20260101000000_create_foo.up.sql" {
		t.Errorf("got[0].upFile = %q, want %q", first.upFile, "20260101000000_create_foo.up.sql")
	}
	if first.downFile != "20260101000000_create_foo.down.sql" {
		t.Errorf("got[0].downFile = %q, want %q", first.downFile, "20260101000000_create_foo.down.sql")
	}
}

// TestParseMigrations_Empty 驗證空輸入回傳空結果而非 error。
func TestParseMigrations_Empty(t *testing.T) {
	got, err := parseMigrations(nil)
	if err != nil {
		t.Fatalf("parseMigrations(nil) error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

// TestParseMigrations_MissingDownFile_ReturnsError 驗證只有 up 沒有對應 down
// 時回傳 error，而不是靜靜地產生一個沒辦法回滾的 migration。
func TestParseMigrations_MissingDownFile_ReturnsError(t *testing.T) {
	names := []string{"20260819120000_create_properties.up.sql"}

	if _, err := parseMigrations(names); err == nil {
		t.Fatal("parseMigrations() error = nil, want non-nil（缺少 down 檔案）")
	}
}

// TestParseMigrations_MissingUpFile_ReturnsError 驗證只有 down 沒有對應 up
// 時回傳 error。
func TestParseMigrations_MissingUpFile_ReturnsError(t *testing.T) {
	names := []string{"20260819120000_create_properties.down.sql"}

	if _, err := parseMigrations(names); err == nil {
		t.Fatal("parseMigrations() error = nil, want non-nil（缺少 up 檔案）")
	}
}

// TestParseMigrations_MalformedFilename_ReturnsError 驗證不符合
// "<timestamp>_<name>.<up|down>.sql" 格式的檔名會被拒絕，而不是被靜靜忽略。
func TestParseMigrations_MalformedFilename_ReturnsError(t *testing.T) {
	cases := []string{
		"create_properties.up.sql",     // 缺少時間戳前綴
		"20260819120000_create.sql",    // 缺少 up/down 標記
		"20260819120000_create.mid.sql", // up/down 標記錯誤
	}

	for _, name := range cases {
		if _, err := parseMigrations([]string{name}); err == nil {
			t.Errorf("parseMigrations([%q]) error = nil, want non-nil", name)
		}
	}
}

// TestParseMigrations_DuplicateUpFile_ReturnsError 驗證同一版本出現兩個 up
// 檔案時回傳 error，避免套用順序不確定。
func TestParseMigrations_DuplicateUpFile_ReturnsError(t *testing.T) {
	names := []string{
		"20260819120000_create_properties.up.sql",
		"20260819120000_create_properties.down.sql",
	}
	// 模擬重複：同 version、同 direction 但不同大小寫也視為不同檔名，
	// 這裡直接重複同一個檔名代表「同 version 出現第二個 up」。
	names = append(names, "20260819120000_create_properties.up.sql")

	if _, err := parseMigrations(names); err == nil {
		t.Fatal("parseMigrations() error = nil, want non-nil（重複的 up 檔案）")
	}
}
