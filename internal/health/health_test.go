// 本檔是 health 套件的單元測試：只測 Service.Check 產生的 Report 內容
// 與 Mount 依 Report 選擇的狀態碼，全部用假的 in-memory Checker 實作，
// 不碰 Docker、不連任何外部服務。
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fakeChecker 是測試用的假 Checker：依建構時給定的 err 決定 Check 的結果。
type fakeChecker struct {
	name string
	err  error
}

func (f *fakeChecker) Name() string { return f.name }

func (f *fakeChecker) Check(ctx context.Context) error { return f.err }

func TestService_Check_AllOK(t *testing.T) {
	svc := NewService(
		&fakeChecker{name: "postgres"},
		&fakeChecker{name: "redis"},
	)

	report := svc.Check(context.Background())

	if report.Status != "ok" {
		t.Fatalf("report.Status = %q, want %q", report.Status, "ok")
	}
	if report.Checks["postgres"] != "ok" {
		t.Fatalf("report.Checks[postgres] = %q, want %q", report.Checks["postgres"], "ok")
	}
	if report.Checks["redis"] != "ok" {
		t.Fatalf("report.Checks[redis] = %q, want %q", report.Checks["redis"], "ok")
	}
}

func TestService_Check_OneFails(t *testing.T) {
	svc := NewService(
		&fakeChecker{name: "postgres", err: errors.New("connection refused")},
		&fakeChecker{name: "redis"},
	)

	report := svc.Check(context.Background())

	if report.Status != "degraded" {
		t.Fatalf("report.Status = %q, want %q", report.Status, "degraded")
	}
	if report.Checks["postgres"] != "down" {
		t.Fatalf("report.Checks[postgres] = %q, want %q", report.Checks["postgres"], "down")
	}
	if report.Checks["redis"] != "ok" {
		t.Fatalf("report.Checks[redis] = %q, want %q", report.Checks["redis"], "ok")
	}
}

func TestService_Check_MultipleFail(t *testing.T) {
	svc := NewService(
		&fakeChecker{name: "postgres", err: errors.New("timeout")},
		&fakeChecker{name: "redis", err: errors.New("timeout")},
	)

	report := svc.Check(context.Background())

	if report.Status != "degraded" {
		t.Fatalf("report.Status = %q, want %q", report.Status, "degraded")
	}
	if report.Checks["postgres"] != "down" {
		t.Fatalf("report.Checks[postgres] = %q, want %q", report.Checks["postgres"], "down")
	}
	if report.Checks["redis"] != "down" {
		t.Fatalf("report.Checks[redis] = %q, want %q", report.Checks["redis"], "down")
	}
}

func TestService_Check_NoCheckers(t *testing.T) {
	svc := NewService()

	report := svc.Check(context.Background())

	if report.Status != "ok" {
		t.Fatalf("report.Status = %q, want %q", report.Status, "ok")
	}
	if len(report.Checks) != 0 {
		t.Fatalf("report.Checks = %v, want empty", report.Checks)
	}
}

func TestMount_AllOK_Returns200(t *testing.T) {
	svc := NewService(&fakeChecker{name: "postgres"}, &fakeChecker{name: "redis"})

	r := chi.NewRouter()
	Mount(svc)(r)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz 失敗: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("狀態碼 = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body Report
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("解析回應 JSON 失敗: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("body.Status = %q, want %q", body.Status, "ok")
	}
	if body.Checks["postgres"] != "ok" || body.Checks["redis"] != "ok" {
		t.Fatalf("body.Checks = %v, want all ok", body.Checks)
	}
}

func TestMount_OneDown_Returns503(t *testing.T) {
	svc := NewService(
		&fakeChecker{name: "postgres", err: errors.New("down")},
		&fakeChecker{name: "redis"},
	)

	r := chi.NewRouter()
	Mount(svc)(r)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz 失敗: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("狀態碼 = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var body Report
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("解析回應 JSON 失敗: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("body.Status = %q, want %q", body.Status, "degraded")
	}
	if body.Checks["postgres"] != "down" {
		t.Fatalf("body.Checks[postgres] = %q, want %q", body.Checks["postgres"], "down")
	}
}
