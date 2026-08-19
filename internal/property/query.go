package property

import (
	"context"

	"github.com/google/uuid"
)

// 分頁查詢的預設值與上限。
const (
	// defaultPage 是 Page 未提供或非正整數時的預設頁碼（1-based）。
	defaultPage = 1
	// defaultPageSize 是 PageSize 未提供或非正整數時的預設每頁筆數。
	defaultPageSize = 20
	// maxPageSize 是 PageSize 的上限，超過此值一律夾限為 maxPageSize。
	maxPageSize = 100
)

// ListFilter 是列表查詢的分頁與篩選條件。
//
// Status、RentalMode 為 nil 代表不依該欄位篩選；City 為空字串代表不篩選城市。
// Page、PageSize 若不合法（例如 0 或負數），normalize 會補上預設值。
type ListFilter struct {
	Page       int
	PageSize   int
	Status     *Status
	RentalMode *RentalMode
	City       string
}

// normalize 回傳補上預設值、夾限過的 f：
//   - Page 小於 1 時視為第 1 頁。
//   - PageSize 小於 1 時視為 defaultPageSize；大於 maxPageSize 時夾限為 maxPageSize。
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

// ListResult 是一次分頁查詢的結果。
type ListResult struct {
	// Items 是當頁資料，固定以 created_at 由新到舊排序（同時間以 id 為次要
	// 排序鍵，避免分頁跳號）。
	Items []*Property
	// Total 是套用篩選後、分頁前的總筆數，與 len(Items) 無關。
	Total int
}

// Get 依 id 查詢單一物件；查無資料時，回傳的錯誤帶有
// apperr.CodePropertyNotFound（由 Repository 實作負責轉譯）。
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Property, error) {
	return s.repo.GetByID(ctx, id)
}

// List 依 f 分頁列出物件。Page／PageSize 的預設值與上限夾限統一在這裡
// （透過 f.normalize()）處理，Repository 實作只需信任收到的 f 已經合法。
func (s *Service) List(ctx context.Context, f ListFilter) (ListResult, error) {
	return s.repo.List(ctx, f.normalize())
}
