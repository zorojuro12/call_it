package relay

import (
	"context"
	"testing"
)

func TestEnsureGroupIsIdempotent(t *testing.T) {
	ctx := context.Background()
	stream, group := testStreamAndGroup(t)

	r := New(testClient, stream, group, "consumer-1", nil)

	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() first call = %v, want nil", err)
	}
	if err := r.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup() second call = %v, want nil (must swallow BUSYGROUP)", err)
	}

	groups, err := testClient.XInfoGroups(ctx, stream).Result()
	if err != nil {
		t.Fatalf("XINFO GROUPS: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("XINFO GROUPS returned %d groups, want 1", len(groups))
	}
	if groups[0].Name != group {
		t.Errorf("group name = %q, want %q", groups[0].Name, group)
	}
}
