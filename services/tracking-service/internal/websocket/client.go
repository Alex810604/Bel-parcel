package websocket

import (
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

func (c *client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetPingHandler(func(appData string) error {
		go hub.updateHeartbeat(c.id)
		deadline := time.Now().Add(1 * time.Second)
		return c.conn.WriteControl(websocket.PongMessage, []byte(appData), deadline)
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Warn("WebSocket closed unexpectedly", "error", err, "client", c.id)
			}
			break
		}
	}
}
