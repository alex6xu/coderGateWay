package sessionrun

import (
	"sync"
)

// Hub fans out persisted events to live SSE subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan Event]struct{})}
}

func (h *Hub) Subscribe(runID string) chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[runID] == nil {
		h.subs[runID] = make(map[chan Event]struct{})
	}
	h.subs[runID][ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(runID string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.subs[runID]; m != nil {
		delete(m, ch)
		if len(m) == 0 {
			delete(h.subs, runID)
		}
	}
	// Do not close(ch): a concurrent Publish must not send on a closed channel.
}

func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[ev.RunID] {
		select {
		case ch <- ev:
		default:
			// Drop if subscriber is too slow; they can replay from DB.
		}
	}
}
