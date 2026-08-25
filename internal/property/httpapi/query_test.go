package httpapi

import (
	"testing"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/property"
)

// TestToListFilter_PageAndPageSize 驗證 Page／PageSize 為 nil（未提供）時
// 轉成 0，讓領域層的 property.ListFilter.normalize 補上預設值；型別錯誤
// （例如 page=abc）在本輪已經改由綁定階段的 apihttp.ParamErrorHandler 攔下，
// 不再是這個函式的職責，所以這裡不再涵蓋那個案例。
func TestToListFilter_PageAndPageSize(t *testing.T) {
	cases := []struct {
		name         string
		params       api.ListPropertiesParams
		wantPage     int
		wantPageSize int
	}{
		{name: "都缺漏", params: api.ListPropertiesParams{}, wantPage: 0, wantPageSize: 0},
		{
			name:         "都提供",
			params:       api.ListPropertiesParams{Page: intPtr(3), PageSize: intPtr(10)},
			wantPage:     3,
			wantPageSize: 10,
		},
		{
			name:         "page 是負數原樣帶入交給 normalize 處理",
			params:       api.ListPropertiesParams{Page: intPtr(-5)},
			wantPage:     -5,
			wantPageSize: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toListFilter(tc.params)
			if got.Page != tc.wantPage {
				t.Fatalf("toListFilter(%+v).Page = %d, want %d", tc.params, got.Page, tc.wantPage)
			}
			if got.PageSize != tc.wantPageSize {
				t.Fatalf("toListFilter(%+v).PageSize = %d, want %d", tc.params, got.PageSize, tc.wantPageSize)
			}
		})
	}
}

// TestToListFilter_StatusAndRentalMode 驗證 Status／RentalMode 只有在值為
// 合法列舉值時才設定篩選條件；nil 或非法列舉值一律視為「不篩選」——產生的
// 綁定不會驗證 enum 成員，所以「非法列舉值」在這一層永遠是可能發生的輸入。
func TestToListFilter_StatusAndRentalMode(t *testing.T) {
	cases := []struct {
		name           string
		params         api.ListPropertiesParams
		wantStatus     *property.Status
		wantRentalMode *property.RentalMode
	}{
		{name: "都缺漏", params: api.ListPropertiesParams{}, wantStatus: nil, wantRentalMode: nil},
		{
			name:       "合法的 status",
			params:     api.ListPropertiesParams{Status: statusParamPtr(api.ListPropertiesParamsStatusVACANT)},
			wantStatus: statusPtr(property.StatusVacant),
		},
		{
			name:       "非法的 status 視為不篩選",
			params:     api.ListPropertiesParams{Status: statusParamPtr("NOT_A_STATUS")},
			wantStatus: nil,
		},
		{
			name:           "合法的 rental_mode",
			params:         api.ListPropertiesParams{RentalMode: rentalModeParamPtr(api.ListPropertiesParamsRentalModeMANAGED)},
			wantRentalMode: rentalModePtr(property.RentalModeManaged),
		},
		{
			name:           "非法的 rental_mode 視為不篩選",
			params:         api.ListPropertiesParams{RentalMode: rentalModeParamPtr("SUBLEASE")},
			wantRentalMode: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toListFilter(tc.params)

			if (got.Status == nil) != (tc.wantStatus == nil) {
				t.Fatalf("toListFilter(%+v).Status = %v, want %v", tc.params, got.Status, tc.wantStatus)
			}
			if tc.wantStatus != nil && *got.Status != *tc.wantStatus {
				t.Fatalf("toListFilter(%+v).Status = %v, want %v", tc.params, *got.Status, *tc.wantStatus)
			}

			if (got.RentalMode == nil) != (tc.wantRentalMode == nil) {
				t.Fatalf("toListFilter(%+v).RentalMode = %v, want %v", tc.params, got.RentalMode, tc.wantRentalMode)
			}
			if tc.wantRentalMode != nil && *got.RentalMode != *tc.wantRentalMode {
				t.Fatalf("toListFilter(%+v).RentalMode = %v, want %v", tc.params, *got.RentalMode, *tc.wantRentalMode)
			}
		})
	}
}

// TestToListFilter_City 驗證 City 為 nil 時視為空字串（代表不篩選），提供時原樣傳遞。
func TestToListFilter_City(t *testing.T) {
	cases := []struct {
		name     string
		params   api.ListPropertiesParams
		wantCity string
	}{
		{name: "缺漏", params: api.ListPropertiesParams{}, wantCity: ""},
		{name: "提供城市", params: api.ListPropertiesParams{City: stringPtr("臺北市")}, wantCity: "臺北市"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toListFilter(tc.params)
			if got.City != tc.wantCity {
				t.Fatalf("toListFilter(%+v).City = %q, want %q", tc.params, got.City, tc.wantCity)
			}
		})
	}
}

func intPtr(n int) *int { return &n }

func stringPtr(s string) *string { return &s }

func statusPtr(s property.Status) *property.Status { return &s }

func rentalModePtr(m property.RentalMode) *property.RentalMode { return &m }

func statusParamPtr(s api.ListPropertiesParamsStatus) *api.ListPropertiesParamsStatus { return &s }

func rentalModeParamPtr(m api.ListPropertiesParamsRentalMode) *api.ListPropertiesParamsRentalMode {
	return &m
}
