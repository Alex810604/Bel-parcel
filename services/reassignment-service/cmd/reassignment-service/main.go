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

	"bel-parcel/services/reassignment-service/internal/config"
	"bel-parcel/services/reassignment-service/internal/infra/db"
	ikafka "bel-parcel/services/reassignment-service/internal/infra/kafka"
	"bel-parcel/services/reassignment-service/internal/metrics"
	"bel-parcel/services/reassignment-service/internal/outbox"
	rsvc "bel-parcel/services/reassignment-service/internal/reassignment"
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
	shutdownTracer := setupOTLP(cfg)
	defer shutdownTracer()

	reassignDB, err := db.NewPgxPool(cfg.DB.ReassignmentDSN)
	if err != nil {
		slog.Error("failed to connect to Reassignment DB", "error", err)
		os.Exit(1)
	}
	defer reassignDB.Close()
	if err := outbox.EnsureSchema(reassignDB); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := reassignDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS processed_events (
				event_id TEXT PRIMARY KEY,
				occurred_at TIMESTAMPTZ NOT NULL
			)
		`); err != nil {
			slog.Error("failed to ensure processed_events schema", "error", err)
			os.Exit(1)
		}
		if _, err := reassignDB.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS pending_confirmations (
				trip_id TEXT PRIMARY KEY,
				batch_id TEXT NOT NULL,
				carrier_id TEXT NOT NULL,
				assigned_at TIMESTAMPTZ NOT NULL,
				timeout_at TIMESTAMPTZ NOT NULL,
				status TEXT NOT NULL DEFAULT 'pending'
			)
		`); err != nil {
			slog.Error("failed to ensure pending_confirmations schema", "error", err)
			os.Exit(1)
		}
	}

	producer := ikafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	defer producer.Close()

	svc := rsvc.NewService(reassignDB, cfg.Kafka.CommandTopic, cfg.Worker.ConfirmationTimeout)
	consumer := ikafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, cfg.Kafka.ConsumeTopics, cfg.Kafka.Timeout)
	defer consumer.Close()
	w := rsvc.NewWorker(reassignDB, cfg.Worker.Interval, cfg.Kafka.CommandTopic)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go outbox.StartPublisher(ctx, reassignDB, producer)
	go consumer.Start(ctx, func(topic string, key, value []byte) error {
		return svc.HandleEvent(ctx, topic, key, value)
	})
	go w.Start(ctx)

	// Manual commands are processed by routing-service; reassignment-service only publishes reassign commands.

	// Health check server
	mux := http.NewServeMux()
	metrics.Init(mux)
	metrics.StartGauges(ctx, reassignDB)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := reassignDB.Ping(r.Context()); err != nil {
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
		slog.Info("starting health check server", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	slog.Info("shutting down gracefully...")

	// Stop worker
	cancel()

	// Stop server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "error", err)
	}

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

	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("reassignment-service"),
		),
	)

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
