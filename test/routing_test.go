// 本檔對應 BDD scenario：
// 「spec 宣告的每一個 endpoint 都真的路由得到」。
//
// 路徑清單直接用 api.GetSwagger() 讀出來（而不是手抄一份），這樣未來
// api/openapi.yaml 新增 endpoint 時，這個測試會自動涵蓋到，不會漏測。
package test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yongde2900/chuchu2/api"
)

// chiDefault404Body 是 chi 路由不到任何 handler 時的預設回應 body（純文字，
// 不是 JSON）。這個測試的核心斷言就是「服務的任何回應都不會是這一句」。
const chiDefault404Body = "404 page not found"

// placeholderPathParamID 是替換 spec 路徑樣板中 {id} 用的佔位值——是否為
// 服務中實際存在的資料無關緊要，這個測試只在乎「路由得到」，不在乎
// 「查得到資料」。
const placeholderPathParamID = "00000000-0000-0000-0000-000000000000"

// TestRouting_AllDeclaredEndpointsAreRouted 對 api/openapi.yaml 宣告的每一個
// path + method 各送出一次請求，確認沒有任何一次回應是 chi 的預設 404，且
// 每一次回應的 Content-Type 都以 "application/json" 開頭。
func TestRouting_AllDeclaredEndpointsAreRouted(t *testing.T) {
	doc, err := api.GetSwagger()
	if err != nil {
		t.Fatalf("api.GetSwagger() 失敗: %v", err)
	}

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	for _, pathTemplate := range doc.Paths.InMatchingOrder() {
		item := doc.Paths.Value(pathTemplate)
		for method := range item.Operations() {
			method := strings.ToUpper(method)
			pathTemplate := pathTemplate

			t.Run(method+" "+pathTemplate, func(t *testing.T) {
				actualPath := strings.ReplaceAll(pathTemplate, "{id}", placeholderPathParamID)

				var reqBody io.Reader
				if method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut {
					reqBody = strings.NewReader("{}")
				}

				req, err := http.NewRequest(method, baseURL+actualPath, reqBody)
				if err != nil {
					t.Fatalf("建立 request 失敗: %v", err)
				}
				if reqBody != nil {
					req.Header.Set("Content-Type", "application/json")
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("%s %s 失敗: %v, output:\n%s", method, actualPath, err, output())
				}
				defer resp.Body.Close()

				raw, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("讀取回應 body 失敗: %v", err)
				}

				if strings.TrimSpace(string(raw)) == chiDefault404Body {
					t.Fatalf("%s %s 回應是 chi 的預設 404，spec 宣告的路由沒有真的接上: body=%s", method, actualPath, raw)
				}

				contentType := resp.Header.Get("Content-Type")
				if !strings.HasPrefix(contentType, "application/json") {
					t.Fatalf("%s %s 的 Content-Type = %q, want 開頭為 %q, body=%s", method, actualPath, contentType, "application/json", raw)
				}
			})
		}
	}
}
