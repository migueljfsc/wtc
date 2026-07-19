// Package metrics owns the Prometheus registry and every instrument wtc
// exports (P16). Package-level on purpose: wtc is a single binary with one
// serve process, so threading a metrics struct through every constructor buys
// nothing — call sites increment the exported instruments directly. Tests
// assert deltas (counters are process-global and shared across tests).
package metrics

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry is wtc's own (never the client_golang global default) so nothing
// outside this package can register collectors and the exposition surface is
// exactly what this file declares.
var registry = prometheus.NewRegistry()

var factory = promauto.With(registry)

var (
	// Ingested counts events stored as NEW rows, by source. Together with
	// Deduped it partitions every accepted delivery: rate(Ingested) is real
	// change flow, rate(Deduped) is replay/redelivery noise.
	Ingested = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_ingested_total",
		Help: "Events accepted and stored as new ledger rows, by source.",
	}, []string{"source"})

	// Deduped counts deliveries merged onto an existing row (dedup_key hit) —
	// poller sweeps, webhook redeliveries, status-lifecycle updates.
	Deduped = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_deduped_total",
		Help: "Deliveries merged onto an existing row via dedup_key, by source.",
	}, []string{"source"})

	// Suppressed counts events dropped inside a suppression window (flux and
	// argocd re-notify on every reconcile/resync — trap #1).
	Suppressed = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_suppressed_total",
		Help: "Events dropped inside a suppression window, by source.",
	}, []string{"source"})

	// Filtered counts events dropped at ingest by a source's allow/deny scope
	// (flux and argocd only — the push sources' analog of poller repo scope).
	// Distinct from Suppressed: these never belong in the ledger at all.
	Filtered = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_filtered_total",
		Help: "Events dropped at ingest by an allow/deny scope, by source.",
	}, []string{"source"})

	// MappingErrors counts mapping-webhook template failures (P14). The
	// delivery is rejected so the sender can retry; alert on any increase.
	MappingErrors = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_mapping_errors_total",
		Help: "Mapping-webhook template/normalization failures, by source.",
	}, []string{"source"})

	// NotifySent counts notification deliveries accepted by a sink (P21), by
	// subscription name and sink type.
	NotifySent = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_notify_sent_total",
		Help: "Notification deliveries accepted by the sink, by subscription and sink type.",
	}, []string{"notification", "sink"})

	// NotifyFailed counts failed delivery ATTEMPTS (each retry that fails
	// increments); a delivery that eventually succeeds still leaves its failed
	// attempts counted. Alert on sustained rate, not any single increment.
	NotifyFailed = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_notify_failed_total",
		Help: "Failed notification delivery attempts, by subscription and sink type.",
	}, []string{"notification", "sink"})

	// NotifyDropped counts deliveries abandoned entirely — queue_full (bounded
	// queue overflow at enqueue) or retries_exhausted. Any increase means a
	// notification the operator subscribed to was lost.
	NotifyDropped = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "wtc_notify_dropped_total",
		Help: "Notification deliveries dropped (queue_full | retries_exhausted), by subscription and sink type.",
	}, []string{"notification", "sink", "reason"})

	// PollLastSuccess is the unix time of the last successful poll per
	// (source, repo, resource). Lag is derived in PromQL:
	// time() - wtc_poll_last_success_timestamp_seconds.
	PollLastSuccess = factory.NewGaugeVec(prometheus.GaugeOpts{
		Name: "wtc_poll_last_success_timestamp_seconds",
		Help: "Unix time of the last successful poll, per source/repo/resource.",
	}, []string{"source", "repo", "resource"})

	// HTTPDuration observes request latency. The path label is the ROUTE
	// PATTERN (e.g. /api/v1/where/{ref}), never the raw URL — raw paths carry
	// shas and would explode cardinality.
	HTTPDuration = factory.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "wtc_http_request_duration_seconds",
		Help:    "HTTP request duration by route pattern, method, and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"path", "method", "status"})

	// SSEConnections tracks currently open /stream connections.
	SSEConnections = factory.NewGauge(prometheus.GaugeOpts{
		Name: "wtc_sse_connections",
		Help: "Currently open SSE /api/stream connections.",
	})
)

func init() {
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		dbSizeCollector{desc: prometheus.NewDesc(
			"wtc_db_size_bytes",
			"Database size in bytes (per-backend query, sampled at scrape time).",
			[]string{"backend"}, nil,
		)},
	)
}

// Handler serves the registry in the Prometheus exposition format.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// dbSize holds the store-provided size callback. A settable callback behind a
// permanently-registered collector (instead of registering per store) keeps
// registration idempotent — tests open many stores per process.
var dbSize struct {
	mu      sync.Mutex
	backend string
	size    func(context.Context) (int64, error)
}

// SetDBSize wires the wtc_db_size_bytes gauge to a store. The last caller
// wins; a nil size func detaches the gauge (it stops being exported).
func SetDBSize(backend string, size func(context.Context) (int64, error)) {
	dbSize.mu.Lock()
	defer dbSize.mu.Unlock()
	dbSize.backend = backend
	dbSize.size = size
}

// dbSizeCollector samples the database size on every scrape. On error (or
// before SetDBSize) it emits nothing — a missing series is honest, a zero is a
// lie.
type dbSizeCollector struct{ desc *prometheus.Desc }

func (c dbSizeCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c dbSizeCollector) Collect(ch chan<- prometheus.Metric) {
	dbSize.mu.Lock()
	backend, size := dbSize.backend, dbSize.size
	dbSize.mu.Unlock()
	if size == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := size(ctx)
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(n), backend)
}
