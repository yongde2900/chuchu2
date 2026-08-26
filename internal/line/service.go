package line

import (
	"context"
	"time"
)

// Repository 是領域層對持久化的唯一出口；實作在 pgrepo 子套件。
type Repository interface {
	// Upsert 以 UserID 為鍵寫入 u，實作必須同時滿足三件事：
	//  1. 沒有該 UserID 的記錄時新增一筆。
	//  2. 已有記錄且 u.LastEventAtMillis >= 既有值時，覆寫狀態與時間戳，絕不新增第二筆。
	//  3. 已有記錄且 u.LastEventAtMillis < 既有值時（亂序抵達的舊事件），整筆保持不變，
	//     且不算錯誤。
	// 第 3 點是擋住亂序重送的唯一防線，而且必須在單一 SQL 敘述內完成——先 SELECT 再 UPDATE
	// 的兩次來回之間有競態窗口，LINE 會併發重送同一批事件。
	Upsert(ctx context.Context, u *User) error
}

// Service 封裝事件套用與持久化。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Handle 依抵達順序逐一套用 events；任一事件寫入失敗即中止並回傳該錯誤，
// 由 HTTP 層轉成 500 讓 LINE 重送整批。本輪刻意沒有「部分成功」的語意。
func (s *Service) Handle(ctx context.Context, events []Event) error {
	for _, e := range events {
		now := time.Now().UTC()
		u := &User{
			UserID:            e.UserID,
			Status:            e.Status(),
			LastEventAtMillis: e.OccurredAtMillis,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := s.repo.Upsert(ctx, u); err != nil {
			return err
		}
	}
	return nil
}
