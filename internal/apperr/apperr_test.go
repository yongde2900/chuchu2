package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// TestHTTPStatus 驗證 code 到 HTTP 狀態碼的完整映射表，
// HTTPStatus 是這個映射的唯一入口。
func TestHTTPStatus(t *testing.T) {
	cases := []struct {
		code Code
		want int
	}{
		{CodeValidationFailed, http.StatusBadRequest},
		{CodePropertyNotFound, http.StatusNotFound},
		{CodePropertyDuplicate, http.StatusConflict},
		{CodeInvalidStatusTransition, http.StatusConflict},
		{CodeInternal, http.StatusInternalServerError},
		{Code("SOME_UNKNOWN_CODE"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			if got := HTTPStatus(tc.code); got != tc.want {
				t.Fatalf("HTTPStatus(%q) = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}

// TestError_Unwrap 驗證 Wrap 建立的錯誤能被 errors.Unwrap 拆出底層錯誤。
func TestError_Unwrap(t *testing.T) {
	cause := errors.New("底層原因")
	wrapped := Wrap(CodeInternal, "包裝訊息", cause)

	if got := errors.Unwrap(wrapped); got != cause {
		t.Fatalf("errors.Unwrap(wrapped) = %v, want %v", got, cause)
	}

	if !errors.Is(wrapped, cause) {
		t.Fatalf("errors.Is(wrapped, cause) = false, want true")
	}
}

// TestError_AsType 驗證 New 建立的應用層錯誤能用 errors.AsType 從一個
// 更上層被包裝過的 error 中取出來（本專案慣例：優先用 errors.AsType，不用 errors.As）。
func TestError_AsType(t *testing.T) {
	appErr := New(CodePropertyNotFound, "找不到物件")
	outer := fmt.Errorf("包了一層: %w", appErr)

	got, ok := errors.AsType[*Error](outer)
	if !ok {
		t.Fatalf("errors.AsType[*Error](outer) ok = false, want true")
	}
	if got.Code != CodePropertyNotFound {
		t.Fatalf("got.Code = %q, want %q", got.Code, CodePropertyNotFound)
	}

	plain := errors.New("跟 apperr 完全無關的錯誤")
	if _, ok := errors.AsType[*Error](plain); ok {
		t.Fatalf("errors.AsType[*Error](plain) ok = true, want false")
	}
}

// TestError_Error 驗證 Error() 回傳非空訊息。
func TestError_Error(t *testing.T) {
	err := New(CodeValidationFailed, "驗證失敗訊息")
	if got := err.Error(); got != "驗證失敗訊息" {
		t.Fatalf("err.Error() = %q, want %q", got, "驗證失敗訊息")
	}
}

// TestValidation 驗證 Validation 建構子回傳 CodeValidationFailed 且帶入的
// details 會原封不動放進 Details。
func TestValidation(t *testing.T) {
	details := []FieldError{
		{Field: "name", Reason: "required"},
		{Field: "age", Reason: "must be positive"},
	}
	err := Validation(details...)

	if err.Code != CodeValidationFailed {
		t.Fatalf("err.Code = %q, want %q", err.Code, CodeValidationFailed)
	}
	if len(err.Details) != 2 {
		t.Fatalf("len(err.Details) = %d, want 2", len(err.Details))
	}
	if err.Details[0] != details[0] || err.Details[1] != details[1] {
		t.Fatalf("err.Details = %+v, want %+v", err.Details, details)
	}
}

// TestSentinel_WithError_DoesNotPollute 驗證 apperr.NotFound 這類套件層級的
// sentinel 被兩個不同的底層錯誤各自呼叫 WithError 時，得到兩個獨立的值，
// 而且 sentinel 本身的底層錯誤不會被任何一次呼叫改動——這正是 BDD-002
// 「apperr 的共用 sentinel 不會被 WithError 汙染」這條 scenario 要驗證的行為。
func TestSentinel_WithError_DoesNotPollute(t *testing.T) {
	causeA := errors.New("底層原因 A")
	causeB := errors.New("底層原因 B")

	derivedA := NotFound.WithError(causeA)
	derivedB := NotFound.WithError(causeB)

	// 1. 兩次 WithError 得到兩個不同的指標。
	if derivedA == derivedB {
		t.Fatalf("derivedA and derivedB share the same pointer, want distinct")
	}
	if derivedA == NotFound || derivedB == NotFound {
		t.Fatalf("WithError returned the sentinel itself, want a new *Error")
	}

	// 2. 各自 errors.Unwrap 拿到自己的底層錯誤。
	if got := errors.Unwrap(derivedA); got != causeA {
		t.Fatalf("errors.Unwrap(derivedA) = %v, want %v", got, causeA)
	}
	if got := errors.Unwrap(derivedB); got != causeB {
		t.Fatalf("errors.Unwrap(derivedB) = %v, want %v", got, causeB)
	}

	// 3. apperr.NotFound 本身 Unwrap() 仍為 nil。
	if got := NotFound.Unwrap(); got != nil {
		t.Fatalf("NotFound.Unwrap() = %v, want nil (sentinel must stay unpolluted)", got)
	}

	// 4. errors.Is(derived, apperr.NotFound) 為 true。
	if !errors.Is(derivedA, NotFound) {
		t.Fatalf("errors.Is(derivedA, NotFound) = false, want true")
	}
	if !errors.Is(derivedB, NotFound) {
		t.Fatalf("errors.Is(derivedB, NotFound) = false, want true")
	}
}

// TestSentinel_WithMessage_DoesNotPollute 驗證 WithMessage 回傳的是帶新 Message
// 的獨立值，不會改到 sentinel 本身的 Message。
func TestSentinel_WithMessage_DoesNotPollute(t *testing.T) {
	originalMessage := NotFound.Message

	derived := NotFound.WithMessage("找不到這間房源")

	if derived == NotFound {
		t.Fatalf("WithMessage returned the sentinel itself, want a new *Error")
	}
	if derived.Message != "找不到這間房源" {
		t.Fatalf("derived.Message = %q, want %q", derived.Message, "找不到這間房源")
	}
	if NotFound.Message != originalMessage {
		t.Fatalf("NotFound.Message = %q, want unchanged %q", NotFound.Message, originalMessage)
	}
	if !errors.Is(derived, NotFound) {
		t.Fatalf("errors.Is(derived, NotFound) = false, want true")
	}
}

// TestSentinel_WithDetails_DoesNotShareBackingArray 驗證 WithDetails 不會把
// details 寫回 sentinel，而且兩個從同一個 sentinel 衍生出來的值必須各自擁有
// 獨立的 backing array——只比對長度抓不到「只複製 slice header」這種 bug，
// 必須實際對其中一個 append 之後，斷言另一個沒被動到。
func TestSentinel_WithDetails_DoesNotShareBackingArray(t *testing.T) {
	if len(ValidationFailed.Details) != 0 {
		t.Fatalf("ValidationFailed.Details = %+v, want empty before any WithDetails call", ValidationFailed.Details)
	}

	detailA := FieldError{Field: "name", Reason: "required"}
	detailB := FieldError{Field: "age", Reason: "must be positive"}

	derivedA := ValidationFailed.WithDetails(detailA)
	derivedB := ValidationFailed.WithDetails(detailA)

	// sentinel 本身的 Details 不會被 WithDetails 汙染。
	if len(ValidationFailed.Details) != 0 {
		t.Fatalf("ValidationFailed.Details = %+v, want still empty after WithDetails calls", ValidationFailed.Details)
	}

	// 對 derivedA 的 Details 做 append，藉此在其 backing array 還有多餘容量時
	// 覆寫後續記憶體；若 derivedB 與 derivedA 共用 backing array，這裡就會
	// 意外污染到 derivedB。
	derivedA.Details = append(derivedA.Details, detailB)

	if len(derivedB.Details) != 1 {
		t.Fatalf("len(derivedB.Details) = %d, want 1 (must be unaffected by append on derivedA)", len(derivedB.Details))
	}
	if derivedB.Details[0] != detailA {
		t.Fatalf("derivedB.Details[0] = %+v, want %+v (must be unaffected by append on derivedA)", derivedB.Details[0], detailA)
	}
	if len(derivedA.Details) != 2 {
		t.Fatalf("len(derivedA.Details) = %d, want 2", len(derivedA.Details))
	}

	if !errors.Is(derivedA, ValidationFailed) {
		t.Fatalf("errors.Is(derivedA, ValidationFailed) = false, want true")
	}
}

// TestSentinel_Clone_DoesNotAliasDetailsBackingArray 直接鎖定 clone() 本身「Details
// 要另外配置新的 backing array，不能只複製 slice header」這個責任。
//
// 刻意繞開 WithDetails：WithDetails 每次都會用 append([]FieldError(nil), details...)
// 整個重建 Details，因此即使 clone() 退化成 `c := *e; return &c`（完全不複製
// Details），也會被 WithDetails 事後的重建蓋掉，測不出問題。這裡改用 WithMessage——
// 它只換 Message，Details 完全依賴 clone() 複製——才能真的暴露 clone() 有沒有做對。
//
// 另外刻意讓 base.Details 保留 spare capacity（len < cap），這樣衍生值上的 append
// 才有機會落在 base 底層陣列「已配置但未使用」的區段，驗證不會反過來污染 base。
func TestSentinel_Clone_DoesNotAliasDetailsBackingArray(t *testing.T) {
	base := &Error{
		Code:    CodeValidationFailed,
		Message: "base",
		Details: append(make([]FieldError, 0, 8), FieldError{Field: "name", Reason: "required"}),
	}

	derived := base.WithMessage("derived")

	// 對衍生值的 Details 做「原地」索引寫入——這一定會落在既有 backing array 上，
	// 不受 append 是否觸發重新配置影響——驗證不會反過來動到 base。
	derived.Details[0] = FieldError{Field: "MUTATED", Reason: "MUTATED"}
	if base.Details[0].Field == "MUTATED" {
		t.Fatalf("mutating derived.Details[0] also mutated base.Details[0]; clone() shares Details backing array with base")
	}

	// 另外用 append 驗證 base 底層陣列裡「len 之後、cap 之前」尚未使用的空間也不會
	// 被衍生值的 append 動到——只比對 len(base.Details) 抓不到這種 bug，因為 append
	// 是否共用 backing array 不會改變 base.Details 這個 slice header 本身的 len。
	derived2 := base.WithMessage("derived2")
	derived2.Details = append(derived2.Details, FieldError{Field: "extra", Reason: "extra"})
	baseFullCap := base.Details[:cap(base.Details)]
	if len(baseFullCap) > 1 && baseFullCap[1].Field == "extra" {
		t.Fatalf("append on derived2.Details wrote into base's backing array beyond base's own length")
	}
	if len(base.Details) != 1 {
		t.Fatalf("len(base.Details) = %d, want 1 (unchanged)", len(base.Details))
	}
}

// TestSentinel_Clone_DerivedFromDerived_DoesNotAliasDetails 鎖定 reviewer 指出的
// 「衍生自衍生值」路徑：ValidationFailed.WithDetails(d).WithMessage("x")。
// 第二次的 WithMessage 一樣是透過 clone() 複製 Details，不能與第一次衍生出來的值
// 共用 backing array，否則對第二個值的 Details 原地寫入會回頭污染第一個值。
func TestSentinel_Clone_DerivedFromDerived_DoesNotAliasDetails(t *testing.T) {
	d := FieldError{Field: "name", Reason: "required"}

	first := ValidationFailed.WithDetails(d)
	second := first.WithMessage("second")

	second.Details[0] = FieldError{Field: "MUTATED", Reason: "MUTATED"}
	if first.Details[0].Field == "MUTATED" {
		t.Fatalf("mutating second.Details[0] (derived from first via WithMessage) also mutated first.Details[0]; clone() shares Details backing array across derivations")
	}
	if !errors.Is(second, ValidationFailed) {
		t.Fatalf("errors.Is(second, ValidationFailed) = false, want true")
	}
}

// TestSentinel_Is_MatchesByCodeAcrossDistinctSentinels 驗證 Is 是「target 也是
// *Error 且 Code 相同」這個判定，因此不同 Code 的 sentinel 彼此不應該互相匹配。
func TestSentinel_Is_MatchesByCodeAcrossDistinctSentinels(t *testing.T) {
	derived := NotFound.WithError(errors.New("cause"))

	if errors.Is(derived, Duplicate) {
		t.Fatalf("errors.Is(derived, Duplicate) = true, want false (different Code)")
	}
	if errors.Is(derived, InvalidStatusTransition) {
		t.Fatalf("errors.Is(derived, InvalidStatusTransition) = true, want false (different Code)")
	}
	if errors.Is(derived, Internal) {
		t.Fatalf("errors.Is(derived, Internal) = true, want false (different Code)")
	}
}
