// Package webhookhttp 是 LINE follow/unfollow webhook 的 transport 層，也是
// internal/line 底下唯一同時 import net/http 與
// github.com/line/line-bot-sdk-go 的子套件——SDK 的 webhook 套件本身就 import
// net/http（httphandler.go），任何 import 它的套件都會遞移帶進 net/http，
// 所以只有這裡（而不是 internal/line 或 internal/line/pgrepo）能碰它。
//
// 刻意不用 SDK 的 webhook.WebhookHandler：它自己寫 400/500 狀態碼，繞過
// 這個專案統一的 httpx.WriteError／apperr 錯誤中介層。改用 webhook.ParseRequest
// 自己轉譯錯誤。
package webhookhttp

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"

	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/httpx"
	"github.com/yongde2900/chuchu2/internal/line"
	"github.com/yongde2900/chuchu2/internal/server"
)

type Handler struct {
	svc           *line.Service
	channelSecret string
	logger        *slog.Logger
}

func NewHandler(svc *line.Service, channelSecret string, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, channelSecret: channelSecret, logger: logger}
}

// Mount 掛在 /webhooks/line，不在 /api/v1 之下——這條路由不走 spec-first 產生流程。
func (h *Handler) Mount() server.Mount {
	return func(r chi.Router) {
		r.Post("/webhooks/line", h.handle)
	}
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
	cb, err := webhook.ParseRequest(h.channelSecret, r)
	if err != nil {
		// 簽章錯誤與缺 header 都是 ErrInvalidSignature（缺 header 時
		// ValidateSignature 對空字串 base64-decode 出空 bytes，比對必敗），
		// 兩者都是 401；其餘（body 不是合法 JSON）是 400。
		if errors.Is(err, webhook.ErrInvalidSignature) {
			httpx.WriteError(w, r, h.logger, apperr.LineSignatureInvalid.WithError(err))
			return
		}
		// ParseRequest 的 JSON 解析錯誤把整個請求 body 內嵌進錯誤訊息，
		// 只能進 WithError（給 log），絕不能進 WithMessage（會外洩給呼叫端）。
		httpx.WriteError(w, r, h.logger,
			apperr.ValidationFailed.WithMessage("無法解析 LINE webhook 請求").WithError(err))
		return
	}

	if err := h.svc.Handle(r.Context(), toDomainEvents(cb.Events)); err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// toDomainEvents 只認 follow／unfollow 事件；其餘（message/postback/join 等，
// 含 SDK 未知型別的 webhook.UnknownEvent）安全略過，不視為錯誤——LINE 主控台的
// 連線確認請求（events 為空陣列）也會走到這裡，回傳空 slice。
//
// SDK 的 UnmarshalEvent／UnmarshalSource 目前回傳具體值型別（如
// webhook.FollowEvent、webhook.UserSource）而非指標，但這裡兩種都接——
// 依賴未匯出的回傳型別細節太脆弱，SDK 小版本更新就可能悄悄改掉。
func toDomainEvents(events []webhook.EventInterface) []line.Event {
	out := make([]line.Event, 0, len(events))

	for _, e := range events {
		// FollowEvent／UnfollowEvent 的 Source、Timestamp 欄位形狀一樣，只有型別名不同；
		// 先把可能的 pointer 形式解參照成 value，下面只需要處理 2 種 case 而不是 4 種。
		switch p := e.(type) {
		case *webhook.FollowEvent:
			e = *p
		case *webhook.UnfollowEvent:
			e = *p
		}

		var (
			eventType line.EventType
			source    webhook.SourceInterface
			timestamp int64
		)

		switch ev := e.(type) {
		case webhook.FollowEvent:
			eventType, source, timestamp = line.EventTypeFollow, ev.Source, ev.Timestamp
		case webhook.UnfollowEvent:
			eventType, source, timestamp = line.EventTypeUnfollow, ev.Source, ev.Timestamp
		default:
			continue
		}

		// Source 也可能是 group/room（沒有 UserId），這種事件略過，
		// 絕不能寫進一筆 line_user_id 為空字串的記錄。
		userID := userIDFromSource(source)
		if userID == "" {
			continue
		}

		out = append(out, line.Event{
			UserID:           userID,
			Type:             eventType,
			OccurredAtMillis: timestamp,
		})
	}

	return out
}

func userIDFromSource(source webhook.SourceInterface) string {
	// 同上：先把可能的 pointer 形式解參照成 value，下面只需要處理 1 種 case。
	if p, ok := source.(*webhook.UserSource); ok {
		source = *p
	}

	switch s := source.(type) {
	case webhook.UserSource:
		return s.UserId
	default:
		return ""
	}
}
