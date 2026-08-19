package property

import "testing"

// TestListFilter_Normalize 驗證 Page／PageSize 的預設值與上限夾限邏輯：
// Page 非正數時預設為 1；PageSize 非正數時預設為 20，且不得超過 100。
func TestListFilter_Normalize(t *testing.T) {
	cases := []struct {
		name         string
		in           ListFilter
		wantPage     int
		wantPageSize int
	}{
		{
			name:         "零值全部補上預設值",
			in:           ListFilter{},
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "Page 為負數視為第 1 頁",
			in:           ListFilter{Page: -5, PageSize: 20},
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "Page 為 0 視為第 1 頁",
			in:           ListFilter{Page: 0, PageSize: 20},
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "Page 為正整數維持原值",
			in:           ListFilter{Page: 3, PageSize: 20},
			wantPage:     3,
			wantPageSize: 20,
		},
		{
			name:         "PageSize 為 0 補預設值 20",
			in:           ListFilter{Page: 1, PageSize: 0},
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "PageSize 為負數補預設值 20",
			in:           ListFilter{Page: 1, PageSize: -10},
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "PageSize 未超過上限維持原值",
			in:           ListFilter{Page: 1, PageSize: 50},
			wantPage:     1,
			wantPageSize: 50,
		},
		{
			name:         "PageSize 剛好等於上限維持原值",
			in:           ListFilter{Page: 1, PageSize: 100},
			wantPage:     1,
			wantPageSize: 100,
		},
		{
			name:         "PageSize 超過上限夾限為 100",
			in:           ListFilter{Page: 1, PageSize: 101},
			wantPage:     1,
			wantPageSize: 100,
		},
		{
			name:         "PageSize 遠超過上限也夾限為 100",
			in:           ListFilter{Page: 1, PageSize: 100000},
			wantPage:     1,
			wantPageSize: 100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.normalize()
			if got.Page != tc.wantPage {
				t.Fatalf("normalize().Page = %d, want %d", got.Page, tc.wantPage)
			}
			if got.PageSize != tc.wantPageSize {
				t.Fatalf("normalize().PageSize = %d, want %d", got.PageSize, tc.wantPageSize)
			}
		})
	}
}

// TestListFilter_Normalize_PreservesFilterFields 驗證 normalize 只調整
// Page／PageSize，不會動到 Status／RentalMode／City 篩選條件本身。
func TestListFilter_Normalize_PreservesFilterFields(t *testing.T) {
	status := StatusVacant
	mode := RentalModeManaged

	in := ListFilter{
		Page:       0,
		PageSize:   0,
		Status:     &status,
		RentalMode: &mode,
		City:       "臺北市",
	}

	got := in.normalize()

	if got.Status != &status {
		t.Fatalf("normalize().Status = %v, want %v (同一個指標)", got.Status, &status)
	}
	if got.RentalMode != &mode {
		t.Fatalf("normalize().RentalMode = %v, want %v (同一個指標)", got.RentalMode, &mode)
	}
	if got.City != "臺北市" {
		t.Fatalf("normalize().City = %q, want %q", got.City, "臺北市")
	}
}
