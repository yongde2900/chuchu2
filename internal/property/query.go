package property

import (
	"context"

	"github.com/google/uuid"
)

// 分頁查詢的預設值與上限。
const (
	// 頁碼是 1-based。
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// ListFilter 的 Status／RentalMode 為 nil、City 為空字串都代表不依該欄位篩選。
// Page／PageSize 不合法時由 normalize 補預設值。
type ListFilter struct {
	Page       int
	PageSize   int
	Status     *Status
	RentalMode *RentalMode
	City       string
}

// 補預設值並夾限；小於 1 一律視為未提供。
func (f ListFilter) normalize() ListFilter {
	if f.Page < 1 {
		f.Page = defaultPage
	}
	if f.PageSize < 1 {
		f.PageSize = defaultPageSize
	}
	if f.PageSize > maxPageSize {
		f.PageSize = maxPageSize
	}
	return f
}

type ListResult struct {
	// 固定以 created_at 由新到舊排序，同時間以 id 為次要鍵避免分頁跳號。
	Items []*Property
	// 套用篩選後、分頁前的總筆數，與 len(Items) 無關。
	Total int
}

// 查無資料時錯誤帶有 apperr.CodePropertyNotFound，由 Repository 實作轉譯。
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Property, error) {
	return s.repo.GetByID(ctx, id)
}

// 分頁的預設值與夾限統一在這裡處理，Repository 實作可以信任收到的 f 已經合法。
func (s *Service) List(ctx context.Context, f ListFilter) (ListResult, error) {
	return s.repo.List(ctx, f.normalize())
}
