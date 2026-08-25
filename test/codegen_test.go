// 本檔對應 BDD scenario（@codegen @integration）：
// 「HTTP 層由 api/openapi.yaml 產生，spec 與程式碼不得分岔」規則下的
// 「重新產生程式碼不會產生任何差異」。
//
// 做法：讀下目前已提交的 api/api.gen.go → 在 repo 根目錄重新執行專案的
// 產生指令（go generate ./api/...）→ 再讀一次 → 逐位元組比對兩次內容。
// 測試結束前一律把檔案還原成執行前的內容，避免產生器版本漂移（例如
// oapi-codegen 未來版本更新格式化規則）汙染工作目錄、影響其他測試或
// git 狀態。
package test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// codegenTimeout 給足夠寬鬆的時間：第一次執行時 `go run` 需要下載
// oapi-codegen 這個模組本身（含其相依），在沒有本地快取的機器上可能要
// 數十秒。
const codegenTimeout = 180 * time.Second

// TestCodegen_RegeneratingProducesNoDiff 對應 scenario：
// 「Given api/openapi.yaml 與已提交的 api/api.gen.go 都在版控中，
// When 重新執行專案的程式碼產生指令，
// Then api/api.gen.go 的內容與執行前逐位元組相同」。
func TestCodegen_RegeneratingProducesNoDiff(t *testing.T) {
	repoRoot := testsupport.RepoRoot(t)
	genFile := filepath.Join(repoRoot, "api", "api.gen.go")

	before, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("讀取執行前的 %s 失敗: %v", genFile, err)
	}

	// 不論產生指令是否真的改動了檔案，測試結束前都把檔案還原成執行前的
	// 內容，避免產生器版本漂移（例如 oapi-codegen 未來版本更新了輸出的
	// 格式化規則）汙染工作目錄、影響後續的 git 狀態或其他測試。
	t.Cleanup(func() {
		if err := os.WriteFile(genFile, before, 0o644); err != nil {
			t.Errorf("還原 %s 失敗: %v", genFile, err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), codegenTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "generate", "./api/...")
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("執行 `go generate ./api/...` 失敗: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	after, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("讀取執行後的 %s 失敗: %v", genFile, err)
	}

	assertIdenticalBytes(t, genFile, before, after)
}

// assertIdenticalBytes 逐位元組比對 before 與 after，不相同時指出第一個
// 相異的位元組位置，並附上該位置前後幾個位元組的內容方便定位。
func assertIdenticalBytes(t *testing.T, path string, before, after []byte) {
	t.Helper()

	if bytes.Equal(before, after) {
		return
	}

	if len(before) != len(after) {
		t.Errorf("重新產生 %s 之後長度不同: 執行前 %d bytes, 執行後 %d bytes", path, len(before), len(after))
	}

	minLen := len(before)
	if len(after) < minLen {
		minLen = len(after)
	}

	for i := 0; i < minLen; i++ {
		if before[i] != after[i] {
			t.Fatalf(
				"重新產生 %s 之後內容不同，第一個相異位元組在 offset %d: 執行前=%q, 執行後=%q\n執行前附近內容: %q\n執行後附近內容: %q",
				path, i, before[i], after[i],
				contextAround(before, i),
				contextAround(after, i),
			)
		}
	}

	t.Fatalf("重新產生 %s 之後內容不同，其中一份內容是另一份的前綴（第一個相異位元組在 offset %d，即較短內容的結尾）", path, minLen)
}

// contextAround 回傳 b 中 offset 前後各 20 bytes 的內容，供失敗訊息定位用。
func contextAround(b []byte, offset int) []byte {
	start := offset - 20
	if start < 0 {
		start = 0
	}
	end := offset + 20
	if end > len(b) {
		end = len(b)
	}
	return b[start:end]
}
