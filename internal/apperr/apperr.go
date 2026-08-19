// Package apperr 定義服務全域唯一的應用層錯誤型別 Error，
// 帶有穩定、機器可讀的 Code，並提供 code 到 HTTP 狀態碼的唯一映射點 HTTPStatus。
package apperr

import "net/http"

// Code 是穩定的、機器可讀的錯誤代碼，會原封不動出現在 HTTP 錯誤回應的 code 欄位。
type Code string

const (
	// CodeValidationFailed 表示請求內容未通過驗證。
	CodeValidationFailed Code = "VALIDATION_FAILED"
	// CodePropertyNotFound 表示查無指定的物件（房源）。
	CodePropertyNotFound Code = "PROPERTY_NOT_FOUND"
	// CodePropertyDuplicate 表示物件已存在，重複建立。
	CodePropertyDuplicate Code = "PROPERTY_DUPLICATE"
	// CodeInvalidStatusTransition 表示物件狀態轉換不合法。
	CodeInvalidStatusTransition Code = "PROPERTY_INVALID_STATUS_TRANSITION"
	// CodeInternal 表示未預期的內部錯誤（含 panic 攔截後的降級結果）。
	CodeInternal Code = "INTERNAL"
)

// FieldError 是驗證失敗時，錯誤回應 details 陣列中的一個元素。
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Error 是服務內唯一的應用層錯誤型別。
//
// 所有需要回傳給呼叫端、帶有機器可讀 code 的錯誤都應該是（或包著）*Error。
// err 欄位刻意未匯出：呼叫端要透過 errors.Unwrap／errors.Is／errors.AsType 存取底層原因，
// 不應該直接讀寫它。
type Error struct {
	Code    Code
	Message string
	Details []FieldError
	err     error
}

// Error 實作 error 介面，回傳給人看的訊息。
func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// Unwrap 讓 errors.Is／errors.Unwrap／errors.AsType 能夠繼續往下拆出底層原因。
func (e *Error) Unwrap() error {
	return e.err
}

// New 建立一個沒有底層原因的應用層錯誤。
func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// Wrap 建立一個包住底層錯誤 err 的應用層錯誤；err 可透過 Unwrap 取回。
func Wrap(code Code, msg string, err error) *Error {
	return &Error{Code: code, Message: msg, err: err}
}

// Validation 建立一個 CodeValidationFailed 錯誤，details 會原封不動放進 Details，
// 供 HTTP 層輸出到錯誤回應的 details 陣列。
func Validation(details ...FieldError) *Error {
	return &Error{
		Code:    CodeValidationFailed,
		Message: "驗證失敗",
		Details: details,
	}
}

// HTTPStatus 是 code 到 HTTP 狀態碼的唯一映射點，服務中任何地方都不該自行
// 另外決定「這個 code 該回幾號狀態碼」。未知的 code 一律視為 500。
func HTTPStatus(code Code) int {
	switch code {
	case CodeValidationFailed:
		return http.StatusBadRequest
	case CodePropertyNotFound:
		return http.StatusNotFound
	case CodePropertyDuplicate:
		return http.StatusConflict
	case CodeInvalidStatusTransition:
		return http.StatusConflict
	case CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
