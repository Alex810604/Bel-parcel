package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Server.Port == "" || len(cfg.Kafka.Brokers) == 0 || cfg.Kafka.Topic == "" {
		t.Fatalf("missing defaults")
	}
	if len(cfg.Kafka.ConsumerTopics) == 0 || cfg.Kafka.Timeout <= 0 {
		t.Fatalf("invalid kafka defaults")
	}
}
