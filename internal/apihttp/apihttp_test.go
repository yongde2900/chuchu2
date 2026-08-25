// 本檔是 apihttp 套件的單元測試：直接呼叫三個錯誤 hook（不透過真正跑起來的
// HTTP server），驗證它們各自把對應的錯誤轉成統一形狀的 JSON 回應。
package apihttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/apperr"
)

// errorBody 對應 httpx.ErrorBody 的形狀，這裡不直接 import internal/httpx
// 的型別，用一個等價的匿名結構驗證回應「形狀」本身也符合契約。
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Field  string `json:"field"`
		Reason string `json:"reason"`
	} `json:"details"`
}

func decodeErrorBody(t *testing.T, raw []byte) errorBody {
	t.Helper()

	var body errorBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v, body=%s", err, raw)
	}
	return body
}

// TestParamErrorHandler_InvalidParamFormatError 驗證路徑／查詢參數綁定失敗
// （產生的 *api.InvalidParamFormatError）被轉成 400 VALIDATION_FAILED，
// 且 details 中帶有正確的 field。
func TestParamErrorHandler_InvalidParamFormatError(t *testing.T) {
	handler := ParamErrorHandler(nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/properties/not-a-valid-uuid", nil)

	handler(rec, req, &api.InvalidParamFormatError{ParamName: "id", Err: errors.New("invalid UUID")})

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	body := decodeErrorBody(t, rec.Body.Bytes())
	if body.Code != string(apperr.CodeValidationFailed) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeValidationFailed)
	}
	if len(body.Details) != 1 || body.Details[0].Field != "id" {
		t.Fatalf("body.Details = %+v, want an entry with field %q", body.Details, "id")
	}
}

// TestParamErrorHandler_UnknownErrorType 驗證比對不到任何一個產生的參數
// 錯誤型別時，仍然產出 VALIDATION_FAILED 400，field 留空。
func TestParamErrorHandler_UnknownErrorType(t *testing.T) {
	handler := ParamErrorHandler(nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/properties", nil)

	handler(rec, req, errors.New("某個沒有 ParamName 的錯誤"))

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	body := decodeErrorBody(t, rec.Body.Bytes())
	if body.Code != string(apperr.CodeValidationFailed) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeValidationFailed)
	}
	if len(body.Details) != 1 || body.Details[0].Field != "" {
		t.Fatalf("body.Details = %+v, want an entry with empty field", body.Details)
	}
}

// TestRequestErrorHandler_BodyDecodeFailure 驗證 request body 解析失敗
// （strict handler 對不合法 JSON 的錯誤）被轉成 400 VALIDATION_FAILED。
func TestRequestErrorHandler_BodyDecodeFailure(t *testing.T) {
	handler := RequestErrorHandler(nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/properties", nil)

	handler(rec, req, fmt.Errorf("can't decode JSON body: %w", errors.New("unexpected EOF")))

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	body := decodeErrorBody(t, rec.Body.Bytes())
	if body.Code != string(apperr.CodeValidationFailed) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeValidationFailed)
	}
}

// TestResponseErrorHandler_AppError 驗證 handler 回傳的 *apperr.Error
// 依 Code 轉成對應的狀態碼與 body（這裡用 apperr.NotFound 驗證 404）。
func TestResponseErrorHandler_AppError(t *testing.T) {
	handler := ResponseErrorHandler(nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/properties/00000000-0000-0000-0000-000000000000", nil)

	handler(rec, req, apperr.NotFound)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	body := decodeErrorBody(t, rec.Body.Bytes())
	if body.Code != string(apperr.CodePropertyNotFound) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodePropertyNotFound)
	}
}

// TestResponseErrorHandler_UnclassifiedError_DegradesTo500 對應 BDD scenario
// 「未分類的錯誤降級為 500 且不外洩底層訊息」：handler 回傳一個未被 apperr
// 包裝、帶有敏感內部資訊的錯誤，中介層必須降級成 500 INTERNAL，且回應的
// message 不能包含底層錯誤訊息中的敏感片段。這條路徑沒辦法從真實跑起來的
// 服務可靠地觸發（正常 operation 不會回傳這種錯誤），所以只能用單元測試涵蓋。
func TestResponseErrorHandler_UnclassifiedError_DegradesTo500(t *testing.T) {
	handler := ResponseErrorHandler(nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/properties", nil)

	handler(rec, req, errors.New("pq: connection refused on 10.0.0.7"))

	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	body := decodeErrorBody(t, rec.Body.Bytes())
	if body.Code != string(apperr.CodeInternal) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeInternal)
	}
	if strings.Contains(body.Message, "10.0.0.7") {
		t.Fatalf("body.Message = %q, 不應包含底層錯誤訊息中的敏感片段 %q", body.Message, "10.0.0.7")
	}
}
