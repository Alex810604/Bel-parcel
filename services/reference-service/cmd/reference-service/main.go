package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"bel-parcel/services/reference-service/internal/app"
	"bel-parcel/services/reference-service/internal/auth"
	"bel-parcel/services/reference-service/internal/config"
	"bel-parcel/services/reference-service/internal/infra/db"
	"bel-parcel/services/reference-service/internal/infra/kafka"
	"bel-parcel/services/reference-service/internal/infra/outbox"
	"bel-parcel/services/reference-service/internal/metrics"

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
	operator := "reference-service"
	_ = operator

	pool, err := db.NewPgxPool(cfg.DB.DSN)
	if err != nil {
		slog.Error("failed to connect DB", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	metrics.MustRegister()
	if err := outbox.EnsureSchema(pool); err != nil {
		slog.Error("failed to ensure outbox schema", "error", err)
		os.Exit(1)
	}
	producer, err := kafka.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.Timeout)
	if err != nil {
		slog.Error("failed to create Kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	svc := app.NewService(pool, producer, cfg.Kafka.EventsTopic)
	validator := auth.NewValidator(cfg.Auth.HS256Secret, cfg.Auth.Issuer, cfg.Auth.Audience)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go outbox.StartPublisher(ctx, pool, producer)

	mux := http.NewServeMux()
	h := app.NewHandlers(svc, validator)
	h.Routes(mux)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("DB unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      mux,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	slog.Info("starting reference-service", "port", cfg.Server.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
	}
}
