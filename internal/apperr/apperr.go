// Package apperr 定義服務全域唯一的應用層錯誤型別 Error，
// 帶有穩定、機器可讀的 Code，並提供 code 到 HTTP 狀態碼的唯一映射點 HTTPStatus。
package apperr

import "net/http"

// Code 會原封不動出現在 HTTP 錯誤回應的 code 欄位，因此是對外契約的一部分。
type Code string

const (
	CodeValidationFailed        Code = "VALIDATION_FAILED"
	CodePropertyNotFound        Code = "PROPERTY_NOT_FOUND"
	CodePropertyDuplicate       Code = "PROPERTY_DUPLICATE"
	CodeInvalidStatusTransition Code = "PROPERTY_INVALID_STATUS_TRANSITION"
	// 未預期錯誤的降級目標，panic 攔截後也會落到這裡。
	CodeInternal Code = "INTERNAL"
)

// FieldError 對應錯誤回應 details 陣列中的一個元素。
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Error 是服務內唯一的應用層錯誤型別；要回傳給呼叫端的錯誤都應該是（或包著）*Error。
//
// err 刻意未匯出——底層原因一律透過 errors.Unwrap／Is／AsType 存取。
type Error struct {
	Code    Code
	Message string
	Details []FieldError
	err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	return e.err
}

func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

func Wrap(code Code, msg string, err error) *Error {
	return &Error{Code: code, Message: msg, err: err}
}

func Validation(details ...FieldError) *Error {
	return &Error{
		Code:    CodeValidationFailed,
		Message: "驗證失敗",
		Details: details,
	}
}

// 套件層級的共用 sentinel，每個 Code 一個。呼叫端用 errors.Is 判斷種類，
// 用 With* 衍生出帶請求上下文的新值。
//
// ⚠️ 這些值被所有請求併發共用，所以 With* 絕對不能就地修改接收者——
// 否則一個請求會把自己的底層錯誤寫進全域變數，污染後續所有請求。
var (
	ValidationFailed        = &Error{Code: CodeValidationFailed, Message: "驗證失敗"}
	NotFound                = &Error{Code: CodePropertyNotFound, Message: "找不到指定的物件"}
	Duplicate               = &Error{Code: CodePropertyDuplicate, Message: "相同門牌的物件已存在"}
	InvalidStatusTransition = &Error{Code: CodeInvalidStatusTransition, Message: "不允許的狀態轉換"}
	Internal                = &Error{Code: CodeInternal, Message: "internal server error"}
)

// Details 必須另外配置 backing array，不能只複製 slice header——
// 否則衍生值與 sentinel 會共用同一個陣列。
func (e *Error) clone() *Error {
	c := *e
	if e.Details != nil {
		c.Details = append([]FieldError(nil), e.Details...)
	}
	return &c
}

// 回傳新值而非就地修改，是 sentinel 能被併發共用的唯一理由：
// 若寫成 `e.err = err; return e`，兩個請求會互相覆蓋，連 sentinel 本身都會被污染。
func (e *Error) WithError(err error) *Error {
	c := e.clone()
	c.err = err
	return c
}

// 回傳新值，理由同 WithError。
func (e *Error) WithMessage(msg string) *Error {
	c := e.clone()
	c.Message = msg
	return c
}

// 回傳新值，理由同 WithError。details 另外複製一份，避免呼叫端事後修改傳入的
// slice 影響到回傳值。
func (e *Error) WithDetails(details ...FieldError) *Error {
	c := e.clone()
	c.Details = append([]FieldError(nil), details...)
	return c
}

// 沒有這個方法，errors.Is(derived, apperr.NotFound) 會因為 With* 回傳新指標
// 而一律 false，sentinel 就形同虛設。判定條件只看 Code。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// HTTPStatus 是 code → HTTP 狀態碼的唯一映射點，別處不要自行決定。
// 未知的 code 一律 500。
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
