// 本檔對應 BDD scenario（@contract @integration）：
// 「服務的實際回應與路由集合皆符合 api/openapi.yaml」。
//
// 分成四個測試：
//  1. TestContract_OpenAPIDocumentIsValid —— 對應 Given「api/openapi.yaml 存在
//     且是一份合法的 OpenAPI 3.1 文件」：真的呼叫 doc.Validate(ctx)，不只是
//     成功載入文件。
//  2. TestContract_RegisteredRoutesMatchOpenAPIDocument —— 反向驗證：以
//     chi.Walk 列舉服務實際註冊的路由，與文件宣告的 path+method 集合做「雙向」
//     比對（文件有而實作沒有、實作有而文件沒有，兩者都會讓測試失敗），
//     排除 /debug/ 開頭的除錯路由。
//  3. TestContract_ResponsesMatchOpenAPISchema —— 正向驗證：對文件中宣告的每
//     一個 operation，至少送出一次成功與一次錯誤的真實請求，並用
//     openapi3filter.ValidateResponse 驗證回應狀態碼與 body 是否符合該
//     operation 宣告的 schema（不是只比對狀態碼）。
//  4. TestContract_Healthz503MatchesOpenAPISchema —— 單獨涵蓋 GET /healthz 的
//     503 錯誤回應，因為製造它需要停掉相依服務，這與 TestMain 的共用容器衝突。
package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/internal/config"
	"github.com/yongde2900/chuchu2/internal/health"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
	"github.com/yongde2900/chuchu2/internal/platform/redisclient"
	"github.com/yongde2900/chuchu2/internal/property"
	"github.com/yongde2900/chuchu2/internal/property/httpapi"
	"github.com/yongde2900/chuchu2/internal/property/pgrepo"
	"github.com/yongde2900/chuchu2/internal/server"
	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// loadOpenAPIDocument 載入 api/openapi.yaml 並確認它本身是一份合法的
// OpenAPI 3.1 文件——這是 scenario 的第一個 Given。
func loadOpenAPIDocument(t *testing.T) *openapi3.T {
	t.Helper()

	repoRoot := testsupport.RepoRoot(t)
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(repoRoot, "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("載入 api/openapi.yaml 失敗: %v", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("api/openapi.yaml 未通過 OpenAPI 3.1 驗證: %v", err)
	}

	return doc
}

// TestContract_OpenAPIDocumentIsValid 對應 scenario 的
// 「Given 檔案 api/openapi.yaml 存在且是一份合法的 OpenAPI 3.1 文件」。
func TestContract_OpenAPIDocumentIsValid(t *testing.T) {
	loadOpenAPIDocument(t)
}

// routeKey 是 method+path 的正規化組合，用於雙向比對文件與實作的路由集合。
type routeKey struct {
	Method string
	Path   string
}

func (k routeKey) String() string { return k.Method + " " + k.Path }

// routesFromDocument 從 doc 收集所有宣告的 path+method 組合，method 一律轉大寫
// 以對齊 chi.Walk 回傳的 method 格式。
func routesFromDocument(doc *openapi3.T) map[routeKey]bool {
	routes := make(map[routeKey]bool)
	for _, path := range doc.Paths.InMatchingOrder() {
		item := doc.Paths.Value(path)
		for method := range item.Operations() {
			routes[routeKey{Method: strings.ToUpper(method), Path: path}] = true
		}
	}
	return routes
}

