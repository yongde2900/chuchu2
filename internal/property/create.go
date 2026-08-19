package property

import (
	"strings"

	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// CreateInput 是建立一筆物件所需的輸入。金額欄位（AreaPing、MonthlyRent、
// ManagementFee）刻意以字串進來 —— 呼叫端（httpapi）從 JSON 字串解碼出來的
// 原始字面值原封不動傳進來，由 Validate 負責解析成 decimal 並回報是哪個
// 欄位出錯，而不是讓上層自己猜要怎麼把解析失敗的原因對應回欄位名稱。
type CreateInput struct {
	City, District, StreetAddress, Floor, RoomNo string
	Layout                                       string
	AreaPing, MonthlyRent, ManagementFee         string
	DepositMonths                                int
	RentalMode                                   string
	LandlordName, LandlordPhone                  string
}

// Validate 檢查 in 的每一條驗證規則，回傳所有出錯欄位（不是遇到第一個就回）；
// 沒有任何欄位出錯時回傳 nil。每一項 FieldError 的 Field 等於 JSON 欄位名
// （snake_case，例如 street_address、monthly_rent），供 httpx.WriteError
// 輸出到錯誤回應的 details 陣列。
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

// validatePositiveDecimal 驗證 s 是可解析的十進位數字且嚴格大於 0，
// 不合法時回傳非空的錯誤原因；合法時回傳空字串。
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

// validateNonNegativeDecimal 驗證 s（若非空字串）是可解析的十進位數字且不為負數。
// 空字串視為未提供，交由呼叫端決定預設值（目前為 0），不視為錯誤。
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
