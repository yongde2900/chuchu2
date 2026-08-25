---
title: 物件狀態機的完整轉換表與「單一強制點」設計
type: decisions
date: 2026-08-20
tags: [state-machine, property, status, api]
---

## 四個狀態

`VACANT`（空置）、`OCCUPIED`（出租中）、`RENOVATING`（整修中）、`DELISTED`（已下架）。

## 權威轉換表

`property.CanTransition(from, to Status) bool` 是唯一權威來源。

| from \ to | VACANT | OCCUPIED | RENOVATING | DELISTED |
|---|---|---|---|---|
| **VACANT**     | ✗ | ✓ | ✓ | ✓ |
| **OCCUPIED**   | ✓ | ✗ | ✗ | ✗ |
| **RENOVATING** | ✓ | ✗ | ✗ | ✓ |
| **DELISTED**   | ✓ | ✗ | ✗ | ✗ |

只有七條合法：VACANT→OCCUPIED、VACANT→RENOVATING、VACANT→DELISTED、OCCUPIED→VACANT、
RENOVATING→VACANT、RENOVATING→DELISTED、DELISTED→VACANT。

**整條對角線（同狀態轉換）皆非法** —— `VACANT→VACANT` 也會被拒。這是最容易漏掉的邊角。

業務理由：已下架的物件必須先回到空置才能重新上架；出租中的物件必須先退租（回到空置）才能下架。

## 單一強制點

狀態變更走**自己的 endpoint**（`POST /api/v1/properties/{id}/status`），**不走 PATCH**。
理由是讓狀態轉換規則只有一個強制點。

實作上讓這件事在**型別層面**就不可能出錯：`UpdateInput` 刻意**沒有 `Status` 欄位**，
所以 PATCH 的 body 中即使出現 `"status"`，`encoding/json` 也會直接丟棄，不可能繞過狀態機。
（已實測：帶 `"status":"OCCUPIED"` 的 PATCH 不會改變狀態，但同一請求中的 `monthly_rent` 仍正常更新。）

## 拒絕必須發生在寫入之前

`Service.ChangeStatus` 的順序是 `GetByID` → `CanTransition` 檢查 → 不合法就**直接回錯誤，
不碰 repository 的 Update**。BDD 斷言「非法轉換被拒後，接著 GET 必須看到原狀態不變」，
那條斷言存在的目的就是證明沒有半套寫入。

非法轉換回傳 `apperr.CodeInvalidStatusTransition` → HTTP 409。

## 測試上的教訓

十六格全部要有單元測試，而不是只測 BDD 用到的四格。但要注意：
**十六個子測試通過只證明測試與實作彼此同意** —— 若兩者共享同一個錯誤信念，
套件是綠的而狀態機是錯的。review 時必須拿測試的**期望值**去對照權威表，而不是對照程式碼。

相關：[[rental-mode-flattened-on-property]]
