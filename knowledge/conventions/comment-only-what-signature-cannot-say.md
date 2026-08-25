---
title: 只寫 signature 表達不出來的註解
type: conventions
date: 2026-08-25
tags: [comments, style, go, readability]
---

## 規則

**註解只寫 signature 表達不出來的東西。**

判斷方式：把這段註解刪掉，讀者會不會因此做錯決定？不會 → 刪掉。

## 為什麼

覆述 signature 的註解不是中性的，它有三個實際成本：

1. **會腐爛。** 程式碼改了、註解沒改，讀者面前就有兩個互相矛盾的說法，
   而且不知道該信哪一個。signature 至少有編譯器逼它誠實，散文沒有。
2. **稀釋真正重要的註解。** 當十行註解裡有九行是廢話，第十行那個「這裡不加
   `--bun:split` 測試會因為錯誤的理由通過」就會被跳過去。**訊號被雜訊埋掉。**
3. **假裝已經解釋過了。** 一個掛滿註解的函式看起來像是被說明清楚了，
   於是沒有人再去問「為什麼是這樣」—— 而那才是唯一值得寫下來的東西。

本專案的實例：`property.go` 曾經寫著「Status 的轉換規則由 Task 7 的
Update／ChangeStatus 負責，本套件現階段只宣告列舉值」。`CanTransition` 早就實作在
同一個套件的 `status.go` 裡了，這段註解在寫下的那一輪結束時就已經是錯的。

## 刪

- **覆述 signature**：`// Valid 回報 s 是否為合法的 Status 列舉值` 掛在
  `func (s Status) Valid() bool` 上。
- **覆述程式碼**：逐行說明「這裡做了什麼」，而程式碼本身讀得出來。
- **列舉常數的同義反覆**：`// StatusVacant 表示空置中` 掛在 `StatusVacant Status = "VACANT"` 上。
- **施工鷹架**：指向計畫文件或 task 編號的註解（「由 Task 7 負責」、「PLAN-002 Task 3 新增」、
  「本 task 交付時還沒有人使用」）。程式碼是長期資產，計畫文件不是 ——
  這類註解隔一輪就過時，而且沒有人會回頭修。要追溯歷史該去看 git log 與 `plan/`。
- **已經過時的**：描述的行為跟現在的程式碼不符。

## 留

- **為什麼**：為何選這個做法而不是另一個，以及付出的代價。
  （防護網刻意不緩衝 body，代價是不支援串流回應）
- **約束**：不可以做什麼、必須先做什麼。
  （`internal/property` 不得 import bun／net-http；bun 的 Migrator 必須先 `Init()`）
- **陷阱**：違反直覺、會無聲失敗的東西 —— 這一類最值得寫，因為它們正是
  「程式碼看起來對、實際上錯」的地方。
  （`.tx.` 檔名才有交易保護；nil slice 會 marshal 成 `null`）
- **領域知識**：從程式碼讀不出來的業務語意。（包租 vs 代管在法律上的差異）
- **權威性宣告**：「這是唯一來源，不要自己另外判斷」這類指路。

## 一個實際的取捨例子

`property.CanTransition` 原本的註解有三段：函式做什麼、它是唯一權威來源、
同狀態轉換一律 false。

- 第一段刪 —— `func CanTransition(from, to Status) bool` 已經說完了。
- 第二段留 —— 「不應該自行另外判斷」是規範，signature 說不出來。
- 第三段留 —— `from == to` 回傳 false 是**刻意**的設計（對角線全非法），
  不是實作疏漏；讀者無法從 signature 分辨這兩者。

同理，狀態機的轉換表保留了「為什麼 OCCUPIED 不能直接跳到 DELISTED」（必須先退租，
避免在還有房客時下架），但刪掉了逐條覆述表格內容的清單 —— 表格自己看得懂。

相關：[[test-layout-two-tiers]]、[[property-status-machine]]
