package relay

import (
	"context"
	"sync"

	"github.com/zorojuro12/call_it/backend/internal/events"
)

// fakeProducer records every Produce call it receives, optionally
// failing on demand, so relay tests can assert batching, ack timing, and
// failure handling without a real broker.
type fakeProducer struct {
	mu    sync.Mutex
	calls [][]events.Event
	err   error
}

func (f *fakeProducer) Produce(ctx context.Context, evs []events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, evs)
	return nil
}

func (f *fakeProducer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProducer) lastBatch() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}
