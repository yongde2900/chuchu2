package httpapi

import (
	"testing"

	"github.com/shopspring/decimal"
)

// TestFormatMoney 驗證金額格式化一律補齊到兩位小數 —— decimal.Decimal 預設的
// MarshalJSON 不會做這件事（25000.5 而非 25000.50），response DTO 必須自己
// 用 StringFixed(2) 明確格式化。
func TestFormatMoney(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "已經兩位小數", in: "25000.50", want: "25000.50"},
		{name: "只有一位小數要補零", in: "25000.5", want: "25000.50"},
		{name: "整數要補兩位零", in: "1200", want: "1200.00"},
		{name: "零", in: "0", want: "0.00"},
		{name: "超過兩位小數要四捨五入", in: "8.505", want: "8.51"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decimal.NewFromString(tc.in)
			if err != nil {
				t.Fatalf("decimal.NewFromString(%q) 失敗: %v", tc.in, err)
			}

			if got := formatMoney(d); got != tc.want {
				t.Fatalf("formatMoney(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
