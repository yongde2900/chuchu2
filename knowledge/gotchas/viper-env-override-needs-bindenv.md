---
title: viper 的 AutomaticEnv 對「只存在於環境變數的 key」不可靠，要明確 BindEnv
type: gotchas
date: 2026-08-20
tags: [viper, config, env, testing]
---

## 症狀

`config.Load` 用了 `SetEnvPrefix("CHUCHU")` + `SetEnvKeyReplacer(".", "_")` + `AutomaticEnv()`，
看起來環境變數覆寫應該生效。但對於**只存在於環境變數、不存在於 yaml 檔中**的 key，
`IsSet()` 與 `Unmarshal()` 有時抓不到 —— viper 的 `AutomaticEnv` 只在 key 已被 viper 認識
（出現在 yaml、有 default、或被 BindEnv 過）時才可靠。

## 為什麼這對 chuchu2 是致命的

整合測試靠 `CHUCHU_POSTGRES_DSN`／`CHUCHU_REDIS_ADDR`／`CHUCHU_SERVER_PORT`
把服務指向 testcontainers 隨機產生的 DSN／埠號，**且不修改已提交的 `config/test.yaml`**。
如果覆寫不生效，整個 testcontainers 測試策略就垮了。

更隱蔽的情境：`config/broken.yaml` 刻意不含 `postgres.dsn`，
BDD 要求「缺 key 就拒絕啟動」；但同時也要能用環境變數把那個缺失的 key**補上**。
這正是 `AutomaticEnv` 最不可靠的那一格。

## 作法

對**每一個已知 key 明確呼叫 `v.BindEnv(...)`**，不要只靠 `AutomaticEnv()`：

```go
for _, key := range knownKeys {
    _ = v.BindEnv(key)   // 讓 viper 認識這個 key，環境變數覆寫才會穩定生效
}
```

`knownKeys` 是所有設定 key 的完整清單（比 `requiredKeys` 更廣 —— 後者只是啟動時必須存在的子集）。

## 驗證方式

跑一次「key 完全不在 yaml、只由環境變數提供」的案例：

```bash
CHUCHU_POSTGRES_DSN="postgres://..." go run ./cmd/api --config=broken
```

應該要能通過必要 key 檢查並正常啟動；若仍報缺 key，就是 BindEnv 沒補齊。

相關：[[testcontainers-shared-vs-dedicated]]
