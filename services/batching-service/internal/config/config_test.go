package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Server.Port == "" || cfg.Kafka.GroupID == "" {
		t.Fatalf("missing defaults")
	}
	if len(cfg.Kafka.Brokers) == 0 {
		t.Fatalf("missing brokers")
	}
	if cfg.Kafka.Timeout <= 0 || cfg.Batching.FlushInterval <= 0 {
		t.Fatalf("invalid durations")
	}
	_ = time.Second
}
