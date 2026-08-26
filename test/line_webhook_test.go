// 本檔對應 BDD feature「接收 LINE Messaging API 的 follow / unfollow webhook」。
//
// 除了「資料庫寫入失敗」那個 scenario 外，全部使用 TestMain 起的共用 Postgres／
// Redis 容器（透過 startInProcessAPI(t, sharedPostgres(), sharedRedis())）——
// 每個 scenario 用不同的 userId，共用容器不會互相汙染。
package test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/yongde2900/chuchu2/internal/testsupport"
)

// lineChannelSecret 必須與 config/test.yaml 的 line.channel_secret 一致——
// 子行程啟動的測試讀 yaml，行程內啟動的測試（startInProcessAPI）用這個常數，
// 兩邊對不上會讓 webhook 測試全部 401，且不容易看出原因。
const lineChannelSecret = "test-channel-secret"

// lineSignature 用 HMAC-SHA256 + base64 對 body 算出 LINE 要求的 x-line-signature。
// SDK 只提供驗證（webhook.ValidateSignature），沒有「產生簽章」的輔助函式，
// 測試端只能自己算。
func lineSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// postLineWebhook 送出一個 POST /webhooks/line 請求。signature 為空字串時
// 完全不帶 x-line-signature header（對應「缺少 header」的 scenario），
// 而不是帶一個空字串的 header——兩者對 LINE 平台／SDK 的驗證行為不同。
func postLineWebhook(t *testing.T, baseURL string, body []byte, signature string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/webhooks/line", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("建立 webhook 請求失敗: %v", err)
	}
	if signature != "" {
		req.Header.Set("x-line-signature", signature)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /webhooks/line 失敗: %v", err)
	}
	return resp
}

func followEventBody(userID string, occurredAtMillis int64) []byte {
	return lineWebhookEventBody("follow", userID, occurredAtMillis)
}

func unfollowEventBody(userID string, occurredAtMillis int64) []byte {
	return lineWebhookEventBody("unfollow", userID, occurredAtMillis)
}

// lineWebhookEventBody 組出單一 follow／unfollow 事件的 webhook body。
// webhookEventId／replyToken 的值不影響測試斷言，固定填一組合法格式的字串。
func lineWebhookEventBody(eventType, userID string, occurredAtMillis int64) []byte {
	body := map[string]any{
		"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"events": []map[string]any{
			{
				"type":      eventType,
				"mode":      "active",
				"timestamp": occurredAtMillis,
				"source": map[string]any{
					"type":   "user",
					"userId": userID,
				},
				"webhookEventId": "01FZ74A0TQ3DAK6H0V06KZ8Y8X",
				"replyToken":     "reply-token",
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		// body 是固定結構的 map，marshal 不會失敗。
		panic(fmt.Sprintf("marshal line webhook body 失敗: %v", err))
	}
	return raw
}

// eventOfTypeBody 組出只包含一個指定型別事件（如 message/postback/join）的
// webhook body，用於「非 follow/unfollow 事件被忽略」的 Scenario Outline。
// 只填 type 欄位：SDK 對已知事件型別的其他欄位是「有才解析」，body 缺欄位
// 仍會成功 unmarshal，最終落在 toDomainEvents 的 default case 被略過。
func eventOfTypeBody(eventType string) []byte {
	body := map[string]any{
		"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"events": []map[string]any{
			{"type": eventType},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(fmt.Sprintf("marshal line webhook body 失敗: %v", err))
	}
	return raw
}

// lineErrorResponseBody 對應 httpx.ErrorBody 的形狀，這裡不直接 import
// internal/httpx 的型別，用等價的匿名結構驗證回應「形狀」本身也符合契約。
type lineErrorResponseBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func decodeLineErrorBody(t *testing.T, r io.Reader) lineErrorResponseBody {
	t.Helper()

	var body lineErrorResponseBody
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		t.Fatalf("解析錯誤回應 body 失敗: %v", err)
	}
	return body
}

// openPostgresDSN 對指定的 dsn 開一個獨立的 *bun.DB 連線，供直接驗證資料庫狀態用。
func openPostgresDSN(t *testing.T, dsn string) *bun.DB {
	t.Helper()

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	bunDB := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { bunDB.Close() })

	return bunDB
}

// lineUserRow 查詢 line_users 中指定 userID 的記錄。found 為 false 時代表查無資料，
// 不是錯誤——「查無資料」正是簽章驗證失敗那幾個 scenario 要斷言的結果。
func lineUserRow(t *testing.T, dsn, userID string) (status string, lastEventAt int64, found bool) {
	t.Helper()

	db := openPostgresDSN(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	row := db.QueryRowContext(ctx,
		"SELECT status, last_event_at FROM line_users WHERE line_user_id = ?", userID)
	if err := row.Scan(&status, &lastEventAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false
		}
		t.Fatalf("查詢 line_users 失敗: %v", err)
	}
	return status, lastEventAt, true
}

func lineUsersCount(t *testing.T, dsn string) int {
	t.Helper()

	db := openPostgresDSN(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM line_users").Scan(&count); err != nil {
		t.Fatalf("查詢 line_users 筆數失敗: %v", err)
	}
	return count
}

