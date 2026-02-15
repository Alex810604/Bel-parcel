package app

import (
	"testing"
	"time"

	"bel-parcel/services/reference-service/internal/infra/outbox"
)

func TestOutboxEvent_Structure(t *testing.T) {
	now := time.Now().UTC()
	evt := outbox.Event{
		ID:            "id-1",
		EventType:     "events.reference_updated",
		CorrelationID: "pvp-1",
		Topic:         "events.reference_updated",
		PartitionKey:  "pvp-1",
		Payload:       []byte(`{"data":{"update_type":"pickup_point"}}`),
		OccurredAt:    now,
	}
	if evt.EventType != "events.reference_updated" {
		t.Fatalf("unexpected event type: %s", evt.EventType)
	}
	if evt.Topic == "" || evt.PartitionKey == "" {
		t.Fatalf("topic/partition missing")
	}
	if evt.OccurredAt.IsZero() {
		t.Fatalf("occurred_at missing")
	}
}
