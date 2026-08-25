// Package testsupport 提供整合測試共用的輔助函式。
package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

// RepoRoot 以 go.mod 所在位置為準，讓整合測試能以 repo 根目錄當工作目錄。
func RepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取得目前工作目錄失敗: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("往上找不到含有 go.mod 的 repo 根目錄（從 %s 開始）", dir)
		}
		dir = parent
	}
}
