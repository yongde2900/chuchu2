// Package line 是「LINE 官方帳號 follow/unfollow」的領域層：型別與 Service。
//
// 這一層刻意不 import github.com/uptrace/bun（持久化細節屬於 pgrepo 子套件）、
// 不 import net/http、也不 import github.com/line/line-bot-sdk-go（webhook 轉譯
// 屬於 webhookhttp 子套件），維持領域邏輯與框架無關。
package line

import "time"

// Status 是使用者目前對官方帳號的關係狀態。
type Status string

const (
	StatusFollowing Status = "FOLLOWING"
	StatusBlocked   Status = "BLOCKED"
)

// EventType 是 LINE webhook 事件的種類，只保留與追蹤狀態相關的兩種。
type EventType string

const (
	EventTypeFollow   EventType = "FOLLOW"
	EventTypeUnfollow EventType = "UNFOLLOW"
)

// Event 是 transport 層轉譯後的領域事件。OccurredAtMillis 直接沿用 LINE 的毫秒時間戳，
// 不轉成 time.Time：它在領域中的唯一用途是比大小擋亂序。
type Event struct {
	UserID           string
	Type             EventType
	OccurredAtMillis int64
}

// Status 是本事件套用後該有的狀態：FOLLOW → FOLLOWING、UNFOLLOW → BLOCKED。
func (e Event) Status() Status {
	if e.Type == EventTypeFollow {
		return StatusFollowing
	}
	return StatusBlocked
}

type User struct {
	UserID            string
	Status            Status
	LastEventAtMillis int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
