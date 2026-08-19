---
title: 共用容器 vs 專屬容器 —— 哪些測試絕不能碰 TestMain 的共用容器
type: gotchas
date: 2026-08-20
tags: [testcontainers, testing, docker, integration]
---

## 規則

`test/` package 的 `TestMain` 啟動**一組共用的** Postgres 與 Redis 供整個 package 使用
（16+ 個 scenario 各起一組會慢到不可用），並對共用 Postgres 跑過 `migrate.Up`。
一般測試以 **`TRUNCATE properties`** 取得乾淨狀態，**不重建容器**。

**但有兩類測試必須自起「用完即丟」的專屬容器，絕對不可以動共用容器：**

1. **會停掉相依服務的測試** —— 健康檢查的「相依服務斷線」情境
   （`test/health_test.go`、`test/contract_test.go` 的 `/healthz` 503 案例）。
2. **會刪除資料表的測試** —— migration 的 up→down 往返
   （`test/migrate_test.go`，最後一步 `down` 會 DROP `properties`）。

## 為什麼

停掉或清空共用容器會讓**同一個 package 中其後所有測試連鎖失敗**，
而且失敗訊息會指向**無辜的測試** —— 除錯成本極高，因為報錯的地方不是出問題的地方。

## 實作要點

- `testsupport.StartPostgres(t)` / `StartRedis(t)` 回傳的 `stop` 必須能**主動、立即**停掉容器
  （斷線情境要用），同時以 `t.Cleanup` 註冊收尾。
  **`stop` 必須以 `sync.Once` 保護** —— 斷線測試會先手動呼叫，`t.Cleanup` 之後又會再呼叫一次。
- 斷線情境的步驟順序很重要：
  起專屬容器 → `StartAPI` 指向它們 → **先確認 `/healthz` 回 200** → 停掉其中一個 → 再打 `/healthz` 斷言 503。
  **少了中間那步「先確認 200」，測試就無法區分「探針正確偵測到斷線」與「探針從頭到尾就沒連上過」。**
- `-count=1` 是必要的：Go 會快取測試結果，容器背後的測試從快取回綠會騙人。

## 驗證方式

跑 `go test -race -count=1 -v ./test/...` 並觀察 testcontainers 的 log，
確認確實存在**兩個以上不同的容器 id**：一個是 `TestMain` 的長生命週期共用容器，
另一個是在單一測試內建立、使用、隨即停止的專屬容器。

相關：[[go-run-orphan-process-group]]、[[viper-env-override-needs-bindenv]]
