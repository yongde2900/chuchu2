// Package api 存放由 api/openapi.yaml 產生的 HTTP 層程式碼。
//
// api.gen.go 絕對不得手動編輯——改 openapi.yaml 再重跑 go generate。
package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml openapi.yaml
