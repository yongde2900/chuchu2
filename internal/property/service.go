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

// 新物件的 Status 一律是 VACANT，ID 由服務端產生。
func (s *Service) Create(ctx context.Context, in CreateInput) (*Property, error) {
	if errs := in.Validate(); len(errs) > 0 {
		return nil, apperr.Validation(errs...)
	}

	// Validate 已確保可解析，這裡必定為 nil；仍檢查以免日後規則變動時默默吞掉錯誤。
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
