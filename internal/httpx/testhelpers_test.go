package httpx

import "context"

// withTestRequestID 讓測試能直接把一個已知的 request id 放進 context，
// 不必真的經過 RequestID middleware。因為 context key 型別未匯出，
// 這個 helper 必須留在 httpx package 內（同package測試檔）。
func withTestRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}
