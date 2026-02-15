package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPutRetriesAndContentType(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"bad"}`))
			return
		}
		if calls < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`oops`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()
	c := &ServiceClient{BaseURL: ts.URL, HTTPClient: &http.Client{Timeout: 2 * time.Second}}
	ctx := context.Background()
	_, err := c.Put(ctx, "/test", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 calls")
	}
}

func TestPutUnexpectedStatusBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad"}`))
	}))
	defer ts.Close()
	c := &ServiceClient{BaseURL: ts.URL, HTTPClient: &http.Client{Timeout: 2 * time.Second}}
	ctx := context.Background()
	_, err := c.Put(ctx, "/test", strings.NewReader(`{}`))
	if err == nil || !strings.Contains(err.Error(), "unexpected status") || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("expected unexpected status with body")
	}
}
