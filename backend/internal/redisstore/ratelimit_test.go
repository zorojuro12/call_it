package redisstore

import (
	"context"
	"testing"
	"time"
)

func TestAllow_UnderLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id := testID(t, "id")

	seen := map[string]bool{}

	d1, err := store.Allow(ctx, "test", id, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow() 1st = %v, want nil", err)
	}
	if !d1.Allowed || d1.Remaining != 2 || d1.RetryAfter != 0 || d1.Member == "" {
		t.Errorf("Allow() 1st = %+v, want Allowed=true Remaining=2 RetryAfter=0 non-empty Member", d1)
	}
	seen[d1.Member] = true

	d2, err := store.Allow(ctx, "test", id, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow() 2nd = %v, want nil", err)
	}
	if !d2.Allowed || d2.Remaining != 1 {
		t.Errorf("Allow() 2nd = %+v, want Allowed=true Remaining=1", d2)
	}
	if seen[d2.Member] {
		t.Errorf("Allow() 2nd Member %q duplicates an earlier call's Member", d2.Member)
	}
	seen[d2.Member] = true

	d3, err := store.Allow(ctx, "test", id, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow() 3rd = %v, want nil", err)
	}
	if !d3.Allowed || d3.Remaining != 0 {
		t.Errorf("Allow() 3rd = %+v, want Allowed=true Remaining=0", d3)
	}
	if seen[d3.Member] {
		t.Errorf("Allow() 3rd Member %q duplicates an earlier call's Member", d3.Member)
	}

	if d3.ResetAt.Before(time.Now()) || d3.ResetAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("Allow() 3rd ResetAt = %v, want within 1 minute of now and after now", d3.ResetAt)
	}
}
