package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server struct {
		Port string
	}
	DB struct {
		DSN         string
		TrackingDSN string
	}
	Kafka struct {
		Brokers       []string
		GroupID       string
		Timeout       time.Duration
		ConsumeTopics []string
	}
	Tracking struct {
		DeviationThresholdMeters float64
		LateThresholdMinutes     int
	}
	OTLP struct {
		Endpoint string
	}
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetDefault("server.port", "8087")
	v.SetDefault("db.dsn", "postgres://user:pass@localhost:5432/tracking_db?sslmode=disable")
	v.SetDefault("db.trackingdsn", "")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.groupid", "tracking-service")
	v.SetDefault("kafka.consumetopics", []string{"трипы.назначены", "трипы.подтверждены", "местоположение.перевозчика", "алерты.требуется_ручное_назначение", "команды.переназначить"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("tracking.deviation_threshold_meters", 500.0)
	v.SetDefault("tracking.late_threshold_minutes", 15)
	v.SetDefault("otlp.endpoint", "")

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if cfg.DB.DSN == "" && cfg.DB.TrackingDSN != "" {
		cfg.DB.DSN = cfg.DB.TrackingDSN
	}
	return &cfg, nil
}
