//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestTrackingService_DockerCompose(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("E2E not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	run(ctx, t, "docker-compose", "-f", "../../../docker-compose.yml", "up", "-d", "postgres", "kafka", "kafka-init")
	defer run(context.Background(), t, "docker-compose", "-f", "../../../docker-compose.yml", "down", "-v")
	time.Sleep(5 * time.Second)
	cmd := exec.CommandContext(ctx, "go", "run", "../cmd/tracking-service/main.go")
	cmd.Env = append(os.Environ(),
		"SERVER_PORT=8086",
		"DB_DSN=postgres://user:pass@localhost:5432/trip_db?sslmode=disable",
		"KAFKA_BROKERS=localhost:9092",
		"KAFKA_TIMEOUT=5s",
		"KAFKA_GROUPID=tracking-service",
		"KAFKA_CONSUMETOPICS=events.carrier_location",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start tracking-service: %v", err)
	}
	defer cmd.Process.Kill()
	time.Sleep(2 * time.Second)
	resp1, err1 := http.Get("http://localhost:8086/healthz")
	if err1 != nil || resp1.StatusCode != http.StatusOK {
		t.Fatalf("healthz failed: %v status=%d", err1, resp1.StatusCode)
	}
	resp2, err2 := http.Get("http://localhost:8086/readyz")
	if err2 != nil || resp2.StatusCode != http.StatusOK {
		t.Fatalf("readyz failed: %v status=%d", err2, resp2.StatusCode)
	}
}
func run(ctx context.Context, t *testing.T, name string, args ...string) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %s %v: %v", name, args, err)
	}
}
