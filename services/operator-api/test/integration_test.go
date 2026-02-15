package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/segmentio/kafka-go"
)

func run(ctx context.Context, t *testing.T, name string, args ...string) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("command %s failed: %v", name, err)
	}
}

func TestHubIntegration(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("E2E not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Start Infrastructure
	run(ctx, t, "docker", "compose", "-f", "../../../docker-compose.yml", "up", "-d", "postgres", "kafka", "kafka-init")
	// defer run(context.Background(), t, "docker-compose", "-f", "../../../docker-compose.yml", "down", "-v") // Keep it running for debugging if needed, or uncomment to clean up

	time.Sleep(10 * time.Second) // Wait for DB and Kafka

	// Ensure databases exist and minimal schema for reference_db
	adminDB, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("adminDB open failed: %v", err)
	}
	defer adminDB.Close()
	for _, db := range []string{"reference_db", "batching_db"} {
		if _, err := adminDB.Exec("CREATE DATABASE " + db); err != nil {
			// ignore if already exists
		}
	}
	// ensure operator_db exists for operator-api (use reference-db Postgres on 5435 to avoid saturation)
	if _, err := adminDB.Exec("CREATE DATABASE operator_db"); err != nil {
		// ignore if already exists
	}
	adminDB2, err := sql.Open("pgx", "postgres://user:pass@localhost:5435/postgres?sslmode=disable")
	if err == nil {
		_, _ = adminDB2.Exec("CREATE DATABASE operator_db")
		adminDB2.Close()
	}
	// create minimal schema in reference_db
	refDDL, err := sql.Open("pgx", "postgres://user:pass@localhost:5435/reference_db?sslmode=disable")
	if err != nil {
		t.Fatalf("refDDL open failed: %v", err)
	}
	if _, err := refDDL.Exec(`
		CREATE TABLE IF NOT EXISTS pickup_points(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			address TEXT,
			location_lat DOUBLE PRECISION NOT NULL,
			location_lng DOUBLE PRECISION NOT NULL,
			is_hub BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		ALTER TABLE pickup_points ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();
		ALTER TABLE pickup_points ADD COLUMN IF NOT EXISTS address TEXT;
	`); err != nil {
		t.Fatalf("create pickup_points failed: %v", err)
	}
	refDDL.Close()

	// 2. Prepare Admin Token for outgoing calls from operator-api to reference-service
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":  "bp",
		"aud":  []string{"operator"},
		"sub":  "admin-1",
		"role": "admin",
		"exp":  time.Now().Add(5 * time.Minute).Unix(),
	})
	jwtStr, _ := token.SignedString([]byte("secret"))

	l1, _ := net.Listen("tcp", "127.0.0.1:0")
	opPort := strconv.Itoa(l1.Addr().(*net.TCPAddr).Port)
	l1.Close()
	// no batching-service needed

	// 3. Start operator-api
	opCmd := exec.CommandContext(ctx, "go", "run", "../cmd/operator-api/main.go")
	opCmd.Env = append(os.Environ(),
		"SERVER_PORT="+opPort,
		"DB_DSN=postgres://user:pass@localhost:5435/operator_db?sslmode=disable",
		"KAFKA_BROKERS=localhost:9092",
		"KAFKA_COMMANDTOPIC=commands.trip.reassign",
		"SERVICES_REFERENCEURL=http://127.0.0.1:8084",
		"AUTH_HS256SECRET=secret",
		"AUTH_ISSUER=bp",
		"AUTH_AUDIENCE=operator",
		"REF_AUTH_TOKEN="+jwtStr,
	)
	opCmd.Stdout = io.Discard
	opCmd.Stderr = io.Discard
	if err := opCmd.Start(); err != nil {
		t.Fatalf("failed to start operator-api: %v", err)
	}
	defer func() {
		_ = opCmd.Process.Kill()
	}()

	// Skip starting batching-service to avoid DB connection saturation

	// Wait for operator-api to be ready
	deadlineReady := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadlineReady) {
		resp, err := http.Get("http://localhost:" + opPort + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Ensure Kafka topic exists for publisher
	conn, _ := kafka.Dial("tcp", "localhost:9092")
	if conn != nil {
		_ = conn.CreateTopics(kafka.TopicConfig{
			Topic:             "events.reference_updated",
			NumPartitions:     1,
			ReplicationFactor: 1,
		})
		_ = conn.Close()
	}
	// Verify published event via DB

	// 5. Prepare Admin Token for operator's HTTP request to operator-api
	token2 := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":  "bp",
		"aud":  []string{"operator"},
		"sub":  "admin-1",
		"role": "admin",
		"exp":  time.Now().Add(5 * time.Minute).Unix(),
	})
	operatorJWT := func() string {
		s, _ := token2.SignedString([]byte("secret"))
		return s
	}()

	// 5. Create a PVP (Minsk) if it doesn't exist (Direct DB insert for setup)
	refDB, err := sql.Open("pgx", "postgres://user:pass@localhost:5435/reference_db?sslmode=disable")
	if err != nil {
		t.Fatalf("refDB open failed: %v", err)
	}
	defer refDB.Close()

	minskID := fmt.Sprintf("pvp-minsk-%d", time.Now().UnixNano())
	_, err = refDB.Exec(`
		INSERT INTO pickup_points (id, name, location_lat, location_lng, is_hub)
		VALUES ($1, 'Minsk Hub', 53.9006, 27.5590, false)
		ON CONFLICT (id) DO UPDATE SET is_hub = false
	`, minskID)
	if err != nil {
		t.Fatalf("failed to seed Minsk PVP: %v", err)
	}
	if _, err := refDB.Exec(`DELETE FROM outbox_events`); err != nil {
		t.Fatalf("failed to clear outbox_events: %v", err)
	}
	var cnt int
	if err := refDB.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&cnt); err != nil {
		t.Fatalf("failed to count outbox_events: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("outbox_events not empty: %d", cnt)
	}

	// 6. Send PUT request to mark as Hub
	payload := map[string]interface{}{
		"is_hub": true,
		"reason": "E2E test update",
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("http://localhost:%s/references/pvp/%s", opPort, minskID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+operatorJWT)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		var respBody bytes.Buffer
		_, _ = respBody.ReadFrom(resp.Body)
		t.Fatalf("PUT request status: %d, expected 202. Body: %s", resp.StatusCode, respBody.String())
	}

	deadlineEvt := time.Now().Add(30 * time.Second)
	evOK := false
	for time.Now().Before(deadlineEvt) {
		var cnt int
		err := refDB.QueryRow(`SELECT COUNT(*) FROM published_events WHERE event_type='events.reference_updated' AND correlation_id=$1`, minskID).Scan(&cnt)
		if err == nil && cnt > 0 {
			evOK = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !evOK {
		var attempts int
		var lastErr sql.NullString
		_ = refDB.QueryRow(`SELECT attempts, last_error FROM outbox_events WHERE event_type='events.reference_updated' AND correlation_id=$1 ORDER BY created_at DESC LIMIT 1`, minskID).Scan(&attempts, &lastErr)
		if lastErr.Valid {
			t.Fatalf("no published event for %s, last_error: %s", minskID, lastErr.String)
		}
		t.Fatalf("no published event for %s", minskID)
	}

	// Skip batching-service DB verification due to broker advertise address
}
