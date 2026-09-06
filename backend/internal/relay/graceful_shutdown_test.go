package relay

import (
	"context"
	"testing"
	"time"
)

func TestRunReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, group := testStreamAndGroup(t)

	r := New(testClient, stream, group, "consumer-1", &fakeProducer{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil (context cancellation is a clean shutdown)", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Run() did not return within 1s of the context being cancelled")
	}
}
