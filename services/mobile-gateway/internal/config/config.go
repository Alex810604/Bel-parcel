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
		Brokers             []string
		Timeout             time.Duration
		TopicPickedUp       string
		TopicDeliveredToPVP string
		TopicReceivedByPVP  string
		TopicCarrierLocation string
	}
	OTLP struct {
		Endpoint string
	}
	Auth struct {
		HS256Secret string
		Issuer      string
		Audience    string
	}
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetDefault("server.port", "8088")
	v.SetDefault("db.dsn", "postgres://user:pass@localhost:5432/mobile_gateway_db?sslmode=disable")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.topicpickedup", "events.batch_picked_up")
	v.SetDefault("kafka.topicdeliveredtopvp", "events.batch_delivered_to_pvp")
	v.SetDefault("kafka.topicreceivedbypvp", "events.batch_received_by_pvp")
	v.SetDefault("kafka.topiccarrierlocation", "events.carrier_location")
	v.SetDefault("otlp.endpoint", "")
	v.SetDefault("auth.hs256secret", "")
	v.SetDefault("auth.issuer", "")
	v.SetDefault("auth.audience", "")
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
