package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Server.Port == "" || cfg.Kafka.EventsTopic == "" || len(cfg.Kafka.Brokers) == 0 {
		t.Fatalf("missing defaults")
	}
}
