package store

import (
	"sync"

	"github.com/migueljfsc/wtc/internal/model"
)

// broadcaster fans newly-stored events out to live subscribers (the SSE
// stream). Sends are non-blocking: a subscriber that can't keep up drops
// events rather than stalling the writer — the live view is best-effort and
// the client can always refetch.
type broadcaster struct {
	mu   sync.Mutex
	subs map[chan model.Event]struct{}
}

const subBuffer = 64

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[chan model.Event]struct{})}
}

func (b *broadcaster) subscribe() chan model.Event {
	ch := make(chan model.Event, subBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broadcaster) unsubscribe(ch chan model.Event) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *broadcaster) publish(ev model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default: // slow subscriber — drop
		}
	}
}

// Subscribe returns a channel that receives every newly-inserted event until
// Unsubscribe is called. Intended for the SSE handler.
func (s *Store) Subscribe() chan model.Event { return s.broadcast.subscribe() }

// Unsubscribe removes and closes a Subscribe channel.
func (s *Store) Unsubscribe(ch chan model.Event) { s.broadcast.unsubscribe(ch) }
