// Package pgrepo 是 internal/line.Repository 的 Postgres 實作，也是
// internal/line 底下唯一 import github.com/uptrace/bun 的子套件——理由與
// internal/property/pgrepo 相同：持久化細節跟領域層隔開。
package pgrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/yongde2900/chuchu2/internal/line"
)

// 欄位需與 db/ 底下的 create_line_users migration 保持一致。
//
// alias 必須明確釘死成 line_users：.On(...) 觸發 bun 的
// `INSERT INTO "line_users" AS "<alias>"`，alias 預設值是底線化的 struct 型別
// 名稱（lineUserModel → line_user_model），不是表名——沒有 alias tag，
// Upsert 的 WHERE 子句會在執行期報 "missing FROM-clause entry for table
// \"line_users\""。
type lineUserModel struct {
	bun.BaseModel `bun:"table:line_users,alias:line_users"`

	UserID            string    `bun:"line_user_id,pk"`
	Status            string    `bun:"status"`
	LastEventAtMillis int64     `bun:"last_event_at"`
	CreatedAt         time.Time `bun:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at"`
}

type LineUserRepository struct {
	db *bun.DB
}

func New(db *bun.DB) *LineUserRepository {
	return &LineUserRepository{db: db}
}

var _ line.Repository = (*LineUserRepository)(nil)

// Upsert 用單一 SQL 敘述完成「沒有記錄就新增、事件較新就覆寫、事件較舊（亂序
// 重送）就整筆不動」——WHERE 子句掛在 DO UPDATE 上，擋亂序的判斷與寫入之間
// 沒有競態窗口。RowsAffected() == 0 不當錯誤處理：那正是亂序事件被正確擋下的樣子。
func (r *LineUserRepository) Upsert(ctx context.Context, u *line.User) error {
	model := toModel(u)

	_, err := r.db.NewInsert().
		Model(model).
		On("CONFLICT (line_user_id) DO UPDATE").
		Set("status = EXCLUDED.status").
		Set("last_event_at = EXCLUDED.last_event_at").
		Set("updated_at = EXCLUDED.updated_at").
		Where("line_users.last_event_at <= EXCLUDED.last_event_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("寫入 line_users 失敗: %w", err)
	}

	return nil
}

func toModel(u *line.User) *lineUserModel {
	return &lineUserModel{
		UserID:            u.UserID,
		Status:            string(u.Status),
		LastEventAtMillis: u.LastEventAtMillis,
		CreatedAt:         u.CreatedAt,
		UpdatedAt:         u.UpdatedAt,
	}
}
