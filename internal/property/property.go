// Package property 是「物件（房源）」的領域層：型別、驗證規則與 Service。
//
// 這一層刻意不 import github.com/uptrace/bun（持久化細節屬於 pgrepo 子套件）、
// 也不 import net/http（HTTP 轉譯屬於 httpapi 子套件），維持領域邏輯與框架無關。
package property

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RentalMode 區分「包租」與「代管」兩種法律上截然不同的營運模式：
//   - 包租（MASTER_LEASE）：業者向房東承租整個物件，不論是否出租都要付房東固定
//     租金，再轉租給房客。存在兩份租約，空置風險由業者承擔。
//   - 代管（MANAGED）：業者只負責管理，租約只有一份（房東↔房客），業者收管理
//     服務費。空置風險由房東承擔。
type RentalMode string

const (
	// RentalModeMasterLease 是「包租」。
	RentalModeMasterLease RentalMode = "MASTER_LEASE"
	// RentalModeManaged 是「代管」。
	RentalModeManaged RentalMode = "MANAGED"
)

// Valid 回報 m 是否為合法的 RentalMode 列舉值。
func (m RentalMode) Valid() bool {
	switch m {
	case RentalModeMasterLease, RentalModeManaged:
		return true
	default:
		return false
	}
}

// Status 是物件目前的狀態。新建物件一律是 StatusVacant；其餘狀態與轉換規則
// 由 Task 7 的 Update／ChangeStatus 負責，本套件現階段只宣告列舉值。
type Status string

const (
	// StatusVacant 表示空置中。
	StatusVacant Status = "VACANT"
	// StatusOccupied 表示已出租／已入住。
	StatusOccupied Status = "OCCUPIED"
	// StatusRenovating 表示整修中。
	StatusRenovating Status = "RENOVATING"
	// StatusDelisted 表示已下架。
	StatusDelisted Status = "DELISTED"
)

// Valid 回報 s 是否為合法的 Status 列舉值。
func (s Status) Valid() bool {
	switch s {
	case StatusVacant, StatusOccupied, StatusRenovating, StatusDelisted:
		return true
	default:
		return false
	}
}

// Layout 是物件的格局。
type Layout string

const (
	// LayoutWholeUnit 是整層住家。
	LayoutWholeUnit Layout = "WHOLE_UNIT"
	// LayoutIndependentSuite 是獨立套房。
	LayoutIndependentSuite Layout = "INDEPENDENT_SUITE"
	// LayoutSharedSuite 是分租套房。
	LayoutSharedSuite Layout = "SHARED_SUITE"
	// LayoutSingleRoom 是雅房。
	LayoutSingleRoom Layout = "SINGLE_ROOM"
)

// Valid 回報 l 是否為合法的 Layout 列舉值。
func (l Layout) Valid() bool {
	switch l {
	case LayoutWholeUnit, LayoutIndependentSuite, LayoutSharedSuite, LayoutSingleRoom:
		return true
	default:
		return false
	}
}

// Property 是包租代管服務的物件（房源）主檔。
//
// 已知且刻意接受的模型取捨（見專案 CLAUDE.md）：
//   - 「包租 vs 代管」嚴格來說是委託契約的屬性而非房屋單位的屬性，本輪刻意
//     扁平化成 RentalMode 這個欄位，不建委託契約資料表。
//   - 房東資訊（LandlordName／LandlordPhone）本輪內嵌在物件上，刻意沒有房東
//     資料表、沒有外鍵。
type Property struct {
	ID            uuid.UUID
	City          string
	District      string
	StreetAddress string
	Floor         string
	RoomNo        string
	Layout        Layout
	AreaPing      decimal.Decimal
	MonthlyRent   decimal.Decimal
	ManagementFee decimal.Decimal
	DepositMonths int
	RentalMode    RentalMode
	Status        Status
	LandlordName  string
	LandlordPhone string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
