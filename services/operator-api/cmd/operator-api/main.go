package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"bel-parcel/services/operator-api/internal/app"
	"bel-parcel/services/operator-api/internal/auth"
	"bel-parcel/services/operator-api/internal/clients"
	"bel-parcel/services/operator-api/internal/config"
	"bel-parcel/services/operator-api/internal/infra/db"
	"bel-parcel/services/operator-api/internal/infra/kafka"
	"bel-parcel/services/operator-api/internal/outbox"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func main() {
	if os.Getenv("BP_CMD_TEST") == "1" {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Auth.HS256Secret == "" {
		slog.Error("empty AUTH_HS256SECRET, refusing to start")
		os.Exit(1)
	}
	setupLogger()
	shutdown := setupOTLP(cfg)
	defer shutdown()
	var operatorDB *pgxpool.Pool
	var operatorDBErr error
	for i := 0; i < 10; i++ {
		p, e := db.NewPgxPool(cfg.DB.DSN)
		if e == nil {
			operatorDB = p
			break
		}
		operatorDBErr = e
		slog.Error("failed to connect operator DB", "error", e, "attempt", i+1)
		time.Sleep(1 * time.Second)
	}
	if operatorDBErr != nil {
		slog.Error("operator DB connection failed", "error", operatorDBErr)
		os.Exit(1)
	}
	defer operatorDB.Close()

	if err := outbox.EnsureSchema(operatorDB); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := operatorDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS trips_cache (
				id TEXT PRIMARY KEY,
				origin_warehouse_id TEXT,
				pickup_point_id TEXT,
				carrier_id TEXT,
				assigned_at TIMESTAMPTZ,
				status TEXT,
				updated_at TIMESTAMPTZ DEFAULT NOW()
			);
			CREATE TABLE IF NOT EXISTS batches_cache (
				id TEXT PRIMARY KEY,
				trip_id TEXT,
				updated_at TIMESTAMPTZ DEFAULT NOW()
			);
		`); err != nil {
			slog.Error("failed to ensure cache schema", "error", err)
			os.Exit(1)
		}
	}
	producer, err := kafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	if err != nil {
		slog.Error("failed to create Kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	cls := clients.NewClients(
		buildHTTPClient(cfg),
		cfg.Services.RoutingURL,
		cfg.Services.BatchingURL,
		cfg.Services.OrderURL,
		cfg.Services.ReferenceURL,
	)

	svc := app.NewService(operatorDB, cls, producer, cfg.Kafka.CommandTopic)
	validator := auth.NewValidator(cfg.Auth.HS256Secret, cfg.Auth.Issuer, cfg.Auth.Audience)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outbox.StartPublisher(ctx, operatorDB, producer)

	consumer := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, cfg.Kafka.SyncTopics, cfg.Kafka.Timeout)
	defer consumer.Close()
	consumer.Start(ctx, func(topic string, key, value []byte) error {
		return svc.SyncCache(ctx, topic, value)
	})

	reqDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests.",
			Buckets: []float64{0.05, 0.1, 0.3, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"method", "route", "code"},
	)
	reqTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "route", "code"},
	)
	prometheus.MustRegister(reqDuration, reqTotal)

	measure := func(route string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			h(rec, r)
			el := time.Since(start).Seconds()
			code := strconv.Itoa(rec.status)
			reqDuration.WithLabelValues(r.Method, route, code).Observe(el)
			reqTotal.WithLabelValues(r.Method, route, code).Inc()
		}
	}

	writeJSONError := func(w http.ResponseWriter, status int, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": message})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := operatorDB.Ping(r.Context()); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "operatorDB unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /trips", measure("/trips", auth.RequireRoles(validator, []string{"user", "moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status != "" {
			allowed := map[string]bool{
				"ASSIGNED":   true,
				"IN_TRANSIT": true,
				"DELIVERED":  true,
				"FAILED":     true,
				"PENDING":    true,
			}
			if !allowed[status] {
				writeJSONError(w, http.StatusBadRequest, "invalid status")
				return
			}
		}
		pvz := r.URL.Query().Get("pvz")
		carrier := r.URL.Query().Get("carrier")
		var datePtr *time.Time
		if d := r.URL.Query().Get("date"); d != "" {
			if t, e := time.Parse("2006-01-02", d); e == nil {
				datePtr = &t
			}
		}
		trips, err := svc.ListTrips(r.Context(), status, pvz, carrier, datePtr)
		if err != nil {
			slog.Error("list trips failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(trips)
	})))
	mux.HandleFunc("GET /trips/{trip_id}", measure("/trips/{trip_id}", auth.RequireRoles(validator, []string{"user", "moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("trip_id")
		d, err := svc.TripDetails(r.Context(), id)
		if err != nil {
			slog.Error("trip details failed", "error", err, "trip_id", id)
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d)
	})))
	mux.HandleFunc("POST /trips/{trip_id}/reassign", measure("/trips/{trip_id}/reassign", auth.RequireRoles(validator, []string{"moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("trip_id")
		var body struct {
			NewCarrierID string `json:"new_carrier_id"`
			Reason       string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad request")
			return
		}
		if body.NewCarrierID == "" || body.Reason == "" {
			writeJSONError(w, http.StatusBadRequest, "bad request")
			return
		}
		u := auth.FromContext(r)
		if u == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := svc.ReassignTrip(r.Context(), id, body.NewCarrierID, body.Reason, u.ID); err != nil {
			slog.Error("reassign failed", "error", err, "trip_id", id)
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})))
	mux.HandleFunc("GET /delays", measure("/delays", auth.RequireRoles(validator, []string{"user", "moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		h := 1.5
		if v := r.URL.Query().Get("hours"); v != "" {
			if t, e := time.ParseDuration(v + "h"); e == nil {
				h = t.Hours()
			}
		}
		trips, err := svc.MonitorDelays(r.Context(), time.Duration(h*float64(time.Hour)))
		if err != nil {
			slog.Error("monitor delays failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(trips)
	})))
	mux.HandleFunc("GET /references/{type}", measure("/references/{type}", auth.RequireRoles(validator, []string{"user", "moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		typ := r.PathValue("type")
		allowedTypes := map[string]bool{"warehouses": true, "pvz": true, "carriers": true}
		if !allowedTypes[typ] {
			writeJSONError(w, http.StatusBadRequest, "invalid type")
			return
		}
		q := r.URL.Query().Get("q")
		refs, err := svc.SearchReferences(r.Context(), typ, q)
		if err != nil {
			slog.Error("search references failed", "error", err, "type", typ)
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refs)
	})))
	mux.HandleFunc("PUT /references/pvp/{id}", measure("/references/pvp/{id}", auth.RequireRoles(validator, []string{"admin", "moderator"}, func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			IsHub  bool   `json:"is_hub"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad request")
			return
		}
		if strings.TrimSpace(body.Reason) == "" {
			writeJSONError(w, http.StatusBadRequest, "reason is required")
			return
		}
		u := auth.FromContext(r)
		if u == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := svc.UpdatePickupPoint(r.Context(), id, body.IsHub, body.Reason, u.ID); err != nil {
			slog.Error("update pickup point failed", "error", err, "id", id)
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})))
	mux.HandleFunc("PUT /references/carriers/{carrier_id}", measure("/references/carriers/{carrier_id}", auth.RequireRoles(validator, []string{"admin"}, func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("carrier_id")
		var body struct {
			IsActive bool   `json:"is_active"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad request")
			return
		}
		if body.Reason == "" {
			writeJSONError(w, http.StatusBadRequest, "reason is required")
			return
		}
		u := auth.FromContext(r)
		if u == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := svc.UpdateCarrierStatus(r.Context(), id, body.IsActive, body.Reason, u.ID); err != nil {
			slog.Error("update carrier status failed", "error", err, "id", id)
			writeJSONError(w, http.StatusInternalServerError, "internal")
			return
		}

		slog.Info("carrier status updated",
			"carrier_id", id,
			"is_active", body.IsActive,
			"reason", body.Reason,
			"operator_id", u.ID,
		)

		w.WriteHeader(http.StatusAccepted)
	})))
	mux.Handle("GET /metrics", promhttp.Handler())
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		slog.Info("starting operator-api", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
		}
	}()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	slog.Info("shutting down gracefully...")
	tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tcancel()
	_ = srv.Shutdown(tctx)
	slog.Info("server stopped")
}

func setupLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

type authTransport struct {
	base  http.RoundTripper
	token string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.base == nil {
		return nil, errors.New("no base transport")
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

func buildHTTPClient(cfg *config.Config) *http.Client {
	base := &http.Transport{}
	tok := os.Getenv("REF_AUTH_TOKEN")
	if tok == "" {
		return &http.Client{
			Timeout:   5 * time.Second,
			Transport: base,
		}
	}
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &authTransport{
			base:  base,
			token: tok,
		},
	}
}

func setupOTLP(cfg *config.Config) func() {
	if cfg.OTLP.Endpoint == "" {
		return func() {}
	}
	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(cfg.OTLP.Endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return func() {}
	}
	res, _ := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("operator-api"),
	))
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	}
}
