// 本檔對應 BDD scenario：
// 「handler 回傳 apperr 時由中介層轉成對應的狀態碼與 body」、
// 「領域層的衝突錯誤同樣經由中介層轉譯」、
// 「無法解析的 request body 由中介層轉成 400」、
// 「路徑參數格式錯誤由中介層轉成 400」、
// 「查詢參數型別錯誤由中介層轉成 400」，以及 Scenario Outline
// 「每一條錯誤路徑的回應都是統一的 JSON 形狀」（三個錯誤 hook 各自的觸發
// 路徑，加上 handler 回傳 apperr、領域層驗證失敗，共五個 Examples）。
//
// 本檔涵蓋的是「錯誤中介層本身」這件事：不論錯誤是哪個 hook 攔下的
// （ParamErrorHandler／RequestErrorHandler／ResponseErrorHandler），輸出的
// JSON 形狀都是同一份 httpx.ErrorBody。「未分類的錯誤降級為 500」這條無法
// 從真實跑起來的服務可靠觸發，改用 internal/apihttp/apihttp_test.go 的單元
// 測試涵蓋。
package test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// genericErrorBody 對應 Scenario Outline「每一條錯誤路徑的回應都是統一的
// JSON 形狀」要求的最小子集：code、message、request_id 三個欄位皆存在。
type genericErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// doGet 對 url 送出一次 GET 請求，回傳狀態碼、回應 header 與原始 body。
func doGet(t *testing.T, url string, output func() string) (int, http.Header, []byte) {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s 失敗: %v, output:\n%s", url, err, output())
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

// doPost 對 url 送出一次 POST 請求，body 是未經過 json.Marshal、直接照字面值
// 送出的 rawBody（可用來製造不合法 JSON），回傳狀態碼、回應 header 與原始 body。
func doPost(t *testing.T, url, rawBody string, output func() string) (int, http.Header, []byte) {
	t.Helper()

	resp, err := http.Post(url, "application/json", strings.NewReader(rawBody))
	if err != nil {
		t.Fatalf("POST %s 失敗: %v, output:\n%s", url, err, output())
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

// TestErrorShape_NotFound_Returns404WithCode 對應 BDD scenario
// 「handler 回傳 apperr 時由中介層轉成對應的狀態碼與 body」。
func TestErrorShape_NotFound_Returns404WithCode(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	status, _, raw := doGet(t, baseURL+"/api/v1/properties/00000000-0000-0000-0000-000000000000", output)
	if status != http.StatusNotFound {
		t.Fatalf("狀態碼 = %d, want %d, body=%s", status, http.StatusNotFound, raw)
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "PROPERTY_NOT_FOUND" {
		t.Fatalf("body.code = %q, want %q", body.Code, "PROPERTY_NOT_FOUND")
	}
	if body.RequestID == "" {
		t.Fatalf("body.request_id 為空字串，want 非空")
	}
}

// TestErrorShape_InvalidStatusTransition_Returns409WithCode 對應 BDD scenario
// 「領域層的衝突錯誤同樣經由中介層轉譯」：一筆狀態為 RENOVATING 的物件，
// 送出 status 為 OCCUPIED（RENOVATING 只能轉到 DELISTED，RENOVATING→OCCUPIED
// 不合法）。
func TestErrorShape_InvalidStatusTransition_Returns409WithCode(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	createStatus, createRaw := postProperty(t, baseURL, validPropertyRequestBody(), output)
	if createStatus != http.StatusCreated {
		t.Fatalf("建檔狀態碼 = %d, want %d, body=%s", createStatus, http.StatusCreated, createRaw)
	}
	var created propertyResponseBody
	if err := json.Unmarshal(createRaw, &created); err != nil {
		t.Fatalf("解析建檔回應 JSON 失敗: %v, body=%s", err, createRaw)
	}

	// VACANT -> RENOVATING 是合法轉換，先把物件帶到 RENOVATING。
	toRenovatingStatus, toRenovatingRaw := postPropertyStatus(t, baseURL, created.ID, "RENOVATING", output)
	if toRenovatingStatus != http.StatusOK {
		t.Fatalf("轉成 RENOVATING 失敗，狀態碼 = %d, body=%s", toRenovatingStatus, toRenovatingRaw)
	}

	// RENOVATING -> OCCUPIED 不在合法轉換表中。
	status, raw := postPropertyStatus(t, baseURL, created.ID, "OCCUPIED", output)
	if status != http.StatusConflict {
		t.Fatalf("狀態碼 = %d, want %d, body=%s", status, http.StatusConflict, raw)
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "PROPERTY_INVALID_STATUS_TRANSITION" {
		t.Fatalf("body.code = %q, want %q", body.Code, "PROPERTY_INVALID_STATUS_TRANSITION")
	}
}

// TestErrorShape_UnparsableRequestBody_Returns400 對應 BDD scenario
// 「無法解析的 request body 由中介層轉成 400」。
func TestErrorShape_UnparsableRequestBody_Returns400(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	status, _, raw := doPost(t, baseURL+"/api/v1/properties", "{", output)
	if status != http.StatusBadRequest {
		t.Fatalf("狀態碼 = %d, want %d, body=%s", status, http.StatusBadRequest, raw)
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("body.code = %q, want %q", body.Code, "VALIDATION_FAILED")
	}
}

// TestErrorShape_InvalidPathParam_Returns400WithFieldID 對應 BDD scenario
// 「路徑參數格式錯誤由中介層轉成 400」。
func TestErrorShape_InvalidPathParam_Returns400WithFieldID(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	status, _, raw := doGet(t, baseURL+"/api/v1/properties/not-a-valid-uuid", output)
	if status != http.StatusBadRequest {
		t.Fatalf("狀態碼 = %d, want %d, body=%s", status, http.StatusBadRequest, raw)
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("body.code = %q, want %q", body.Code, "VALIDATION_FAILED")
	}
	if !hasFieldError(body.Details, "id") {
		t.Fatalf("body.details = %+v, want an entry with field %q", body.Details, "id")
	}
}

// TestErrorShape_InvalidQueryParamType_Returns400WithFieldPage 對應 BDD
// scenario「查詢參數型別錯誤由中介層轉成 400」。
func TestErrorShape_InvalidQueryParamType_Returns400WithFieldPage(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	status, _, raw := doGet(t, baseURL+"/api/v1/properties?page=abc", output)
	if status != http.StatusBadRequest {
		t.Fatalf("狀態碼 = %d, want %d, body=%s", status, http.StatusBadRequest, raw)
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("body.code = %q, want %q", body.Code, "VALIDATION_FAILED")
	}
	if !hasFieldError(body.Details, "page") {
		t.Fatalf("body.details = %+v, want an entry with field %q", body.Details, "page")
	}
}

// TestErrorShape_ScenarioOutline_UnifiedJSONShape 對應 Scenario Outline
// 「每一條錯誤路徑的回應都是統一的 JSON 形狀」，涵蓋三個錯誤 hook 各自的
// 觸發路徑（路徑參數綁定失敗、查詢參數綁定失敗、request body 解析失敗），
// 加上 handler 回傳 apperr、領域層驗證失敗，共五個 Examples。
func TestErrorShape_ScenarioOutline_UnifiedJSONShape(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	cases := []struct {
		name string
		do   func(t *testing.T) (status int, header http.Header, raw []byte)
	}{
		{
			name: "路徑參數綁定失敗",
			do: func(t *testing.T) (int, http.Header, []byte) {
				return doGet(t, baseURL+"/api/v1/properties/not-a-valid-uuid", output)
			},
		},
		{
			name: "查詢參數綁定失敗",
			do: func(t *testing.T) (int, http.Header, []byte) {
				return doGet(t, baseURL+"/api/v1/properties?page=abc", output)
			},
		},
		{
			name: "request body 解析失敗",
			do: func(t *testing.T) (int, http.Header, []byte) {
				return doPost(t, baseURL+"/api/v1/properties", "{", output)
			},
		},
		{
			name: "handler 回傳 apperr",
			do: func(t *testing.T) (int, http.Header, []byte) {
				return doGet(t, baseURL+"/api/v1/properties/00000000-0000-0000-0000-000000000000", output)
			},
		},
		{
			name: "領域層驗證失敗",
			do: func(t *testing.T) (int, http.Header, []byte) {
				truncateProperties(t)
				reqBody := validPropertyRequestBody()
				reqBody["monthly_rent"] = "0"
				raw, err := json.Marshal(reqBody)
				if err != nil {
					t.Fatalf("序列化 request body 失敗: %v", err)
				}
				return doPost(t, baseURL+"/api/v1/properties", string(raw), output)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, header, raw := tc.do(t)

			contentType := header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("Content-Type = %q, want 開頭為 %q, body=%s", contentType, "application/json", raw)
			}

			var body genericErrorBody
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("回應 body 不可解析成含 code/message/request_id 的物件: %v, body=%s", err, raw)
			}
			if body.Code == "" {
				t.Fatalf("body.code 為空字串，body=%s", raw)
			}
			if body.RequestID == "" {
				t.Fatalf("request_id 為空字串，body=%s", raw)
			}
		})
	}
}
