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
		Brokers       []string
		Timeout       time.Duration
		GroupID       string
		ConsumeTopics []string
		ProduceTopic  string
		DLQTopic      string
	}
	Batching struct {
		MaxSize       int
		FlushInterval time.Duration
	}
	OTLP struct {
		Endpoint string
	}
}

func Load() (*Config, error) {
	v := viper.New()

	v.SetDefault("server.port", "8083")
	v.SetDefault("db.dsn", "postgres://user:pass@localhost:5432/batch_db?sslmode=disable")
	v.SetDefault("kafka.brokers", []string{"redpanda:9092"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.groupid", "batching-service")
	v.SetDefault("kafka.consumetopics", []string{"orders.created", "events.reference_updated", "events.batch_delivered_to_pvp"})
	v.SetDefault("kafka.producetopic", "batches.formed")
	v.SetDefault("kafka.dlqtopic", "dlq.batching")
	v.SetDefault("batching.maxsize", 10)
	v.SetDefault("batching.flushinterval", 1*time.Minute)
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
	return &cfg, nil
}
