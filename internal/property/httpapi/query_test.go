package httpapi

import (
	"net/url"
	"testing"

	"github.com/yongde2900/chuchu2/internal/property"
)

// TestParseListFilter_PageAndPageSize 驗證 page／page_size 的解析：
// 合法數字原樣帶入，缺漏或無法解析一律轉成 0（讓領域層的
// property.ListFilter.normalize 補上預設值），不視為錯誤。
func TestParseListFilter_PageAndPageSize(t *testing.T) {
	cases := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
	}{
		{name: "都缺漏", query: "", wantPage: 0, wantPageSize: 0},
		{name: "都是合法數字", query: "page=3&page_size=10", wantPage: 3, wantPageSize: 10},
		{name: "page 不是數字", query: "page=abc&page_size=10", wantPage: 0, wantPageSize: 10},
		{name: "page_size 不是數字", query: "page=3&page_size=xyz", wantPage: 3, wantPageSize: 0},
		{name: "page 是負數原樣帶入交給 normalize 處理", query: "page=-5", wantPage: -5, wantPageSize: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q) 失敗: %v", tc.query, err)
			}

			got := parseListFilter(q)
			if got.Page != tc.wantPage {
				t.Fatalf("parseListFilter(%q).Page = %d, want %d", tc.query, got.Page, tc.wantPage)
			}
			if got.PageSize != tc.wantPageSize {
				t.Fatalf("parseListFilter(%q).PageSize = %d, want %d", tc.query, got.PageSize, tc.wantPageSize)
			}
		})
	}
}

// TestParseListFilter_StatusAndRentalMode 驗證 status／rental_mode 只有在
// 值為合法列舉值時才設定篩選條件；缺漏或非法列舉值一律視為「不篩選」。
func TestParseListFilter_StatusAndRentalMode(t *testing.T) {
	cases := []struct {
		name           string
		query          string
		wantStatus     *property.Status
		wantRentalMode *property.RentalMode
	}{
		{name: "都缺漏", query: "", wantStatus: nil, wantRentalMode: nil},
		{
			name:       "合法的 status",
			query:      "status=VACANT",
			wantStatus: statusPtr(property.StatusVacant),
		},
		{
			name:       "非法的 status 視為不篩選",
			query:      "status=NOT_A_STATUS",
			wantStatus: nil,
		},
		{
			name:           "合法的 rental_mode",
			query:          "rental_mode=MANAGED",
			wantRentalMode: rentalModePtr(property.RentalModeManaged),
		},
		{
			name:           "非法的 rental_mode 視為不篩選",
			query:          "rental_mode=SUBLEASE",
			wantRentalMode: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q) 失敗: %v", tc.query, err)
			}

			got := parseListFilter(q)

			if (got.Status == nil) != (tc.wantStatus == nil) {
				t.Fatalf("parseListFilter(%q).Status = %v, want %v", tc.query, got.Status, tc.wantStatus)
			}
			if tc.wantStatus != nil && *got.Status != *tc.wantStatus {
				t.Fatalf("parseListFilter(%q).Status = %v, want %v", tc.query, *got.Status, *tc.wantStatus)
			}

			if (got.RentalMode == nil) != (tc.wantRentalMode == nil) {
				t.Fatalf("parseListFilter(%q).RentalMode = %v, want %v", tc.query, got.RentalMode, tc.wantRentalMode)
			}
			if tc.wantRentalMode != nil && *got.RentalMode != *tc.wantRentalMode {
				t.Fatalf("parseListFilter(%q).RentalMode = %v, want %v", tc.query, *got.RentalMode, *tc.wantRentalMode)
			}
		})
	}
}

// TestParseListFilter_City 驗證 city 原樣傳遞，缺漏時為空字串（代表不篩選）。
func TestParseListFilter_City(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		wantCity string
	}{
		{name: "缺漏", query: "", wantCity: ""},
		{name: "提供城市", query: "city=" + url.QueryEscape("臺北市"), wantCity: "臺北市"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("url.ParseQuery(%q) 失敗: %v", tc.query, err)
			}

			got := parseListFilter(q)
			if got.City != tc.wantCity {
				t.Fatalf("parseListFilter(%q).City = %q, want %q", tc.query, got.City, tc.wantCity)
			}
		})
	}
}

func statusPtr(s property.Status) *property.Status { return &s }

func rentalModePtr(m property.RentalMode) *property.RentalMode { return &m }
