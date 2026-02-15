package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bel-parcel/services/operator-api/internal/clients"

	"github.com/stretchr/testify/assert"
)

func TestService_ListTrips(t *testing.T) {
	mockTrips := []Trip{
		{ID: "trip-1", Status: "ASSIGNED"},
		{ID: "trip-2", Status: "IN_TRANSIT"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/trips", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockTrips)
	}))
	defer server.Close()

	cls := clients.NewClients(nil, server.URL, server.URL, server.URL, server.URL)
	svc := NewService(nil, cls, nil, "topic")

	trips, err := svc.ListTrips(context.Background(), "", "", "", nil)
	assert.NoError(t, err)
	assert.Len(t, trips, 2)
	assert.Equal(t, "trip-1", trips[0].ID)
}

func TestService_TripDetails_Aggregation(t *testing.T) {
	tripID := "trip-1"
	mockTrip := Trip{ID: tripID, Status: "ASSIGNED"}
	mockBatches := []string{"batch-1", "batch-2"}

	mux := http.NewServeMux()
	mux.HandleFunc("/trips/"+tripID, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(mockTrip)
	})
	mux.HandleFunc("/batches", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, tripID, r.URL.Query().Get("trip_id"))
		json.NewEncoder(w).Encode(mockBatches)
	})
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		batchID := r.URL.Query().Get("batch_id")
		if batchID == "batch-1" {
			json.NewEncoder(w).Encode([]string{"order-1"})
		} else if batchID == "batch-2" {
			json.NewEncoder(w).Encode([]string{"order-2"})
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	cls := clients.NewClients(nil, server.URL, server.URL, server.URL, server.URL)
	svc := NewService(nil, cls, nil, "topic")

	details, err := svc.TripDetails(context.Background(), tripID)
	assert.NoError(t, err)
	assert.NotNil(t, details["trip"])
	assert.Len(t, details["batches"], 2)
	assert.Len(t, details["orders"], 2)
}

func TestService_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cls := clients.NewClients(nil, server.URL, server.URL, server.URL, server.URL)
	svc := NewService(nil, cls, nil, "topic")

	_, err := svc.ListTrips(context.Background(), "", "", "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "routing service unavailable")
}

func TestService_Caching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode([]Trip{{ID: "trip-1"}})
	}))
	defer server.Close()

	cls := clients.NewClients(nil, server.URL, server.URL, server.URL, server.URL)
	svc := NewService(nil, cls, nil, "topic")

	// First call
	_, _ = svc.ListTrips(context.Background(), "", "", "", nil)
	assert.Equal(t, 1, callCount)

	// Second call (should be cached)
	_, _ = svc.ListTrips(context.Background(), "", "", "", nil)
	assert.Equal(t, 1, callCount)
}
