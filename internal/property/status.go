package property

// transitions 是狀態機的唯一權威定義。不在集合中的目標一律非法，
// 包含起始狀態本身——同狀態轉換全部非法是刻意的，不是漏寫。
//
// 兩條不是自明的業務規則：
//   - OCCUPIED 只能回到 VACANT。出租中的物件必須先退租才能整修或下架，
//     避免在還有房客的狀態下被下架。
//   - DELISTED 只能回到 VACANT。下架物件要重新出租或整修，一律先經過空置。
var transitions = map[Status]map[Status]bool{
	StatusVacant: {
		StatusOccupied:   true,
		StatusRenovating: true,
		StatusDelisted:   true,
	},
	StatusOccupied: {
		StatusVacant: true,
	},
	StatusRenovating: {
		StatusVacant:   true,
		StatusDelisted: true,
	},
	StatusDelisted: {
		StatusVacant: true,
	},
}

// CanTransition 是狀態機規則的唯一權威來源——需要判斷轉換合不合法的程式碼
// 一律呼叫它，不要自行另外判斷。from == to 一律回傳 false。
func CanTransition(from, to Status) bool {
	return transitions[from][to]
}