// buildAssembledRouter 依照與 cmd/api/main.go 完全相同的 mount 清單
// （health.Mount + propertyHandler.Mount）重建一次 router，只用於
// chi.Walk 取得實際註冊的路由表——不會真的啟動 HTTP server。
//
// 這裡的 mount 清單必須與 cmd/api/main.go 保持一致，否則反向比對會漏掉路由。
// 走這條路（在 test/ 內重建 router）而不是讓 internal/server 多匯出一個
// 只有測試會用到的組裝函式，是因為 server.NewRouter 已經回傳 *chi.Mux，
// 在這裡重建的成本比改動正式程式碼更低、也更不侵入。
func buildAssembledRouter(t *testing.T) *chi.Mux {
	t.Helper()

	db := openSharedPostgres(t)

	redisClient, err := redisclient.Open(context.Background(), config.RedisConfig{Addr: sharedRedis()})
	if err != nil {
		t.Fatalf("建立 redis client 失敗: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	logger := logging.New("error", io.Discard)

	healthSvc := health.NewService(
		health.NewPostgresChecker(db),
		health.NewRedisChecker(redisClient),
	)

	propertyRepo := pgrepo.New(db)
	propertySvc := property.NewService(propertyRepo)
	propertyHandler := httpapi.NewHandler(propertySvc, logger)

	return server.NewRouter(server.Options{
		Debug:  true,
		Logger: logger,
	}, health.Mount(healthSvc), propertyHandler.Mount())
}

// routesFromImplementation 用 chi.Walk 列舉 r 實際註冊的路由，排除 /debug/
// 開頭的除錯路由（只在 server.debug 為 true 時掛載，不屬於公開契約）。
func routesFromImplementation(t *testing.T, r *chi.Mux) map[routeKey]bool {
	t.Helper()

	routes := make(map[routeKey]bool)
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/debug/") {
			return nil
		}
		routes[routeKey{Method: method, Path: route}] = true
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk 失敗: %v", err)
	}
	return routes
}

// TestContract_RegisteredRoutesMatchOpenAPIDocument 對應 scenario 的
// 「openapi.yaml 宣告的 path + method 集合，與服務實際註冊的路由集合完全相同」。
//
// 雙向比對：文件有而實作沒有、實作有而文件沒有，兩者都必須讓測試失敗。
func TestContract_RegisteredRoutesMatchOpenAPIDocument(t *testing.T) {
	doc := loadOpenAPIDocument(t)
	docRoutes := routesFromDocument(doc)

	implRouter := buildAssembledRouter(t)
	implRoutes := routesFromImplementation(t, implRouter)

	for r := range docRoutes {
		if !implRoutes[r] {
			t.Errorf("openapi.yaml 宣告了 %s，但服務並未實際註冊這個路由", r)
		}
	}
	for r := range implRoutes {
		if !docRoutes[r] {
			t.Errorf("服務註冊了 %s，但 openapi.yaml 未文件化這個路由", r)
		}
	}
}

// newKinRouter 用 doc 建立一個 kin-openapi 的 routers.Router，供
// openapi3filter.ValidateResponse 找出每次請求對應的 operation。
//
// api/openapi.yaml 的 servers 刻意宣告成相對路徑「/」（不綁定固定 host），
// 這樣 FindRoute 只靠 method+path 比對，不受測試中隨機取得的埠號影響。
func newKinRouter(t *testing.T, doc *openapi3.T) routers.Router {
	t.Helper()

	r, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("建立 kin-openapi router 失敗: %v", err)
	}
	return r
}

