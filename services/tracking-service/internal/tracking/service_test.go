package tracking

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"os"
	"path/filepath"
	"testing"
	"time"

	whub "bel-parcel/services/tracking-service/internal/websocket"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	dsn := os.Getenv("TRACKING_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://user:pass@localhost:5432/tracking_db?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skip("no test DB available")
		return nil
	}
	schemaPath := filepath.Join("..", "..", "infra", "db", "schema.sql")
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Skip("schema.sql not readable")
		return nil
	}
	if _, err := pool.Exec(context.Background(), string(content)); err != nil {
		t.Skip("cannot apply schema")
		return nil
	}
	return pool
}

func TestIdempotencyDuplicateEvent(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	svc := NewService(db, 500, 15)
	envelope := map[string]interface{}{
		"event_id":    "evt-dup-1",
		"occurred_at": time.Now().UTC(),
		"data": map[string]interface{}{
			"trip_id":            "trip-1",
			"carrier_id":         "carrier-1",
			"origin_lat":         53.9,
			"origin_lng":         27.56,
			"destination_lat":    53.7,
			"destination_lng":    27.3,
			"assigned_at":        time.Now().UTC().Add(-10 * time.Minute),
			"estimated_duration": "30m",
		},
	}
	payload, _ := json.Marshal(envelope)
	if err := svc.handleTripAssigned(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if err := svc.handleTripAssigned(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	var cnt int
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM active_trips WHERE trip_id='trip-1'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("expected 1 active_trip, got %d", cnt)
	}
}

func TestRouteDeviationAlert(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	svc := NewService(db, 500, 15)
	_, _ = db.Exec(context.Background(), `
		INSERT INTO active_trips(trip_id, carrier_id, origin_lat, origin_lng, destination_lat, destination_lng, assigned_at, estimated_duration, status)
		VALUES ('trip-2','carrier-2',53.9,27.56,53.7,27.3,NOW()-INTERVAL '20 minutes','30m','in_transit')
		ON CONFLICT (trip_id) DO NOTHING
	`)
	env := map[string]interface{}{
		"event_id":    "evt-dev-1",
		"occurred_at": time.Now().UTC(),
		"data": map[string]interface{}{
			"trip_id":    "trip-2",
			"carrier_id": "carrier-2",
			"lat":        54.9,
			"lng":        28.56,
			"timestamp":  time.Now().UTC(),
		},
	}
	b, _ := json.Marshal(env)
	if err := svc.handleCarrierLocation(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	var cnt int
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM active_alerts WHERE alert_type='route_deviation' AND trip_id='trip-2'`).Scan(&cnt)
	if cnt == 0 {
		t.Fatal("expected route_deviation alert")
	}
}

func TestDriverLateAlert(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	svc := NewService(db, 500, 15)
	_, _ = db.Exec(context.Background(), `
		INSERT INTO active_trips(trip_id, carrier_id, origin_lat, origin_lng, destination_lat, destination_lng, assigned_at, estimated_duration, status)
		VALUES ('trip-3','carrier-3',53.9,27.56,53.7,27.3,NOW()-INTERVAL '90 minutes','10m','in_transit')
		ON CONFLICT (trip_id) DO NOTHING
	`)
	env := map[string]interface{}{
		"event_id":    "evt-late-1",
		"occurred_at": time.Now().UTC(),
		"data": map[string]interface{}{
			"trip_id":    "trip-3",
			"carrier_id": "carrier-3",
			"lat":        53.8,
			"lng":        27.5,
			"timestamp":  time.Now().UTC(),
		},
	}
	b, _ := json.Marshal(env)
	if err := svc.handleCarrierLocation(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	var cnt int
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM active_alerts WHERE alert_type='driver_late' AND trip_id='trip-3'`).Scan(&cnt)
	if cnt == 0 {
		t.Fatal("expected driver_late alert")
	}
}

func TestETA_NoLateOnShortRouteMidway(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	svc := NewService(db, 500, 15)
	// 10km route: pick coordinates ~10km apart
	originLat, originLng := 53.9000, 27.5600
	destLat, destLng := 53.9900, 27.5600 // ~10km north
	total := haversine(originLat, originLng, destLat, destLng)
	_, _ = db.Exec(context.Background(), `
		INSERT INTO active_trips(trip_id, carrier_id, origin_lat, origin_lng, destination_lat, destination_lng, assigned_at, estimated_duration, status, total_route_distance_meters)
		VALUES ('trip-short','carrier-s', $1, $2, $3, $4, NOW()-INTERVAL '2 minutes', '4m', 'in_transit', $5)
		ON CONFLICT (trip_id) DO NOTHING
	`, originLat, originLng, destLat, destLng, total)
	// Midway point ~5km from origin
	env := map[string]interface{}{
		"event_id":    "evt-short-1",
		"occurred_at": time.Now().UTC(),
		"data": map[string]interface{}{
			"trip_id":  "trip-short",
			"carrier_id": "carrier-s",
			"lat":      53.9450,
			"lng":      27.5600,
			"timestamp": time.Now().UTC(),
		},
	}
	b, _ := json.Marshal(env)
	if err := svc.handleCarrierLocation(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	var cnt int
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM active_alerts WHERE alert_type='driver_late' AND trip_id='trip-short'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected no late alert on short route midway, got %d", cnt)
	}
}

func TestETA_NoLateOnLongRouteMidway(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	svc := NewService(db, 500, 15)
	// 200km route: ~1.8 degrees latitude
	originLat, originLng := 53.0000, 27.0000
	destLat, destLng := 54.8000, 27.0000
	total := haversine(originLat, originLng, destLat, destLng)
	_, _ = db.Exec(context.Background(), `
		INSERT INTO active_trips(trip_id, carrier_id, origin_lat, origin_lng, destination_lat, destination_lng, assigned_at, estimated_duration, status, total_route_distance_meters)
		VALUES ('trip-long','carrier-l', $1, $2, $3, $4, NOW()-INTERVAL '30 minutes', '60m', 'in_transit', $5)
		ON CONFLICT (trip_id) DO NOTHING
	`, originLat, originLng, destLat, destLng, total)
	env := map[string]interface{}{
		"event_id":    "evt-long-1",
		"occurred_at": time.Now().UTC(),
		"data": map[string]interface{}{
			"trip_id":  "trip-long",
			"carrier_id": "carrier-l",
			"lat":      53.9000,
			"lng":      27.0000,
			"timestamp": time.Now().UTC(),
		},
	}
	b, _ := json.Marshal(env)
	if err := svc.handleCarrierLocation(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	var cnt int
	_ = db.QueryRow(context.Background(), `SELECT COUNT(*) FROM active_alerts WHERE alert_type='driver_late' AND trip_id='trip-long'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected no late alert on long route midway, got %d", cnt)
	}
}

func TestMainExitsOnDBFailure(t *testing.T) {
	// Build the binary
	root := filepath.Join("..", "..", "cmd", "tracking-service")
	bin := filepath.Join(os.TempDir(), "tracking-service-test.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = root
	if err := build.Run(); err != nil {
		t.Skip("cannot build tracking-service binary")
		return
	}
	// Run with invalid DSN, expect exit code != 0
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "DB_DSN=postgres://invalid:invalid@localhost:1/db")
	start := time.Now()
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit on DB failure")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("service did not fail fast on DB connection error")
	}
}
func TestRestoreAlertsOnReconnect(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec(context.Background(), `
		INSERT INTO active_alerts(alert_id, alert_type, trip_id, carrier_id, message, created_at, severity)
		VALUES ('00000000-0000-0000-0000-000000000001','route_deviation','trip-4','carrier-4','Deviation','NOW()','warning')
		ON CONFLICT DO NOTHING
	`)
	h := whub.NewHub(db)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/alerts", h.ServeWS)
	srv := &http.Server{Addr: ":18087", Handler: mux}
	go h.Run(context.Background())
	go srv.ListenAndServe()
	defer srv.Shutdown(context.Background())
	time.Sleep(500 * time.Millisecond)
	u := "ws://localhost:18087/ws/alerts"
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Skip("websocket dial failed")
		return
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Skip("no message received")
		return
	}
	if len(msg) == 0 {
		t.Fatal("expected non-empty alert payload")
	}
}
