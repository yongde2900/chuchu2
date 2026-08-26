// 本檔對應 BDD feature「接收 LINE Messaging API 的 follow / unfollow webhook」
// 的事件語意端到端 scenarios：unfollow 標記封鎖而非刪除、封鎖後重加好友回到
// 同一筆記錄、多事件一次處理、重送冪等、亂序事件不覆蓋較新狀態。
//
// 重用 line_webhook_test.go 定義的 helper（lineChannelSecret／lineSignature／
// postLineWebhook／followEventBody／unfollowEventBody／lineUserRow／
// lineUsersCount／openPostgresDSN），全部走共用容器（sharedPostgres／
// sharedRedis）——本檔用到的每個 userId 都是獨立的，不會互相汙染。
package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// lineUserCreatedAt 查詢 line_users 中指定 userID 的 created_at，用來驗證
// 「unfollow 標記封鎖」沒有走「刪除後重建」這條會通過純狀態檢查、但其實
// 弄丟原始建立時間的邪路。
func lineUserCreatedAt(t *testing.T, dsn, userID string) (createdAt time.Time, found bool) {
	t.Helper()

	db := openPostgresDSN(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	row := db.QueryRowContext(ctx,
		"SELECT created_at FROM line_users WHERE line_user_id = ?", userID)
	if err := row.Scan(&createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false
		}
		t.Fatalf("查詢 line_users.created_at 失敗: %v", err)
	}
	return createdAt, true
}

// twoFollowEventsBody 組出一次帶兩個 follow 事件（依序為 userA、userB）的
// webhook body，用於「一次 webhook 帶多個事件」的 scenario——
// followEventBody／unfollowEventBody 只能組單一事件，這裡不重複那兩個函式，
// 直接組出等價結構的多事件版本。
func twoFollowEventsBody(userA string, timestampA int64, userB string, timestampB int64) []byte {
	body := map[string]any{
		"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"events": []map[string]any{
			{
				"type":           "follow",
				"mode":           "active",
				"timestamp":      timestampA,
				"source":         map[string]any{"type": "user", "userId": userA},
				"webhookEventId": "01FZ74A0TQ3DAK6H0V06KZ8Y8X",
				"replyToken":     "reply-token-a",
			},
			{
				"type":           "follow",
				"mode":           "active",
				"timestamp":      timestampB,
				"source":         map[string]any{"type": "user", "userId": userB},
				"webhookEventId": "01FZ74A0TQ3DAK6H0V06KZ8Y8Y",
				"replyToken":     "reply-token-b",
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(fmt.Sprintf("marshal line webhook body 失敗: %v", err))
	}
	return raw
}

// TestLineEventSemantics_Unfollow_MarksBlocked_DoesNotDelete 對應 scenario
// 「unfollow 事件把好友標記為封鎖而不是刪除記錄」。
func TestLineEventSemantics_Unfollow_MarksBlocked_DoesNotDelete(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000010"

	// Given 該 userId 已經是狀態 FOLLOWING 的好友。
	followBody := followEventBody(userID, 1000)
	followResp := postLineWebhook(t, baseURL, followBody, lineSignature(lineChannelSecret, followBody))
	followResp.Body.Close()
	if followResp.StatusCode != http.StatusOK {
		t.Fatalf("建立初始 FOLLOWING 狀態失敗: status = %d, output:\n%s", followResp.StatusCode, output())
	}
	createdAtBefore, found := lineUserCreatedAt(t, sharedPostgres(), userID)
	if !found {
		t.Fatalf("建立初始 FOLLOWING 狀態後查不到 %q 的記錄", userID)
	}

	// When 一個該 userId 的 unfollow 事件送達。
	unfollowBody := unfollowEventBody(userID, 2000)
	resp := postLineWebhook(t, baseURL, unfollowBody, lineSignature(lineChannelSecret, unfollowBody))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusOK, output())
	}

	status, _, found := lineUserRow(t, sharedPostgres(), userID)
	if !found {
		t.Fatalf("line_users 中 %q 的記錄不見了，unfollow 不應該刪除記錄", userID)
	}
	if status != "BLOCKED" {
		t.Fatalf("status = %q, want %q", status, "BLOCKED")
	}

	// created_at 必須不變：若是「刪除後以 BLOCKED 重新插入」，狀態檢查也會過，
	// 只有比對 created_at 才抓得到。
	createdAtAfter, found := lineUserCreatedAt(t, sharedPostgres(), userID)
	if !found {
		t.Fatalf("unfollow 後查不到 %q 的 created_at", userID)
	}
	if !createdAtAfter.Equal(createdAtBefore) {
		t.Fatalf("created_at 變了：before = %v, after = %v，代表記錄被刪除重建而非原地更新",
			createdAtBefore, createdAtAfter)
	}
}

