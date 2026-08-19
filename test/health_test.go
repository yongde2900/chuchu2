// 本檔對應 BDD scenario（@infra @integration、@infra @error @integration）：
// 「相依服務皆可連線時健康檢查回報健康」與「任一相依服務斷線時健康檢查回報不健康」。
package test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// healthReport 對應 health.Report 的 JSON 形狀（刻意不直接 import
// internal/health，用等價的匿名結構驗證回應「形狀」本身也符合契約）。
type healthReport struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func TestHealth_AllDependenciesUp_ReturnsOK(t *testing.T) {
	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	body, status := getHealthz(t, baseURL, output)

	if status != http.StatusOK {
		t.Fatalf("狀態碼 = %d, want %d, body=%+v, output:\n%s", status, http.StatusOK, body, output())
	}
	if body.Status != "ok" {
		t.Fatalf("body.Status = %q, want %q", body.Status, "ok")
	}
	if body.Checks["postgres"] != "ok" {
		t.Fatalf("body.Checks[postgres] = %q, want %q", body.Checks["postgres"], "ok")
	}
	if body.Checks["redis"] != "ok" {
		t.Fatalf("body.Checks[redis] = %q, want %q", body.Checks["redis"], "ok")
	}
}

// TestHealth_PostgresDown_ReturnsDegraded503 對應 Scenario Outline 的
// 「Postgres | checks.postgres」這一行。
//
// 這裡起一組專屬、用完即丟的容器再主動停掉 Postgres 那個 —— 絕對不可以碰
// TestMain 的共用容器，停掉它會讓同 package 之後所有測試連鎖失敗。
func TestHealth_PostgresDown_ReturnsDegraded503(t *testing.T) {
	pgDSN, pgStop := testsupport.StartPostgres(t)
	redisAddr, _ := testsupport.StartRedis(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": pgDSN,
		"CHUCHU_REDIS_ADDR":   redisAddr,
	})

	// 先確認服務真的連得上兩個相依服務（200），才能區分「探針正確偵測到斷線」
	// 與「探針從頭到尾就沒連上過」。
	body, status := getHealthz(t, baseURL, output)
	if status != http.StatusOK || body.Status != "ok" {
		t.Fatalf("斷線前 /healthz 應為 200/ok，實際 status=%d body=%+v, output:\n%s", status, body, output())
	}

	pgStop()

	assertHealthzDegraded(t, baseURL, "postgres", output)
}

// TestHealth_RedisDown_ReturnsDegraded503 對應 Scenario Outline 的
// 「Redis | checks.redis」這一行。
func TestHealth_RedisDown_ReturnsDegraded503(t *testing.T) {
	pgDSN, _ := testsupport.StartPostgres(t)
	redisAddr, redisStop := testsupport.StartRedis(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": pgDSN,
		"CHUCHU_REDIS_ADDR":   redisAddr,
	})

	body, status := getHealthz(t, baseURL, output)
	if status != http.StatusOK || body.Status != "ok" {
		t.Fatalf("斷線前 /healthz 應為 200/ok，實際 status=%d body=%+v, output:\n%s", status, body, output())
	}

	redisStop()

	assertHealthzDegraded(t, baseURL, "redis", output)
}

// getHealthz 對 baseURL 發出一次 GET /healthz，回傳解析後的 body 與狀態碼。
func getHealthz(t *testing.T, baseURL string, output func() string) (healthReport, int) {
	t.Helper()

	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz 失敗: %v, output:\n%s", err, output())
	}
	defer resp.Body.Close()

	var body healthReport
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("解析 /healthz 回應 JSON 失敗: %v", err)
	}

	return body, resp.StatusCode
}

// assertHealthzDegraded 輪詢 /healthz 直到回報 503 且 downCheck 為 "down"，
// 或逾時。斷線後探測到失效理論上是下一次請求就會反映，這裡保留短暫輪詢
// 只是為了吸收容器終止後網路堆疊釋放資源的些微延遲。
func assertHealthzDegraded(t *testing.T, baseURL, downCheck string, output func() string) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	var lastBody healthReport
	var lastStatus int

	for time.Now().Before(deadline) {
		lastBody, lastStatus = getHealthz(t, baseURL, output)
		if lastStatus == http.StatusServiceUnavailable && lastBody.Checks[downCheck] == "down" {
			if lastBody.Status != "degraded" {
				t.Fatalf("body.Status = %q, want %q", lastBody.Status, "degraded")
			}
			return
		}
		time.Sleep(300 * time.Millisecond)
	}

	t.Fatalf("等待 /healthz 回報 503 且 checks.%s=down 逾時：最後 status=%d body=%+v, output:\n%s",
		downCheck, lastStatus, lastBody, output())
}
