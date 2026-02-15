package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"bel-parcel/services/tracking-service/internal/config"
	"bel-parcel/services/tracking-service/internal/infra/db"
	"bel-parcel/services/tracking-service/internal/infra/kafka"
	"bel-parcel/services/tracking-service/internal/metrics"
	"bel-parcel/services/tracking-service/internal/outbox"
	tservice "bel-parcel/services/tracking-service/internal/tracking"
	whub "bel-parcel/services/tracking-service/internal/websocket"

	"github.com/jackc/pgx/v5/pgxpool"
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

	var trackDB *pgxpool.Pool
	trackDB, err = db.NewPgxPool(cfg.DB.DSN)
	if err != nil {
		slog.Error("failed to connect to DB", "error", err)
		os.Exit(1)
	}
	defer trackDB.Close()

	// Ensure schema
	schemaPath := filepath.Join("..", "..", "internal", "infra", "db", "schema.sql")
	content, rerr := os.ReadFile(schemaPath)
	if rerr == nil {
		if _, err := trackDB.Exec(context.Background(), string(content)); err != nil {
			slog.Error("failed to ensure schema", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Error("failed to read schema.sql", "error", rerr)
		os.Exit(1)
	}
	if err := outbox.EnsureSchema(trackDB); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}

	consumer := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, cfg.Kafka.ConsumeTopics, cfg.Kafka.Timeout)
	cctx, ccancel := context.WithCancel(context.Background())
	defer ccancel()

	// WebSocket Hub
	hub := whub.NewHub(trackDB)
	go hub.Run(cctx)
	go outbox.StartPublisher(cctx, trackDB, hub)

	svc := tservice.NewService(trackDB, cfg.Tracking.DeviationThresholdMeters, cfg.Tracking.LateThresholdMinutes)
	consumer.Start(cctx, func(topic string, key, value []byte) error {
		return svc.HandleEvent(cctx, topic, key, value)
	})

	mux := http.NewServeMux()
	metrics.Init(mux)
	metrics.StartGauges(cctx, trackDB)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := trackDB.Ping(r.Context()); err != nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /ws/alerts", func(w http.ResponseWriter, r *http.Request) {
		if hub != nil {
			hub.ServeWS(w, r)
		} else {
			http.Error(w, "Hub not ready", http.StatusServiceUnavailable)
		}
	})
	// Legacy debug endpoints removed

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: mux,
	}
	go func() {
		slog.Info("starting tracking-service", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
	slog.Info("shutting down gracefully...")
	ccancel()
	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	consumer.Close()
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
			semconv.ServiceName("tracking-service"),
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
