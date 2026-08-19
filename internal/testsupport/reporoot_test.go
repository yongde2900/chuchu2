package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRoot_PointsAtModuleRoot(t *testing.T) {
	root := RepoRoot(t)

	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		t.Fatalf("RepoRoot() = %q, expected go.mod to exist there: %v", root, err)
	}
}
