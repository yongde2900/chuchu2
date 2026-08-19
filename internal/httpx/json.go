package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// ErrorBody 是所有錯誤回應共用的統一形狀。
type ErrorBody struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	RequestID string              `json:"request_id"`
	Details   []apperr.FieldError `json:"details,omitempty"`
}

// WriteJSON 把 v 編碼成 JSON 寫進回應，並設定狀態碼與 Content-Type。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError 把 err 轉成統一的錯誤回應寫出去。
//
// 用 errors.AsType[*apperr.Error] 從 err 的錯誤鏈中取出應用層錯誤（本專案 Go 1.26
// 慣例：優先用 errors.AsType，不用 errors.As）；取不到時，代表這是一個未預期、
// 沒有機器可讀 code 的錯誤，一律降級為 INTERNAL 500，不外洩原始錯誤內容給呼叫端。
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	requestID := RequestIDFrom(r.Context())

	appErr, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		logger.ErrorContext(r.Context(), "未分類的錯誤，降級為 INTERNAL",
			"request_id", requestID,
			"error", err.Error(),
		)
		appErr = apperr.New(apperr.CodeInternal, "internal server error")
	}

	WriteJSON(w, apperr.HTTPStatus(appErr.Code), ErrorBody{
		Code:      string(appErr.Code),
		Message:   appErr.Message,
		RequestID: requestID,
		Details:   appErr.Details,
	})
}

// DecodeJSON 把 request body 解析成 T。解析失敗時回傳一個
// CodeValidationFailed 的 *apperr.Error，包住原始的解析錯誤。
func DecodeJSON[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		var zero T
		return zero, apperr.Wrap(apperr.CodeValidationFailed, "無法解析請求 JSON", err)
	}
	return v, nil
}
