package property

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// UpdateInput 是更新一筆物件所需的輸入。每個欄位都是指標：nil 代表「這個
// 欄位不更動」，非 nil 代表「把這個欄位改成這個值」。
//
// 刻意沒有 Status 欄位——狀態變更只能透過 ChangeStatus（並經過
// CanTransition 的狀態機檢查），不能透過 PATCH 這個「全欄位覆寫」的入口繞過
// 狀態機。這是型別層面的強制：即使 httpapi 把整包 request body 解碼進來，
// UpdateInput 也沒有地方可以放 status，PATCH 永遠不可能變更狀態。
//
// 金額欄位（MonthlyRent、ManagementFee）沿用 CreateInput 的慣例，以字串進來，
// 由 Validate 負責解析並回報是哪個欄位出錯。
type UpdateInput struct {
	MonthlyRent   *string
	ManagementFee *string
	DepositMonths *int
	Layout        *string
	LandlordName  *string
	LandlordPhone *string
}

// Validate 檢查 in 中每個非 nil 欄位是否合法，回傳所有出錯欄位（不是遇到第
// 一個就回）；沒有任何欄位出錯時回傳 nil。nil 欄位一律視為「未提供」，不會
// 被判定為錯誤。
//
// 驗證規則沿用 CreateInput 的語意：
//   - monthly_rent 若提供，必須可解析且嚴格大於 0。
//   - management_fee 若提供，必須可解析且不可為負數；空字串沿用 Create 的
//     語意視為「未提供」，不視為錯誤。
//   - deposit_months 若提供，不可為負數。
//   - layout 若提供，必須是四個合法值之一。
//   - landlord_name、landlord_phone 沒有格式限制。
func (in UpdateInput) Validate() []apperr.FieldError {
	var errs []apperr.FieldError

	if in.MonthlyRent != nil {
		if reason := validatePositiveDecimal(*in.MonthlyRent); reason != "" {
			errs = append(errs, apperr.FieldError{Field: "monthly_rent", Reason: reason})
		}
	}

	if in.ManagementFee != nil {
		if reason := validateNonNegativeDecimal(*in.ManagementFee); reason != "" {
			errs = append(errs, apperr.FieldError{Field: "management_fee", Reason: reason})
		}
	}

	if in.DepositMonths != nil && *in.DepositMonths < 0 {
		errs = append(errs, apperr.FieldError{Field: "deposit_months", Reason: "不可為負數"})
	}

	if in.Layout != nil && !Layout(*in.Layout).Valid() {
		errs = append(errs, apperr.FieldError{
			Field:  "layout",
			Reason: "必須是 WHOLE_UNIT、INDEPENDENT_SUITE、SHARED_SUITE 或 SINGLE_ROOM 之一",
		})
	}

	return errs
}

// Update 驗證 in，把非 nil 的欄位套用到 id 對應的物件上並持久化，回傳更新後
// 的完整物件。
//
// 驗證失敗時回傳 apperr.CodeValidationFailed（帶完整的 Details）；
// 查無該 id 時，錯誤原封不動往上傳（已經是 apperr.CodePropertyNotFound，由
// GetByID 回報）。任何成功的更新都會把 UpdatedAt 設成 time.Now()，確保嚴格
// 晚於原本的 CreatedAt／UpdatedAt。
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Property, error) {
	if errs := in.Validate(); len(errs) > 0 {
		return nil, apperr.Validation(errs...)
	}

	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if in.MonthlyRent != nil {
		// Validate 已確保這個字串可解析且為正數，這裡的錯誤必定為 nil，
		// 但仍檢查以避免未來 Validate 規則變動時默默吞掉解析失敗。
		d, err := decimal.NewFromString(*in.MonthlyRent)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeValidationFailed, "monthly_rent 解析失敗", err)
		}
		p.MonthlyRent = d
	}

	if in.ManagementFee != nil {
		// 沿用 Create 的語意：空字串視為「未提供具體金額」，套用預設值 0。
		fee := decimal.Zero
		if *in.ManagementFee != "" {
			d, err := decimal.NewFromString(*in.ManagementFee)
			if err != nil {
				return nil, apperr.Wrap(apperr.CodeValidationFailed, "management_fee 解析失敗", err)
			}
			fee = d
		}
		p.ManagementFee = fee
	}

	if in.DepositMonths != nil {
		p.DepositMonths = *in.DepositMonths
	}

	if in.Layout != nil {
		p.Layout = Layout(*in.Layout)
	}

	if in.LandlordName != nil {
		p.LandlordName = *in.LandlordName
	}

	if in.LandlordPhone != nil {
		p.LandlordPhone = *in.LandlordPhone
	}

	p.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// ChangeStatus 把 id 對應的物件狀態轉換到 target，回傳轉換後的完整物件。
//
// 轉換是否合法由 CanTransition（狀態機的唯一權威來源）判斷。轉換非法時回傳
// apperr.CodeInvalidStatusTransition（HTTP 409），且刻意在呼叫 s.repo.Update
// 之前就回傳——先 GetByID 取得目前狀態、再檢查 CanTransition，不合法就直接
// 回錯誤，不會碰 Repository 的 Update，確保拒絕發生在寫入之前、資料庫裡的
// 狀態完全不受影響。
func (s *Service) ChangeStatus(ctx context.Context, id uuid.UUID, target Status) (*Property, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !CanTransition(p.Status, target) {
		return nil, apperr.New(
			apperr.CodeInvalidStatusTransition,
			fmt.Sprintf("不允許把物件狀態從 %s 轉換到 %s", p.Status, target),
		)
	}

	p.Status = target
	p.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}
