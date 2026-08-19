// 本檔對應 BDD scenario：
// 「以 id 查詢單一物件」、「查詢不存在的物件」（@error）、
// 「分頁列出物件並依建檔時間由新到舊排序」、
// 「依條件篩選物件列表」（Scenario Outline，五個 Examples）。
//
// Background 是「properties 資料表為空」：每個測試前都會對 TestMain 起的共用
// Postgres 容器 TRUNCATE properties，取得乾淨狀態，不重建容器。
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// propertyDetailBody 對應 GET /api/v1/properties/{id} 成功回應的 JSON 形狀
// （只列出本測試斷言用得到的欄位）。
type propertyDetailBody struct {
	ID          string `json:"id"`
	MonthlyRent string `json:"monthly_rent"`
	RentalMode  string `json:"rental_mode"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// propertyListItemBody 是列表回應中單一項目的形狀（只列出斷言用得到的欄位）。
type propertyListItemBody struct {
	ID string `json:"id"`
}

// propertyListBody 對應 GET /api/v1/properties 成功回應的 JSON 形狀。
// Items 刻意不加任何額外處理——json.Unmarshal 對 JSON null 與 JSON []
// 的解碼結果不同（前者是 nil slice、後者是非 nil 空 slice），
// 這正是本檔用來驗證「空結果回 [] 而非 null」的關鍵。
type propertyListBody struct {
	Items []propertyListItemBody `json:"items"`
	Total int                    `json:"total"`
}

// getProperty 對 baseURL + /api/v1/properties/{id} 送出一次 GET 請求，
// 回傳狀態碼與原始回應 body。
func getProperty(t *testing.T, baseURL, id string, output func() string) (int, []byte) {
	t.Helper()
	return getJSON(t, baseURL+"/api/v1/properties/"+id, output)
}

// listProperties 對 baseURL + /api/v1/properties?query 送出一次 GET 請求，
// 回傳狀態碼與原始回應 body。
func listProperties(t *testing.T, baseURL, query string, output func() string) (int, []byte) {
	t.Helper()

	u := baseURL + "/api/v1/properties"
	if query != "" {
		u += "?" + query
	}
	return getJSON(t, u, output)
}

// getJSON 對 u 送出一次 GET 請求，回傳狀態碼與原始回應 body。
func getJSON(t *testing.T, u string, output func() string) (int, []byte) {
	t.Helper()

	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s 失敗: %v, output:\n%s", u, err, output())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	return resp.StatusCode, respBody
}

// TestPropertyGetByID_Success_Returns200 對應 BDD scenario「以 id 查詢單一物件」。
func TestPropertyGetByID_Success_Returns200(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	reqBody := validPropertyRequestBody()
	reqBody["rental_mode"] = "MANAGED"

	createStatus, createRaw := postProperty(t, baseURL, reqBody, output)
	if createStatus != http.StatusCreated {
		t.Fatalf("建檔狀態碼 = %d, want %d, body=%s, output:\n%s", createStatus, http.StatusCreated, createRaw, output())
	}

	var created propertyResponseBody
	if err := json.Unmarshal(createRaw, &created); err != nil {
		t.Fatalf("解析建檔回應 JSON 失敗: %v, body=%s", err, createRaw)
	}

	status, raw := getProperty(t, baseURL, created.ID, output)
	if status != http.StatusOK {
		t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", status, http.StatusOK, raw, output())
	}

	var body propertyDetailBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析回應 JSON 失敗: %v, body=%s", err, raw)
	}

	if body.ID != created.ID {
		t.Fatalf("body.id = %q, want %q", body.ID, created.ID)
	}
	if body.MonthlyRent != "25000.50" {
		t.Fatalf("body.monthly_rent = %q, want %q", body.MonthlyRent, "25000.50")
	}
	if body.RentalMode != "MANAGED" {
		t.Fatalf("body.rental_mode = %q, want %q", body.RentalMode, "MANAGED")
	}

	if _, err := time.Parse(time.RFC3339, body.CreatedAt); err != nil {
		t.Fatalf("body.created_at = %q 不是合法的 RFC3339: %v", body.CreatedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, body.UpdatedAt); err != nil {
		t.Fatalf("body.updated_at = %q 不是合法的 RFC3339: %v", body.UpdatedAt, err)
	}
}

// TestPropertyGetByID_NotFound_Returns404 對應 BDD scenario「查詢不存在的物件」（@error）。
func TestPropertyGetByID_NotFound_Returns404(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	status, raw := getProperty(t, baseURL, "00000000-0000-0000-0000-000000000000", output)
	if status != http.StatusNotFound {
		t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", status, http.StatusNotFound, raw, output())
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "PROPERTY_NOT_FOUND" {
		t.Fatalf("body.code = %q, want %q", body.Code, "PROPERTY_NOT_FOUND")
	}
}

// TestPropertyGetByID_InvalidUUID_Returns400 驗證路徑上的 {id} 不是合法 UUID
// 時回 400 VALIDATION_FAILED（規格明列的邊界，雖未直接寫在 Gherkin scenario
// 的 Then 子句裡，但屬於本 task 的契約要求）。
func TestPropertyGetByID_InvalidUUID_Returns400(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	status, raw := getProperty(t, baseURL, "not-a-valid-uuid", output)
	if status != http.StatusBadRequest {
		t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", status, http.StatusBadRequest, raw, output())
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, raw)
	}
	if body.Code != "VALIDATION_FAILED" {
		t.Fatalf("body.code = %q, want %q", body.Code, "VALIDATION_FAILED")
	}
}

// createPropertiesSequentially 依序（非並行）建立 n 筆物件，每筆的
// room_no 各不相同以滿足 (city, district, street_address, floor, room_no)
// 唯一鍵，並回傳依「建立順序」排列的 id 陣列（ids[0] 是第 1 筆建立的）。
//
// 依序（非並行）送出真正的 HTTP 請求，讓伺服器端 time.Now() 產生的
// created_at 隨呼叫順序嚴格遞增——Go 的 monotonic clock 保證同一機器上兩次
// time.Now() 呼叫不遞減，而兩次 HTTP round trip 之間的間隔遠大於時鐘解析度，
// 實務上足以保證嚴格遞增；即使真的同一時間戳，repository 端以 id 作為次要
// 排序鍵這件事本身不影響本測試的正確性判準，因為這裡測的就是那個排序契約。
func createPropertiesSequentially(t *testing.T, baseURL string, output func() string, n int) []string {
	t.Helper()

	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		reqBody := validPropertyRequestBody()
		reqBody["room_no"] = fmt.Sprintf("R%03d", i)

		status, raw := postProperty(t, baseURL, reqBody, output)
		if status != http.StatusCreated {
			t.Fatalf("建立第 %d 筆物件失敗，狀態碼 = %d, body=%s, output:\n%s", i, status, raw, output())
		}

		var created propertyResponseBody
		if err := json.Unmarshal(raw, &created); err != nil {
			t.Fatalf("解析第 %d 筆物件回應 JSON 失敗: %v, body=%s", i, err, raw)
		}
		ids = append(ids, created.ID)
	}

	return ids
}

// TestPropertyList_Pagination_SortedByCreatedAtDescending 對應 BDD scenario
// 「分頁列出物件並依建檔時間由新到舊排序」。
func TestPropertyList_Pagination_SortedByCreatedAtDescending(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	ids := createPropertiesSequentially(t, baseURL, output, 25)

	status, raw := listProperties(t, baseURL, "page=1&page_size=10", output)
	if status != http.StatusOK {
		t.Fatalf("page=1 狀態碼 = %d, want %d, body=%s, output:\n%s", status, http.StatusOK, raw, output())
	}

	var page1 propertyListBody
	if err := json.Unmarshal(raw, &page1); err != nil {
		t.Fatalf("解析 page=1 回應 JSON 失敗: %v, body=%s", err, raw)
	}

	if page1.Total != 25 {
		t.Fatalf("page=1 total = %d, want 25", page1.Total)
	}
	if len(page1.Items) != 10 {
		t.Fatalf("page=1 items 長度 = %d, want 10", len(page1.Items))
	}
	if page1.Items[0].ID != ids[24] {
		t.Fatalf("page=1 items[0].id = %q, want 第 25 筆建立的 id %q", page1.Items[0].ID, ids[24])
	}
	if page1.Items[9].ID != ids[15] {
		t.Fatalf("page=1 items[9].id = %q, want 第 16 筆建立的 id %q", page1.Items[9].ID, ids[15])
	}

	status2, raw2 := listProperties(t, baseURL, "page=3&page_size=10", output)
	if status2 != http.StatusOK {
		t.Fatalf("page=3 狀態碼 = %d, want %d, body=%s, output:\n%s", status2, http.StatusOK, raw2, output())
	}

	var page3 propertyListBody
	if err := json.Unmarshal(raw2, &page3); err != nil {
		t.Fatalf("解析 page=3 回應 JSON 失敗: %v, body=%s", err, raw2)
	}

	if len(page3.Items) != 5 {
		t.Fatalf("page=3 items 長度 = %d, want 5", len(page3.Items))
	}
}

// filterFixtureProperty 是「依條件篩選物件列表」Scenario Outline 中的一筆 fixture。
type filterFixtureProperty struct {
	label      string
	city       string
	status     string
	rentalMode string
}

// insertFilterFixture 直接以 SQL 寫入 4 筆 fixture 物件（而不是透過
// POST /api/v1/properties + 一個尚未存在的狀態轉換 endpoint），因為其中兩筆
// 的 status 不是新建物件預設的 VACANT（乙為 OCCUPIED、丁為 RENOVATING），
// 而「變更物件狀態」是 Task 7 的範圍，本 task 不得為了這個 fixture 去實作
// POST /{id}/status。回傳 label 到 id 的對照表，供後續驗證「items 恰好包含
// 哪些編號」使用。
func insertFilterFixture(t *testing.T, fixtures []filterFixtureProperty) map[string]string {
	t.Helper()

	db := openSharedPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	ids := make(map[string]string, len(fixtures))
	base := time.Now().UTC().Add(-time.Hour)

	for i, f := range fixtures {
		id := uuid.New()
		ids[f.label] = id.String()
		createdAt := base.Add(time.Duration(i) * time.Minute)

		insertFilterFixtureRow(t, db, ctx, id, f, createdAt)
	}

	return ids
}

// insertFilterFixtureRow 插入單一筆 fixture 物件，欄位需與
// db/20260819120000_create_properties.up.sql 保持一致。
func insertFilterFixtureRow(
	t *testing.T,
	db *bun.DB,
	ctx context.Context,
	id uuid.UUID,
	f filterFixtureProperty,
	createdAt time.Time,
) {
	t.Helper()

	_, err := db.ExecContext(ctx, `
		INSERT INTO properties (
			id, city, district, street_address, floor, room_no, layout,
			area_ping, monthly_rent, management_fee, deposit_months,
			rental_mode, status, landlord_name, landlord_phone,
			created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?
		)`,
		id, f.city, "測試區", "測試路 "+f.label+" 號", "1", f.label,
		"INDEPENDENT_SUITE",
		"8.50", "25000.00", "0", 2,
		f.rentalMode, f.status, "測試房東", "0900000000",
		createdAt, createdAt,
	)
	if err != nil {
		t.Fatalf("插入 fixture 物件（%s）失敗: %v", f.label, err)
	}
}

// TestPropertyList_Filter 對應 BDD Scenario Outline「依條件篩選物件列表」，
// 涵蓋全部五個 Examples。
func TestPropertyList_Filter(t *testing.T) {
	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	fixtures := []filterFixtureProperty{
		{label: "甲", city: "臺北市", status: "VACANT", rentalMode: "MASTER_LEASE"},
		{label: "乙", city: "臺北市", status: "OCCUPIED", rentalMode: "MANAGED"},
		{label: "丙", city: "新北市", status: "VACANT", rentalMode: "MANAGED"},
		{label: "丁", city: "新北市", status: "RENOVATING", rentalMode: "MASTER_LEASE"},
	}
	ids := insertFilterFixture(t, fixtures)

	cases := []struct {
		name       string
		query      string
		wantTotal  int
		wantLabels []string
	}{
		{name: "status=VACANT", query: "status=VACANT", wantTotal: 2, wantLabels: []string{"甲", "丙"}},
		{name: "rental_mode=MANAGED", query: "rental_mode=MANAGED", wantTotal: 2, wantLabels: []string{"乙", "丙"}},
		{name: "city=新北市", query: "city=" + url.QueryEscape("新北市"), wantTotal: 2, wantLabels: []string{"丙", "丁"}},
		{
			name:       "city=臺北市&status=VACANT",
			query:      "city=" + url.QueryEscape("臺北市") + "&status=VACANT",
			wantTotal:  1,
			wantLabels: []string{"甲"},
		},
		{name: "status=DELISTED", query: "status=DELISTED", wantTotal: 0, wantLabels: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := listProperties(t, baseURL, tc.query, output)
			if status != http.StatusOK {
				t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", status, http.StatusOK, raw, output())
			}

			var body propertyListBody
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("解析回應 JSON 失敗: %v, body=%s", err, raw)
			}

			if body.Total != tc.wantTotal {
				t.Fatalf("total = %d, want %d (body=%s)", body.Total, tc.wantTotal, raw)
			}

			if body.Items == nil {
				t.Fatalf("items 為 JSON null，want 空陣列 []（body=%s）", raw)
			}
			if len(body.Items) != len(tc.wantLabels) {
				t.Fatalf("items 長度 = %d, want %d (body=%s)", len(body.Items), len(tc.wantLabels), raw)
			}

			wantIDs := make(map[string]bool, len(tc.wantLabels))
			for _, label := range tc.wantLabels {
				wantIDs[ids[label]] = true
			}
			for _, item := range body.Items {
				if !wantIDs[item.ID] {
					t.Fatalf("items 出現非預期的 id %q（body=%s）", item.ID, raw)
				}
			}
		})
	}
}
