package main

import (
	"context"
	"encoding/json"
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

	"bel-parcel/services/order-service/internal/app"
	"bel-parcel/services/order-service/internal/config"
	"bel-parcel/services/order-service/internal/delivery/handler"
	"bel-parcel/services/order-service/internal/infra/db"
	"bel-parcel/services/order-service/internal/infra/kafka"
	"bel-parcel/services/order-service/internal/outbox"

	"github.com/google/uuid"
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

	dbPool, err := db.NewPgxPool(cfg.DB.DSN)
	if err != nil {
		slog.Error("failed to connect to DB", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	if err := outbox.EnsureSchema(dbPool); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}

	kafkaProducer, err := kafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	if err != nil {
		slog.Error("failed to create Kafka producer", "error", err)
		os.Exit(1)
	}
	defer kafkaProducer.Close()

	service := app.NewOrderService(dbPool, kafkaProducer, cfg.Kafka.Topic)

	consumer := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.GroupID, cfg.Kafka.ConsumerTopics, cfg.Kafka.Timeout)
	cctx, ccancel := context.WithCancel(context.Background())
	defer ccancel()
	go outbox.StartPublisher(cctx, dbPool, kafkaProducer)
	consumer.Start(cctx, func(topic string, key, value []byte) error {
		if err := service.HandleKafkaEvent(cctx, topic, key, value); err != nil {
			now := time.Now().UTC()
			envelope := map[string]interface{}{
				"event_id":       uuid.NewString(),
				"event_type":     "dlq",
				"occurred_at":    now,
				"correlation_id": string(key),
				"data": map[string]interface{}{
					"original_topic": topic,
					"error":          err.Error(),
					"payload":        string(value),
				},
			}
			payload, e := json.Marshal(envelope)
			if e == nil {
				tx, e := dbPool.Begin(cctx)
				if e == nil {
					evt := outbox.Event{
						ID:            uuid.NewString(),
						EventType:     "dlq",
						CorrelationID: string(key),
						Topic:         cfg.Kafka.DLQTopic,
						PartitionKey:  string(key),
						Payload:       payload,
						OccurredAt:    now,
					}
					if e = outbox.EnqueueTx(cctx, tx, evt); e == nil {
						_ = tx.Commit(cctx)
					} else {
						_ = tx.Rollback(cctx)
					}
				}
			}
			return err
		}
		return nil
	})

	handler := handler.NewHandler(service)

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: handler,
	}

	go func() {
		slog.Info("starting HTTP server", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	slog.Info("shutting down gracefully...")
	// Cancel consumer context to stop fetch loops and give them time to exit
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
		otlptracehttp.WithInsecure(), // или TLS в продакшене
	)
	if err != nil {
		slog.Error("failed to create OTLP exporter", "error", err)
		return func() {}
	}

	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("order-service"),
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
