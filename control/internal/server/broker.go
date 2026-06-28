package server

import (
	"sync"

	"tethon/internal/protocol"
)

type broker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan protocol.Event]struct{}
}

func newBroker() *broker {
	return &broker{subscribers: make(map[string]map[chan protocol.Event]struct{})}
}

func (b *broker) subscribe(sessionID string) (<-chan protocol.Event, func()) {
	ch := make(chan protocol.Event, 16)

	b.mu.Lock()
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = make(map[chan protocol.Event]struct{})
	}
	b.subscribers[sessionID][ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subscribers[sessionID], ch)
		if len(b.subscribers[sessionID]) == 0 {
			delete(b.subscribers, sessionID)
		}
		b.mu.Unlock()
		close(ch)
	}
	return ch, unsubscribe
}

func (b *broker) publish(event protocol.Event) {
	b.mu.Lock()
	subs := make([]chan protocol.Event, 0, len(b.subscribers[event.SessionID]))
	for ch := range b.subscribers[event.SessionID] {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		ch <- event
	}
}
