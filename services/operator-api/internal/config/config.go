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
	Services struct {
		RoutingURL   string
		BatchingURL  string
		OrderURL     string
		ReferenceURL string
	}
	Kafka struct {
		Brokers      []string
		Timeout      time.Duration
		CommandTopic string
		GroupID      string
		SyncTopics   []string
	}
	OTLP struct {
		Endpoint string
	}
	Auth struct {
		HS256Secret string
		Issuer      string
		Audience    string
	}
	RefMTLS struct {
		Enabled  bool
		CertPath string
		KeyPath  string
		CAPath   string
	}
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetDefault("server.port", "8090")
	v.SetDefault("db.dsn", "postgres://user:pass@localhost:5432/operator_db?sslmode=disable")
	v.SetDefault("services.routingurl", "http://localhost:8081")
	v.SetDefault("services.batchingurl", "http://localhost:8082")
	v.SetDefault("services.orderurl", "http://localhost:8083")
	v.SetDefault("services.referenceurl", "http://localhost:8084")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.timeout", 5*time.Second)
	v.SetDefault("kafka.commandtopic", "commands.trip.reassign")
	v.SetDefault("kafka.groupid", "operator-api-sync")
	v.SetDefault("kafka.synctopics", []string{"trips.updated", "batches.updated"})
	v.SetDefault("otlp.endpoint", "")
	v.SetDefault("auth.hs256secret", "")
	v.SetDefault("auth.issuer", "")
	v.SetDefault("auth.audience", "")
	v.SetDefault("refmtls.enabled", false)
	v.SetDefault("refmtls.certpath", "")
	v.SetDefault("refmtls.keypath", "")
	v.SetDefault("refmtls.capath", "")
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
