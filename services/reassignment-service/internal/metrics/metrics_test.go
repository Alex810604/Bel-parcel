package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitRegistersMetricsHandler(t *testing.T) {
	mux := http.NewServeMux()
	Init(mux)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
