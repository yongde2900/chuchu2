// 本檔對應 BDD scenario：「防護網不會干擾正常的成功回應」。
//
// 這是整合層的驗證：對行程內起起來的完整服務（middleware chain 裡已經掛上
// EnsureJSONError）送一個會成功的 GET /api/v1/properties 請求，
// 確認 200 且原始回應 bytes
// 就是合法的 PropertyList——尤其 items 是 "[]" 而不是 "null"，這件事
// 只有看原始 bytes 才驗證得到：json.Unmarshal 對 JSON null 與 JSON []
// 的解碼結果雖然不同（nil slice vs. 非 nil 空 slice），但很容易在斷言時
// 不小心用「長度是否為 0」去判斷，那樣兩者會被誤判成一樣。
//
// 單元層「同一個 handler 掛與不掛防護網，原始 bytes 完全相同」的驗證見
// internal/httpx/safetynet_test.go 的
// TestEnsureJSONError_SuccessPassthrough_ByteIdentical；本檔只驗證整合層
// 的「防護網真的掛在正式組裝出來的服務上，且沒有干擾成功回應」。
package test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestSafetyNet_SuccessfulPropertyList_NotInterferedWith 對應 BDD scenario
// 「防護網不會干擾正常的成功回應」。
func TestSafetyNet_SuccessfulPropertyList_NotInterferedWith(t *testing.T) {
	truncateProperties(t)

	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	resp, err := http.Get(baseURL + "/api/v1/properties")
	if err != nil {
		t.Fatalf("GET /api/v1/properties 失敗: %v, output:\n%s", err, output())
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("狀態碼 = %d, want %d, body=%s, output:\n%s", resp.StatusCode, http.StatusOK, rawBody, output())
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want 開頭為 %q, body=%s", contentType, "application/json", rawBody)
	}

	// 直接看原始 bytes：properties 資料表是空的（truncateProperties），
	// items 欄位必須是字面上的 "[]"，不能是 "null"——這是防護網「不緩衝
	// body、原樣放行成功回應」這個設計是否真的沒有意外改動 body 的
	// 最直接證據。
	raw := string(rawBody)
	if !strings.Contains(raw, `"items":[]`) {
		t.Fatalf("回應 body 中沒有找到原始的 \"items\":[]（可能被防護網動過手腳，或本來就回了 null）: %s", raw)
	}
	if strings.Contains(raw, `"items":null`) {
		t.Fatalf("回應 body 的 items 是 JSON null，want 空陣列 []: %s", raw)
	}

	var body propertyListBody
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("解析回應 JSON 失敗: %v, body=%s", err, rawBody)
	}
	if body.Total != 0 {
		t.Fatalf("total = %d, want 0, body=%s", body.Total, rawBody)
	}
	if body.Items == nil {
		t.Fatalf("items 解碼結果為 nil slice，want 非 nil 的空 slice, body=%s", rawBody)
	}
	if len(body.Items) != 0 {
		t.Fatalf("items 長度 = %d, want 0, body=%s", len(body.Items), rawBody)
	}
}