// TestLineEventSemantics_RefollowAfterBlock_SameRow_BackToFollowing 對應 scenario
// 「封鎖後重新加好友會讓同一筆記錄回到 FOLLOWING，不會產生第二筆」。
func TestLineEventSemantics_RefollowAfterBlock_SameRow_BackToFollowing(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000011"

	// Given 該 userId 的記錄狀態為 BLOCKED（先 follow 建立記錄，再 unfollow 封鎖）。
	followBody := followEventBody(userID, 1000)
	followResp := postLineWebhook(t, baseURL, followBody, lineSignature(lineChannelSecret, followBody))
	followResp.Body.Close()
	if followResp.StatusCode != http.StatusOK {
		t.Fatalf("建立初始 FOLLOWING 狀態失敗: status = %d, output:\n%s", followResp.StatusCode, output())
	}

	unfollowBody := unfollowEventBody(userID, 2000)
	unfollowResp := postLineWebhook(t, baseURL, unfollowBody, lineSignature(lineChannelSecret, unfollowBody))
	unfollowResp.Body.Close()
	if unfollowResp.StatusCode != http.StatusOK {
		t.Fatalf("建立初始 BLOCKED 狀態失敗: status = %d, output:\n%s", unfollowResp.StatusCode, output())
	}
	if status, _, found := lineUserRow(t, sharedPostgres(), userID); !found || status != "BLOCKED" {
		t.Fatalf("前置狀態不是 BLOCKED: status = %q, found = %v", status, found)
	}

	// When 一個時間戳比封鎖事件更新的 follow 事件送達。
	refollowBody := followEventBody(userID, 3000)
	resp := postLineWebhook(t, baseURL, refollowBody, lineSignature(lineChannelSecret, refollowBody))
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

	count := lineUserCountForID(t, sharedPostgres(), userID)
	if count != 1 {
		t.Fatalf("line_users 中 %q 的記錄有 %d 筆, want 恰好 1 筆", userID, count)
	}
}

// TestLineEventSemantics_MultipleEventsInOneRequest_AllProcessed 對應 scenario
// 「一次 webhook 帶多個事件時全部都會被處理」。
func TestLineEventSemantics_MultipleEventsInOneRequest_AllProcessed(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userA := "U0000000000000000000000000000020"
	userB := "U0000000000000000000000000000021"

	body := twoFollowEventsBody(userA, 1000, userB, 1000)
	resp := postLineWebhook(t, baseURL, body, lineSignature(lineChannelSecret, body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, output:\n%s", resp.StatusCode, http.StatusOK, output())
	}

	statusA, _, foundA := lineUserRow(t, sharedPostgres(), userA)
	if !foundA {
		t.Fatalf("line_users 中查不到 %q 的記錄", userA)
	}
	if statusA != "FOLLOWING" {
		t.Fatalf("userA status = %q, want %q", statusA, "FOLLOWING")
	}

	statusB, _, foundB := lineUserRow(t, sharedPostgres(), userB)
	if !foundB {
		t.Fatalf("line_users 中查不到 %q 的記錄", userB)
	}
	if statusB != "FOLLOWING" {
		t.Fatalf("userB status = %q, want %q", statusB, "FOLLOWING")
	}
}

// TestLineEventSemantics_ResendSameRequestTwice_Idempotent 對應 scenario
// 「同一個事件被重送兩次仍然只有一筆記錄」。刻意重送完全相同的 bytes 與簽章，
// 而不是重新產生 body：換一個新的 timestamp 就變成另一個事件，不是重送。
func TestLineEventSemantics_ResendSameRequestTwice_Idempotent(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000030"
	body := followEventBody(userID, 1000)
	signature := lineSignature(lineChannelSecret, body)

	resp1 := postLineWebhook(t, baseURL, body, signature)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("第一次 status = %d, want %d, output:\n%s", resp1.StatusCode, http.StatusOK, output())
	}

	resp2 := postLineWebhook(t, baseURL, body, signature)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("第二次 status = %d, want %d, output:\n%s", resp2.StatusCode, http.StatusOK, output())
	}

	count := lineUserCountForID(t, sharedPostgres(), userID)
	if count != 1 {
		t.Fatalf("line_users 中 %q 的記錄有 %d 筆, want 恰好 1 筆", userID, count)
	}

	status, _, found := lineUserRow(t, sharedPostgres(), userID)
	if !found {
		t.Fatalf("line_users 中查不到 %q 的記錄", userID)
	}
	if status != "FOLLOWING" {
		t.Fatalf("status = %q, want %q", status, "FOLLOWING")
	}
}

