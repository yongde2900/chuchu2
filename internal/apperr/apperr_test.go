package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestHTTPStatus(t *testing.T) {
	cases := []struct {
		code Code
		want int
	}{
		{CodeValidationFailed, http.StatusBadRequest},
		{CodePropertyNotFound, http.StatusNotFound},
		{CodePropertyDuplicate, http.StatusConflict},
		{CodeInvalidStatusTransition, http.StatusConflict},
		{CodeLineSignatureInvalid, http.StatusUnauthorized},
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

func TestError_Error(t *testing.T) {
	err := New(CodeValidationFailed, "驗證失敗訊息")
	if got := err.Error(); got != "驗證失敗訊息" {
		t.Fatalf("err.Error() = %q, want %q", got, "驗證失敗訊息")
	}
}

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

// sentinel 被兩個不同底層錯誤各自 WithError 後，必須得到兩個獨立的值，
// 且 sentinel 本身不受影響——否則會造成跨請求的資料污染。
func TestSentinel_WithError_DoesNotPollute(t *testing.T) {
	causeA := errors.New("底層原因 A")
	causeB := errors.New("底層原因 B")

	derivedA := NotFound.WithError(causeA)
	derivedB := NotFound.WithError(causeB)

	if derivedA == derivedB {
		t.Fatalf("derivedA and derivedB share the same pointer, want distinct")
	}
	if derivedA == NotFound || derivedB == NotFound {
		t.Fatalf("WithError returned the sentinel itself, want a new *Error")
	}

	if got := errors.Unwrap(derivedA); got != causeA {
		t.Fatalf("errors.Unwrap(derivedA) = %v, want %v", got, causeA)
	}
	if got := errors.Unwrap(derivedB); got != causeB {
		t.Fatalf("errors.Unwrap(derivedB) = %v, want %v", got, causeB)
	}

	if got := NotFound.Unwrap(); got != nil {
		t.Fatalf("NotFound.Unwrap() = %v, want nil (sentinel must stay unpolluted)", got)
	}

	if !errors.Is(derivedA, NotFound) {
		t.Fatalf("errors.Is(derivedA, NotFound) = false, want true")
	}
	if !errors.Is(derivedB, NotFound) {
		t.Fatalf("errors.Is(derivedB, NotFound) = false, want true")
	}
}

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

// 只比對長度抓不到「只複製 slice header」這種 bug，
// 必須實際對其中一個 append 之後，斷言另一個沒被動到。
func TestSentinel_WithDetails_DoesNotShareBackingArray(t *testing.T) {
	if len(ValidationFailed.Details) != 0 {
		t.Fatalf("ValidationFailed.Details = %+v, want empty before any WithDetails call", ValidationFailed.Details)
	}

	detailA := FieldError{Field: "name", Reason: "required"}
	detailB := FieldError{Field: "age", Reason: "must be positive"}

	derivedA := ValidationFailed.WithDetails(detailA)
	derivedB := ValidationFailed.WithDetails(detailA)

	if len(ValidationFailed.Details) != 0 {
		t.Fatalf("ValidationFailed.Details = %+v, want still empty after WithDetails calls", ValidationFailed.Details)
	}

	// 若 derivedB 與 derivedA 共用 backing array，這個 append 會污染到 derivedB。
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

// 鎖定 clone() 本身「Details 必須另外配置 backing array」的責任。
//
// ⚠️ 刻意繞開 WithDetails：它每次都用 append(nil, details...) 整個重建 Details，
// 即使 clone() 退化成 `c := *e; return &c` 也會被事後重建蓋掉，測不出問題。
// 改用 WithMessage——它只換 Message，Details 完全依賴 clone()。
//
// base.Details 也刻意保留 spare capacity（len < cap），衍生值的 append 才有機會
// 落在 base 底層陣列已配置但未使用的區段。
func TestSentinel_Clone_DoesNotAliasDetailsBackingArray(t *testing.T) {
	base := &Error{
		Code:    CodeValidationFailed,
		Message: "base",
		Details: append(make([]FieldError, 0, 8), FieldError{Field: "name", Reason: "required"}),
	}

	derived := base.WithMessage("derived")

	// 原地索引寫入必定落在既有 backing array 上，不受 append 是否重新配置影響。
	derived.Details[0] = FieldError{Field: "MUTATED", Reason: "MUTATED"}
	if base.Details[0].Field == "MUTATED" {
		t.Fatalf("mutating derived.Details[0] also mutated base.Details[0]; clone() shares Details backing array with base")
	}

	// 只比對 len(base.Details) 抓不到這種 bug：append 是否共用 backing array
	// 不會改變 base.Details 這個 slice header 的 len。
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

// 「衍生自衍生值」路徑：WithDetails(d).WithMessage("x")。第二次 clone() 一樣
// 不能與第一次的衍生值共用 backing array。
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

// Is 的判定是「target 也是 *Error 且 Code 相同」，不同 Code 的 sentinel 不該互相匹配。
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
