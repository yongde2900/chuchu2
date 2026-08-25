package property

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// UpdateInput 每個欄位都是指標：nil 代表不更動該欄位。
//
// **刻意沒有 Status 欄位**——狀態變更只能走 ChangeStatus 並通過 CanTransition。
// 這是型別層面的強制：即使整包 request body 解碼進來，也沒有地方放 status，
// PATCH 永遠繞不過狀態機。
//
// 金額以字串進來的理由同 CreateInput。
type UpdateInput struct {
	MonthlyRent   *string
	ManagementFee *string
	DepositMonths *int
	Layout        *string
	LandlordName  *string
	LandlordPhone *string
}

// 回傳**所有**出錯欄位，不是遇到第一個就回。nil 欄位視為未提供，不判定為錯誤。
// 驗證規則沿用 CreateInput：租金須大於 0、管理費不可為負（空字串視為未提供）、
// 押金月數不可為負、格局須是合法列舉值。
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

// 成功的更新一律把 UpdatedAt 設成 time.Now()，確保嚴格晚於原本的時間戳。
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Property, error) {
	if errs := in.Validate(); len(errs) > 0 {
		return nil, apperr.Validation(errs...)
	}

	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if in.MonthlyRent != nil {
		// Validate 已確保可解析，這裡必定為 nil；仍檢查以免日後規則變動時默默吞掉錯誤。
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

// 合法性由 CanTransition 判斷，且**拒絕必須發生在寫入之前**：
// 先 GetByID 取得目前狀態、檢查通過才碰 Repository.Update，
// 確保非法轉換完全不影響資料庫。
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
