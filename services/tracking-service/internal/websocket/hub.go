package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Hub struct {
	mu         sync.Mutex
	clients    map[*client]bool
	register   chan *client
	unregister chan *client
	db         *pgxpool.Pool
}

type client struct {
	conn *websocket.Conn
	send chan []byte
	id   string
}

func NewHub(db *pgxpool.Pool) *Hub {
	return &Hub{
		clients:    make(map[*client]bool),
		register:   make(chan *client),
		unregister: make(chan *client),
		db:         db,
	}
}

func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-h.register:
			h.clients[c] = true
			func() {
				ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := h.db.Exec(ctx2, `
					INSERT INTO websocket_sessions(session_id, connected_at, last_heartbeat)
					VALUES ($1, NOW(), NOW())
					ON CONFLICT (session_id) DO UPDATE SET last_heartbeat=NOW()
				`, c.id); err != nil {
					slog.Warn("ws session insert failed", "error", err, "session_id", c.id)
				}
			}()
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
				func() {
					ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if _, err := h.db.Exec(ctx2, `
						DELETE FROM websocket_sessions WHERE session_id=$1
					`, c.id); err != nil {
						slog.Warn("ws session delete failed", "error", err, "session_id", c.id)
					}
				}()
			}
		case <-t.C:
			h.cleanupSessions(ctx)
		}
	}
}

func (h *Hub) BroadcastJSON(payload interface{}) error {
	msg, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			close(c.send)
			delete(h.clients, c)
		}
	}
	return nil
}

func (h *Hub) cleanupSessions(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = h.db.Exec(ctx, `
		DELETE FROM websocket_sessions WHERE last_heartbeat < NOW() - INTERVAL '10 minutes'
	`)
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 256), id: r.RemoteAddr}
	h.register <- c
	go h.restoreAlerts(c)
	go c.readPump(h)
	go h.writePump(c)
}

func (h *Hub) writePump(c *client) {
	defer func() {
		h.unregister <- c
		c.conn.Close()
	}()
	for {
		msg, ok := <-c.send
		if !ok {
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (h *Hub) restoreAlerts(c *client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := h.db.Query(ctx, `
		SELECT alert_type, trip_id, carrier_id, message, severity, created_at
		FROM active_alerts
		WHERE resolved_at IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	type alert struct {
		AlertType string    `json:"alert_type"`
		TripID    string    `json:"trip_id"`
		CarrierID string    `json:"carrier_id"`
		Message   string    `json:"message"`
		Severity  string    `json:"severity"`
		CreatedAt time.Time `json:"created_at"`
	}
	for rows.Next() {
		var a alert
		if err := rows.Scan(&a.AlertType, &a.TripID, &a.CarrierID, &a.Message, &a.Severity, &a.CreatedAt); err != nil {
			continue
		}
		msg := map[string]interface{}{
			"type":    "alert",
			"payload": a,
		}
		b, _ := json.Marshal(msg)
		select {
		case c.send <- b:
		default:
		}
	}
}

func (h *Hub) updateHeartbeat(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = h.db.Exec(ctx, `
		UPDATE websocket_sessions SET last_heartbeat=NOW() WHERE session_id=$1
	`, sessionID)
}
