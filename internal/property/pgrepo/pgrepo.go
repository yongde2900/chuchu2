// Package pgrepo 是 internal/property.Repository 的 Postgres 實作，也是
// internal/property 底下唯一 import github.com/uptrace/bun 的子套件——持久化
// 細節（bun model、SQLSTATE 判斷）刻意跟領域層（internal/property）隔開。
package pgrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/property"
)

// pgUniqueViolation 是 Postgres 回報「唯一鍵衝突」的 SQLSTATE
// （https://www.postgresql.org/docs/current/errcodes-appendix.html）。
const pgUniqueViolation = "23505"

// 欄位需與 db/ 底下的 create_properties migration 保持一致。
type propertyModel struct {
	bun.BaseModel `bun:"table:properties"`

	ID            uuid.UUID       `bun:"id,pk"`
	City          string          `bun:"city"`
	District      string          `bun:"district"`
	StreetAddress string          `bun:"street_address"`
	Floor         string          `bun:"floor"`
	RoomNo        string          `bun:"room_no"`
	Layout        string          `bun:"layout"`
	AreaPing      decimal.Decimal `bun:"area_ping"`
	MonthlyRent   decimal.Decimal `bun:"monthly_rent"`
	ManagementFee decimal.Decimal `bun:"management_fee"`
	DepositMonths int             `bun:"deposit_months"`
	RentalMode    string          `bun:"rental_mode"`
	Status        string          `bun:"status"`
	LandlordName  string          `bun:"landlord_name"`
	LandlordPhone string          `bun:"landlord_phone"`
	CreatedAt     time.Time       `bun:"created_at"`
	UpdatedAt     time.Time       `bun:"updated_at"`
}

type PropertyRepository struct {
	db *bun.DB
}

func New(db *bun.DB) *PropertyRepository {
	return &PropertyRepository{db: db}
}

var _ property.Repository = (*PropertyRepository)(nil)

// 刻意不先 SELECT 再 INSERT 判斷重複（那有 race）：直接 INSERT，
// 讓 properties_address_key 唯一索引把關，命中時轉成 CodePropertyDuplicate。
func (r *PropertyRepository) Create(ctx context.Context, p *property.Property) error {
	model := toModel(p)

	if _, err := r.db.NewInsert().Model(model).Exec(ctx); err != nil {
		if pgErr, ok := errors.AsType[pgdriver.Error](err); ok && pgErr.Field('C') == pgUniqueViolation {
			return apperr.New(apperr.CodePropertyDuplicate, "相同門牌的物件已存在")
		}
		return fmt.Errorf("新增物件失敗: %w", err)
	}

	return nil
}

func toModel(p *property.Property) *propertyModel {
	return &propertyModel{
		ID:            p.ID,
		City:          p.City,
		District:      p.District,
		StreetAddress: p.StreetAddress,
		Floor:         p.Floor,
		RoomNo:        p.RoomNo,
		Layout:        string(p.Layout),
		AreaPing:      p.AreaPing,
		MonthlyRent:   p.MonthlyRent,
		ManagementFee: p.ManagementFee,
		DepositMonths: p.DepositMonths,
		RentalMode:    string(p.RentalMode),
		Status:        string(p.Status),
		LandlordName:  p.LandlordName,
		LandlordPhone: p.LandlordPhone,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func toDomain(m *propertyModel) *property.Property {
	return &property.Property{
		ID:            m.ID,
		City:          m.City,
		District:      m.District,
		StreetAddress: m.StreetAddress,
		Floor:         m.Floor,
		RoomNo:        m.RoomNo,
		Layout:        property.Layout(m.Layout),
		AreaPing:      m.AreaPing,
		MonthlyRent:   m.MonthlyRent,
		ManagementFee: m.ManagementFee,
		DepositMonths: m.DepositMonths,
		RentalMode:    property.RentalMode(m.RentalMode),
		Status:        property.Status(m.Status),
		LandlordName:  m.LandlordName,
		LandlordPhone: m.LandlordPhone,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

// 查無資料時把 sql.ErrNoRows 轉譯成 apperr.CodePropertyNotFound。
func (r *PropertyRepository) GetByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	model := new(propertyModel)

	if err := r.db.NewSelect().Model(model).Where("id = ?", id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.New(apperr.CodePropertyNotFound, "找不到指定的物件")
		}
		return nil, fmt.Errorf("查詢物件失敗: %w", err)
	}

	return toDomain(model), nil
}

// RowsAffected 為 0 理論上不會發生（呼叫端已先 GetByID 確認存在），
// 但仍轉成 CodePropertyNotFound，不讓「資料在兩次呼叫間被刪除」被誤判為成功。
func (r *PropertyRepository) Update(ctx context.Context, p *property.Property) error {
	model := toModel(p)

	res, err := r.db.NewUpdate().Model(model).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新物件失敗: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("讀取更新物件影響筆數失敗: %w", err)
	}
	if n == 0 {
		return apperr.New(apperr.CodePropertyNotFound, "找不到指定的物件")
	}

	return nil
}

// 以 created_at 由新到舊排序、id 為次要鍵，避免同一 created_at 時分頁跳號。
// Total 用 ScanAndCount 在同一次查詢取得，不受 Limit／Offset 影響。
func (r *PropertyRepository) List(ctx context.Context, f property.ListFilter) (property.ListResult, error) {
	var models []*propertyModel

	q := r.db.NewSelect().Model(&models)
	if f.Status != nil {
		q = q.Where("status = ?", string(*f.Status))
	}
	if f.RentalMode != nil {
		q = q.Where("rental_mode = ?", string(*f.RentalMode))
	}
	if f.City != "" {
		q = q.Where("city = ?", f.City)
	}

	offset := (f.Page - 1) * f.PageSize
	total, err := q.
		Order("created_at DESC", "id DESC").
		Limit(f.PageSize).
		Offset(offset).
		ScanAndCount(ctx)
	if err != nil {
		return property.ListResult{}, fmt.Errorf("查詢物件列表失敗: %w", err)
	}

	items := make([]*property.Property, 0, len(models))
	for _, m := range models {
		items = append(items, toDomain(m))
	}

	return property.ListResult{Items: items, Total: total}, nil
}
