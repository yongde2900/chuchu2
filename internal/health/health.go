// Package health 提供相依服務（Postgres、Redis）的健康檢查探針、
// 彙整成單一報告的 Service，以及掛載 GET /healthz 的 server.Mount。
//
// 為了保持 internal/server 不 import 任何 feature 套件的依賴反轉原則
// （見專案 CLAUDE.md），是本套件透過 Mount 把路由掛給 cmd/api 組裝，
// 而不是反過來由 internal/server 認識 health。
package health

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/internal/httpx"
	"github.com/yongde2900/chuchu2/internal/server"
)

// Checker 是單一相依服務的探針；Name 回傳的字串即為 Report.Checks 與
// /healthz 回應 JSON 的 checks 物件中的 key。
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Report 是一次健康檢查的彙整結果。
type Report struct {
	Status string            `json:"status"` // "ok" 或 "degraded"
	Checks map[string]string `json:"checks"` // 每項為 "ok" 或 "down"
}

// Service 依序（平行）跑過所有 Checker，彙整成一份 Report。
type Service struct {
	checkers []Checker
}

// NewService 用給定的 checkers 建立 Service。
func NewService(checkers ...Checker) *Service {
	return &Service{checkers: checkers}
}

// Check 對每個 Checker 呼叫 Check(ctx)，彙整成 Report。
// 只要有任一個 Checker 回傳非 nil error，整體 Status 就是 "degraded"；
// 全部成功（或沒有任何 Checker）則 Status 為 "ok"。
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

// Mount 掛載 GET /healthz：全部相依服務都健康時回 200，
// 任一相依服務不健康時回 503。回應 body 一律是 Report 的 JSON。
func Mount(svc *Service) server.Mount {
	return func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
			report := svc.Check(r.Context())

			status := http.StatusOK
			if report.Status != "ok" {
				status = http.StatusServiceUnavailable
			}

			httpx.WriteJSON(w, status, report)
		})
	}
}
