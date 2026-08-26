package webhookhttp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/yongde2900/chuchu2/internal/line"
)

const testChannelSecret = "test-channel-secret"

// fakeRepo 是 line.Repository 的假實作，記錄 Upsert 收到什麼、可以注入固定錯誤。
type fakeRepo struct {
	upserted []line.User
	err      error
}

func (f *fakeRepo) Upsert(_ context.Context, u *line.User) error {
	if f.err != nil {
		return f.err
	}
	f.upserted = append(f.upserted, *u)
	return nil
}

func newTestHandler(repo *fakeRepo) http.Handler {
	svc := line.NewService(repo)
	h := NewHandler(svc, testChannelSecret, slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := chi.NewRouter()
	h.Mount()(r)
	return r
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func followEventBody(userID string) []byte {
	body := fmt.Sprintf(`{
		"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"events": [
			{
				"type": "follow",
				"mode": "active",
				"timestamp": 1462629479859,
				"source": {"type": "user", "userId": %q},
				"webhookEventId": "01FZ74A0TQ3DAK6H0V06KZ8Y8X",
				"deliveryContext": {"isRedelivery": false},
				"replyToken": "reply-token"
			}
		]
	}`, userID)
	return []byte(body)
}

func eventOfTypeBody(eventType string) []byte {
	body := fmt.Sprintf(`{
		"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"events": [
			{"type": %q}
		]
	}`, eventType)
	return []byte(body)
}

func doRequest(t *testing.T, handler http.Handler, body []byte, signature string, setSignature bool) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/webhooks/line", bytes.NewReader(body))
	if setSignature {
		req.Header.Set("x-line-signature", signature)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("解析錯誤回應 body 失敗: %v, raw=%s", err, rec.Body.String())
	}
	return body.Code
}

func TestHandle_ValidSignature_FollowEvent_Accepted(t *testing.T) {
	repo := &fakeRepo{}
	handler := newTestHandler(repo)

	body := followEventBody("U0000000000000000000000000000001")
	rec := doRequest(t, handler, body, sign(testChannelSecret, body), true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(repo.upserted) != 1 {
		t.Fatalf("len(repo.upserted) = %d, want 1", len(repo.upserted))
	}
	if repo.upserted[0].UserID != "U0000000000000000000000000000001" {
		t.Fatalf("UserID = %q, want %q", repo.upserted[0].UserID, "U0000000000000000000000000000001")
	}
	if repo.upserted[0].Status != line.StatusFollowing {
		t.Fatalf("Status = %q, want %q", repo.upserted[0].Status, line.StatusFollowing)
	}
}

func TestHandle_WrongSignature_Rejected(t *testing.T) {
	repo := &fakeRepo{}
	handler := newTestHandler(repo)

	body := followEventBody("U0000000000000000000000000000002")
	rec := doRequest(t, handler, body, sign("some-other-secret", body), true)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want prefix application/json", ct)
	}
	if code := decodeErrorCode(t, rec); code != "LINE_SIGNATURE_INVALID" {
		t.Fatalf("code = %q, want %q", code, "LINE_SIGNATURE_INVALID")
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("len(repo.upserted) = %d, want 0", len(repo.upserted))
	}
}

func TestHandle_MissingSignatureHeader_Rejected(t *testing.T) {
	repo := &fakeRepo{}
	handler := newTestHandler(repo)

	body := followEventBody("U0000000000000000000000000000003")
	rec := doRequest(t, handler, body, "", false)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec); code != "LINE_SIGNATURE_INVALID" {
		t.Fatalf("code = %q, want %q", code, "LINE_SIGNATURE_INVALID")
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("len(repo.upserted) = %d, want 0", len(repo.upserted))
	}
}

func TestHandle_ValidSignature_InvalidJSON_Rejected(t *testing.T) {
	repo := &fakeRepo{}
	handler := newTestHandler(repo)

	body := []byte("{")
	rec := doRequest(t, handler, body, sign(testChannelSecret, body), true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec); code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q, want %q", code, "VALIDATION_FAILED")
	}
	// ParseRequest 的解析錯誤把整個 body 內嵌進錯誤訊息，絕不能外洩給呼叫端。
	if strings.Contains(rec.Body.String(), "failed to unmarshal") {
		t.Fatalf("回應 body 洩漏了底層解析錯誤訊息: %s", rec.Body.String())
	}
}

func TestHandle_EmptyEventsArray_Accepted(t *testing.T) {
	repo := &fakeRepo{}
	handler := newTestHandler(repo)

	body := []byte(`{"destination": "Uxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "events": []}`)
	rec := doRequest(t, handler, body, sign(testChannelSecret, body), true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("len(repo.upserted) = %d, want 0", len(repo.upserted))
	}
}

func TestHandle_NonFollowUnfollowEvents_IgnoredButAccepted(t *testing.T) {
	for _, eventType := range []string{"message", "postback", "join"} {
		t.Run(eventType, func(t *testing.T) {
			repo := &fakeRepo{}
			handler := newTestHandler(repo)

			body := eventOfTypeBody(eventType)
			rec := doRequest(t, handler, body, sign(testChannelSecret, body), true)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if len(repo.upserted) != 0 {
				t.Fatalf("len(repo.upserted) = %d, want 0", len(repo.upserted))
			}
		})
	}
}

func TestHandle_RepositoryError_InternalServerError(t *testing.T) {
	repo := &fakeRepo{err: fmt.Errorf("connection refused to db at 10.0.0.5:5432")}
	handler := newTestHandler(repo)

	body := followEventBody("U0000000000000000000000000000099")
	rec := doRequest(t, handler, body, sign(testChannelSecret, body), true)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want prefix application/json", ct)
	}
	if code := decodeErrorCode(t, rec); code != "INTERNAL" {
		t.Fatalf("code = %q, want %q", code, "INTERNAL")
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatalf("回應 body 洩漏了底層錯誤訊息: %s", rec.Body.String())
	}
}
