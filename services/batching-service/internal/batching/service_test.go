package batching

import (
	"math"
	"testing"
)

func TestHaversine_ZeroDistance(t *testing.T) {
	d := haversine(53.9, 27.5667, 53.9, 27.5667)
	if math.Abs(d) > 0.001 {
		t.Fatalf("expected ~0 distance, got %f", d)
	}
}

func TestHaversine_Symmetry(t *testing.T) {
	a := haversine(53.9, 27.5667, 52.1, 23.7)
	b := haversine(52.1, 23.7, 53.9, 27.5667)
	if math.Abs(a-b) > 0.0001 {
		t.Fatalf("expected symmetry, got a=%f b=%f", a, b)
	}
}

func TestHaversine_EquatorOneDegreeLongitude(t *testing.T) {
	d := haversine(0, 0, 0, 1)
	if d < 110_000 || d > 112_000 {
		t.Fatalf("expected ~111km, got %f", d)
	}
}

