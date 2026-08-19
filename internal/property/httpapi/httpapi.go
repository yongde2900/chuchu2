// Package httpapi 是「物件（房源）」的 HTTP 轉譯層：request/response DTO 與
// route handler，也是 internal/property 底下唯一 import net/http（含
// github.com/go-chi/chi/v5）的子套件。
package httpapi

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/httpx"
	"github.com/yongde2900/chuchu2/internal/property"
	"github.com/yongde2900/chuchu2/internal/server"
)

// Handler 提供物件相關的 HTTP route handler。
type Handler struct {
	svc    *property.Service
	logger *slog.Logger
}

// NewHandler 用給定的 svc、logger 建立 Handler。
func NewHandler(svc *property.Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Mount 掛載 /api/v1/properties 相關路由：POST（建檔）、GET /{id}（單筆查詢）、
// GET（分頁列表）、PATCH /{id}（欄位更新，不含狀態）、
// POST /{id}/status（狀態機轉換，狀態變更唯一的入口）。
func (h *Handler) Mount() server.Mount {
	return func(r chi.Router) {
		r.Post("/api/v1/properties", h.create)
		r.Get("/api/v1/properties/{id}", h.getByID)
		r.Get("/api/v1/properties", h.list)
		r.Patch("/api/v1/properties/{id}", h.update)
		r.Post("/api/v1/properties/{id}/status", h.changeStatus)
	}
}

// createPropertyRequest 對應 POST /api/v1/properties 的 request body。
// 金額欄位刻意宣告為 string，讓十進位數字的解析與驗證統一交給
// property.CreateInput.Validate 處理，不在這一層先做任何轉型判斷。
type createPropertyRequest struct {
	City          string `json:"city"`
	District      string `json:"district"`
	StreetAddress string `json:"street_address"`
	Floor         string `json:"floor"`
	RoomNo        string `json:"room_no"`
	Layout        string `json:"layout"`
	AreaPing      string `json:"area_ping"`
	MonthlyRent   string `json:"monthly_rent"`
	ManagementFee string `json:"management_fee"`
	DepositMonths int    `json:"deposit_months"`
	RentalMode    string `json:"rental_mode"`
	LandlordName  string `json:"landlord_name"`
	LandlordPhone string `json:"landlord_phone"`
}

// toCreateInput 把 request body 轉成 property.CreateInput，欄位一對一映射，
// 不做任何額外轉換（驗證與解析交給領域層）。
func (req createPropertyRequest) toCreateInput() property.CreateInput {
	return property.CreateInput{
		City:          req.City,
		District:      req.District,
		StreetAddress: req.StreetAddress,
		Floor:         req.Floor,
		RoomNo:        req.RoomNo,
		Layout:        req.Layout,
		AreaPing:      req.AreaPing,
		MonthlyRent:   req.MonthlyRent,
		ManagementFee: req.ManagementFee,
		DepositMonths: req.DepositMonths,
		RentalMode:    req.RentalMode,
		LandlordName:  req.LandlordName,
		LandlordPhone: req.LandlordPhone,
	}
}

// propertyResponse 對應物件在 HTTP 回應中的 JSON 形狀。
//
// 金額欄位（AreaPing、MonthlyRent、ManagementFee）刻意宣告為 string 並用
// StringFixed(2) 格式化 —— decimal.Decimal 預設的 MarshalJSON 不會補齊小數
// 位數（25000.5 而非 25000.50），且 JSON number 在 JavaScript 客戶端往返
// 會損失十進位精度，兩者都不符合本專案「金額一律以字串往返」的慣例。
type propertyResponse struct {
	ID            string `json:"id"`
	City          string `json:"city"`
	District      string `json:"district"`
	StreetAddress string `json:"street_address"`
	Floor         string `json:"floor"`
	RoomNo        string `json:"room_no"`
	Layout        string `json:"layout"`
	AreaPing      string `json:"area_ping"`
	MonthlyRent   string `json:"monthly_rent"`
	ManagementFee string `json:"management_fee"`
	DepositMonths int    `json:"deposit_months"`
	RentalMode    string `json:"rental_mode"`
	Status        string `json:"status"`
	LandlordName  string `json:"landlord_name"`
	LandlordPhone string `json:"landlord_phone"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// formatMoney 把 d 格式化成固定兩位小數的字串（例如 25000.5 → "25000.50"）。
func formatMoney(d decimal.Decimal) string {
	return d.StringFixed(2)
}

// toPropertyResponse 把領域層的 *property.Property 轉成 propertyResponse。
func toPropertyResponse(p *property.Property) propertyResponse {
	return propertyResponse{
		ID:            p.ID.String(),
		City:          p.City,
		District:      p.District,
		StreetAddress: p.StreetAddress,
		Floor:         p.Floor,
		RoomNo:        p.RoomNo,
		Layout:        string(p.Layout),
		AreaPing:      formatMoney(p.AreaPing),
		MonthlyRent:   formatMoney(p.MonthlyRent),
		ManagementFee: formatMoney(p.ManagementFee),
		DepositMonths: p.DepositMonths,
		RentalMode:    string(p.RentalMode),
		Status:        string(p.Status),
		LandlordName:  p.LandlordName,
		LandlordPhone: p.LandlordPhone,
		CreatedAt:     p.CreatedAt.Format(timeFormat),
		UpdatedAt:     p.UpdatedAt.Format(timeFormat),
	}
}

// timeFormat 是時間戳欄位在 JSON 回應中的格式（RFC3339，帶時區）。
const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

// create 處理 POST /api/v1/properties：解析 request body、呼叫 Service.Create、
// 成功時回 201 與建立好的物件，失敗時交給 httpx.WriteError 統一轉譯。
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	req, err := httpx.DecodeJSON[createPropertyRequest](r)
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	p, err := h.svc.Create(r.Context(), req.toCreateInput())
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toPropertyResponse(p))
}

// getByID 處理 GET /api/v1/properties/{id}：解析路徑上的 id、呼叫
// Service.Get，成功時回 200 與該物件，id 不是合法 UUID 時回 400
// VALIDATION_FAILED，查無資料時（由 Service／Repository 回報）交給
// httpx.WriteError 統一轉譯成 404 PROPERTY_NOT_FOUND。
func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parsePathID(w, r)
	if !ok {
		return
	}

	p, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPropertyResponse(p))
}

// parsePathID 解析路徑上的 {id} 為 uuid.UUID；不是合法 UUID 時直接寫出 400
// VALIDATION_FAILED 錯誤回應並回傳 ok=false，呼叫端應立即 return。
func (h *Handler) parsePathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, h.logger, apperr.Validation(apperr.FieldError{
			Field:  "id",
			Reason: "必須是合法的 UUID",
		}))
		return uuid.UUID{}, false
	}
	return id, true
}

// updatePropertyRequest 對應 PATCH /api/v1/properties/{id} 的 request body。
//
// 刻意沒有 status 欄位——即使呼叫端在 body 中放了 "status"，
// encoding/json 解碼時也只會忽略這個未知欄位，不會有任何效果，讓「PATCH
// 不能繞過狀態機」這件事在型別層面就不可能發生（呼應
// property.UpdateInput 的設計）。
//
// 每個欄位都宣告成指標：JSON 沒有出現該欄位時解碼結果是 nil（代表「不更
// 動」），出現但值為 null 時同樣是 nil；只有明確提供非 null 值時才是非 nil，
// 對應到 property.UpdateInput 的「nil 代表不更動」語意。
type updatePropertyRequest struct {
	MonthlyRent   *string `json:"monthly_rent"`
	ManagementFee *string `json:"management_fee"`
	DepositMonths *int    `json:"deposit_months"`
	Layout        *string `json:"layout"`
	LandlordName  *string `json:"landlord_name"`
	LandlordPhone *string `json:"landlord_phone"`
}

// toUpdateInput 把 request body 轉成 property.UpdateInput，欄位一對一映射。
func (req updatePropertyRequest) toUpdateInput() property.UpdateInput {
	return property.UpdateInput{
		MonthlyRent:   req.MonthlyRent,
		ManagementFee: req.ManagementFee,
		DepositMonths: req.DepositMonths,
		Layout:        req.Layout,
		LandlordName:  req.LandlordName,
		LandlordPhone: req.LandlordPhone,
	}
}

// update 處理 PATCH /api/v1/properties/{id}：解析路徑上的 id 與 request
// body、呼叫 Service.Update，成功時回 200 與更新後的完整物件。id 不是合法
// UUID 或 body 未通過驗證時回 400，查無資料時交給 httpx.WriteError 統一轉譯
// 成 404。
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parsePathID(w, r)
	if !ok {
		return
	}

	req, err := httpx.DecodeJSON[updatePropertyRequest](r)
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	p, err := h.svc.Update(r.Context(), id, req.toUpdateInput())
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPropertyResponse(p))
}

// changeStatusRequest 對應 POST /api/v1/properties/{id}/status 的 request body。
type changeStatusRequest struct {
	Status string `json:"status"`
}

// changeStatus 處理 POST /api/v1/properties/{id}/status：解析路徑上的 id、
// 驗證 body 中的 status 是合法列舉值、呼叫 Service.ChangeStatus。
// id 不是合法 UUID 或 status 不是合法列舉值時回 400 VALIDATION_FAILED；
// 查無資料時回 404；轉換不合法時（由 Service.ChangeStatus／CanTransition
// 判斷）交給 httpx.WriteError 統一轉譯成 409 PROPERTY_INVALID_STATUS_TRANSITION。
func (h *Handler) changeStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := h.parsePathID(w, r)
	if !ok {
		return
	}

	req, err := httpx.DecodeJSON[changeStatusRequest](r)
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	target := property.Status(req.Status)
	if !target.Valid() {
		httpx.WriteError(w, r, h.logger, apperr.Validation(apperr.FieldError{
			Field:  "status",
			Reason: "必須是 VACANT、OCCUPIED、RENOVATING 或 DELISTED 之一",
		}))
		return
	}

	p, err := h.svc.ChangeStatus(r.Context(), id, target)
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPropertyResponse(p))
}

// propertyListResponse 對應 GET /api/v1/properties 成功回應的 JSON 形狀。
//
// Items 刻意以 make([]propertyResponse, 0, ...) 建立（而不是讓零值 nil slice
// 流進來）——nil slice 經 encoding/json 編碼會輸出 JSON null，而不是空陣列
// []，兩者對前端消費者而言語意不同，本專案的契約要求一律是 []。
type propertyListResponse struct {
	Items []propertyResponse `json:"items"`
	Total int                `json:"total"`
}

// list 處理 GET /api/v1/properties：解析查詢字串（page、page_size、status、
// rental_mode、city）成 property.ListFilter、呼叫 Service.List，回 200 與
// 分頁結果。Page／PageSize 的預設值與上限夾限由領域層
// （property.ListFilter.normalize，透過 Service.List 呼叫）負責，這一層只做
// 「字串轉型」，無效或缺漏的參數一律轉成零值交給領域層決定預設。
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	filter := parseListFilter(r.URL.Query())

	result, err := h.svc.List(r.Context(), filter)
	if err != nil {
		httpx.WriteError(w, r, h.logger, err)
		return
	}

	items := make([]propertyResponse, 0, len(result.Items))
	for _, p := range result.Items {
		items = append(items, toPropertyResponse(p))
	}

	httpx.WriteJSON(w, http.StatusOK, propertyListResponse{
		Items: items,
		Total: result.Total,
	})
}

// parseListFilter 把 GET /api/v1/properties 的查詢字串轉成
// property.ListFilter。
//
//   - page、page_size：無法解析（缺漏或非數字）一律轉成 0，讓
//     property.ListFilter.normalize 補上預設值，不視為錯誤。
//   - status、rental_mode：只有解析出合法列舉值才設定篩選條件；缺漏或非法
//     列舉值一律視為「不篩選該欄位」，不視為錯誤（查詢參數本來就不像
//     request body 需要嚴格驗證）。
//   - city：原樣傳遞，空字串代表不篩選。
func parseListFilter(q url.Values) property.ListFilter {
	filter := property.ListFilter{
		Page:     parseNonNegativeInt(q.Get("page")),
		PageSize: parseNonNegativeInt(q.Get("page_size")),
		City:     q.Get("city"),
	}

	if status := property.Status(q.Get("status")); status.Valid() {
		filter.Status = &status
	}
	if mode := property.RentalMode(q.Get("rental_mode")); mode.Valid() {
		filter.RentalMode = &mode
	}

	return filter
}

// parseNonNegativeInt 解析 s 為整數；s 為空字串或無法解析時一律回傳 0
// （而不是報錯），交由呼叫端決定要不要當成「未提供」處理。
func parseNonNegativeInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
