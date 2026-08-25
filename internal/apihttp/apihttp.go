// Package apihttp 是產生的 HTTP 層與 apperr／httpx 之間的唯一接線點，
// 把 oapi-codegen 的三個 error hook 統一轉成 httpx.ErrorBody。
//
// 本套件不得 import 任何 feature 套件（health、httpapi、property），只認得
// api.StrictServerInterface——這讓它能被單元測試直接測，也讓 cmd/api
// 保持是唯一的組裝點。
package apihttp

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/httpx"
	"github.com/yongde2900/chuchu2/internal/server"
)

// Mount 把 si 接上三個錯誤 hook，組成完整的 chi 路由表。
func Mount(si api.StrictServerInterface, logger *slog.Logger) server.Mount {
	strict := api.NewStrictHandlerWithOptions(si, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  RequestErrorHandler(logger),
		ResponseErrorHandlerFunc: ResponseErrorHandler(logger),
	})

	return func(r chi.Router) {
		api.HandlerWithOptions(strict, api.ChiServerOptions{
			BaseRouter:       r,
			ErrorHandlerFunc: ParamErrorHandler(logger),
		})
	}
}

// ParamErrorHandler 接的是 api.ChiServerOptions.ErrorHandlerFunc：
// 路徑／查詢參數綁定失敗（{id} 不是合法 UUID、page 不是合法整數）。
func ParamErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		httpx.WriteError(w, r, logger, apperr.ValidationFailed.
			WithDetails(apperr.FieldError{Field: paramName(err), Reason: "格式不正確"}).
			WithError(err))
	}
}

// 五個產生的綁定錯誤型別各自帶 ParamName，那是 details[].field 的來源。
// 全部比對不到時回傳空字串，呼叫端仍照常產出 VALIDATION_FAILED 400。
func paramName(err error) string {
	if e, ok := errors.AsType[*api.InvalidParamFormatError](err); ok {
		return e.ParamName
	}
	if e, ok := errors.AsType[*api.RequiredParamError](err); ok {
		return e.ParamName
	}
	if e, ok := errors.AsType[*api.UnmarshalingParamError](err); ok {
		return e.ParamName
	}
	if e, ok := errors.AsType[*api.TooManyValuesForParamError](err); ok {
		return e.ParamName
	}
	if e, ok := errors.AsType[*api.RequiredHeaderError](err); ok {
		return e.ParamName
	}
	return ""
}

// RequestErrorHandler 接的是 api.StrictHTTPServerOptions.RequestErrorHandlerFunc：
// request body 解析失敗。原始 err 只包進 WithError 供 log 使用，
// 不會出現在回應 body 的 message 欄位。
func RequestErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		httpx.WriteError(w, r, logger, apperr.ValidationFailed.
			WithMessage("無法解析請求 JSON").
			WithError(err))
	}
}

// ResponseErrorHandler 接的是 api.StrictHTTPServerOptions.ResponseErrorHandlerFunc：
// handler 回傳的 error 落地的唯一地方。分類與降級都由 httpx.WriteError 負責。
func ResponseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		httpx.WriteError(w, r, logger, err)
	}
}
