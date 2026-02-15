package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Server.Port == "" || len(cfg.Kafka.Brokers) == 0 || cfg.Kafka.GroupID == "" {
		t.Fatalf("missing defaults")
	}
	if cfg.DB.TripDSN == "" {
		t.Fatalf("missing TripDSN")
	}
}
