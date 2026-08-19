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

// apiStartPollInterval 是輪詢 API 是否已接受連線的間隔。
const apiStartPollInterval = 100 * time.Millisecond

// apiStartTimeout 是等待 API 接受連線的上限；`go run` 第一次要先編譯，
// 冷機器上可能要數秒，這裡給足夠寬鬆的時間。
const apiStartTimeout = 60 * time.Second

// apiStopTimeout 是優雅終止 API 子行程樹的上限，逾時後改用 SIGKILL。
const apiStopTimeout = 10 * time.Second

// syncBuffer 是一個帶鎖的 io.Writer，讓子行程的 stdout/stderr 在 -race
// 之下也能安全地邊寫邊讀。
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

// StartAPI 以 `go run ./cmd/api --config=<configName>` 啟動服務，工作目錄為
// repo 根目錄，並以 env 中的 CHUCHU_ 前綴環境變數覆寫設定（例如注入
// testcontainers 產生的隨機 DSN／addr）。
//
// StartAPI 自己會用一個空閒 port 覆寫 CHUCHU_SERVER_PORT，避免多個測試
// 平行執行時搶 config/<name>.yaml 裡的固定 port。
//
// 回傳的 baseURL 指向該 port；output 可取得目前為止累積的 stdout/stderr；
// stop 會終止整個子行程樹（見下方關於 `go run` 行程樹的說明）並可安全重複呼叫。
//
// StartAPI 在回傳前會輪詢 baseURL 直到 TCP 連線可被接受，逾時則讓測試立即
// t.Fatalf 並附上目前為止的輸出，避免測試在服務還沒起來前就送出請求。
func StartAPI(t *testing.T, configName string, env map[string]string) (baseURL string, output func() string, stop func()) {
	t.Helper()

	repoRoot := RepoRoot(t)
	port := freePort(t)

	cmd := exec.Command("go", "run", "./cmd/api", fmt.Sprintf("--config=%s", configName))
	cmd.Dir = repoRoot

	// `go run` 會 fork 出編譯後的 binary 子行程；殺掉 `go run` 本身不會
	// 連帶殺掉它，因此建立獨立的 process group，stop 時整組一起殺。
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

// killProcessGroup 對 cmd 所在的整個 process group 送出訊號，涵蓋 `go run`
// 本身與它 fork 出來的編譯後 binary。
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}

// waitForTCP 每隔 apiStartPollInterval 嘗試對 addr 建立 TCP 連線，
// 直到成功或逾時。
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

// freePort 向作業系統要一個目前空閒的 TCP port。
// 回傳後立刻釋放，因此理論上存在極小的競爭窗口，但這是測試輔助函式中
// 取得可用 port 的標準作法。
func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("取得空閒 port 失敗: %v", err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}
