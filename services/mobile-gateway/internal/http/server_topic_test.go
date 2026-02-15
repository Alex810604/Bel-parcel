package httpserver

import (
	"testing"

	"bel-parcel/services/mobile-gateway/internal/auth"
)

func TestResolveTopic_MappingPairs(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("s", "iss", "aud"), map[string]string{
		"events.batch_picked_up":        "T1",
		"batch_picked_up":               "T2",
		"events.batch_delivered_to_pvp": "T3",
		"batch_delivered_to_pvp":        "T4",
		"events.batch_received_by_pvp":  "T5",
		"batch_received_by_pvp":         "T6",
		"events.carrier_location":       "T7",
		"carrier.location":              "T8",
	})
	type pair struct {
		in  string
		exp string
	}
	cases := []pair{
		{"events.batch_picked_up", "T1"},
		{"batch_picked_up", "T2"},
		{"events.batch_delivered_to_pvp", "T3"},
		{"batch_delivered_to_pvp", "T4"},
		{"events.batch_received_by_pvp", "T5"},
		{"batch_received_by_pvp", "T6"},
		{"events.carrier_location", "T7"},
		{"carrier.location", "T8"},
	}
	for _, c := range cases {
		if got, ok := s.resolveTopic(c.in); !ok || got == "" {
			t.Fatalf("expected mapping for %s", c.in)
		}
	}
}
