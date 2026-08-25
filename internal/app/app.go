// Package app 是服務的組裝點：把各 feature 的 handler 併成完整的
// api.StrictServerInterface，接上錯誤中介層，組出可直接服務的 http.Handler。
//
// 為什麼不留在 cmd/api：`package main` 沒辦法被 import，整合測試因此只能用
// 子行程跑起整支 binary，而子行程裡的 handler 設不了中斷點、也無法用
// httptest 在行程內驅動。抽成套件之後，測試可以直接 NewHandler 再包
// httptest.NewServer，debugger 就能一路跟進 handler → service → repo。
//
// cmd/api 仍然是唯一的**行程**進入點，負責設定載入、開連線、signal
// 與優雅關閉；這裡只負責「把元件接起來」這一件事。
package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"

	"github.com/yongde2900/chuchu2/api"
	"github.com/yongde2900/chuchu2/internal/apihttp"
	"github.com/yongde2900/chuchu2/internal/health"
	"github.com/yongde2900/chuchu2/internal/property"
	"github.com/yongde2900/chuchu2/internal/property/httpapi"
	"github.com/yongde2900/chuchu2/internal/property/pgrepo"
	"github.com/yongde2900/chuchu2/internal/server"
)

// Deps 是組裝所需的外部相依。連線由呼叫端負責開啟與關閉——
// 這一層不碰設定檔，也不決定連線何時關掉。
type Deps struct {
	DB     *bun.DB
	Redis  *redis.Client
	Logger *slog.Logger

	// Debug 為 true 時才掛載 GET /debug/panic。
	Debug bool
}

// NewHandler 依 d 組出完整的 HTTP handler：feature 的 handler → 統一錯誤
// 中介層 → router 的 middleware chain。
func NewHandler(d Deps) http.Handler {
	healthSvc := health.NewService(
		health.NewPostgresChecker(d.DB),
		health.NewRedisChecker(d.Redis),
	)
	propertySvc := property.NewService(pgrepo.New(d.DB))

	srv := &apiServer{
		healthAPI:   health.NewAPI(healthSvc),
		propertyAPI: httpapi.NewAPI(propertySvc),
	}

	return server.NewRouter(server.Options{
		Debug:  d.Debug,
		Logger: d.Logger,
	}, apihttp.Mount(srv, d.Logger))
}

// apiServer 把兩個 feature 的 handler 併成完整的 api.StrictServerInterface。
//
// 不能用匿名內嵌：health.API 與 httpapi.API 匯出名稱都是 API，內嵌後隱含欄位名
// 同為 "API"，編譯期會以 "API redeclared" 拒絕。具名欄位＋顯式轉發語意等價。
type apiServer struct {
	healthAPI   *health.API
	propertyAPI *httpapi.API
}

var _ api.StrictServerInterface = (*apiServer)(nil)

func (s *apiServer) GetHealthz(ctx context.Context, req api.GetHealthzRequestObject) (api.GetHealthzResponseObject, error) {
	return s.healthAPI.GetHealthz(ctx, req)
}

func (s *apiServer) ListProperties(ctx context.Context, req api.ListPropertiesRequestObject) (api.ListPropertiesResponseObject, error) {
	return s.propertyAPI.ListProperties(ctx, req)
}

func (s *apiServer) CreateProperty(ctx context.Context, req api.CreatePropertyRequestObject) (api.CreatePropertyResponseObject, error) {
	return s.propertyAPI.CreateProperty(ctx, req)
}

func (s *apiServer) GetProperty(ctx context.Context, req api.GetPropertyRequestObject) (api.GetPropertyResponseObject, error) {
	return s.propertyAPI.GetProperty(ctx, req)
}

func (s *apiServer) UpdateProperty(ctx context.Context, req api.UpdatePropertyRequestObject) (api.UpdatePropertyResponseObject, error) {
	return s.propertyAPI.UpdateProperty(ctx, req)
}

func (s *apiServer) ChangePropertyStatus(ctx context.Context, req api.ChangePropertyStatusRequestObject) (api.ChangePropertyStatusResponseObject, error) {
	return s.propertyAPI.ChangePropertyStatus(ctx, req)
}
