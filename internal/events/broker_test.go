package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/marcpfuller/grimoire/internal/events"
)

func TestBroker_Subscribe(t *testing.T) {
	b := events.New()
	ch := b.Subscribe()
	assert.NotNil(t, ch)
	b.Unsubscribe(ch)
}

func TestBroker_Broadcast_sends_to_subscriber(t *testing.T) {
	b := events.New()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	e := events.Event{Type: "command_started", Data: map[string]any{"short_id": "abc"}}
	b.Broadcast(e)

	select {
	case got := <-ch:
		assert.Equal(t, e, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestBroker_Broadcast_no_subscribers(t *testing.T) {
	b := events.New()
	// Should not panic or block with no subscribers.
	b.Broadcast(events.Event{Type: "ping"})
}

func TestBroker_Broadcast_multiple_subscribers_each_receive(t *testing.T) {
	b := events.New()
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	defer b.Unsubscribe(ch1)
	defer b.Unsubscribe(ch2)

	b.Broadcast(events.Event{Type: "command_finished"})

	for _, ch := range []chan events.Event{ch1, ch2} {
		select {
		case got := <-ch:
			assert.Equal(t, "command_finished", got.Type)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestBroker_Unsubscribe_closes_channel(t *testing.T) {
	b := events.New()
	ch := b.Subscribe()
	b.Unsubscribe(ch)

	// After Unsubscribe the channel must be closed so range/select on it works.
	_, open := <-ch
	assert.False(t, open, "channel should be closed after Unsubscribe")
}

func TestBroker_Broadcast_drops_when_buffer_full(t *testing.T) {
	b := events.New()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	// Fill the buffer (capacity 16) + one extra; must not block.
	done := make(chan struct{})
	go func() {
		for i := range 20 {
			b.Broadcast(events.Event{Type: "ping", Data: i})
		}
		close(done)
	}()

	select {
	case <-done:
		// success — Broadcast returned without blocking
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Broadcast blocked when subscriber buffer was full")
	}
}

func TestBroker_concurrent_safe(t *testing.T) {
	b := events.New()
	var wg sync.WaitGroup

	// Concurrent subscribes, broadcasts, and unsubscribes.
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := b.Subscribe()
			b.Broadcast(events.Event{Type: "ping"})
			b.Unsubscribe(ch)
		}()
	}
	wg.Wait()
}

func TestBroker_unsubscribed_does_not_receive(t *testing.T) {
	b := events.New()
	ch := b.Subscribe()
	b.Unsubscribe(ch) // unsubscribe before broadcast

	b.Broadcast(events.Event{Type: "command_started"})

	// Drain the closed channel — should be empty.
	count := 0
	for range ch {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestBroker_Broadcast_does_not_deliver_to_unrelated_subscriber(t *testing.T) {
	b := events.New()
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	defer b.Unsubscribe(ch2)

	b.Unsubscribe(ch1) // ch1 removed before broadcast

	b.Broadcast(events.Event{Type: "command_started"})

	// ch2 should still receive it.
	select {
	case got := <-ch2:
		assert.Equal(t, "command_started", got.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("active subscriber did not receive event")
	}
}
