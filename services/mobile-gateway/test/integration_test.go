//go:build integration

package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"github.com/segmentio/kafka-go"
)

func TestMobileGateway_DockerCompose(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("E2E not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Start infra
	run(ctx, t, "docker-compose", "-f", "../../../docker-compose.yml", "up", "-d")
	defer run(context.Background(), t, "docker-compose", "-f", "../../../docker-compose.yml", "down", "-v")
	time.Sleep(5 * time.Second)
	// Start mobile-gateway
	cmd := exec.CommandContext(ctx, "go", "run", "../cmd/mobile-gateway/main.go")
	cmd.Env = append(os.Environ(),
		"SERVER_PORT=8088",
		"DB_DSN=postgres://user:pass@localhost:5432/trip_db?sslmode=disable",
		"KAFKA_BROKERS=localhost:9092",
		"KAFKA_TIMEOUT=5s",
		"KAFKA_TOPICPICKEDUP=events.batch_picked_up",
		"KAFKA_TOPICDELIVEREDTOPVP=events.batch_delivered_to_pvp",
		"KAFKA_TOPICRECEIVEDBYPVP=events.batch_received_by_pvp",
		"AUTH_HS256SECRET=secret",
		"AUTH_ISSUER=bp",
		"AUTH_AUDIENCE=mobile",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start mobile-gateway: %v", err)
	}
	defer cmd.Process.Kill()
	time.Sleep(2 * time.Second)
	// Prepare JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":  "bp",
		"aud":  []string{"mobile"},
		"sub":  "u-1",
		"role": "carrier",
		"exp":  time.Now().Add(5 * time.Minute).Unix(),
	})
	jwtStr, _ := token.SignedString([]byte("secret"))
	// Send event
	eventID := "it-" + time.Now().Format("20060102150405")
	payload := map[string]interface{}{
		"event_id":   eventID,
		"event_type": "batch_picked_up",
		"data": map[string]interface{}{
			"batch_id":     "b-1",
			"carrier_id":   "c-1",
			"picked_up_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "http://localhost:8088/events", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || (resp.StatusCode != http.StatusAccepted) {
		t.Fatalf("POST /events failed: %v status=%v", err, resp.StatusCode)
	}
	// Verify outbox published
	db, err := sql.Open("postgres", "postgres://user:pass@localhost:5432/trip_db?sslmode=disable")
	if err != nil {
		t.Fatalf("db open failed: %v", err)
	}
	defer db.Close()
	deadline := time.Now().Add(10 * time.Second)
	published := false
	for time.Now().Before(deadline) {
		var st string
		_ = db.QueryRow(`SELECT status FROM outbox_events WHERE event_type=$1 AND event_id=$2`, "batch_picked_up", eventID).Scan(&st)
		if st == "published" {
			published = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !published {
		t.Fatalf("outbox event not published")
	}
	// Verify Kafka message exists
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "it",
		Topic:   "events.batch_picked_up",
		MaxWait: 2 * time.Second,
	})
	defer reader.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, kerr := reader.FetchMessage(ctx2)
	if kerr != nil {
		t.Fatalf("kafka message not observed: %v", kerr)
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
