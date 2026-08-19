package property

// transitions 是物件狀態機的唯一權威定義：key 是起始狀態，value 是「從該狀態
// 可以直接轉換到」的目標狀態集合。不在集合中的目標狀態一律視為非法，
// 包含起始狀態本身（同狀態轉換全部非法，不會出現在任何一個內層 map 裡）。
//
// 業務理由（對應 PLAN 的狀態機表）：
//   - VACANT → OCCUPIED：空置物件成功出租／入住。
//   - VACANT → RENOVATING：空置物件排入整修排程。
//   - VACANT → DELISTED：空置物件暫停對外招租、下架。
//   - OCCUPIED → VACANT：房客退租，物件恢復空置。出租中的物件不能直接跳到
//     RENOVATING 或 DELISTED——必須先退租（回到空置）才能整修或下架，
//     避免在還有房客的狀態下被下架或整修。
//   - RENOVATING → VACANT：整修完成，恢復可招租狀態。
//   - RENOVATING → DELISTED：整修中途決定直接下架，不需要先繞回空置。
//   - DELISTED → VACANT：重新上架。已下架的物件必須先回到空置，才能再走
//     VACANT 的其他合法轉換（出租／整修／再次下架），不允許下架物件直接
//     跳到 OCCUPIED 或 RENOVATING。
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

// CanTransition 回報是否允許把物件狀態從 from 直接轉換到 to。
//
// 這是狀態機規則的唯一權威來源——Service.ChangeStatus 與任何未來需要判斷
// 「這個轉換合不合法」的程式碼都應該呼叫這個函式，不應該自行另外判斷。
// 同狀態轉換（from == to）一律回傳 false。
func CanTransition(from, to Status) bool {
	return transitions[from][to]
}
