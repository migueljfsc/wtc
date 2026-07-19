package notify

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/model"
)

// queueSize bounds the delivery queue. Sized for bursts (a poller backfill
// sweep), not sustained sink outage — overflow increments the dropped counter
// instead of ever blocking ingest.
const queueSize = 256

// defaultBackoff is the wait between delivery attempts: 4 attempts total,
// then drop. Best-effort in-memory delivery; a durable outbox is a future
// enhancement.
var defaultBackoff = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second}

type delivery struct {
	sub          *compiledSub
	ev           model.Event
	transitioned bool
}

// Dispatcher fans stored-event notifications out to configured sinks. One
// bounded queue fed by Enqueue (called from the store's ingest funnel — must
// never block) and one worker goroutine draining it with per-delivery retry.
// A slow sink therefore delays later notifications, never ingest.
type Dispatcher struct {
	subs    *Compiled
	queue   chan delivery
	backoff []time.Duration // test override; defaults to defaultBackoff
	client  *http.Client
	log     *slog.Logger
}

// NewDispatcher builds a dispatcher over a compiled subscription set. Returns
// nil when no subscriptions are configured — callers skip wiring entirely.
func NewDispatcher(c *Compiled, log *slog.Logger) *Dispatcher {
	if c.Empty() {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		subs:    c,
		queue:   make(chan delivery, queueSize),
		backoff: defaultBackoff,
		client:  &http.Client{Timeout: 15 * time.Second},
		log:     log,
	}
}

// Enqueue matches ev against every subscription and queues one delivery per
// match. Non-blocking: a full queue drops the delivery and moves the counter.
// Safe for concurrent use; the caller is the single-writer ingest funnel via
// store.SetNotifyFunc.
func (d *Dispatcher) Enqueue(ev model.Event, transitioned bool) {
	for i := range d.subs.subs {
		sub := &d.subs.subs[i]
		if !sub.matches(ev) {
			continue
		}
		select {
		case d.queue <- delivery{sub: sub, ev: ev, transitioned: transitioned}:
		default:
			metrics.NotifyDropped.WithLabelValues(sub.name, sub.sink.Type, "queue_full").Inc()
			d.log.Warn("notify: queue full, delivery dropped",
				"notification", sub.name, "sink", sub.sink.Type, "event", ev.ID)
		}
	}
}

// Run drains the queue until ctx is cancelled. Queued-but-undelivered items
// are lost at shutdown — the documented best-effort contract.
func (d *Dispatcher) Run(ctx context.Context) {
	d.log.Info("notification dispatcher started", "subscriptions", len(d.subs.subs))
	for {
		select {
		case <-ctx.Done():
			return
		case dl := <-d.queue:
			d.deliver(ctx, dl)
		}
	}
}

// deliver attempts one queued delivery with bounded exponential backoff:
// len(backoff)+1 attempts, then drop with the counter moved. At-least-once:
// a timeout whose request actually landed can re-send on retry.
func (d *Dispatcher) deliver(ctx context.Context, dl delivery) {
	for attempt := 0; ; attempt++ {
		err := d.send(ctx, dl)
		if err == nil {
			metrics.NotifySent.WithLabelValues(dl.sub.name, dl.sub.sink.Type).Inc()
			return
		}
		if ctx.Err() != nil {
			return // shutting down — not a sink failure
		}
		metrics.NotifyFailed.WithLabelValues(dl.sub.name, dl.sub.sink.Type).Inc()
		if attempt >= len(d.backoff) {
			metrics.NotifyDropped.WithLabelValues(dl.sub.name, dl.sub.sink.Type, "retries_exhausted").Inc()
			d.log.Error("notify: delivery dropped after retries",
				"notification", dl.sub.name, "sink", dl.sub.sink.Type, "event", dl.ev.ID, "error", err)
			return
		}
		d.log.Warn("notify: delivery failed, will retry",
			"notification", dl.sub.name, "sink", dl.sub.sink.Type, "attempt", attempt+1, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.backoff[attempt]):
		}
	}
}

func (d *Dispatcher) send(ctx context.Context, dl delivery) error {
	switch dl.sub.sink.Type {
	case SinkSlack:
		return Slack(ctx, dl.sub.sink.URL, eventSlackText(dl.ev, dl.transitioned))
	case SinkWebhook:
		return d.sendWebhook(ctx, dl)
	default: // SinkGrafana — Compile admits exactly these three
		return d.sendGrafanaAnnotation(ctx, dl)
	}
}
