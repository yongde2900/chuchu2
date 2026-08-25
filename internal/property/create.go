package property

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

type CreateInput struct {
	// 這五個欄位構成唯一索引 properties_address_key，重複建檔以它們判定。
	City, District, StreetAddress, Floor, RoomNo string

	Layout string

	// 金額刻意以字串進來：由 Validate 解析成 decimal 並回報是哪個欄位出錯，
	// 上層才不用自己把解析失敗的原因對應回欄位名稱。
	AreaPing, MonthlyRent, ManagementFee string

	DepositMonths int
	RentalMode    string

	LandlordName, LandlordPhone string
}

// 回傳**所有**出錯欄位，不是遇到第一個就回。
// FieldError.Field 必須等於 JSON 欄位名（snake_case），它會直接出現在回應的
// details 陣列裡。
func (in CreateInput) Validate() []apperr.FieldError {
	var errs []apperr.FieldError

	if strings.TrimSpace(in.City) == "" {
		errs = append(errs, apperr.FieldError{Field: "city", Reason: "不可為空"})
	}

	if strings.TrimSpace(in.StreetAddress) == "" {
		errs = append(errs, apperr.FieldError{Field: "street_address", Reason: "不可為空"})
	}

	if reason := validatePositiveDecimal(in.MonthlyRent); reason != "" {
		errs = append(errs, apperr.FieldError{Field: "monthly_rent", Reason: reason})
	}

	if reason := validatePositiveDecimal(in.AreaPing); reason != "" {
		errs = append(errs, apperr.FieldError{Field: "area_ping", Reason: reason})
	}

	if reason := validateNonNegativeDecimal(in.ManagementFee); reason != "" {
		errs = append(errs, apperr.FieldError{Field: "management_fee", Reason: reason})
	}

	if in.DepositMonths < 0 {
		errs = append(errs, apperr.FieldError{Field: "deposit_months", Reason: "不可為負數"})
	}

	if !RentalMode(in.RentalMode).Valid() {
		errs = append(errs, apperr.FieldError{Field: "rental_mode", Reason: "必須是 MASTER_LEASE 或 MANAGED"})
	}

	if !Layout(in.Layout).Valid() {
		errs = append(errs, apperr.FieldError{
			Field:  "layout",
			Reason: "必須是 WHOLE_UNIT、INDEPENDENT_SUITE、SHARED_SUITE 或 SINGLE_ROOM 之一",
		})
	}

	return errs
}

// 不合法時回傳非空的錯誤原因，合法時回傳空字串。
func validatePositiveDecimal(s string) string {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return "必須是合法的十進位數字"
	}
	if !d.IsPositive() {
		return "必須大於 0"
	}
	return ""
}

// 空字串視為未提供，由呼叫端決定預設值，不視為錯誤。
func validateNonNegativeDecimal(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return "必須是合法的十進位數字"
	}
	if d.IsNegative() {
		return "不可為負數"
	}
	return ""
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
