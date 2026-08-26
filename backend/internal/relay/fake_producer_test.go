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

// flatEvents concatenates every call's batch in call order, so a test
// can assert cross-batch ordering.
func (f *fakeProducer) flatEvents() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []events.Event
	for _, batch := range f.calls {
		all = append(all, batch...)
	}
	return all
}

// producerFunc adapts a plain function to Producer, letting a test hook
// side effects (like cancelling a context) onto a successful produce.
type producerFunc func(ctx context.Context, evs []events.Event) error

func (f producerFunc) Produce(ctx context.Context, evs []events.Event) error {
	return f(ctx, evs)
}
