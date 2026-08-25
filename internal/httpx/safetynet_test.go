// 刻意用真正未接 hook 的產生 handler（api.NewStrictHandler + api.Handler，
// 都不帶 options）重現「漏接 hook」，而不是手寫 http.Error 假裝——
// 否則驗證的只是我們自己想像出來的情境，不是 oapi-codegen 的真實預設行為。
//
// import api 不會造成 cycle：api 不 import internal/httpx。
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
)

// 六個方法都不會被呼叫到：請求在路徑參數綁定階段就被預設的 ErrorHandlerFunc
// 攔下，走不到 strict handler 這一層。
type stubStrictServer struct{}

func (stubStrictServer) ListProperties(ctx context.Context, request api.ListPropertiesRequestObject) (api.ListPropertiesResponseObject, error) {
	return nil, nil
}

func (stubStrictServer) CreateProperty(ctx context.Context, request api.CreatePropertyRequestObject) (api.CreatePropertyResponseObject, error) {
	return nil, nil
}

func (stubStrictServer) GetProperty(ctx context.Context, request api.GetPropertyRequestObject) (api.GetPropertyResponseObject, error) {
	return nil, nil
}

func (stubStrictServer) UpdateProperty(ctx context.Context, request api.UpdatePropertyRequestObject) (api.UpdatePropertyResponseObject, error) {
	return nil, nil
}

func (stubStrictServer) ChangePropertyStatus(ctx context.Context, request api.ChangePropertyStatusRequestObject) (api.ChangePropertyStatusResponseObject, error) {
	return nil, nil
}

func (stubStrictServer) GetHealthz(ctx context.Context, request api.GetHealthzRequestObject) (api.GetHealthzResponseObject, error) {
	return nil, nil
}

// 不帶 options 的產生 handler 會套用 oapi-codegen 的預設值
// `http.Error(w, err.Error(), 400)`：{id} 綁定成 UUID 失敗時寫出 text/plain 的
// 400，body 為 "Invalid format for parameter id: ..."。
func TestEnsureJSONError_LeakedHookPlainTextIntercepted(t *testing.T) {
	buf := &syncBuffer{}
	logger := logging.New("info", buf)

	strict := api.NewStrictHandler(stubStrictServer{}, nil)
	generated := api.Handler(strict)

	// 外面要有 RequestID，回應才有非空的 request_id 可以斷言。
	handler := RequestID(EnsureJSONError(logger)(generated))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/not-a-valid-uuid", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want 開頭為 %q", contentType, "application/json")
	}

	rawBody := rec.Body.Bytes()

	// 這個 Unmarshal 本身就是強斷言：若攔截後沒吞掉下游的 Write，純文字會接在
	// JSON 後面，body 不再是單一合法 JSON value，這裡就會失敗。
	var body ErrorBody
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON（可能是純文字接在 JSON 後面外洩了）: %v, body=%s", err, rawBody)
	}

	if body.Code != string(apperr.CodeInternal) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeInternal)
	}
	if body.RequestID == "" {
		t.Fatalf("body.RequestID 為空字串，want 非空")
	}

	if strings.Contains(string(rawBody), "Invalid format for parameter") {
		t.Fatalf("回應 body 包含原本漏接 hook 寫出的純文字，不應該外洩: %s", rawBody)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, `"level":"WARN"`) {
		t.Fatalf("log 輸出中沒有 WARN level 的紀錄，log 內容:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, body.RequestID) {
		t.Fatalf("log 輸出中沒有包含 request_id %q，log 內容:\n%s", body.RequestID, logOutput)
	}
}

// 同一個 handler 掛與不掛防護網，狀態碼、header、原始 body bytes 必須完全相同。
func TestEnsureJSONError_SuccessPassthrough_ByteIdentical(t *testing.T) {
	handlerFn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}

	recWithout := httptest.NewRecorder()
	http.HandlerFunc(handlerFn).ServeHTTP(recWithout, httptest.NewRequest(http.MethodGet, "/api/v1/properties", nil))

	recWith := httptest.NewRecorder()
	guarded := EnsureJSONError(logging.New("info", &syncBuffer{}))(http.HandlerFunc(handlerFn))
	guarded.ServeHTTP(recWith, httptest.NewRequest(http.MethodGet, "/api/v1/properties", nil))

	if recWithout.Code != recWith.Code {
		t.Fatalf("狀態碼不同：不掛防護網 = %d, 掛防護網 = %d", recWithout.Code, recWith.Code)
	}
	if !bytes.Equal(recWithout.Body.Bytes(), recWith.Body.Bytes()) {
		t.Fatalf("body bytes 不同：不掛防護網 = %q, 掛防護網 = %q", recWithout.Body.Bytes(), recWith.Body.Bytes())
	}
	if recWithout.Header().Get("Content-Type") != recWith.Header().Get("Content-Type") {
		t.Fatalf("Content-Type 不同：不掛防護網 = %q, 掛防護網 = %q",
			recWithout.Header().Get("Content-Type"), recWith.Header().Get("Content-Type"))
	}
}

// handler 直接 Write（隱含 200）時，不能因為當下 Content-Type 還沒設定
// 就誤判成需要攔截的錯誤回應。
func TestEnsureJSONError_ImplicitStatusOK_DirectWritePassesThrough(t *testing.T) {
	const wantBody = "沒有明確呼叫 WriteHeader，隱含 200"

	handlerFn := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(wantBody))
	}

	rec := httptest.NewRecorder()
	guarded := EnsureJSONError(logging.New("info", &syncBuffer{}))(http.HandlerFunc(handlerFn))
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != wantBody {
		t.Fatalf("body = %q, want %q", rec.Body.String(), wantBody)
	}
}

// 不只檢查 body 沒外洩，還檢查下游的 Write 回報 (len(p), nil)——
// 下游不能因為被吞掉而誤判成寫入失敗。
func TestEnsureJSONError_InterceptedResponse_SwallowsSubsequentWrites(t *testing.T) {
	const leaked = "這段純文字絕對不能出現在回應 body 中"

	handlerFn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)

		n, err := w.Write([]byte(leaked))
		if err != nil {
			t.Errorf("被攔截後 Write 不應回傳 error，got %v", err)
		}
		if n != len(leaked) {
			t.Errorf("被攔截後 Write 應回報寫入成功的長度 %d，got %d", len(leaked), n)
		}
	}

	buf := &syncBuffer{}
	guarded := RequestID(EnsureJSONError(logging.New("info", buf))(http.HandlerFunc(handlerFn)))

	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d（攔截不應該改變狀態碼）", rec.Code, http.StatusBadRequest)
	}
	if strings.Contains(rec.Body.String(), leaked) {
		t.Fatalf("防護網沒有吞掉下游後續的 Write，純文字外洩: %s", rec.Body.String())
	}

	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應 body 不是合法 JSON: %v, body=%s", err, rec.Body.String())
	}
	if body.Code != string(apperr.CodeInternal) {
		t.Fatalf("body.Code = %q, want %q", body.Code, apperr.CodeInternal)
	}
}

func TestEnsureJSONError_RepeatedWriteHeader_IgnoresSecondCall(t *testing.T) {
	handlerFn := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusInternalServerError) // 應該被忽略
		_, _ = w.Write([]byte("ok"))
	}

	rec := httptest.NewRecorder()
	guarded := EnsureJSONError(logging.New("info", &syncBuffer{}))(http.HandlerFunc(handlerFn))
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d（第二次 WriteHeader 應該被忽略）", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
