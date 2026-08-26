package line

import (
	"go/build"
	"strings"
	"testing"
)

// 這個測試守護 internal/line 的分層邊界：領域層不得認得持久化框架、
// HTTP 標準庫或 LINE SDK，這些屬於尚未存在的 pgrepo／webhookhttp 子套件。
// 用 go/build 讀 import 清單而非執行期反射，因為這是編譯前就能檢查的靜態事實。
func TestLayering_NoForbiddenImports(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("build.ImportDir(\".\") failed: %v", err)
	}

	var violations []string
	for _, imp := range pkg.Imports {
		switch {
		case imp == "github.com/uptrace/bun":
			violations = append(violations, imp)
		case imp == "net/http":
			violations = append(violations, imp)
		case strings.HasPrefix(imp, "github.com/line/line-bot-sdk-go"):
			violations = append(violations, imp)
		}
	}

	if len(violations) > 0 {
		t.Fatalf("internal/line 出現不該有的 import：%v（完整清單：%v）", violations, pkg.Imports)
	}
}