// contractRequest 送出一次真實 HTTP 請求，回傳送出的 *http.Request（供
// FindRoute 比對路由用）、狀態碼、回應 header 與 body。
//
// 既有的 postProperty／getProperty／patchProperty／postPropertyStatus 等
// helper（見 test/property_*_test.go）只回傳 status/body，這裡另外寫一個薄
// 封裝是因為 schema 驗證需要回應 header（Content-Type）才能讓
// openapi3filter 選對 schema；其餘（request body 建構方式、逾時、錯誤訊息
// 風格）都刻意維持與既有 helper 一致。
func contractRequest(t *testing.T, method, urlStr string, reqBody any, output func() string) (req *http.Request, status int, header http.Header, respBody []byte) {
	t.Helper()

	var reader io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("序列化 request body 失敗: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, urlStr, reader)
	if err != nil {
		t.Fatalf("建立 %s %s request 失敗: %v", method, urlStr, err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s 失敗: %v, output:\n%s", method, urlStr, err, output())
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	return req, resp.StatusCode, resp.Header, raw
}

// assertMatchesSchema 驗證 req 對應的 operation 底下，status 這個狀態碼有
// 出現在 openapi.yaml 宣告的 responses 之中，且 body 通過該狀態碼對應的
// schema——用的是 openapi3filter.ValidateResponse，不是單純比對狀態碼。
func assertMatchesSchema(t *testing.T, kinRouter routers.Router, req *http.Request, status int, header http.Header, body []byte) {
	t.Helper()

	route, pathParams, err := kinRouter.FindRoute(req)
	if err != nil {
		t.Fatalf("openapi.yaml 中找不到 %s %s 對應的路由: %v", req.Method, req.URL.Path, err)
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: status,
		Header: header,
		// IncludeResponseStatus: true 讓「回應狀態碼未出現在 responses 之中」
		// 這件事本身就會讓驗證失敗，而不是預設的「未文件化的狀態碼一律放行」。
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
		},
	}
	input.SetBodyBytes(body)

	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		t.Fatalf("%s %s 回應（status=%d）未通過 openapi.yaml 的 schema 驗證: %v\nbody=%s",
			req.Method, req.URL.Path, status, err, body)
	}
}

// TestContract_ResponsesMatchOpenAPISchema 對應 scenario 的「對 openapi.yaml
// 中宣告的每一個 path + method 各發出請求，且對每個 operation 至少涵蓋一種
// 成功回應與一種錯誤回應」與「每一次回應的狀態碼／body 都通過該 operation
// 宣告的 schema 驗證」。
//
// 除了 GET /healthz 的 503（見 TestContract_Healthz503MatchesOpenAPISchema）
// 之外，這裡涵蓋文件中宣告的每一個 operation。
func TestContract_ResponsesMatchOpenAPISchema(t *testing.T) {
	doc := loadOpenAPIDocument(t)
	kinRouter := newKinRouter(t, doc)

	truncateProperties(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": sharedPostgres(),
		"CHUCHU_REDIS_ADDR":   sharedRedis(),
	})

	// --- GET /healthz：成功（200）。---
	{
		req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/healthz", nil, output)
		if status != http.StatusOK {
			t.Fatalf("GET /healthz 狀態碼 = %d, want 200, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}

	// --- POST /api/v1/properties：成功（201）、錯誤（400、409）。---
	var createdID string
	{
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties", validPropertyRequestBody(), output)
		if status != http.StatusCreated {
			t.Fatalf("POST /api/v1/properties 狀態碼 = %d, want 201, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)

		var created propertyResponseBody
		if err := json.Unmarshal(body, &created); err != nil {
			t.Fatalf("解析建檔回應失敗: %v, body=%s", err, body)
		}
		createdID = created.ID
	}
	{
		invalidBody := validPropertyRequestBody()
		invalidBody["monthly_rent"] = "0"
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties", invalidBody, output)
		if status != http.StatusBadRequest {
			t.Fatalf("POST /api/v1/properties（不合法 monthly_rent）狀態碼 = %d, want 400, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties", validPropertyRequestBody(), output)
		if status != http.StatusConflict {
			t.Fatalf("POST /api/v1/properties（重複門牌）狀態碼 = %d, want 409, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}

	// --- GET /api/v1/properties：只涵蓋成功（200）。---
	//
	// 這個 operation 在 openapi.yaml 中刻意只宣告 200：handler
	// （internal/property/httpapi/httpapi.go 的 parseListFilter／
	// parseNonNegativeInt）對任何缺漏或不合法的查詢參數一律轉成預設值、不視
	// 為錯誤——這是先前 task 已經釘死的行為（見該檔案的函式註解），本 task
	// 不得為了湊「至少一種錯誤」去改動實作，也不該為了測試覆蓋率去在文件上
	// 無中生有一個實作中不存在的 400。因此這裡只涵蓋成功回應，是「這個
	// operation 本來就沒有錯誤路徑」的如實反映。
	{
		req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/api/v1/properties", nil, output)
		if status != http.StatusOK {
			t.Fatalf("GET /api/v1/properties 狀態碼 = %d, want 200, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}

	// --- GET /api/v1/properties/{id}：成功（200）、錯誤（400、404）。---
	{
		req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/api/v1/properties/"+createdID, nil, output)
		if status != http.StatusOK {
			t.Fatalf("GET /api/v1/properties/{id} 狀態碼 = %d, want 200, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/api/v1/properties/not-a-valid-uuid", nil, output)
		if status != http.StatusBadRequest {
			t.Fatalf("GET /api/v1/properties/{id}（非法 UUID）狀態碼 = %d, want 400, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/api/v1/properties/00000000-0000-0000-0000-000000000000", nil, output)
		if status != http.StatusNotFound {
			t.Fatalf("GET /api/v1/properties/{id}（不存在）狀態碼 = %d, want 404, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}

	// --- PATCH /api/v1/properties/{id}：成功（200）、錯誤（400、404）。---
	{
		req, status, header, body := contractRequest(t, http.MethodPatch, baseURL+"/api/v1/properties/"+createdID, map[string]any{"monthly_rent": "27000.00"}, output)
		if status != http.StatusOK {
			t.Fatalf("PATCH /api/v1/properties/{id} 狀態碼 = %d, want 200, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodPatch, baseURL+"/api/v1/properties/"+createdID, map[string]any{"monthly_rent": "-1"}, output)
		if status != http.StatusBadRequest {
			t.Fatalf("PATCH /api/v1/properties/{id}（不合法 monthly_rent）狀態碼 = %d, want 400, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodPatch, baseURL+"/api/v1/properties/00000000-0000-0000-0000-000000000000", map[string]any{"monthly_rent": "27000.00"}, output)
		if status != http.StatusNotFound {
			t.Fatalf("PATCH /api/v1/properties/{id}（不存在）狀態碼 = %d, want 404, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}

	// --- POST /api/v1/properties/{id}/status：成功（200）、錯誤（400、404、409）。---
	{
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties/"+createdID+"/status", map[string]any{"status": "RENOVATING"}, output)
		if status != http.StatusOK {
			t.Fatalf("POST /{id}/status 狀態碼 = %d, want 200, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties/"+createdID+"/status", map[string]any{"status": "NOT_A_STATUS"}, output)
		if status != http.StatusBadRequest {
			t.Fatalf("POST /{id}/status（不合法 status）狀態碼 = %d, want 400, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties/00000000-0000-0000-0000-000000000000/status", map[string]any{"status": "OCCUPIED"}, output)
		if status != http.StatusNotFound {
			t.Fatalf("POST /{id}/status（不存在）狀態碼 = %d, want 404, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
	{
		// createdID 此時的狀態是 RENOVATING（前面已把它轉過去）；合法轉換表中
		// RENOVATING 只能到 DELISTED，RENOVATING→OCCUPIED 不合法，用來製造 409。
		req, status, header, body := contractRequest(t, http.MethodPost, baseURL+"/api/v1/properties/"+createdID+"/status", map[string]any{"status": "OCCUPIED"}, output)
		if status != http.StatusConflict {
			t.Fatalf("POST /{id}/status（非法轉換）狀態碼 = %d, want 409, body=%s", status, body)
		}
		assertMatchesSchema(t, kinRouter, req, status, header, body)
	}
}

// TestContract_Healthz503MatchesOpenAPISchema 涵蓋 GET /healthz 的 503 錯誤
// 回應。製造它需要停掉相依服務，這與 TestMain 的共用容器衝突，所以這裡比照
// test/health_test.go 既有作法，另起一組專屬、用完即丟的容器，主動停掉
// Postgres 後再驗證回應是否符合 schema。絕對不可以停掉 TestMain 的共用容器。
func TestContract_Healthz503MatchesOpenAPISchema(t *testing.T) {
	doc := loadOpenAPIDocument(t)
	kinRouter := newKinRouter(t, doc)

	pgDSN, pgStop := testsupport.StartPostgres(t)
	redisAddr, _ := testsupport.StartRedis(t)

	baseURL, output, _ := testsupport.StartAPI(t, "test", map[string]string{
		"CHUCHU_POSTGRES_DSN": pgDSN,
		"CHUCHU_REDIS_ADDR":   redisAddr,
	})

	// 先確認斷線前是 200，才能區分「探針正確偵測到斷線」與「探針從頭到尾就沒
	// 連上過」。
	req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/healthz", nil, output)
	if status != http.StatusOK {
		t.Fatalf("斷線前 /healthz 應為 200，實際 status=%d body=%s", status, body)
	}
	assertMatchesSchema(t, kinRouter, req, status, header, body)

	pgStop()

	deadline := time.Now().Add(15 * time.Second)
	for {
		req, status, header, body := contractRequest(t, http.MethodGet, baseURL+"/healthz", nil, output)
		if status == http.StatusServiceUnavailable {
			assertMatchesSchema(t, kinRouter, req, status, header, body)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 /healthz 回報 503 逾時：最後 status=%d body=%s", status, body)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
