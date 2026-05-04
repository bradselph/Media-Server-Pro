package analytics

import (
	"sync"

	"media-server-pro/pkg/models"
)

// EventSubscription is a fan-out registration for live event streaming.
// Each call to Subscribe returns a channel and an unsubscribe function;
// the caller MUST call the unsubscribe function (typically via defer)
// when done, otherwise the channel registry leaks.
type EventSubscription struct {
	Events <-chan models.AnalyticsEvent
	Cancel func()
}

// subscriberRegistry holds active live-event subscribers. Tracked by a
// sync.Map keyed on a sentinel pointer per subscription so concurrent
// adds and removes don't fight for a single mutex on the hot send path.
type subscriberRegistry struct {
	mu   sync.RWMutex
	subs map[*subscriberSlot]struct{}
}

type subscriberSlot struct {
	ch chan models.AnalyticsEvent
}

func newSubscriberRegistry() *subscriberRegistry {
	return &subscriberRegistry{subs: make(map[*subscriberSlot]struct{})}
}

// Subscribe registers a new live-event subscriber. The returned channel is
// buffered with the given size — a slow consumer that fills the buffer
// will start dropping events rather than blocking the broadcaster. 64 is
// a reasonable default for a dashboard "live tail" use case.
//
// Always defer cancel() — it removes the subscription from the registry
// and closes the channel. Without it the registry leaks one slot per
// abandoned subscriber.
func (m *Module) Subscribe(buffer int) EventSubscription {
	if buffer <= 0 {
		buffer = 64
	}
	if m.subs == nil {
		// Defensive: a Module that was never Started shouldn't crash here.
		return EventSubscription{Events: nil, Cancel: func() {}}
	}
	slot := &subscriberSlot{ch: make(chan models.AnalyticsEvent, buffer)}
	m.subs.mu.Lock()
	m.subs.subs[slot] = struct{}{}
	m.subs.mu.Unlock()
	cancel := func() {
		m.subs.mu.Lock()
		if _, ok := m.subs.subs[slot]; ok {
			delete(m.subs.subs, slot)
			close(slot.ch)
		}
		m.subs.mu.Unlock()
	}
	return EventSubscription{Events: slot.ch, Cancel: cancel}
}

// broadcastEvent fans the given event out to every live subscriber.
// Non-blocking: a slow subscriber's buffer overflow drops events rather
// than backpressuring the analytics hot path.
func (m *Module) broadcastEvent(ev models.AnalyticsEvent) {
	if m.subs == nil {
		return
	}
	m.subs.mu.RLock()
	defer m.subs.mu.RUnlock()
	for slot := range m.subs.subs {
		select {
		case slot.ch <- ev:
		default:
			// Subscriber is too slow — drop. The dashboard will still see
			// fresher events as soon as it catches up.
		}
	}
}

// closeAllSubscribers is called from Stop to shut down every live
// subscriber cleanly so SSE handlers see channel close and exit.
func (m *Module) closeAllSubscribers() {
	if m.subs == nil {
		return
	}
	m.subs.mu.Lock()
	defer m.subs.mu.Unlock()
	for slot := range m.subs.subs {
		delete(m.subs.subs, slot)
		close(slot.ch)
	}
}
