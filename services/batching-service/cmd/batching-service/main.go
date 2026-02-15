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

	"bel-parcel/services/batching-service/internal/batching"
	"bel-parcel/services/batching-service/internal/config"
	"bel-parcel/services/batching-service/internal/infra/db"
	"bel-parcel/services/batching-service/internal/infra/kafka"
	"bel-parcel/services/batching-service/internal/metrics"
	"bel-parcel/services/batching-service/internal/outbox"
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

	batchDB, err := db.NewPgxPool(cfg.DB.DSN)
	if err != nil {
		slog.Error("failed to connect to Batch DB", "error", err)
		os.Exit(1)
	}
	defer batchDB.Close()

	if err := batching.EnsureSchema(batchDB); err != nil {
		slog.Error("failed to ensure batching schema", "error", err)
		os.Exit(1)
	}
	if err := outbox.EnsureSchema(batchDB); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}

	producer, err := kafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	if err != nil {
		slog.Error("failed to create Kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	consumer := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, cfg.Kafka.ConsumeTopics, cfg.Kafka.Timeout)
	defer consumer.Close()

	svc := batching.NewService(batchDB, producer, cfg.Kafka.ProduceTopic, cfg.Batching.MaxSize, cfg.Batching.FlushInterval).WithDLQ(cfg.Kafka.DLQTopic)

	cctx, ccancel := context.WithCancel(context.Background())
	defer ccancel()

	go outbox.StartPublisher(cctx, batchDB, producer)

	consumer.Start(cctx, func(topic string, key, value []byte) error {
		if err := svc.HandleEvent(cctx, topic, key, value); err != nil {
			svc.PublishDLQ(cctx, topic, key, value, err)
			return err
		}
		return nil
	})

	go func() {
		t := time.NewTicker(cfg.Batching.FlushInterval)
		defer t.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
				svc.FlushExpired(context.Background())
			}
		}
	}()

	mux := http.NewServeMux()
	metrics.Init(mux)
	metrics.StartDLQGauge(cctx, batchDB)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := batchDB.Ping(r.Context()); err != nil {
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
		slog.Info("starting batching-service", "port", cfg.Server.Port)
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
		semconv.ServiceName("batching-service"),
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
