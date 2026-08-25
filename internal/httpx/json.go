package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yongde2900/chuchu2/internal/apperr"
)

// ErrorBody 是所有錯誤回應共用的統一形狀，也是對外契約的一部分。
type ErrorBody struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	RequestID string              `json:"request_id"`
	Details   []apperr.FieldError `json:"details,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError 是錯誤 → HTTP 回應的唯一出口。
//
// 錯誤鏈中取不到 *apperr.Error 時，代表這是未預期、沒有機器可讀 code 的錯誤，
// 一律降級為 INTERNAL 500，**原始訊息只進 log，不外洩給呼叫端**。
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
