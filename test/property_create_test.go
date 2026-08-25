// 本檔對應 BDD scenario：
// 「建立一筆包租物件」、「相同門牌重複建檔被拒絕」（@error）、
// 「欄位驗證失敗時回報是哪個欄位出錯」（Scenario Outline，@error，九個 Examples）。
//
// Background 是「properties 資料表為空」：每個測試（含每個 subtest）前都會對
// TestMain 起的共用 Postgres 容器 TRUNCATE properties，取得乾淨狀態，
// 不重建容器。
package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// dbQueryTimeout 是每次直接查共用 Postgres 容器驗證資料庫狀態的逾時上限。
const dbQueryTimeout = 10 * time.Second

// propertyResponseBody 對應 POST /api/v1/properties 成功回應的 JSON 形狀
// （只列出本測試斷言用得到的欄位）。
type propertyResponseBody struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	MonthlyRent string `json:"monthly_rent"`
	RentalMode  string `json:"rental_mode"`
}

// errorResponseBodyWithDetails 對應 httpx.ErrorBody 的形狀，這裡不直接
// import internal/httpx 的型別，用一個等價的匿名結構驗證回應「形狀」本身也符合契約。
type errorResponseBodyWithDetails struct {
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	RequestID string           `json:"request_id"`
	Details   []fieldErrorBody `json:"details"`
}

type fieldErrorBody struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// validPropertyRequestBody 回傳 BDD「建立一筆包租物件」範例中的合法 request body。
func validPropertyRequestBody() map[string]any {
	return map[string]any{
		"city":           "臺北市",
		"district":       "大安區",
		"street_address": "復興南路一段 100 號",
		"floor":          "5",
		"room_no":        "A",
		"layout":         "INDEPENDENT_SUITE",
		"area_ping":      "8.50",
		"monthly_rent":   "25000.50",
		"management_fee": "1200.00",
		"deposit_months": 2,
		"rental_mode":    "MASTER_LEASE",
		"landlord_name":  "王大明",
		"landlord_phone": "0912345678",
	}
}

// TestPropertyCreate_Success_Returns201WithVacantStatus 對應 BDD scenario
// 「建立一筆包租物件」。
func TestPropertyCreate_Success_Returns201WithVacantStatus(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	status, raw := postProperty(t, baseURL, validPropertyRequestBody(), output)

	if status != http.StatusCreated {
		t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", status, http.StatusCreated, raw, output())
	}

	var body propertyResponseBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析回應 JSON 失敗: %v, body=%s", err, raw)
	}

	if _, err := uuid.Parse(body.ID); err != nil {
		t.Fatalf("body.id = %q 不是合法 UUID: %v", body.ID, err)
	}
	if body.Status != "VACANT" {
		t.Fatalf("body.status = %q, want %q", body.Status, "VACANT")
	}
	if body.MonthlyRent != "25000.50" {
		t.Fatalf("body.monthly_rent = %q, want %q", body.MonthlyRent, "25000.50")
	}
	if body.RentalMode != "MASTER_LEASE" {
		t.Fatalf("body.rental_mode = %q, want %q", body.RentalMode, "MASTER_LEASE")
	}

	gotRent := queryMonthlyRent(t, body.ID)
	wantRent := decimal.RequireFromString("25000.50")
	if !gotRent.Equal(wantRent) {
		t.Fatalf("資料庫 monthly_rent = %s, want %s", gotRent, wantRent)
	}
}

// TestPropertyCreate_DuplicateAddress_Returns409 對應 BDD scenario
// 「相同門牌重複建檔被拒絕」（@error）。
func TestPropertyCreate_DuplicateAddress_Returns409(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	firstStatus, firstRaw := postProperty(t, baseURL, validPropertyRequestBody(), output)
	if firstStatus != http.StatusCreated {
		t.Fatalf("第一次建檔狀態碼 = %d, want %d, body=%s, output:\n%s", firstStatus, http.StatusCreated, firstRaw, output())
	}

	// 相同的 city、district、street_address、floor、room_no；其餘欄位無關緊要，
	// 這裡沿用完全相同的 body 即滿足「以完全相同的 city、district、
	// street_address、floor、room_no」這個前提。
	secondStatus, secondRaw := postProperty(t, baseURL, validPropertyRequestBody(), output)

	if secondStatus != http.StatusConflict {
		t.Fatalf("第二次建檔狀態碼 = %d, want %d, body=%s, output:\n%s", secondStatus, http.StatusConflict, secondRaw, output())
	}

	var body errorResponseBodyWithDetails
	if err := json.Unmarshal(secondRaw, &body); err != nil {
		t.Fatalf("解析錯誤回應 JSON 失敗: %v, body=%s", err, secondRaw)
	}
	if body.Code != "PROPERTY_DUPLICATE" {
		t.Fatalf("body.code = %q, want %q", body.Code, "PROPERTY_DUPLICATE")
	}

	count := countProperties(t)
	if count != 1 {
		t.Fatalf("properties 資料筆數 = %d, want 1", count)
	}
}

