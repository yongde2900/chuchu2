---
title: LINE 若加第二個領域（如訊息），改用 Content-Based Router 收斂在 internal/line 頂層
type: decisions
date: 2026-08-26
tags: [line, webhook, architecture, content-based-router, future-work]
---

## 現況（PLAN-003 完成時）

`internal/line` 目前只有一個領域（follow/unfollow），結構跟 `internal/property` 同一個模式：

```
internal/line/               純領域＋service＋Repository（Event/Status/Service）
internal/line/pgrepo/        唯一碰 bun 的地方
internal/line/webhookhttp/   唯一碰 net/http 與 LINE SDK 的地方——自己 Mount 路由、自己做
                              webhook.ParseRequest 之後的分類＋轉譯（toDomainEvents）
```

見 [[property-service-layering]]。

## 決定：未來加第二個 LINE 領域時要怎麼拆

這是**還沒發生**的決定——目前只有一個領域，不需要現在就重構（YAGNI）。但下次真的要加第二個
LINE Messaging API 領域（討論時舉的例子是「訊息內容」，跟 follow/unfollow 不同，不會落地到
`line_users`、沒有 status 概念、是 append-only 的事件流）時，要照下面這個形狀改，不要照上一次
臨時想到的「廣播＋各領域自篩」版本做。

### 目標結構

```
internal/line/                    package line —— 唯一的 dispatcher
                                   整個 line feature area 裡唯一 import net/http 與 LINE SDK 的地方
internal/line/follow/             package follow —— 純領域（現有 internal/line 的內容搬過來改名）
                                   不 import bun、net/http、LINE SDK
internal/line/follow/pgrepo/      唯一碰 bun 的 follow 子套件
internal/line/message/            package message —— 純領域（新加的）
internal/line/message/pgrepo/     唯一碰 bun 的 message 子套件
```

`internal/line/webhookhttp` 這個子套件**整個收掉**，不要在每個領域底下各開一份 transport 套件。
理由：分類（這批事件歸哪個領域）跟轉譯（SDK 型別 → 領域 Event）都得碰 SDK 型別，既然 dispatcher
已經是唯一合法碰 SDK 的地方，兩件事乾脆一起在 dispatcher 做完，轉譯完直接呼叫
`follow.Service.Handle(ctx, events)` / `message.Service.Handle(ctx, events)`。`follow`／`message`
兩個領域套件因此維持跟 `internal/property` 一樣的純粹度。

```go
// internal/line/dispatcher.go —— package line
func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
    cb, err := webhook.ParseRequest(h.channelSecret, r)
    if err != nil { ... }   // 簽章／解析錯誤處理照舊（見 [[line-sdk-webhook-value-not-pointer-types]]）

    var followEvents []follow.Event
    var msgEvents []message.Event
    for _, e := range cb.Events {
        switch ev := e.(type) {
        case webhook.FollowEvent, *webhook.FollowEvent,
             webhook.UnfollowEvent, *webhook.UnfollowEvent:
            followEvents = append(followEvents, toFollowEvent(ev))
        case webhook.MessageEvent, *webhook.MessageEvent:
            msgEvents = append(msgEvents, toMessageEvent(ev))
        // 其餘型別：兩邊都不歸類，安靜略過
        }
    }
    if len(followEvents) > 0 {
        if err := h.followSvc.Handle(r.Context(), followEvents); err != nil { ...; return }
    }
    if len(msgEvents) > 0 {
        if err := h.msgSvc.Handle(r.Context(), msgEvents); err != nil { ...; return }
    }
    w.WriteHeader(http.StatusOK)
}
```

`internal/app` 組裝時會 import `internal/line`、`internal/line/follow`、`internal/line/follow/pgrepo`
（加上未來的 `message` 那一組），跟現在 import `internal/property` 一堆子套件是同一種模式。

## 為什麼選 Content-Based Router 而不是 Publish-Subscribe 廣播

規劃這個功能時先想到的版本是「dispatcher 廣播完整事件清單給每個領域，各領域自己 type-switch
篩選自己要的」（EIP 的 Publish-Subscribe Channel 模式）。後來改成 Content-Based Router
（分類規則集中在 dispatcher 一個地方），理由：

1. **語意上更貼合實際情況**：每個 LINE 事件恰好只屬於一個領域（follow 事件永遠不會同時是
   message 事件），這是「路由到唯一正確目的地」的問題，不是「多個獨立訂閱者都想看同一則
   訊息」的問題——後者才是 Publish-Subscribe 原本要解決的場景。
2. **業界慣例**：Stripe 等主流 webhook 消費端幾乎都是中心化 switch on `event.type`，不是
   每個 handler 各自篩選。
3. **可讀性**：新人要理解「LINE 事件怎麼分流」只需要讀一個檔案的一個 switch，不用拼湊
   N 個領域各自的 filter 邏輯。

代價（接受的取捨）：新增領域要回來改 dispatcher 的分類 switch，違反嚴格的 OCP「不修改既有
程式碼」。接受這個代價是因為 LINE Messaging API 的事件型別數量小而穩定，不是開放式的
plugin 生態系，集中管理比分散管理值得。

## 實作這個遷移時要注意的坑

- **套件改名是真正的重構，不是搬檔案**：`internal/line`（package `line`）搬到
  `internal/line/follow`（package `follow`）之後，`line.Event`、`line.Status`、`line.Service`
  等所有引用都要改名——`internal/line/pgrepo`、`internal/app`、所有測試檔（
  `internal/line/*_test.go`、`test/line_webhook_test.go`、`test/line_event_semantics_test.go`）
  都要動。下次規劃時這是第一個 task（遷移），其他 task 要建立在遷移完成的基礎上，不要漏排。
- **dispatcher 子套件絕對不要取名 `webhook`**：LINE SDK 本身的 package 就叫
  `github.com/line/line-bot-sdk-go/v8/linebot/webhook`，如果新目錄取名
  `internal/line/webhook`，同一個檔案要 import 兩個都叫 `webhook` 的 package，會逼你加
  import alias。放在 `internal/line` 頂層（`package line`）就沒這問題。
- **`line_users` 資料表名不受影響**：SQL 表名跟 Go package path 沒有耦合，不需要跟著改。
- **驗證分層的指令要多排一個例外**：[[property-service-layering]] 記錄的
  `grep`/`go list` 驗證指令目前只排除 `internal/app`；`internal/line` 頂層之後也會變成
  「刻意 import 多個子套件」的例外，驗證時要一併排除，且該條知識庫記錄要跟著更新。

相關：[[property-service-layering]]、[[line-sdk-webhook-value-not-pointer-types]]、
[[bun-upsert-on-conflict-alias-trap]]。
