package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/property"
)

// TestFormatMoney 驗證金額格式化一律補齊到兩位小數 —— decimal.Decimal 預設的
// MarshalJSON 不會做這件事（25000.5 而非 25000.50），toAPIProperty 必須自己
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

// TestToAPIProperty 驗證領域物件轉成 api.Property 時，欄位一對一映射，
// 且金額欄位固定兩位小數字串化。
func TestToAPIProperty(t *testing.T) {
	id := uuid.New()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	p := &property.Property{
		ID:            id,
		City:          "臺北市",
		District:      "大安區",
		StreetAddress: "復興南路一段 100 號",
		Floor:         "5",
		RoomNo:        "A",
		Layout:        property.LayoutIndependentSuite,
		AreaPing:      decimal.RequireFromString("8.5"),
		MonthlyRent:   decimal.RequireFromString("25000.5"),
		ManagementFee: decimal.RequireFromString("1200"),
		DepositMonths: 2,
		RentalMode:    property.RentalModeMasterLease,
		Status:        property.StatusVacant,
		LandlordName:  "王大明",
		LandlordPhone: "0912345678",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	got := toAPIProperty(p)

	want := api.Property{
		Id:            id,
		City:          "臺北市",
		District:      "大安區",
		StreetAddress: "復興南路一段 100 號",
		Floor:         "5",
		RoomNo:        "A",
		Layout:        api.PropertyLayoutINDEPENDENTSUITE,
		AreaPing:      "8.50",
		MonthlyRent:   "25000.50",
		ManagementFee: "1200.00",
		DepositMonths: 2,
		RentalMode:    api.PropertyRentalModeMASTERLEASE,
		Status:        api.PropertyStatusVACANT,
		LandlordName:  "王大明",
		LandlordPhone: "0912345678",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if got != want {
		t.Fatalf("toAPIProperty() = %+v, want %+v", got, want)
	}
}
