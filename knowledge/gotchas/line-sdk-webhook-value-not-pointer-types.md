---
title: line-bot-sdk-go v8 的 webhook 事件／來源解析回傳的是 value type，不是指標
type: gotchas
date: 2026-08-26
tags: [line-sdk, webhook, third-party-api]
---

## 情境

`github.com/line/line-bot-sdk-go/v8@v8.22.0` 的 `webhook.ParseRequest` 回傳
`*webhook.CallbackRequest`，其中 `Events []EventInterface` 是介面切片，要用 type switch
取出具體型別（`FollowEvent`／`UnfollowEvent`／...）；`Source SourceInterface` 也一樣，
要取出 `UserSource` 才拿得到 `UserId`。

PLAN-003 規劃階段（讀 SDK 原始碼時）誤以為這些具體型別是以指標回傳
（`*webhook.FollowEvent`、`*webhook.UserSource`），因為介面切片放指標是更常見的寫法。

## 事實

讀 `vendor/github.com/line/line-bot-sdk-go/v8/linebot/webhook/model_event.go` 與
`model_source.go` 確認：`UnmarshalEvent` 與 `UnmarshalSource` 的 `case` 分支
（例如 `case "follow": ... return follow, nil`）回傳的都是**值型別**
（`webhook.FollowEvent`、`webhook.UserSource`），不是指標。

## 影響

如果 `toDomainEvents` 的 type switch 只寫 `case *webhook.FollowEvent:` 這種指標形式，
switch 永遠命中不到，所有 follow／unfollow 事件都會安靜落到「未知型別，略過」的分支 ——
**不會報錯，只是所有事件都被吃掉**，用手寫的假事件做單元測試也測不出來（因為測試同樣
可以「巧合地」用錯的型別建構假資料）。只有打真實 HTTP、走 SDK 真正的
`json.Unmarshal` → `UnmarshalEvent` 路徑的整合測試才會發現。

## 作法

type switch 同時列出 value 與 pointer 兩種形式（防禦性，即使目前只有 value 形式會命中，
未來 SDK 版本改變回傳形式也不會無聲失效）：

```go
switch e := ev.(type) {
case webhook.FollowEvent:
    ...
case *webhook.FollowEvent:
    ...
}
```

## 一般教訓

**第三方 SDK 的介面實作是值型別還是指標型別，不能用「介面切片通常放指標」這種慣例推斷，
要直接讀該版本的原始碼確認。** 這條資訊只對 v8.22.0 保證成立，升級 SDK 版本時要重新確認。

相關：[[bun-upsert-on-conflict-alias-trap]]（同一個 task 裡另一個「讀 vendor 原始碼才發現」的坑）。
