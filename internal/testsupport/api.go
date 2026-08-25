package testsupport

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"
)

const apiStartPollInterval = 100 * time.Millisecond

// `go run` 第一次要先編譯，冷機器上可能要數秒，給寬鬆的上限。
const apiStartTimeout = 60 * time.Second

// 優雅終止子行程樹的上限，逾時後改用 SIGKILL。
const apiStopTimeout = 10 * time.Second

// 帶鎖的 io.Writer，讓子行程的輸出在 -race 之下也能安全地邊寫邊讀。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// StartAPI 以 `go run ./cmd/api` 啟動服務，env 中的 CHUCHU_ 環境變數會覆寫設定
// （用來注入 testcontainers 的隨機 DSN／addr）。
//
// 自己挑一個空閒 port 覆寫 CHUCHU_SERVER_PORT，避免測試互搶 yaml 裡的固定 port。
// 回傳前會輪詢到 TCP 可連線為止，測試才不會在服務起來之前就送請求。
// stop 可安全重複呼叫。
func StartAPI(t *testing.T, configName string, env map[string]string) (baseURL string, output func() string, stop func()) {
	t.Helper()

	repoRoot := RepoRoot(t)
	port := freePort(t)

	cmd := exec.Command("go", "run", "./cmd/api", fmt.Sprintf("--config=%s", configName))
	cmd.Dir = repoRoot

	// `go run` 會 fork 出編譯後的 binary；殺掉 `go run` 本身不會連帶殺掉它，
	// 因此建立獨立的 process group，stop 時整組一起殺，避免留下佔埠的孤兒行程。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	envs := append(os.Environ(), fmt.Sprintf("CHUCHU_SERVER_PORT=%d", port))
	for k, v := range env {
		envs = append(envs, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envs

	outBuf := &syncBuffer{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("啟動 `go run ./cmd/api --config=%s` 失敗: %v", configName, err)
	}

	var stopOnce sync.Once
	stopFn := func() {
		stopOnce.Do(func() {
			killProcessGroup(cmd, syscall.SIGTERM)

			done := make(chan struct{})
			go func() {
				_ = cmd.Wait()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(apiStopTimeout):
				killProcessGroup(cmd, syscall.SIGKILL)
				<-done
			}
		})
	}
	t.Cleanup(stopFn)

	baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	if !waitForTCP(fmt.Sprintf("127.0.0.1:%d", port), apiStartTimeout) {
		out := outBuf.String()
		stopFn()
		t.Fatalf("API 在 %s 內未接受連線（port=%d）。累積輸出:\n%s", apiStartTimeout, port, out)
	}

	return baseURL, outBuf.String, stopFn
}

// 對整個 process group 送訊號，涵蓋 `go run` 與它 fork 出來的 binary。
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}

func waitForTCP(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, apiStartPollInterval)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(apiStartPollInterval)
	}
	return false
}

// 回傳後立刻釋放，理論上有極小的競爭窗口——這是取得可用 port 的標準作法。
func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取得空閒 port 失敗: %v", err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}