func TestLineWebhook_ValidSignature_FollowEvent_Accepted(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000001"
	body := followEventBody(userID, 1462629479859)

	resp := postLineWebhook(t, baseURL, body, lineSignature(lineChannelSecret, body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusOK, output())
	}

	status, _, found := lineUserRow(t, sharedPostgres(), userID)
	if !found {
		t.Fatalf("line_users 中查不到 %q 的記錄", userID)
	}
	if status != "FOLLOWING" {
		t.Fatalf("status = %q, want %q", status, "FOLLOWING")
	}
}

func TestLineWebhook_WrongSignature_RejectedWithoutSideEffect(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000002"
	body := followEventBody(userID, 1462629479859)

	resp := postLineWebhook(t, baseURL, body, lineSignature("some-other-channel-secret", body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusUnauthorized, output())
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want prefix application/json", ct)
	}

	errBody := decodeLineErrorBody(t, resp.Body)
	if errBody.Code != "LINE_SIGNATURE_INVALID" {
		t.Fatalf("code = %q, want %q", errBody.Code, "LINE_SIGNATURE_INVALID")
	}

	if _, _, found := lineUserRow(t, sharedPostgres(), userID); found {
		t.Fatalf("line_users 中不應該有 %q 的記錄", userID)
	}
}

func TestLineWebhook_MissingSignatureHeader_RejectedWithoutSideEffect(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000003"
	body := followEventBody(userID, 1462629479859)

	resp := postLineWebhook(t, baseURL, body, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusUnauthorized, output())
	}

	errBody := decodeLineErrorBody(t, resp.Body)
	if errBody.Code != "LINE_SIGNATURE_INVALID" {
		t.Fatalf("code = %q, want %q", errBody.Code, "LINE_SIGNATURE_INVALID")
	}

	if _, _, found := lineUserRow(t, sharedPostgres(), userID); found {
		t.Fatalf("line_users 中不應該有 %q 的記錄", userID)
	}
}

func TestLineWebhook_ValidSignature_InvalidJSON_Rejected(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	body := []byte("{")

	resp := postLineWebhook(t, baseURL, body, lineSignature(lineChannelSecret, body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusBadRequest, output())
	}

	errBody := decodeLineErrorBody(t, resp.Body)
	if errBody.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q, want %q", errBody.Code, "VALIDATION_FAILED")
	}
}

func TestLineWebhook_EmptyEventsArray_Accepted(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	before := lineUsersCount(t, sharedPostgres())

	body := []byte(`{"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "events": []}`)
	resp := postLineWebhook(t, baseURL, body, lineSignature(lineChannelSecret, body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusOK, output())
	}

	after := lineUsersCount(t, sharedPostgres())
	if after != before {
		t.Fatalf("line_users 筆數變成 %d，want 維持 %d", after, before)
	}
}

func TestLineWebhook_NonFollowUnfollowEvents_IgnoredButAccepted(t *testing.T) {
	for _, eventType := range []string{"message", "postback", "join"} {
		t.Run(eventType, func(t *testing.T) {
			baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

			before := lineUsersCount(t, sharedPostgres())

			body := eventOfTypeBody(eventType)
			resp := postLineWebhook(t, baseURL, body, lineSignature(lineChannelSecret, body))
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusOK, output())
			}

			after := lineUsersCount(t, sharedPostgres())
			if after != before {
				t.Fatalf("line_users 筆數變成 %d，want 維持 %d", after, before)
			}
		})
	}
}

// 用專屬、未跑過 migration 的 Postgres 容器（沒有 line_users 資料表）製造寫入失敗，
// 驗證失敗會回 500 讓 LINE 有機會重送，而不是把底層 SQL 錯誤洩漏給呼叫端。
// 必須用 testsupport.StartPostgres(t) 開專屬容器，絕不可動 TestMain 的共用容器。
func TestLineWebhook_RepositoryWriteFailure_InternalServerError(t *testing.T) {
	dsn, _ := testsupport.StartPostgres(t)

	baseURL, output := startInProcessAPI(t, dsn, sharedRedis())

	body := followEventBody("U0000000000000000000000000000098", 1462629479859)
	resp := postLineWebhook(t, baseURL, body, lineSignature(lineChannelSecret, body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusInternalServerError, output())
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want prefix application/json", ct)
	}

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("讀取回應 body 失敗: %v", err)
	}

	var errBody lineErrorResponseBody
	if err := json.Unmarshal(rawBody, &errBody); err != nil {
		t.Fatalf("解析錯誤回應 body 失敗: %v, raw=%s", err, rawBody)
	}
	if errBody.Code != "INTERNAL" {
		t.Fatalf("code = %q, want %q", errBody.Code, "INTERNAL")
	}
	if strings.Contains(string(rawBody), "line_users") || strings.Contains(string(rawBody), "SQLSTATE") {
		t.Fatalf("回應 body 洩漏了底層 SQL 錯誤: %s", rawBody)
	}
}
