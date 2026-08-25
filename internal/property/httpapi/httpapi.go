// Package httpapi 把 api.StrictServerInterface 中與物件相關的五個 operation
// 接到 property.Service，是 internal/property 底下唯一 import 產生的 api 套件的子套件。
//
// 這一層刻意很薄，只做 api 型別 ↔ 領域型別的轉換。錯誤一律直接 return，
// 由 internal/apihttp 轉成 HTTP 回應——這裡不寫回應 body，也不知道狀態碼，
// 因此連 net/http 都不需要 import。
package httpapi

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/apperr"
	"github.com/yongde2900/chuchu2/internal/property"
)

type API struct {
	svc *property.Service
}

func NewAPI(svc *property.Service) *API {
	return &API{svc: svc}
}

// Items 必須用 make 而不是零值 nil slice——nil slice 經 encoding/json
// 會輸出 null 而不是 []，而契約要求空結果一律是 []。
func (a *API) ListProperties(ctx context.Context, req api.ListPropertiesRequestObject) (api.ListPropertiesResponseObject, error) {
	result, err := a.svc.List(ctx, toListFilter(req.Params))
	if err != nil {
		return nil, err
	}

	items := make([]api.Property, 0, len(result.Items))
	for _, p := range result.Items {
		items = append(items, toAPIProperty(p))
	}

	return api.ListProperties200JSONResponse{
		Items: items,
		Total: result.Total,
	}, nil
}

// req.Body 理論上不會是 nil（空 body 的解碼失敗已由 RequestErrorHandler 攔下），
// 但產生的型別沒有強制 non-nil，仍防禦性處理以免 codegen 行為變動造成 nil deref。
func (a *API) CreateProperty(ctx context.Context, req api.CreatePropertyRequestObject) (api.CreatePropertyResponseObject, error) {
	if req.Body == nil {
		return nil, apperr.ValidationFailed.WithMessage("request body 不可為空")
	}
	body := req.Body

	managementFee := ""
	if body.ManagementFee != nil {
		managementFee = *body.ManagementFee
	}

	p, err := a.svc.Create(ctx, property.CreateInput{
		City:          body.City,
		District:      body.District,
		StreetAddress: body.StreetAddress,
		Floor:         body.Floor,
		RoomNo:        body.RoomNo,
		Layout:        string(body.Layout),
		AreaPing:      body.AreaPing,
		MonthlyRent:   body.MonthlyRent,
		ManagementFee: managementFee,
		DepositMonths: body.DepositMonths,
		RentalMode:    string(body.RentalMode),
		LandlordName:  body.LandlordName,
		LandlordPhone: body.LandlordPhone,
	})
	if err != nil {
		return nil, err
	}

	return api.CreateProperty201JSONResponse(toAPIProperty(p)), nil
}

// req.Id 已經是綁定好的合法 UUID——格式錯誤在綁定階段就被
// apihttp.ParamErrorHandler 攔下，不會走到這裡。
func (a *API) GetProperty(ctx context.Context, req api.GetPropertyRequestObject) (api.GetPropertyResponseObject, error) {
	p, err := a.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return api.GetProperty200JSONResponse(toAPIProperty(p)), nil
}

func (a *API) UpdateProperty(ctx context.Context, req api.UpdatePropertyRequestObject) (api.UpdatePropertyResponseObject, error) {
	if req.Body == nil {
		return nil, apperr.ValidationFailed.WithMessage("request body 不可為空")
	}
	body := req.Body

	var layout *string
	if body.Layout != nil {
		l := string(*body.Layout)
		layout = &l
	}

	p, err := a.svc.Update(ctx, req.Id, property.UpdateInput{
		MonthlyRent:   body.MonthlyRent,
		ManagementFee: body.ManagementFee,
		DepositMonths: body.DepositMonths,
		Layout:        layout,
		LandlordName:  body.LandlordName,
		LandlordPhone: body.LandlordPhone,
	})
	if err != nil {
		return nil, err
	}

	return api.UpdateProperty200JSONResponse(toAPIProperty(p)), nil
}

// status 是否為合法列舉值必須自己驗——產生的綁定不做 enum 檢查。
// 轉換合不合法則由 Service 經 property.CanTransition 判斷。
func (a *API) ChangePropertyStatus(ctx context.Context, req api.ChangePropertyStatusRequestObject) (api.ChangePropertyStatusResponseObject, error) {
	if req.Body == nil {
		return nil, apperr.ValidationFailed.WithMessage("request body 不可為空")
	}

	target := property.Status(req.Body.Status)
	if !target.Valid() {
		return nil, apperr.Validation(apperr.FieldError{
			Field:  "status",
			Reason: "必須是 VACANT、OCCUPIED、RENOVATING 或 DELISTED 之一",
		})
	}

	p, err := a.svc.ChangeStatus(ctx, req.Id, target)
	if err != nil {
		return nil, err
	}

	return api.ChangePropertyStatus200JSONResponse(toAPIProperty(p)), nil
}

// 兩種無效輸入的處理刻意不同：
//   - 型別錯誤（page=abc）在綁定階段就被 apihttp.ParamErrorHandler 攔成 400。
//   - enum 值錯誤（status=BOGUS）產生的綁定不驗證，這裡靠 Valid() 過濾，
//     視為「不篩選該欄位」而非錯誤。
//
// Page／PageSize 為 nil 時傳 0，交給 ListFilter.normalize 補預設值。
func toListFilter(params api.ListPropertiesParams) property.ListFilter {
	var filter property.ListFilter

	if params.Page != nil {
		filter.Page = *params.Page
	}
	if params.PageSize != nil {
		filter.PageSize = *params.PageSize
	}
	if params.City != nil {
		filter.City = *params.City
	}

	if params.Status != nil {
		status := property.Status(*params.Status)
		if status.Valid() {
			filter.Status = &status
		}
	}
	if params.RentalMode != nil {
		mode := property.RentalMode(*params.RentalMode)
		if mode.Valid() {
			filter.RentalMode = &mode
		}
	}

	return filter
}

// 金額一律固定兩位小數（25000.5 → "25000.50"）。decimal 預設的 MarshalJSON
// 不補齊位數，而 JSON number 在 JavaScript 客戶端往返會損失十進位精度。
func formatMoney(d decimal.Decimal) string {
	return d.StringFixed(2)
}

// toAPIProperty 是本套件唯一的「領域物件 → API 物件」轉換函式，五個 operation 共用。
func toAPIProperty(p *property.Property) api.Property {
	return api.Property{
		Id:            p.ID,
		City:          p.City,
		District:      p.District,
		StreetAddress: p.StreetAddress,
		Floor:         p.Floor,
		RoomNo:        p.RoomNo,
		Layout:        api.PropertyLayout(p.Layout),
		AreaPing:      formatMoney(p.AreaPing),
		MonthlyRent:   formatMoney(p.MonthlyRent),
		ManagementFee: formatMoney(p.ManagementFee),
		DepositMonths: p.DepositMonths,
		RentalMode:    api.PropertyRentalMode(p.RentalMode),
		Status:        api.PropertyStatus(p.Status),
		LandlordName:  p.LandlordName,
		LandlordPhone: p.LandlordPhone,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}
