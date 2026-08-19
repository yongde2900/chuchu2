// Package test 存放整合測試，一個 feature 面向一個檔。
// 本檔對應 BDD scenario：「設定檔缺少必要欄位時拒絕啟動」與
// 「以有效設定檔啟動服務」。
package test

import (
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// TestStartup_ValidConfig_ServerAcceptsConnections 對應 BDD scenario：
//
//	Given 設定檔 config/test.yaml 存在且填妥 server.port、postgres.dsn、redis.addr
//	When 以 `go run ./cmd/api --config=test` 啟動服務
//	Then 程序持續執行不退出
//	And 對 server.port 發出的 TCP 連線可被接受
//
// Postgres／Redis 的 DSN／addr 用 TestMain 起的共用容器覆寫，確保
// 「填妥且可連線」這個前提在沒有原生 Postgres/Redis 的機器上也成立。
func TestStartup_ValidConfig_ServerAcceptsConnections(t *testing.T) {
	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	addr := strings.TrimPrefix(baseURL, "http://")
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("TCP 連線到 %s 失敗: %v, output:\n%s", addr, err, output())
	}
	conn.Close()

	// 程序持續執行不退出：能再成功發出一次請求即代表服務仍存活，
	// 而不是接受了一次連線後就結束。
	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz 失敗，服務可能已非預期退出: %v, output:\n%s", err, output())
	}
	resp.Body.Close()
}

// TestStartup_MissingRequiredConfigKey 對應 BDD scenario：
//
//	Given 設定檔 config/broken.yaml 存在但沒有 postgres.dsn 這個 key
//	When 以 `go run ./cmd/api --config=broken` 啟動服務
//	Then 程序在 5 秒內退出且 exit code 不為 0
//	And stderr 輸出的訊息中包含字串 "postgres.dsn"
//
// 這個測試不需要 Docker：設定載入失敗發生在連任何外部服務之前。
func TestStartup_MissingRequiredConfigKey(t *testing.T) {
	repoRoot := testsupport.RepoRoot(t)

	cmd := exec.Command("go", "run", "./cmd/api", "--config=broken")
	cmd.Dir = repoRoot

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("process exited with code 0, want non-zero exit code")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("process did not exit normally: %v", err)
		}
		if exitErr.ExitCode() == 0 {
			t.Fatalf("exit code = 0, want non-zero")
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("process did not exit within 5 seconds")
	}

	if got := stderr.String(); !strings.Contains(got, "postgres.dsn") {
		t.Fatalf("stderr = %q, want it to contain %q", got, "postgres.dsn")
	}
}
