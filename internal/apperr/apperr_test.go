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
