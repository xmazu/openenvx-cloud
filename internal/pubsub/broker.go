package pubsub

import (
	"sync"
)

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string][]chan string
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string][]chan string),
	}
}

func (b *Broker) Subscribe(jobID string) chan string {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan string, 100)
	b.subscribers[jobID] = append(b.subscribers[jobID], ch)
	return ch
}

func (b *Broker) Unsubscribe(jobID string, ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[jobID]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[jobID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}

	if len(b.subscribers[jobID]) == 0 {
		delete(b.subscribers, jobID)
	}
}

func (b *Broker) Publish(jobID string, line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if subs, ok := b.subscribers[jobID]; ok {
		for _, ch := range subs {
			select {
			case ch <- line:
			default:
			}
		}
	}
}
