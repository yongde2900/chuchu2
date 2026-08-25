// 物件查詢：單筆查詢、分頁排序、條件篩選。
//
// 每個測試前對共用容器 TRUNCATE properties 取得乾淨狀態，不重建容器。
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
)

// 只列出斷言用得到的欄位。
type propertyDetailBody struct {
	ID          string `json:"id"`
	MonthlyRent string `json:"monthly_rent"`
	RentalMode  string `json:"rental_mode"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type propertyListItemBody struct {
	ID string `json:"id"`
}

// Items 刻意不加任何處理：json.Unmarshal 對 null 與 [] 的結果不同
// （nil slice vs 非 nil 空 slice），這是驗證「空結果回 [] 而非 null」的關鍵。
type propertyListBody struct {
	Items []propertyListItemBody `json:"items"`
	Total int                    `json:"total"`
}

func getProperty(t *testing.T, baseURL, id string, output func() string) (int, []byte) {
	t.Helper()
	return getJSON(t, baseURL+"/api/v1/properties/"+id, output)
}

func listProperties(t *testing.T, baseURL, query string, output func() string) (int, []byte) {
	t.Helper()

	u := baseURL + "/api/v1/properties"
	if query != "" {
		u += "?" + query
	}
	return getJSON(t, u, output)
}

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

func TestPropertyGetByID_Success_Returns200(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

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

func TestPropertyGetByID_NotFound_Returns404(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

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

// {id} 不是合法 UUID 的邊界：規格有要求，但沒有寫成獨立的 Gherkin scenario。
func TestPropertyGetByID_InvalidUUID_Returns400(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

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

// 回傳依建立順序排列的 id（ids[0] 是第 1 筆）。room_no 各不相同以滿足唯一鍵。
//
// 必須依序、不可並行：這樣伺服器端的 created_at 才會隨呼叫順序嚴格遞增，
// 排序斷言才有意義。
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

func TestPropertyList_Pagination_SortedByCreatedAtDescending(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

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

type filterFixtureProperty struct {
	label      string
	city       string
	status     string
	rentalMode string
}

// 直接以 SQL 寫入 fixture 而不走 API：其中兩筆的 status 不是新建預設的 VACANT，
// 用 API 建立還得多跑一次狀態轉換，把查詢測試綁到不相干的 endpoint 上。
// 回傳 label → id 的對照表。
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

// 欄位需與 db/ 底下的 create_properties migration 保持一致。
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

// 涵蓋 Scenario Outline 的全部五個 Examples。
func TestPropertyList_Filter(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

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
