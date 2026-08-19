// Package migrate 套用／回滾 db/ 目錄下的 migration SQL 檔案，並以
// schema_migrations 資料表記錄已套用版本。
package migrate

import (
	"fmt"
	"regexp"
	"sort"
)

// migration 代表一組成對的 up/down migration 檔案。
// version 是檔名去掉 ".up.sql"／".down.sql" 副檔名後的部分（例如
// "20260819120000_create_properties"），其時間戳前綴決定套用順序。
type migration struct {
	version  string
	upFile   string
	downFile string
}

// migrationFilePattern 比對 "<timestamp>_<name>.<up|down>.sql" 這種檔名格式。
// timestamp 要求全為數字，藉由固定格式讓字串排序等同時間排序。
var migrationFilePattern = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

// parseMigrations 是純函式：把 db/ 目錄下的檔名（不含路徑，例如透過
// fs.ReadDir 取得）解析成一組依 version 由舊到新排序的 migration。
//
// 每個 version 都必須同時有 up 與 down 兩個檔案，否則視為設定錯誤而回傳 error
// ——一個「up 到一半但沒有對應 down」的 migration 沒辦法安全回滾。
// 呼叫端負責決定方向：Up 依回傳順序（由舊到新）套用；Down 反向（由新到舊）套用。
func parseMigrations(filenames []string) ([]migration, error) {
	byVersion := make(map[string]*migration)
	var order []string

	for _, name := range filenames {
		m := migrationFilePattern.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("migration 檔名格式錯誤: %q（預期格式為 <timestamp>_<name>.<up|down>.sql）", name)
		}

		timestamp, label, direction := m[1], m[2], m[3]
		version := timestamp + "_" + label

		entry, ok := byVersion[version]
		if !ok {
			entry = &migration{version: version}
			byVersion[version] = entry
			order = append(order, version)
		}

		switch direction {
		case "up":
			if entry.upFile != "" {
				return nil, fmt.Errorf("migration %q 有重複的 up 檔案: %q 與 %q", version, entry.upFile, name)
			}
			entry.upFile = name
		case "down":
			if entry.downFile != "" {
				return nil, fmt.Errorf("migration %q 有重複的 down 檔案: %q 與 %q", version, entry.downFile, name)
			}
			entry.downFile = name
		}
	}

	sort.Strings(order)

	migrations := make([]migration, 0, len(order))
	for _, version := range order {
		entry := byVersion[version]
		if entry.upFile == "" {
			return nil, fmt.Errorf("migration %q 缺少 up 檔案（只找到 %q）", version, entry.downFile)
		}
		if entry.downFile == "" {
			return nil, fmt.Errorf("migration %q 缺少 down 檔案（只找到 %q）", version, entry.upFile)
		}
		migrations = append(migrations, *entry)
	}

	return migrations, nil
}
