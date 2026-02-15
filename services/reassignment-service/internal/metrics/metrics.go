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
	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reassignment_errors_total",
			Help: "Count of publish errors per topic",
		},
		[]string{"topic"},
	)
	OutboxQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "reassignment_outbox_queue_size",
			Help: "Number of pending/error events in outbox",
		},
	)
	DLQSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "reassignment_dlq_size",
			Help: "Number of events in dead letter queue",
		},
	)
	ReassignmentsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "reassignments_total",
			Help: "Total number of reassign commands published",
		},
	)
	ConfirmationTimeoutsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "confirmation_timeouts_total",
			Help: "Total number of confirmation timeouts detected",
		},
	)
)

func Init(mux *http.ServeMux) {
	prometheus.MustRegister(ErrorsTotal, OutboxQueueSize, DLQSize, ReassignmentsTotal, ConfirmationTimeoutsTotal)
	mux.Handle("/metrics", promhttp.Handler())
}

func StartGauges(ctx context.Context, db *pgxpool.Pool) {
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
					SELECT COUNT(*) FROM outbox_events WHERE status IN ('pending','error')
				`).Scan(&cnt)
				OutboxQueueSize.Set(float64(cnt))
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
					SELECT COUNT(*) FROM dead_letter_queue
				`).Scan(&cnt)
				DLQSize.Set(float64(cnt))
			}
		}
	}()
}
