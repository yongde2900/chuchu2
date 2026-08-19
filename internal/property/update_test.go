package property

import "testing"

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// TestUpdateInput_Validate_AllNilIsValid 驗證 UpdateInput 的零值（所有欄位皆
// nil，代表「什麼都不更動」）不會被判定為錯誤。
func TestUpdateInput_Validate_AllNilIsValid(t *testing.T) {
	in := UpdateInput{}
	if errs := in.Validate(); errs != nil {
		t.Fatalf("Validate() = %+v, want nil", errs)
	}
}

// TestUpdateInput_Validate_MonthlyRent 驗證 monthly_rent 若有提供，必須是可
// 解析且嚴格大於 0 的十進位數字字串；nil（未提供）一律視為合法。
func TestUpdateInput_Validate_MonthlyRent(t *testing.T) {
	cases := []struct {
		name    string
		rent    *string
		wantErr bool
	}{
		{name: "nil 不驗證", rent: nil, wantErr: false},
		{name: "合法正數", rent: strPtr("27000.00"), wantErr: false},
		{name: "零", rent: strPtr("0"), wantErr: true},
		{name: "負數", rent: strPtr("-1"), wantErr: true},
		{name: "無法解析", rent: strPtr("abc"), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := UpdateInput{MonthlyRent: tc.rent}
			errs := in.Validate()
			if got := containsField(errs, "monthly_rent"); got != tc.wantErr {
				t.Fatalf("containsField(Validate(), monthly_rent) = %v, want %v (errs=%+v)", got, tc.wantErr, errs)
			}
		})
	}
}

// TestUpdateInput_Validate_ManagementFee 驗證 management_fee 若提供則必須是
// 合法且非負的十進位數字；空字串沿用 Create 的語意視為未提供；nil 一律合法。
func TestUpdateInput_Validate_ManagementFee(t *testing.T) {
	cases := []struct {
		name    string
		fee     *string
		wantErr bool
	}{
		{name: "nil 不驗證", fee: nil, wantErr: false},
		{name: "空字串視為未提供", fee: strPtr(""), wantErr: false},
		{name: "合法非負數字", fee: strPtr("1200.00"), wantErr: false},
		{name: "零", fee: strPtr("0"), wantErr: false},
		{name: "負數", fee: strPtr("-1"), wantErr: true},
		{name: "無法解析", fee: strPtr("abc"), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := UpdateInput{ManagementFee: tc.fee}
			errs := in.Validate()
			if got := containsField(errs, "management_fee"); got != tc.wantErr {
				t.Fatalf("containsField(Validate(), management_fee) = %v, want %v (errs=%+v)", got, tc.wantErr, errs)
			}
		})
	}
}

// TestUpdateInput_Validate_DepositMonths 驗證 deposit_months 若提供不可為負數；
// nil 一律合法。
func TestUpdateInput_Validate_DepositMonths(t *testing.T) {
	cases := []struct {
		name    string
		months  *int
		wantErr bool
	}{
		{name: "nil 不驗證", months: nil, wantErr: false},
		{name: "零", months: intPtr(0), wantErr: false},
		{name: "正數", months: intPtr(3), wantErr: false},
		{name: "負數", months: intPtr(-1), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := UpdateInput{DepositMonths: tc.months}
			errs := in.Validate()
			if got := containsField(errs, "deposit_months"); got != tc.wantErr {
				t.Fatalf("containsField(Validate(), deposit_months) = %v, want %v (errs=%+v)", got, tc.wantErr, errs)
			}
		})
	}
}

// TestUpdateInput_Validate_Layout 驗證 layout 若提供必須是四個合法值之一；
// nil 一律合法。
func TestUpdateInput_Validate_Layout(t *testing.T) {
	cases := []struct {
		name    string
		layout  *string
		wantErr bool
	}{
		{name: "nil 不驗證", layout: nil, wantErr: false},
		{name: "合法值", layout: strPtr(string(LayoutSharedSuite)), wantErr: false},
		{name: "非法值", layout: strPtr("CASTLE"), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := UpdateInput{Layout: tc.layout}
			errs := in.Validate()
			if got := containsField(errs, "layout"); got != tc.wantErr {
				t.Fatalf("containsField(Validate(), layout) = %v, want %v (errs=%+v)", got, tc.wantErr, errs)
			}
		})
	}
}

// TestUpdateInput_Validate_LandlordFieldsNeverError 驗證 landlord_name／
// landlord_phone 沒有格式限制，任何字串值（含空字串）都不應被判定為錯誤。
func TestUpdateInput_Validate_LandlordFieldsNeverError(t *testing.T) {
	in := UpdateInput{LandlordName: strPtr(""), LandlordPhone: strPtr("")}
	if errs := in.Validate(); errs != nil {
		t.Fatalf("Validate() = %+v, want nil", errs)
	}
}
