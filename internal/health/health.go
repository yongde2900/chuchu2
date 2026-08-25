// Package health 提供相依服務（Postgres、Redis）的健康檢查探針與彙整報告。
//
// 路由由產生的程式碼提供，本套件只實作 GetHealthz operation——
// 因此不 import chi／net-http，也不 import internal/server。
package health

import (
	"context"
	"sync"

	"github.com/yongde2900/chuchu2/api"
)

// Checker 是單一相依服務的探針；Name 即為 checks 物件中的 key。
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

type Report struct {
	Status string            `json:"status"` // "ok" 或 "degraded"
	Checks map[string]string `json:"checks"` // 每項為 "ok" 或 "down"
}

// Service 平行跑過所有 Checker，彙整成一份 Report。
type Service struct {
	checkers []Checker
}

func NewService(checkers ...Checker) *Service {
	return &Service{checkers: checkers}
}

// 任一 Checker 失敗，整體就是 "degraded"；全部成功（或沒有任何 Checker）為 "ok"。
func (s *Service) Check(ctx context.Context) Report {
	checks := make(map[string]string, len(s.checkers))

	type result struct {
		name string
		ok   bool
	}

	results := make(chan result, len(s.checkers))
	var wg sync.WaitGroup
	for _, c := range s.checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()
			err := c.Check(ctx)
			results <- result{name: c.Name(), ok: err == nil}
		}(c)
	}
	wg.Wait()
	close(results)

	allOK := true
	for r := range results {
		if r.ok {
			checks[r.name] = "ok"
		} else {
			checks[r.name] = "down"
			allOK = false
		}
	}

	status := "ok"
	if !allOK {
		status = "degraded"
	}

	return Report{Status: status, Checks: checks}
}

type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

// 全部健康回 200，任一不健康回 503。
func (a *API) GetHealthz(ctx context.Context, _ api.GetHealthzRequestObject) (api.GetHealthzResponseObject, error) {
	report := a.svc.Check(ctx)

	body := api.HealthReport{
		Status: api.HealthReportStatus(report.Status),
		Checks: report.Checks,
	}

	if report.Status != "ok" {
		return api.GetHealthz503JSONResponse(body), nil
	}
	return api.GetHealthz200JSONResponse(body), nil
}
