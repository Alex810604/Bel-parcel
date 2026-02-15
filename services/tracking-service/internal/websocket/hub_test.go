package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupDB(t *testing.T) *pgxpool.Pool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, "postgres://user:pass@localhost:5432/trip_db?sslmode=disable")
	if err != nil {
		t.Fatalf("db pool failed: %v", err)
	}
	// verify connectivity; skip tests if DB is not available
	if _, err := pool.Exec(ctx, `SELECT 1`); err != nil {
		t.Skipf("skipping: postgres not available: %v", err)
	}
	_, _ = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS websocket_sessions(
			session_id TEXT PRIMARY KEY,
			operator_id TEXT,
			connected_at TIMESTAMPTZ NOT NULL,
			last_heartbeat TIMESTAMPTZ NOT NULL
		)
	`)
	_, _ = pool.Exec(ctx, `TRUNCATE TABLE websocket_sessions`)
	return pool
}

func startHub(t *testing.T, db *pgxpool.Pool) (*Hub, *httptest.Server) {
	h := NewHub(db)
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	t.Cleanup(cancel)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeWS(w, r)
	}))
	t.Cleanup(srv.Close)
	return h, srv
}

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	url := "ws" + srv.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

func countSessions(t *testing.T, db *pgxpool.Pool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var n int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM websocket_sessions`).Scan(&n)
	return n
}

func TestSessionSavedOnConnect(t *testing.T) {
	db := setupDB(t)
	_, srv := startHub(t, db)
	conn := dialWS(t, srv)
	defer conn.Close()
	time.Sleep(200 * time.Millisecond)
	if count := countSessions(t, db); count != 1 {
		t.Fatalf("expected 1 session, got %d", count)
	}
}

func TestSessionDeletedOnDisconnect(t *testing.T) {
	db := setupDB(t)
	_, srv := startHub(t, db)
	conn := dialWS(t, srv)
	time.Sleep(200 * time.Millisecond)
	_ = conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(time.Second))
	time.Sleep(300 * time.Millisecond)
	if count := countSessions(t, db); count != 0 {
		t.Fatalf("expected 0 sessions, got %d", count)
	}
}

func TestHeartbeatUpdatedOnPing(t *testing.T) {
	db := setupDB(t)
	_, srv := startHub(t, db)
	conn := dialWS(t, srv)
	defer conn.Close()
	time.Sleep(150 * time.Millisecond)
	// initial heartbeat time
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var hb1 time.Time
	_ = db.QueryRow(ctx, `SELECT last_heartbeat FROM websocket_sessions LIMIT 1`).Scan(&hb1)
	_ = conn.WriteControl(websocket.PingMessage, []byte("hb"), time.Now().Add(time.Second))
	time.Sleep(200 * time.Millisecond)
	var hb2 time.Time
	_ = db.QueryRow(ctx, `SELECT last_heartbeat FROM websocket_sessions LIMIT 1`).Scan(&hb2)
	if !hb2.After(hb1) {
		t.Fatalf("heartbeat not updated")
	}
}

func TestCloseFrameHandled(t *testing.T) {
	db := setupDB(t)
	_, srv := startHub(t, db)
	conn := dialWS(t, srv)
	time.Sleep(150 * time.Millisecond)
	_ = conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(time.Second))
	time.Sleep(300 * time.Millisecond)
	if count := countSessions(t, db); count != 0 {
		t.Fatalf("expected 0 sessions, got %d", count)
	}
}

func TestReadLimitProtection(t *testing.T) {
	db := setupDB(t)
	_, srv := startHub(t, db)
	conn := dialWS(t, srv)
	defer conn.Close()
	time.Sleep(150 * time.Millisecond)
	payload := make([]byte, 1000)
	_ = conn.WriteMessage(websocket.TextMessage, payload)
	time.Sleep(300 * time.Millisecond)
	if count := countSessions(t, db); count != 0 {
		t.Fatalf("expected 0 sessions after oversized frame, got %d", count)
	}
}
