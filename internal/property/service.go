package property

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// Repository 是領域層對持久化的唯一出口；實作在 pgrepo 子套件。
type Repository interface {
	// Create 新增一筆物件。命中唯一鍵（同 city／district／street_address／
	// floor／room_no）時，實作必須回傳 apperr.CodePropertyDuplicate 的錯誤。
	Create(ctx context.Context, p *Property) error

	// GetByID 依 id 查詢單一物件。查無資料時，實作必須回傳
	// apperr.CodePropertyNotFound 的錯誤。
	GetByID(ctx context.Context, id uuid.UUID) (*Property, error)

	// List 依 f 分頁列出物件，回傳套用篩選後、分頁前的總筆數與當頁資料。
	List(ctx context.Context, f ListFilter) (ListResult, error)

	// Update 以 p.ID 為鍵，把 p 的欄位整筆覆寫回持久層（呼叫端已經先
	// GetByID 取得完整物件、修改要更動的欄位，再呼叫 Update）。呼叫端保證
	// 呼叫前 p.ID 對應的資料存在；一般不會發生找不到的情況。
	Update(ctx context.Context, p *Property) error
}

// Service 是物件領域的應用服務，封裝驗證、ID／時間戳產生與持久化。
type Service struct {
	repo Repository
}

// NewService 用給定的 repo 建立 Service。
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create 驗證 in，建立一筆新物件（Status 一律為 VACANT，ID 由服務端產生 UUID）
// 並交給 Repository 持久化。
//
// 驗證失敗時回傳 apperr.CodeValidationFailed（帶完整的 Details）；
// Repository 回報重複建檔時，錯誤原封不動往上傳（已經是 apperr.CodePropertyDuplicate）。
func (s *Service) Create(ctx context.Context, in CreateInput) (*Property, error) {
	if errs := in.Validate(); len(errs) > 0 {
		return nil, apperr.Validation(errs...)
	}

	// Validate 已確保這三個欄位可解析，這裡的錯誤必定為 nil，但仍檢查以避免
	// 未來 Validate 規則變動時默默吞掉解析失敗。
	areaPing, err := decimal.NewFromString(in.AreaPing)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeValidationFailed, "area_ping 解析失敗", err)
	}
	monthlyRent, err := decimal.NewFromString(in.MonthlyRent)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeValidationFailed, "monthly_rent 解析失敗", err)
	}

	managementFee := decimal.Zero
	if in.ManagementFee != "" {
		managementFee, err = decimal.NewFromString(in.ManagementFee)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeValidationFailed, "management_fee 解析失敗", err)
		}
	}

	now := time.Now().UTC()
	p := &Property{
		ID:            uuid.New(),
		City:          in.City,
		District:      in.District,
		StreetAddress: in.StreetAddress,
		Floor:         in.Floor,
		RoomNo:        in.RoomNo,
		Layout:        Layout(in.Layout),
		AreaPing:      areaPing,
		MonthlyRent:   monthlyRent,
		ManagementFee: managementFee,
		DepositMonths: in.DepositMonths,
		RentalMode:    RentalMode(in.RentalMode),
		Status:        StatusVacant,
		LandlordName:  in.LandlordName,
		LandlordPhone: in.LandlordPhone,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}
