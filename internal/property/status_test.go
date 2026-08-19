package property

import "testing"

// TestCanTransition 窮舉 4×4＝16 種 (from, to) 組合，逐一斷言 CanTransition 的
// 回傳值，確保狀態機的權威定義（見 status.go）與這張表完全一致——特別是
// 對角線（同狀態轉換）全部應為非法，包含 VACANT→VACANT。
func TestCanTransition(t *testing.T) {
	all := []Status{StatusVacant, StatusOccupied, StatusRenovating, StatusDelisted}

	want := map[[2]Status]bool{
		{StatusVacant, StatusVacant}:         false,
		{StatusVacant, StatusOccupied}:       true,
		{StatusVacant, StatusRenovating}:     true,
		{StatusVacant, StatusDelisted}:       true,
		{StatusOccupied, StatusVacant}:       true,
		{StatusOccupied, StatusOccupied}:     false,
		{StatusOccupied, StatusRenovating}:   false,
		{StatusOccupied, StatusDelisted}:     false,
		{StatusRenovating, StatusVacant}:     true,
		{StatusRenovating, StatusOccupied}:   false,
		{StatusRenovating, StatusRenovating}: false,
		{StatusRenovating, StatusDelisted}:   true,
		{StatusDelisted, StatusVacant}:       true,
		{StatusDelisted, StatusOccupied}:     false,
		{StatusDelisted, StatusRenovating}:   false,
		{StatusDelisted, StatusDelisted}:     false,
	}

	for _, from := range all {
		for _, to := range all {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				wantVal, ok := want[[2]Status{from, to}]
				if !ok {
					t.Fatalf("測試表格缺少 (%s, %s) 這一格", from, to)
				}
				if got := CanTransition(from, to); got != wantVal {
					t.Fatalf("CanTransition(%s, %s) = %v, want %v", from, to, got, wantVal)
				}
			})
		}
	}
}
