// Package events provides a simple fan-out broker for server-sent events.
// Handlers call Broadcast to push an event to all active SSE subscribers.
package events

import (
	"sync"
)

// Event is a single server-sent event. Type maps to the SSE "event:" field;
// Data is JSON-serialised into the "data:" field.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Broker fan-outs events to all currently subscribed channels.
// All methods are safe for concurrent use.
type Broker struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// New returns a ready-to-use Broker.
func New() *Broker {
	return &Broker{subs: make(map[chan Event]struct{})}
}

// Subscribe creates and registers a new subscriber channel. The caller must
// call Unsubscribe when it is no longer interested in events (typically via
// defer). The channel is buffered so that a slow subscriber does not block
// Broadcast for everyone else.
func (b *Broker) Subscribe() chan Event {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel from the broker and closes it, signalling
// the receiver that no more events will arrive.
func (b *Broker) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast delivers e to every active subscriber. Subscribers whose buffer
// is full are skipped — they will re-sync on their next HTTP fetch. This
// ensures Broadcast never blocks.
func (b *Broker) Broadcast(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // subscriber is slow; drop rather than block
		}
	}
}
