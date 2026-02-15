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
		Brokers []string
		Topic   string
		GroupID string
		ConsumerTopics []string
		Timeout time.Duration
		DLQTopic string
	}
	OTLP struct {
		Endpoint string
	}
}

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", "8080")
	v.SetDefault("db.dsn", "postgres://user:pass@localhost:5432/order_db?sslmode=disable")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.topic", "orders.created")
	v.SetDefault("kafka.groupid", "order-service")
	v.SetDefault("kafka.consumertopics", []string{
		"batches.formed",
		"events.batch_picked_up",
		"events.batch_delivered_to_pvp",
		"events.batch_received_by_pvp",
	})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.dlqtopic", "dlq.order")
	v.SetDefault("otlp.endpoint", "")

	// Config file
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")

	// Env vars
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
