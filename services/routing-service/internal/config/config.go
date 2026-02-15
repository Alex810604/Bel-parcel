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
		TripDSN string
		RefDSN  string
	}
	Kafka struct {
		Brokers       []string
		Timeout       time.Duration
		GroupID       string
		ConsumeTopics []string
		ProduceTopic  string
	}
	OTLP struct {
		Endpoint string
	}
}

func Load() (*Config, error) {
	v := viper.New()

	v.SetDefault("server.port", "8081")
	v.SetDefault("db.tripdsn", "postgres://user:pass@localhost:5432/trip_db?sslmode=disable")
	v.SetDefault("db.refdsn", "postgres://user:pass@localhost:5432/reference_db?sslmode=disable")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.groupid", "routing-service")
	v.SetDefault("kafka.consumetopics", []string{"batches.formed", "commands.trip.reassign", "events.batch_picked_up", "events.batch_delivered_to_pvp", "events.carrier_location", "events.reference_updated"})
	v.SetDefault("kafka.producetopic", "trips.assigned")
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
