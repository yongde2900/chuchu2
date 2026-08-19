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

// propertyModel 對應 properties 資料表，欄位需與
// db/20260819120000_create_properties.up.sql 保持一致。
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

// PropertyRepository 是 property.Repository 的 Postgres 實作。
type PropertyRepository struct {
	db *bun.DB
}

// New 用給定的 db 建立 PropertyRepository。
func New(db *bun.DB) *PropertyRepository {
	return &PropertyRepository{db: db}
}

// 編譯期斷言：PropertyRepository 滿足 property.Repository。
var _ property.Repository = (*PropertyRepository)(nil)

// Create 新增一筆物件。
//
// 刻意不先 SELECT 再 INSERT 判斷重複（有 race）：直接 INSERT，讓
// properties_address_key 這個唯一索引把關，命中時把 Postgres SQLSTATE 23505
// 轉成 apperr.CodePropertyDuplicate。
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

// toModel 把領域層的 *property.Property 轉成 bun 用的 propertyModel。
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

// toDomain 把 propertyModel 轉回領域層的 *property.Property（toModel 的反向轉換）。
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

// GetByID 依 id 查詢單一物件；查無資料時把 bun 的 sql.ErrNoRows 轉譯成
// apperr.CodePropertyNotFound。
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

// Update 以 p.ID 為鍵，把 p 整筆（除 id 外的所有欄位）覆寫回 properties 資料
// 表。呼叫端（Service.Update／Service.ChangeStatus）已經先 GetByID 確認資料
// 存在，這裡若 RowsAffected 為 0（理論上不會發生，除非資料在兩次呼叫間被
// 刪除），仍轉譯成 apperr.CodePropertyNotFound，不讓呼叫端誤判為成功。
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

// List 依 f 分頁列出物件，固定以 created_at 由新到舊排序、以 id 為次要排序鍵
// （避免同一 created_at 時分頁跳號）；Total 是套用篩選後、分頁前的總筆數，
// 用 ScanAndCount 在同一次查詢中取得（不受 Limit／Offset 影響）。
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
