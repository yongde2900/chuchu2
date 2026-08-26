// 本檔提供整合測試的「行程內」啟動方式：直接用 internal/app 組出 handler，
// 再包一層 httptest.NewServer。
//
// 相對於 testsupport.StartAPI（以子行程跑 `go run ./cmd/api`），行程內啟動的
// 差別在於：
//   - **中斷點有效**。`dlv test ./test/ -- -test.run TestXxx` 可以一路跟進
//     handler → service → repo；子行程裡的程式碼 debugger 碰不到。
//   - 快很多，省掉每個測試 `go run` 重新編譯的時間。
//   - 測得到的東西少一樣：**行程層面的行為**（服務會不會退出、設定壞掉時的
//     exit code）只能用子行程驗，那留在 startup_test.go。
package test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/yongde2900/chuchu2/internal/app"
	"github.com/yongde2900/chuchu2/internal/config"
	"github.com/yongde2900/chuchu2/internal/platform/logging"
	"github.com/yongde2900/chuchu2/internal/platform/postgres"
	"github.com/yongde2900/chuchu2/internal/platform/redisclient"
)

// lineChannelSecret 定義在 line_webhook_test.go（同 package test，這裡直接沿用）——
// 必須與 config/test.yaml 的 line.channel_secret 一致：子行程啟動的測試
// （testsupport.StartAPI）讀 yaml，行程內啟動的測試用這個常數，兩邊對不上會讓
// webhook 測試全部 401，且不容易看出原因。

// startInProcessAPI 在同一個行程內起一個真正的 HTTP server，回傳 baseURL 與
// 取得 server 端 log 的函式（供斷言失敗時輸出，對應 StartAPI 的 output）。
//
// Debug 固定為 true，與 config/test.yaml 一致，讓 /debug/panic 這條路由存在。
func startInProcessAPI(t *testing.T, dsn, redisAddr string) (baseURL string, output func() string) {
	t.Helper()

	ctx := context.Background()

	db, err := postgres.Open(ctx, config.PostgresConfig{DSN: dsn, MaxOpenConns: 10})
	if err != nil {
		t.Fatalf("建立 postgres 連線池失敗: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	redisClient, err := redisclient.Open(ctx, config.RedisConfig{Addr: redisAddr})
	if err != nil {
		t.Fatalf("建立 redis client 失敗: %v", err)
	}
	t.Cleanup(func() { redisClient.Close() })

	logs := &syncBuffer{}
	handler := app.NewHandler(app.Deps{
		DB:                db,
		Redis:             redisClient,
		Logger:            logging.New("info", logs),
		Debug:             true,
		LineChannelSecret: lineChannelSecret,
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL, logs.String
}
