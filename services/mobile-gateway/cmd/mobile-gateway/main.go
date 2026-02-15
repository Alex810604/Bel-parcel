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

	"bel-parcel/services/mobile-gateway/internal/auth"
	"bel-parcel/services/mobile-gateway/internal/config"
	httpserver "bel-parcel/services/mobile-gateway/internal/http"
	"bel-parcel/services/mobile-gateway/internal/infra/db"
	"bel-parcel/services/mobile-gateway/internal/infra/kafka"
	"bel-parcel/services/mobile-gateway/internal/metrics"
	"bel-parcel/services/mobile-gateway/internal/outbox"

	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	if cfg.Auth.HS256Secret == "" {
		slog.Error("empty AUTH_HS256SECRET, refusing to start")
		os.Exit(1)
	}
	if cfg.Auth.Issuer == "" || cfg.Auth.Audience == "" {
		slog.Error("empty AUTH_ISSUER or AUTH_AUDIENCE, refusing to start")
		os.Exit(1)
	}
	setupLogger()
	shutdown := setupOTLP(cfg)
	defer shutdown()
	pool, err := db.NewPgxPool(cfg.DB.DSN)
	if err != nil {
		slog.Error("failed to connect DB", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := outbox.EnsureSchema(pool); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}
	// Ensure HTTP events log schema
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS http_events_log (
				id UUID PRIMARY KEY,
				event_type TEXT NOT NULL,
				event_id TEXT NOT NULL,
				payload JSONB NOT NULL,
				received_at TIMESTAMPTZ NOT NULL,
				UNIQUE(event_type, event_id)
			)
		`); err != nil {
			slog.Error("failed to ensure http_events_log schema", "error", err)
			os.Exit(1)
		}
	}
	producer, err := kafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	if err != nil {
		slog.Error("failed to create Kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()
	validator := auth.NewValidator(cfg.Auth.HS256Secret, cfg.Auth.Issuer, cfg.Auth.Audience)
	topics := map[string]string{
		"batch_picked_up":               cfg.Kafka.TopicPickedUp,
		"events.batch_picked_up":        cfg.Kafka.TopicPickedUp,
		"batch_delivered_to_pvp":        cfg.Kafka.TopicDeliveredToPVP,
		"events.batch_delivered_to_pvp": cfg.Kafka.TopicDeliveredToPVP,
		"batch_received_by_pvp":         cfg.Kafka.TopicReceivedByPVP,
		"events.batch_received_by_pvp":  cfg.Kafka.TopicReceivedByPVP,
		"carrier.location":              cfg.Kafka.TopicCarrierLocation,
		"events.carrier_location":       cfg.Kafka.TopicCarrierLocation,
	}
	s := httpserver.NewServer(pool, validator, topics)
	mux := s.Routes()
	metrics.Register()
	mux.Handle("GET /metrics", promhttp.Handler())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outbox.StartPublisher(ctx, pool, producer)
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		slog.Info("starting mobile-gateway", "port", cfg.Server.Port)
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
		semconv.ServiceName("mobile-gateway"),
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
