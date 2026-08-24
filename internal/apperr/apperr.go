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

// 套件層級的共用 sentinel，每一個 Code 一個。
//
// 呼叫端以 errors.Is(err, apperr.NotFound) 判斷錯誤種類，並以 With* 方法從 sentinel
// 衍生出帶有請求專屬上下文（底層原因、訊息、details）的新值。因為這些 sentinel 會被整個
// 服務的所有請求併發共用，With* 絕對不能就地修改接收者——否則一個請求就會把自己的底層錯誤
// 寫進全域變數，污染到後續所有請求。詳見 With* 方法上的註解。
var (
	ValidationFailed        = &Error{Code: CodeValidationFailed, Message: "驗證失敗"}
	NotFound                = &Error{Code: CodePropertyNotFound, Message: "找不到指定的物件"}
	Duplicate               = &Error{Code: CodePropertyDuplicate, Message: "相同門牌的物件已存在"}
	InvalidStatusTransition = &Error{Code: CodeInvalidStatusTransition, Message: "不允許的狀態轉換"}
	Internal                = &Error{Code: CodeInternal, Message: "internal server error"}
)

// clone 回傳 e 的一份淺層複製，但 Details 會另外配置一份新的 backing array
// （而不是只複製 slice header），確保衍生值與原值（尤其是套件層級的 sentinel）
// 在 Details 上完全獨立、互不影響。
func (e *Error) clone() *Error {
	c := *e
	if e.Details != nil {
		c.Details = append([]FieldError(nil), e.Details...)
	}
	return &c
}

// WithError 回傳一個以 e 為底、底層原因換成 err 的新 *Error，e 本身不受影響。
//
// 這是 sentinel（如 apperr.NotFound）能被整個服務併發安全共用的唯一理由：
// 如果這裡直接 `e.err = err` 再回傳 e，兩個請求各自呼叫 apperr.NotFound.WithError(...)
// 就會互相覆蓋對方的底層錯誤，而且因為是同一個指標，連 apperr.NotFound 這個全域值
// 自己都會被污染——下一個請求即使沒有呼叫 WithError，也會看到別人留下的底層原因。
func (e *Error) WithError(err error) *Error {
	c := e.clone()
	c.err = err
	return c
}

// WithMessage 回傳一個以 e 為底、Message 換成 msg 的新 *Error，e 本身不受影響。
// 理由同 WithError：sentinel 是共用值，就地修改會污染全域狀態。
func (e *Error) WithMessage(msg string) *Error {
	c := e.clone()
	c.Message = msg
	return c
}

// WithDetails 回傳一個以 e 為底、Details 換成 details 的新 *Error，e 本身不受影響。
// details 會複製進一份新的 backing array，避免呼叫端事後修改傳入的 slice
// 時意外影響到回傳值（或反過來影響到呼叫端）。
func (e *Error) WithDetails(details ...FieldError) *Error {
	c := e.clone()
	c.Details = append([]FieldError(nil), details...)
	return c
}

// Is 讓 errors.Is 能夠判斷「由某個 sentinel 衍生出來的副本」仍然算是同一種錯誤。
// With* 回傳的是全新的 *Error 指標，若沒有這個方法，errors.Is(derived, apperr.NotFound)
// 會因為指標不同而一律回傳 false，sentinel 就形同虛設。判定條件單純看 Code 是否相同。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
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
