package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"bel-parcel/services/routing-service/internal/config"
	"bel-parcel/services/routing-service/internal/infra/db"
	"bel-parcel/services/routing-service/internal/infra/kafka"
	"bel-parcel/services/routing-service/internal/metrics"
	"bel-parcel/services/routing-service/internal/outbox"
	"bel-parcel/services/routing-service/internal/routing"
)

func main() {
	if os.Getenv("BP_CMD_TEST") == "1" {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	setupLogger()
	shutdown := setupOTLP(cfg)
	defer shutdown()

	tripDB, err := db.NewPgxPool(cfg.DB.TripDSN)
	if err != nil {
		slog.Error("failed to connect to Trip DB", "error", err)
		os.Exit(1)
	}
	defer tripDB.Close()
	if err := outbox.EnsureSchema(tripDB); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}
	// Ensure business schema
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := tripDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS trips (
				id UUID PRIMARY KEY,
				origin_warehouse_id TEXT,
				pickup_point_id TEXT,
				carrier_id TEXT,
				status TEXT NOT NULL,
				assigned_at TIMESTAMPTZ NOT NULL
			)
		`); err != nil {
			slog.Error("failed to ensure trips schema", "error", err)
			os.Exit(1)
		}
		if _, err := tripDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS trip_batches (
				trip_id UUID NOT NULL,
				batch_id TEXT NOT NULL,
				PRIMARY KEY (trip_id, batch_id)
			)
		`); err != nil {
			slog.Error("failed to ensure trip_batches schema", "error", err)
			os.Exit(1)
		}
		if _, err := tripDB.Exec(ctx, `
			ALTER TABLE trips ADD COLUMN IF NOT EXISTS assigned_distance_meters INT DEFAULT 0;
		`); err != nil {
			slog.Error("failed to alter trips schema", "error", err)
			os.Exit(1)
		}
		if _, err := tripDB.Exec(ctx, `
			ALTER TABLE trips 
			ADD COLUMN IF NOT EXISTS origin_lat DOUBLE PRECISION DEFAULT 0,
			ADD COLUMN IF NOT EXISTS origin_lng DOUBLE PRECISION DEFAULT 0,
			ADD COLUMN IF NOT EXISTS dest_lat DOUBLE PRECISION DEFAULT 0,
			ADD COLUMN IF NOT EXISTS dest_lng DOUBLE PRECISION DEFAULT 0;
		`); err != nil {
			slog.Error("failed to add coords to trips schema", "error", err)
			os.Exit(1)
		}
		if _, err := tripDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS pending_assignments (
				trip_id UUID PRIMARY KEY,
				timeout_at TIMESTAMPTZ NOT NULL
			)
		`); err != nil {
			slog.Error("failed to ensure pending_assignments schema", "error", err)
			os.Exit(1)
		}
		if _, err := tripDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS processed_events (
				event_id TEXT PRIMARY KEY,
				occurred_at TIMESTAMPTZ NOT NULL
			)
		`); err != nil {
			slog.Error("failed to ensure processed_events schema", "error", err)
			os.Exit(1)
		}
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := tripDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS carrier_positions (
				carrier_id TEXT PRIMARY KEY,
				lat DOUBLE PRECISION NOT NULL,
				lng DOUBLE PRECISION NOT NULL,
				updated_at TIMESTAMPTZ NOT NULL
			)
		`); err != nil {
			slog.Error("failed to ensure carrier_positions schema", "error", err)
			os.Exit(1)
		}
		if _, err := tripDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS carrier_activity_cache (
				carrier_id TEXT PRIMARY KEY,
				is_active BOOLEAN DEFAULT TRUE,
				updated_at TIMESTAMPTZ
			)
		`); err != nil {
			slog.Error("failed to ensure carrier_activity_cache schema", "error", err)
			os.Exit(1)
		}
		// Ensure updated_at exists if table existed with different schema
		if _, err := tripDB.Exec(ctx, `
			ALTER TABLE carrier_activity_cache ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;
		`); err != nil {
			slog.Error("failed to alter carrier_activity_cache schema", "error", err)
			os.Exit(1)
		}
	}

	cctx, ccancel := context.WithCancel(context.Background())
	defer ccancel()

	producer := kafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	defer producer.Close()
	go outbox.StartPublisher(cctx, tripDB, producer)

	consumer := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, cfg.Kafka.ConsumeTopics, cfg.Kafka.Timeout)
	defer consumer.Close()

	svc := routing.NewService(tripDB, producer, cfg.Kafka.ProduceTopic)
	svc.Start(cctx)

	consumer.Start(cctx, func(topic string, key, value []byte) error {
		return svc.HandleEvent(cctx, topic, key, value)
	})

	mux := http.NewServeMux()
	metrics.Init(mux)
	metrics.StartDLQGauge(cctx, tripDB)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := tripDB.Ping(r.Context()); err != nil {
			slog.Error("readiness check failed", "error", err)
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: mux,
	}

	go func() {
		slog.Info("starting routing-service", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	slog.Info("shutting down gracefully...")
	ccancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	slog.Info("server stopped")
}

func setupLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

func setupOTLP(cfg *config.Config) func() {
	if cfg.OTLP.Endpoint == "" {
		slog.Warn("OTLP endpoint not set, tracing disabled")
		return func() {}
	}
	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(cfg.OTLP.Endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		slog.Error("failed to create OTLP exporter", "error", err)
		return func() {}
	}
	res, _ := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("routing-service"),
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
