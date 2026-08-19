// 本檔對應 BDD scenario：
// 「更新物件租金後查詢反映新值」、「合法的物件狀態轉換被接受」、
// 「非法的物件狀態轉換被拒絕」（Scenario Outline，@error，四個 Examples）。
//
// Background 是「properties 資料表為空」：每個測試（含每個 subtest）前都會對
// TestMain 起的共用 Postgres 容器 TRUNCATE properties，取得乾淨狀態，
// 不重建容器。
package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// patchProperty 對 baseURL + /api/v1/properties/{id} 送出一次 PATCH 請求，
// 回傳狀態碼與原始回應 body。
func patchProperty(t *testing.T, baseURL, id string, reqBody map[string]any, output func() string) (int, []byte) {
	t.Helper()

	raw, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("序列化 request body 失敗: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, baseURL+"/api/v1/properties/"+id, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("建立 PATCH request 失敗: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/v1/properties/%s 失敗: %v, output:\n%s", id, err, output())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	return resp.StatusCode, respBody
}

// postPropertyStatus 對 baseURL + /api/v1/properties/{id}/status 送出一次
// POST 請求，回傳狀態碼與原始回應 body。
func postPropertyStatus(t *testing.T, baseURL, id, status string, output func() string) (int, []byte) {
	t.Helper()

	raw, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		t.Fatalf("序列化 request body 失敗: %v", err)
	}

	resp, err := http.Post(baseURL+"/api/v1/properties/"+id+"/status", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /api/v1/properties/%s/status 失敗: %v, output:\n%s", id, err, output())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	return resp.StatusCode, respBody
}

// createPropertyAtStatus 建立一筆物件，並在 wantStatus 不是預設的 VACANT 時，
// 透過合法的狀態轉換路徑（VACANT→OCCUPIED、VACANT→DELISTED 都是一步可達的
// 合法轉換）把它帶到 wantStatus，回傳該物件的 id。
//
// 刻意不用原生 SQL 塞資料：用 POST /{id}/status 這個剛做好的合法轉換路徑到達
// 起始狀態，同時也驗證了系統一致性（見 Task 7 提示）。
func createPropertyAtStatus(t *testing.T, baseURL, wantStatus string, output func() string) string {
	t.Helper()

	createStatus, createRaw := postProperty(t, baseURL, validPropertyRequestBody(), output)
	if createStatus != http.StatusCreated {
		t.Fatalf("建檔狀態碼 = %d, want %d, body=%s, output:\n%s", createStatus, http.StatusCreated, createRaw, output())
	}

	var created propertyResponseBody
	if err := json.Unmarshal(createRaw, &created); err != nil {
		t.Fatalf("解析建檔回應 JSON 失敗: %v, body=%s", err, createRaw)
	}

	if wantStatus == "VACANT" {
		return created.ID
	}

	statusCode, raw := postPropertyStatus(t, baseURL, created.ID, wantStatus, output)
	if statusCode != http.StatusOK {
		t.Fatalf("把物件轉成 %s 失敗，狀態碼 = %d, body=%s, output:\n%s", wantStatus, statusCode, raw, output())
	}

	return created.ID
}

// TestPropertyUpdate_MonthlyRent_ReflectsOnGet 對應 BDD scenario
// 「更新物件租金後查詢反映新值」。
func TestPropertyUpdate_MonthlyRent_ReflectsOnGet(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	createStatus, createRaw := postProperty(t, baseURL, validPropertyRequestBody(), output)
	if createStatus != http.StatusCreated {
		t.Fatalf("建檔狀態碼 = %d, want %d, body=%s, output:\n%s", createStatus, http.StatusCreated, createRaw, output())
	}

	var created propertyDetailBody
	if err := json.Unmarshal(createRaw, &created); err != nil {
		t.Fatalf("解析建檔回應 JSON 失敗: %v, body=%s", err, createRaw)
	}
	if created.MonthlyRent != "25000.50" {
		t.Fatalf("建檔後 monthly_rent = %q, want %q", created.MonthlyRent, "25000.50")
	}

	// 確保伺服器端 time.Now() 兩次呼叫間隔遠大於時鐘解析度，讓 updated_at
	// 嚴格晚於 created_at 這件事在實務上有保障（而不是僥倖巧合）。
	time.Sleep(time.Millisecond)

	patchStatus, patchRaw := patchProperty(t, baseURL, created.ID, map[string]any{"monthly_rent": "27000.00"}, output)
	if patchStatus != http.StatusOK {
		t.Fatalf("PATCH 狀態碼 = %d, want %d, body=%s, output:\n%s", patchStatus, http.StatusOK, patchRaw, output())
	}

	var patched propertyDetailBody
	if err := json.Unmarshal(patchRaw, &patched); err != nil {
		t.Fatalf("解析 PATCH 回應 JSON 失敗: %v, body=%s", err, patchRaw)
	}
	if patched.MonthlyRent != "27000.00" {
		t.Fatalf("PATCH 回應 monthly_rent = %q, want %q", patched.MonthlyRent, "27000.00")
	}

	getStatus, getRaw := getProperty(t, baseURL, created.ID, output)
	if getStatus != http.StatusOK {
		t.Fatalf("GET 狀態碼 = %d, want %d, body=%s, output:\n%s", getStatus, http.StatusOK, getRaw, output())
	}

	var got propertyDetailBody
	if err := json.Unmarshal(getRaw, &got); err != nil {
		t.Fatalf("解析 GET 回應 JSON 失敗: %v, body=%s", err, getRaw)
	}
	if got.MonthlyRent != "27000.00" {
		t.Fatalf("GET monthly_rent = %q, want %q", got.MonthlyRent, "27000.00")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, got.CreatedAt)
	if err != nil {
		t.Fatalf("解析 created_at (%q) 失敗: %v", got.CreatedAt, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, got.UpdatedAt)
	if err != nil {
		t.Fatalf("解析 updated_at (%q) 失敗: %v", got.UpdatedAt, err)
	}
	if !updatedAt.After(createdAt) {
		t.Fatalf("updated_at (%s) 未嚴格晚於 created_at (%s)", updatedAt, createdAt)
	}
}

// TestPropertyChangeStatus_ValidTransition_Returns200 對應 BDD scenario
// 「合法的物件狀態轉換被接受」。
func TestPropertyChangeStatus_ValidTransition_Returns200(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	createStatus, createRaw := postProperty(t, baseURL, validPropertyRequestBody(), output)
	if createStatus != http.StatusCreated {
		t.Fatalf("建檔狀態碼 = %d, want %d, body=%s, output:\n%s", createStatus, http.StatusCreated, createRaw, output())
	}

	var created propertyResponseBody
	if err := json.Unmarshal(createRaw, &created); err != nil {
		t.Fatalf("解析建檔回應 JSON 失敗: %v, body=%s", err, createRaw)
	}
	if created.Status != "VACANT" {
		t.Fatalf("建檔後 status = %q, want %q", created.Status, "VACANT")
	}

	statusCode, raw := postPropertyStatus(t, baseURL, created.ID, "RENOVATING", output)
	if statusCode != http.StatusOK {
		t.Fatalf("POST /status 狀態碼 = %d, want %d, body=%s, output:\n%s", statusCode, http.StatusOK, raw, output())
	}

	var body propertyResponseBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析 POST /status 回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Status != "RENOVATING" {
		t.Fatalf("POST /status 回應 status = %q, want %q", body.Status, "RENOVATING")
	}

	getStatus, getRaw := getProperty(t, baseURL, created.ID, output)
	if getStatus != http.StatusOK {
		t.Fatalf("GET 狀態碼 = %d, want %d, body=%s, output:\n%s", getStatus, http.StatusOK, getRaw, output())
	}

	var got propertyResponseBody
	if err := json.Unmarshal(getRaw, &got); err != nil {
		t.Fatalf("解析 GET 回應 JSON 失敗: %v, body=%s", err, getRaw)
	}
	if got.Status != "RENOVATING" {
		t.Fatalf("GET status = %q, want %q", got.Status, "RENOVATING")
	}
}

// TestPropertyChangeStatus_InvalidTransition_Returns409 對應 BDD Scenario
// Outline「非法的物件狀態轉換被拒絕」，涵蓋全部四個 Examples。
func TestPropertyChangeStatus_InvalidTransition_Returns409(t *testing.T) {
	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	cases := []struct {
		name       string
		fromStatus string
		toStatus   string
	}{
		{name: "DELISTED->OCCUPIED", fromStatus: "DELISTED", toStatus: "OCCUPIED"},
		{name: "DELISTED->RENOVATING", fromStatus: "DELISTED", toStatus: "RENOVATING"},
		{name: "OCCUPIED->DELISTED", fromStatus: "OCCUPIED", toStatus: "DELISTED"},
		{name: "VACANT->VACANT", fromStatus: "VACANT", toStatus: "VACANT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateProperties(t)

			id := createPropertyAtStatus(t, baseURL, tc.fromStatus, output)

			statusCode, raw := postPropertyStatus(t, baseURL, id, tc.toStatus, output)
			if statusCode != http.StatusConflict {
				t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", statusCode, http.StatusConflict, raw, output())
			}

			var body errorResponseBodyWithDetails
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
			}
			if body.Code != "PROPERTY_INVALID_STATUS_TRANSITION" {
				t.Fatalf("body.code = %q, want %q", body.Code, "PROPERTY_INVALID_STATUS_TRANSITION")
			}

			getStatus, getRaw := getProperty(t, baseURL, id, output)
			if getStatus != http.StatusOK {
				t.Fatalf("GET 狀態碼 = %d, want %d, body=%s, output:\n%s", getStatus, http.StatusOK, getRaw, output())
			}

			var got propertyResponseBody
			if err := json.Unmarshal(getRaw, &got); err != nil {
				t.Fatalf("解析 GET 回應 JSON 失敗: %v, body=%s", err, getRaw)
			}
			if got.Status != tc.fromStatus {
				t.Fatalf("GET status = %q, want 仍為 %q（拒絕必須發生在寫入之前，原狀態不應改變）", got.Status, tc.fromStatus)
			}
		})
	}
}
