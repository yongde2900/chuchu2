// 本檔是 health 套件的單元測試：只測 Service.Check 產生的 Report 內容
// 與 API.GetHealthz 依 Report 選擇的 response object，全部用假的 in-memory
// Checker 實作，不碰 Docker、不連任何外部服務。
package health

import (
	"context"
	"errors"
	"testing"

	"github.com/yongde2900/chuchu2/api"
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

// TestAPI_GetHealthz_AllOK_Returns200 驗證全部相依服務健康時，
// API.GetHealthz 回傳 api.GetHealthz200JSONResponse（而不是 503 那個型別）。
func TestAPI_GetHealthz_AllOK_Returns200(t *testing.T) {
	svc := NewService(&fakeChecker{name: "postgres"}, &fakeChecker{name: "redis"})
	a := NewAPI(svc)

	resp, err := a.GetHealthz(context.Background(), api.GetHealthzRequestObject{})
	if err != nil {
		t.Fatalf("GetHealthz 回傳 error: %v", err)
	}

	body, ok := resp.(api.GetHealthz200JSONResponse)
	if !ok {
		t.Fatalf("回應型別 = %T, want api.GetHealthz200JSONResponse", resp)
	}
	if body.Status != api.Ok {
		t.Fatalf("body.Status = %q, want %q", body.Status, api.Ok)
	}
	if body.Checks["postgres"] != "ok" || body.Checks["redis"] != "ok" {
		t.Fatalf("body.Checks = %v, want all ok", body.Checks)
	}
}

// TestAPI_GetHealthz_OneDown_Returns503 驗證任一相依服務不健康時，
// API.GetHealthz 回傳 api.GetHealthz503JSONResponse。
func TestAPI_GetHealthz_OneDown_Returns503(t *testing.T) {
	svc := NewService(
		&fakeChecker{name: "postgres", err: errors.New("down")},
		&fakeChecker{name: "redis"},
	)
	a := NewAPI(svc)

	resp, err := a.GetHealthz(context.Background(), api.GetHealthzRequestObject{})
	if err != nil {
		t.Fatalf("GetHealthz 回傳 error: %v", err)
	}

	body, ok := resp.(api.GetHealthz503JSONResponse)
	if !ok {
		t.Fatalf("回應型別 = %T, want api.GetHealthz503JSONResponse", resp)
	}
	if body.Status != api.Degraded {
		t.Fatalf("body.Status = %q, want %q", body.Status, api.Degraded)
	}
	if body.Checks["postgres"] != "down" {
		t.Fatalf("body.Checks[postgres] = %q, want %q", body.Checks["postgres"], "down")
	}
}
