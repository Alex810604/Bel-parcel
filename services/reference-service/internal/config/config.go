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
		DSN string
	}
	Kafka struct {
		Brokers     []string
		Timeout     time.Duration
		EventsTopic string
	}
	Auth struct {
		HS256Secret string
		Issuer      string
		Audience    string
	}
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("server.port", "8084")
	v.SetDefault("db.dsn", "postgres://postgres:postgres@localhost:5432/reference_db?sslmode=disable")
	v.SetDefault("kafka.brokers", []string{"kafka:9092"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.eventstopic", "events.reference_updated")
	v.SetDefault("auth.hs256secret", "")
	v.SetDefault("auth.issuer", "")
	v.SetDefault("auth.audience", "")

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	// Allow docker-compose style env var KAFKA_TOPIC
	if cfg.Kafka.EventsTopic == "" && v.IsSet("kafka.topic") {
		cfg.Kafka.EventsTopic = v.GetString("kafka.topic")
	}
	return cfg, nil
}