// TestLineEventSemantics_OutOfOrderOlderUnfollow_DoesNotOverwriteNewerFollow
// 對應 scenario 「亂序抵達的舊 unfollow 事件不會覆蓋較新的 follow 狀態」。
// 回應仍是 200（從 LINE 的角度這個請求已被正確處理），只是套用結果是整筆不動。
func TestLineEventSemantics_OutOfOrderOlderUnfollow_DoesNotOverwriteNewerFollow(t *testing.T) {
	baseURL, output := startInProcessAPI(t, sharedPostgres(), sharedRedis())

	userID := "U0000000000000000000000000000031"

	// Given 該 userId 的記錄狀態為 FOLLOWING，且最後套用的事件時間戳為 2000。
	followBody := followEventBody(userID, 2000)
	followResp := postLineWebhook(t, baseURL, followBody, lineSignature(lineChannelSecret, followBody))
	followResp.Body.Close()
	if followResp.StatusCode != http.StatusOK {
		t.Fatalf("建立初始 FOLLOWING 狀態失敗: status = %d, output:\n%s", followResp.StatusCode, output())
	}

	// When 一個時間戳為 1000（比 2000 舊）的 unfollow 事件送達。
	staleUnfollowBody := unfollowEventBody(userID, 1000)
	resp := postLineWebhook(t, baseURL, staleUnfollowBody, lineSignature(lineChannelSecret, staleUnfollowBody))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d（亂序事件從 LINE 角度仍算已處理）, output:\n%s",
			resp.StatusCode, http.StatusOK, output())
	}

	status, lastEventAt, found := lineUserRow(t, sharedPostgres(), userID)
	if !found {
		t.Fatalf("line_users 中查不到 %q 的記錄", userID)
	}
	if status != "FOLLOWING" {
		t.Fatalf("status = %q, want 仍為 %q（不應被較舊的 unfollow 覆蓋）", status, "FOLLOWING")
	}
	// 只檢查 status 抓不到「更新了時間戳但沒改狀態」這種半吊子 bug，
	// 所以額外確認 last_event_at 也完全沒被亂序事件動到。
	if lastEventAt != 2000 {
		t.Fatalf("last_event_at = %d, want 仍為 2000（不應被較舊事件的時間戳覆寫）", lastEventAt)
	}
}

// lineUserCountForID 查詢 line_users 中指定 userID 的記錄筆數，用來斷言
// 「upsert 而非新增第二筆」——lineUsersCount 是整張表的筆數，共用容器裡
// 其他 scenario 也會寫入資料，不能拿來斷言單一 userId 恰好 1 筆。
func lineUserCountForID(t *testing.T, dsn, userID string) int {
	t.Helper()

	db := openPostgresDSN(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM line_users WHERE line_user_id = ?", userID).Scan(&count); err != nil {
		t.Fatalf("查詢 line_users 筆數失敗: %v", err)
	}
	return count
}
