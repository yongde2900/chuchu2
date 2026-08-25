// Package config 負責載入並驗證服務設定。
//
// 設定來源為 config/<name>.yaml，並可用 CHUCHU_ 前綴的環境變數覆寫
// （"." 換成 "_"，例如 CHUCHU_POSTGRES_DSN 覆寫 postgres.dsn）。
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Postgres PostgresConfig `mapstructure:"postgres"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	Debug           bool          `mapstructure:"debug"` // 為 true 時才掛載 /debug/panic
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

type PostgresConfig struct {
	DSN          string `mapstructure:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

// MissingKeyError 表示設定檔（含環境變數覆寫後）缺少一個必要的設定 key。
type MissingKeyError struct {
	Key string
}

// 訊息一定包含缺少的 key 全名，供啟動失敗時輸出到 stderr。
func (e *MissingKeyError) Error() string {
	return fmt.Sprintf("設定缺少必要欄位 %q：請在 config yaml 或以 CHUCHU_ 開頭的環境變數提供這個值", e.Key)
}

// 啟動時必須存在的 key，來自 yaml 或 CHUCHU_ 環境變數皆可。
var requiredKeys = []string{
	"server.port",
	"postgres.dsn",
	"redis.addr",
}

// 逐一明確 BindEnv 是刻意的：viper 的 AutomaticEnv 對「只存在於環境變數、
// 不存在於 yaml」的 key，IsSet/Unmarshal 有時抓不到，必須明確 BindEnv
// 才能確保環境變數覆寫（例如 testcontainers 的隨機 DSN）真的生效。
var knownKeys = []string{
	"server.port",
	"server.debug",
	"server.shutdown_timeout",
	"postgres.dsn",
	"postgres.max_open_conns",
	"redis.addr",
	"redis.password",
	"redis.db",
	"log.level",
}

// 缺少必要 key 時回傳 *MissingKeyError。
func Load(name string) (*Config, error) {
	v := viper.New()
	v.SetConfigName(name)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")

	v.SetEnvPrefix("CHUCHU")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	for _, key := range knownKeys {
		if err := v.BindEnv(key); err != nil {
			return nil, fmt.Errorf("綁定環境變數 %s 失敗: %w", key, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("讀取設定檔 config/%s.yaml 失敗: %w", name, err)
	}

	for _, key := range requiredKeys {
		if !v.IsSet(key) {
			return nil, &MissingKeyError{Key: key}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析設定內容失敗: %w", err)
	}

	return &cfg, nil
}
