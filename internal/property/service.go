package property

import (
	"context"

	"github.com/google/uuid"
)

// Repository 是領域層對持久化的唯一出口；實作在 pgrepo 子套件。
type Repository interface {
	// Create 新增一筆物件。命中唯一鍵（同 city／district／street_address／
	// floor／room_no）時，實作必須回傳 apperr.CodePropertyDuplicate 的錯誤。
	Create(ctx context.Context, p *Property) error

	// GetByID 依 id 查詢單一物件。查無資料時，實作必須回傳
	// apperr.CodePropertyNotFound 的錯誤。
	GetByID(ctx context.Context, id uuid.UUID) (*Property, error)

	// 回傳的總筆數是套用篩選後、分頁前的。
	List(ctx context.Context, f ListFilter) (ListResult, error)

	// 以 p.ID 為鍵整筆覆寫。呼叫端必須先 GetByID 取得完整物件再改欄位，
	// 並保證資料存在。
	Update(ctx context.Context, p *Property) error
}

// Service 封裝驗證、ID／時間戳產生與持久化。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}
