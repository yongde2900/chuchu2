package property

import (
	"testing"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// validCreateInput 回傳一份通過所有驗證規則的 CreateInput，供各測試以此為基準
// 覆寫單一欄位，模擬 BDD Scenario Outline「其餘欄位皆合法」的設定。
func validCreateInput() CreateInput {
	return CreateInput{
		City:          "臺北市",
		District:      "大安區",
		StreetAddress: "復興南路一段 100 號",
		Floor:         "5",
		RoomNo:        "A",
		Layout:        string(LayoutIndependentSuite),
		AreaPing:      "8.50",
		MonthlyRent:   "25000.50",
		ManagementFee: "1200.00",
		DepositMonths: 2,
		RentalMode:    string(RentalModeMasterLease),
		LandlordName:  "王大明",
		LandlordPhone: "0912345678",
	}
}

// TestCreateInput_Validate_Valid 驗證一份合法輸入不會有任何欄位出錯。
func TestCreateInput_Validate_Valid(t *testing.T) {
	in := validCreateInput()
	if errs := in.Validate(); errs != nil {
		t.Fatalf("Validate() = %+v, want nil", errs)
	}
}

// TestCreateInput_Validate_FieldErrors 逐一對應 BDD Scenario Outline
// 「欄位驗證失敗時回報是哪個欄位出錯」的九個 Examples：每一列都必須在其餘
// 欄位皆合法的前提下，讓 Validate 回傳一項 field 等於該欄位名稱的 FieldError。
func TestCreateInput_Validate_FieldErrors(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(in *CreateInput)
		wantField string
	}{
		{
			name:      `city=""`,
			mutate:    func(in *CreateInput) { in.City = "" },
			wantField: "city",
		},
		{
			name:      `street_address=""`,
			mutate:    func(in *CreateInput) { in.StreetAddress = "" },
			wantField: "street_address",
		},
		{
			name:      `monthly_rent="0"`,
			mutate:    func(in *CreateInput) { in.MonthlyRent = "0" },
			wantField: "monthly_rent",
		},
		{
			name:      `monthly_rent="-1"`,
			mutate:    func(in *CreateInput) { in.MonthlyRent = "-1" },
			wantField: "monthly_rent",
		},
		{
			name:      `monthly_rent="abc"`,
			mutate:    func(in *CreateInput) { in.MonthlyRent = "abc" },
			wantField: "monthly_rent",
		},
		{
			name:      `area_ping="0"`,
			mutate:    func(in *CreateInput) { in.AreaPing = "0" },
			wantField: "area_ping",
		},
		{
			name:      `deposit_months=-1`,
			mutate:    func(in *CreateInput) { in.DepositMonths = -1 },
			wantField: "deposit_months",
		},
		{
			name:      `rental_mode="SUBLEASE"`,
			mutate:    func(in *CreateInput) { in.RentalMode = "SUBLEASE" },
			wantField: "rental_mode",
		},
		{
			name:      `layout="CASTLE"`,
			mutate:    func(in *CreateInput) { in.Layout = "CASTLE" },
			wantField: "layout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validCreateInput()
			tc.mutate(&in)

			errs := in.Validate()
			if !containsField(errs, tc.wantField) {
				t.Fatalf("Validate() = %+v, want an entry with field %q", errs, tc.wantField)
			}
		})
	}
}

// TestCreateInput_Validate_ReportsAllFieldErrors 驗證 Validate 回傳「所有」
// 出錯欄位，而不是遇到第一個就回。
func TestCreateInput_Validate_ReportsAllFieldErrors(t *testing.T) {
	in := validCreateInput()
	in.City = ""
	in.StreetAddress = ""
	in.MonthlyRent = "abc"
	in.AreaPing = "0"
	in.DepositMonths = -1
	in.RentalMode = "SUBLEASE"
	in.Layout = "CASTLE"

	errs := in.Validate()

	wantFields := []string{"city", "street_address", "monthly_rent", "area_ping", "deposit_months", "rental_mode", "layout"}
	for _, f := range wantFields {
		if !containsField(errs, f) {
			t.Fatalf("Validate() = %+v, missing field %q", errs, f)
		}
	}
	if len(errs) != len(wantFields) {
		t.Fatalf("len(Validate()) = %d, want %d (errs=%+v)", len(errs), len(wantFields), errs)
	}
}

// TestCreateInput_Validate_ManagementFee 驗證 management_fee 若提供則必須是
// 合法且非負的十進位數字；未提供（空字串）則視為未出錯，交由 Service 補預設值。
func TestCreateInput_Validate_ManagementFee(t *testing.T) {
	cases := []struct {
		name    string
		fee     string
		wantErr bool
	}{
		{name: "空字串視為未提供", fee: "", wantErr: false},
		{name: "合法非負數字", fee: "1200.00", wantErr: false},
		{name: "零", fee: "0", wantErr: false},
		{name: "負數", fee: "-1", wantErr: true},
		{name: "無法解析", fee: "abc", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validCreateInput()
			in.ManagementFee = tc.fee

			errs := in.Validate()
			got := containsField(errs, "management_fee")
			if got != tc.wantErr {
				t.Fatalf("containsField(Validate(), management_fee) = %v, want %v (errs=%+v)", got, tc.wantErr, errs)
			}
		})
	}
}

// containsField 回報 errs 中是否存在一項 Field 等於 field 的 FieldError。
func containsField(errs []apperr.FieldError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}
