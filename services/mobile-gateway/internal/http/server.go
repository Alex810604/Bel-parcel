package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"bel-parcel/services/mobile-gateway/internal/auth"
	"bel-parcel/services/mobile-gateway/internal/metrics"
	"bel-parcel/services/mobile-gateway/internal/outbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Server struct {
	db        DB
	validator *auth.Validator
	topics    map[string]string
}

type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Ping(ctx context.Context) error
}

func NewServer(db DB, validator *auth.Validator, topics map[string]string) *Server {
	return &Server{db: db, validator: validator, topics: topics}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", auth.RequireRoles(s.validator, []string{"carrier", "pvp_worker"}, func(w http.ResponseWriter, r *http.Request) {
		s.handleEvents(w, r)
	}))
	mux.HandleFunc("POST /location", auth.RequireRoles(s.validator, []string{"carrier"}, func(w http.ResponseWriter, r *http.Request) {
		s.handleCarrierLocation(w, r)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.Ping(r.Context()); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.Ping(r.Context()); err != nil {
			http.Error(w, "unready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	return mux
}

func (s *Server) resolveTopic(eventType string) (string, bool) {
	if t, ok := s.topics[eventType]; ok {
		return t, true
	}
	switch eventType {
	case "events.batch_picked_up":
		if t, ok := s.topics["batch_picked_up"]; ok {
			return t, true
		}
	case "batch_picked_up":
		if t, ok := s.topics["events.batch_picked_up"]; ok {
			return t, true
		}
	case "events.batch_delivered_to_pvp":
		if t, ok := s.topics["batch_delivered_to_pvp"]; ok {
			return t, true
		}
	case "batch_delivered_to_pvp":
		if t, ok := s.topics["events.batch_delivered_to_pvp"]; ok {
			return t, true
		}
	case "events.batch_received_by_pvp":
		if t, ok := s.topics["batch_received_by_pvp"]; ok {
			return t, true
		}
	case "batch_received_by_pvp":
		if t, ok := s.topics["events.batch_received_by_pvp"]; ok {
			return t, true
		}
	case "events.carrier_location":
		if t, ok := s.topics["carrier.location"]; ok {
			return t, true
		}
	case "carrier.location":
		if t, ok := s.topics["events.carrier_location"]; ok {
			return t, true
		}
	}
	return "", false
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	var envelope struct {
		EventID   string          `json:"event_id"`
		EventType string          `json:"event_type"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
		return
	}
	if envelope.EventID == "" || envelope.EventType == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
		return
	}
	topic, ok := s.resolveTopic(envelope.EventType)
	if !ok {
		http.Error(w, "invalid event_type", http.StatusBadRequest)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
		return
	}
	now := time.Now().UTC()
	switch envelope.EventType {
	case "events.batch_picked_up", "batch_picked_up":
		var d struct {
			TripID        string    `json:"trip_id"`
			BatchID       string    `json:"batch_id"`
			CarrierID     string    `json:"carrier_id"`
			PickupPointID string    `json:"pickup_point_id"`
			PickedUpAt    time.Time `json:"picked_up_at"`
		}
		if err := json.Unmarshal(envelope.Data, &d); err != nil || d.TripID == "" || d.BatchID == "" || d.CarrierID == "" || d.PickupPointID == "" {
			http.Error(w, "invalid data", http.StatusBadRequest)
			metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
			return
		}
		if d.PickedUpAt.IsZero() {
			http.Error(w, "invalid time", http.StatusBadRequest)
			metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
			return
		}
	case "events.batch_delivered_to_pvp", "batch_delivered_to_pvp":
		var d struct {
			TripID      string    `json:"trip_id"`
			BatchID     string    `json:"batch_id"`
			CarrierID   string    `json:"carrier_id"`
			PVPID       string    `json:"pvp_id"`
			IsHub       bool      `json:"is_hub"`
			DeliveredAt time.Time `json:"delivered_at"`
		}
		if err := json.Unmarshal(envelope.Data, &d); err != nil || d.TripID == "" || d.BatchID == "" || d.CarrierID == "" || d.PVPID == "" {
			http.Error(w, "invalid data", http.StatusBadRequest)
			metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
			return
		}
		if d.DeliveredAt.IsZero() {
			http.Error(w, "invalid time", http.StatusBadRequest)
			metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
			return
		}
	case "events.batch_received_by_pvp", "batch_received_by_pvp":
		var d struct {
			BatchID    string    `json:"batch_id"`
			PVPID      string    `json:"pvp_id"`
			OrderIDs   []string  `json:"order_ids"`
			ReceivedAt time.Time `json:"received_at"`
		}
		if err := json.Unmarshal(envelope.Data, &d); err != nil || d.BatchID == "" || d.PVPID == "" || len(d.OrderIDs) == 0 {
			http.Error(w, "invalid data", http.StatusBadRequest)
			metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
			return
		}
		if d.ReceivedAt.IsZero() {
			http.Error(w, "invalid time", http.StatusBadRequest)
			metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
			return
		}
	default:
		http.Error(w, "invalid event_type", http.StatusBadRequest)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "400").Inc()
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "500").Inc()
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO http_events_log(id, event_type, event_id, payload, received_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.NewString(), envelope.EventType, envelope.EventID, envelope.Data, now); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "500").Inc()
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"event_id":       envelope.EventID,
		"event_type":     envelope.EventType,
		"occurred_at":    now,
		"correlation_id": envelope.EventID,
		"data":           json.RawMessage(envelope.Data),
	})
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "500").Inc()
		return
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     envelope.EventType,
		EventID:       envelope.EventID,
		CorrelationID: envelope.EventID,
		Topic:         topic,
		PartitionKey:  envelope.EventID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(r.Context(), tx, evt); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "500").Inc()
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/events", "500").Inc()
		return
	}
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("accepted"))
	metrics.HTTPRequestsTotal.WithLabelValues("/events", "202").Inc()
}

func (s *Server) handleCarrierLocation(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		EventID   string    `json:"event_id"`
		CarrierID string    `json:"carrier_id"`
		Latitude  float64   `json:"latitude"`
		Longitude float64   `json:"longitude"`
		Timestamp time.Time `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.CarrierID == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		metrics.HTTPRequestsTotal.WithLabelValues("/location", "400").Inc()
		return
	}
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/location", "500").Inc()
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	id := payload.EventID
	if id == "" {
		id = uuid.NewString()
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO http_events_log(id, event_type, event_id, payload, received_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.NewString(), "events.carrier_location", id, []byte("{}"), now); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/location", "500").Inc()
		return
	}
	envelope := map[string]interface{}{
		"event_id":       id,
		"event_type":     "events.carrier_location",
		"occurred_at":    now,
		"correlation_id": payload.CarrierID,
		"data": map[string]interface{}{
			"carrier_id": payload.CarrierID,
			"latitude":   payload.Latitude,
			"longitude":  payload.Longitude,
			"timestamp":  payload.Timestamp,
		},
	}
	raw, _ := json.Marshal(envelope)
	topic, ok := s.resolveTopic("events.carrier_location")
	if !ok {
		http.Error(w, "invalid event_type", http.StatusBadRequest)
		metrics.HTTPRequestsTotal.WithLabelValues("/location", "400").Inc()
		return
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "events.carrier_location",
		EventID:       id,
		CorrelationID: payload.CarrierID,
		Topic:         topic,
		PartitionKey:  payload.CarrierID,
		Payload:       raw,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(r.Context(), tx, evt); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/location", "500").Inc()
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		metrics.HTTPRequestsTotal.WithLabelValues("/location", "500").Inc()
		return
	}
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("accepted"))
	metrics.HTTPRequestsTotal.WithLabelValues("/location", "202").Inc()
}
