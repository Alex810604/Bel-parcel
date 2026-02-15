package tracking

import (
	"testing"
	"time"
)

func TestHaversineDistance(t *testing.T) {
	d := haversine(53.9, 27.56, 53.9, 27.56)
	if d != 0 {
		t.Fatalf("expected 0, got %f", d)
	}
	d2 := haversine(53.9, 27.56, 53.91, 27.56)
	if d2 <= 1000 || d2 >= 1500 {
		t.Fatalf("expected ~1.1km, got %f", d2)
	}
}

func TestCalculateDeviation(t *testing.T) {
	originLat, originLng := 53.9000, 27.5600
	destLat, destLng := 53.9100, 27.5600
	currentLat, currentLng := 53.9050, 27.5600
	dev := calculateDeviation(currentLat, currentLng, originLat, originLng, destLat, destLng)
	if dev < 0 || dev > 10 {
		t.Fatalf("expected near-line deviation small, got %f", dev)
	}
	currentLat2, currentLng2 := 53.9000, 27.5700
	dev2 := calculateDeviation(currentLat2, currentLng2, originLat, originLng, destLat, destLng)
	if dev2 < 600 || dev2 > 700 {
		t.Fatalf("expected lateral deviation ~650m, got %f", dev2)
	}
}

func TestParseDuration(t *testing.T) {
	if parseDuration("invalid") != 0 {
		t.Fatalf("expected 0 for invalid")
	}
	if parseDuration("2m") != 2*time.Minute {
		t.Fatalf("expected 2m")
	}
}

func TestCalculateEstimatedArrival(t *testing.T) {
	assigned := time.Now().Add(-10 * time.Minute)
	eta := calculateEstimatedArrival(assigned, "30m", 53.9000, 27.5600, 53.9100, 27.5600, haversine(53.9000, 27.5600, 53.9100, 27.5600))
	if eta.Before(time.Now()) {
		t.Fatalf("eta should be in future")
	}
}
