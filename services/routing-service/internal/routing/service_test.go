package routing

import (
	"math"
	"testing"
)

func TestHaversine_ZeroDistance(t *testing.T) {
	d := haversine(53.9, 27.5667, 53.9, 27.5667)
	if math.Abs(d) > 0.001 {
		t.Fatalf("expected ~0, got %f", d)
	}
}

func TestChooseCarrier_EmptyCandidates(t *testing.T) {
	_, _, err := chooseCarrier(0, 0, nil)
	if err == nil || err.Error() != "no active carriers" {
		t.Fatalf("expected no active carriers error, got %v", err)
	}
}

func TestChooseCarrier_NoPositions(t *testing.T) {
	_, _, err := chooseCarrier(0, 0, []carrierCandidate{
		{id: "c1", hasPos: false},
		{id: "c2", hasPos: false},
	})
	if err == nil || err.Error() != "no active carriers within 5km" {
		t.Fatalf("expected no active carriers within 5km error, got %v", err)
	}
}

func TestChooseCarrier_AllOutsideRadius(t *testing.T) {
	_, _, err := chooseCarrier(0, 0, []carrierCandidate{
		{id: "c1", hasPos: true, lat: 0, lng: 0.06},
		{id: "c2", hasPos: true, lat: 0, lng: 0.07},
	})
	if err == nil || err.Error() != "no active carriers within 5km" {
		t.Fatalf("expected no active carriers within 5km error, got %v", err)
	}
}

func TestChooseCarrier_PicksNearestWithinRadius(t *testing.T) {
	id, dist, err := chooseCarrier(0, 0, []carrierCandidate{
		{id: "far", hasPos: true, lat: 0, lng: 0.02},
		{id: "near", hasPos: true, lat: 0, lng: 0.01},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "near" {
		t.Fatalf("expected near, got %q", id)
	}
	if dist <= 0 || dist >= 5000 {
		t.Fatalf("expected distance within (0,5000), got %d", dist)
	}
}

