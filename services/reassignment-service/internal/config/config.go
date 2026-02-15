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
		ReassignmentDSN string
	}
	Worker struct {
		Interval            time.Duration
		ConfirmationTimeout time.Duration
	}
	Kafka struct {
		Brokers       []string
		Timeout       time.Duration
		CommandTopic  string
		GroupID       string
		ConsumeTopics []string
	}
	OTLP struct {
		Endpoint string
	}
}

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", "8082")
	v.SetDefault("db.reassignmentdsn", "postgres://user:pass@localhost:5432/reassignment_db?sslmode=disable")
	v.SetDefault("worker.interval", 1*time.Minute)
	v.SetDefault("worker.confirmation_timeout", 2*time.Hour)
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.commandtopic", "команды.переназначить")
	v.SetDefault("kafka.groupid", "reassignment-service")
	v.SetDefault("kafka.consumetopics", []string{"трипы.назначены", "трипы.подтверждены", "трипы.отклонены", "команды.переназначить"})
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