// TestPropertyCreate_ValidationFailed_ReportsField 對應 BDD Scenario Outline
// 「欄位驗證失敗時回報是哪個欄位出錯」，涵蓋全部九個 Examples：每一列都在其餘
// 欄位皆合法的前提下，對 POST /api/v1/properties 送出請求並驗證：狀態碼 400、
// code 為 VALIDATION_FAILED、details 中存在一項 field 等於該欄位、資料筆數仍為 0。
func TestPropertyCreate_ValidationFailed_ReportsField(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	cases := []struct {
		name  string
		field string
		value any
	}{
		{name: `city=""`, field: "city", value: ""},
		{name: `street_address=""`, field: "street_address", value: ""},
		{name: `monthly_rent="0"`, field: "monthly_rent", value: "0"},
		{name: `monthly_rent="-1"`, field: "monthly_rent", value: "-1"},
		{name: `monthly_rent="abc"`, field: "monthly_rent", value: "abc"},
		{name: `area_ping="0"`, field: "area_ping", value: "0"},
		{name: `deposit_months=-1`, field: "deposit_months", value: -1},
		{name: `rental_mode="SUBLEASE"`, field: "rental_mode", value: "SUBLEASE"},
		{name: `layout="CASTLE"`, field: "layout", value: "CASTLE"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateProperties(t)

			reqBody := validPropertyRequestBody()
			reqBody[tc.field] = tc.value

			status, raw := postProperty(t, baseURL, reqBody, output)

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
			if !hasFieldError(body.Details, tc.field) {
				t.Fatalf("body.details = %+v, want an entry with field %q", body.Details, tc.field)
			}

			count := countProperties(t)
			if count != 0 {
				t.Fatalf("properties 資料筆數 = %d, want 0", count)
			}
		})
	}
}

// hasFieldError 回報 details 中是否存在一項 Field 等於 field 的項目。
func hasFieldError(details []fieldErrorBody, field string) bool {
	for _, d := range details {
		if d.Field == field {
			return true
		}
	}
	return false
}

// postProperty 對 baseURL + /api/v1/properties 送出一次 POST 請求，
// 回傳狀態碼與原始回應 body。
func postProperty(t *testing.T, baseURL string, reqBody map[string]any, output func() string) (int, []byte) {
	t.Helper()

	raw, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("序列化 request body 失敗: %v", err)
	}

	resp, err := http.Post(baseURL+"/api/v1/properties", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /api/v1/properties 失敗: %v, output:\n%s", err, output())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	return resp.StatusCode, respBody
}

// openSharedPostgres 對 TestMain 起的共用 Postgres 容器開一個獨立的 *bun.DB
// 連線，用於本檔直接驗證資料庫狀態（TRUNCATE／計數／查欄位值），
// 不透過 HTTP 回應間接推論。
func openSharedPostgres(t *testing.T) *bun.DB {
	t.Helper()

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(sharedPostgres())))
	bunDB := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { bunDB.Close() })

	return bunDB
}

// truncateProperties 清空共用容器的 properties 資料表，讓每個測試（含每個
// subtest）都是從 BDD Background「properties 資料表為空」開始。
func truncateProperties(t *testing.T) {
	t.Helper()

	db := openSharedPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	if _, err := db.ExecContext(ctx, "TRUNCATE TABLE properties"); err != nil {
		t.Fatalf("TRUNCATE properties 失敗: %v", err)
	}
}

// countProperties 回傳共用容器 properties 資料表目前的資料筆數。
func countProperties(t *testing.T) int {
	t.Helper()

	db := openSharedPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM properties").Scan(&count); err != nil {
		t.Fatalf("查詢 properties 資料筆數失敗: %v", err)
	}

	return count
}

// queryMonthlyRent 回傳共用容器 properties 資料表中 id 該筆的 monthly_rent。
func queryMonthlyRent(t *testing.T, id string) decimal.Decimal {
	t.Helper()

	db := openSharedPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	var rent decimal.Decimal
	if err := db.QueryRowContext(ctx, "SELECT monthly_rent FROM properties WHERE id = ?", id).Scan(&rent); err != nil {
		t.Fatalf("查詢 monthly_rent 失敗: %v", err)
	}

	return rent
}
