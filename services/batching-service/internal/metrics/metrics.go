package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	batchErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "batch_errors_total",
			Help: "Count of outbox publish errors in batching-service",
		},
		[]string{"topic"},
	)
	batchDLQSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "batch_dlq_size",
			Help: "Number of events in batching-service DLQ",
		},
	)
	batchOutboxQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "batch_outbox_queue_size",
			Help: "Number of pending/error events in batching-service outbox",
		},
	)
	BatchFlushDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "batch_flush_duration_seconds",
			Help:    "Time from reaching group size to publishing batches.formed",
			Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
	)
	BatchActiveGroups = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "batch_active_groups",
			Help: "Number of active warehouse+pvp groups in memory",
		},
	)
	BatchOrdersAdded = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "batch_orders_added_total",
			Help: "Count of orders added to groups, labeled by warehouse_id",
		},
		[]string{"warehouse_id"},
	)
)

func Init(mux *http.ServeMux) {
	prometheus.MustRegister(batchErrors, batchDLQSize, batchOutboxQueueSize, BatchFlushDuration, BatchActiveGroups, BatchOrdersAdded)
	mux.Handle("/metrics", promhttp.Handler())
}

func IncBatchError(topic string) {
	batchErrors.WithLabelValues(topic).Inc()
}

func StartDLQGauge(ctx context.Context, db *pgxpool.Pool) {
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var cnt int
				_ = db.QueryRow(context.Background(), "SELECT COUNT(*) FROM dead_letter_queue").Scan(&cnt)
				batchDLQSize.Set(float64(cnt))
			}
		}
	}()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var cnt int
				_ = db.QueryRow(context.Background(), `
					SELECT COUNT(*) FROM outbox_events 
					WHERE status IN ('pending','error')
				`).Scan(&cnt)
				batchOutboxQueueSize.Set(float64(cnt))
			}
		}
	}()
}
