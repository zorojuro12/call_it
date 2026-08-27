package migrate

import (
	"context"
	"strings"
	"testing"
)

// TestUpDownRejectInvalidDSN covers newMigrator's error branch — the one
// path here reachable without live-fault-injecting a real database
// connection. Up and Down share newMigrator, so one case per function
// exercises both call sites.
func TestUpDownRejectInvalidDSN(t *testing.T) {
	const badDSN = "not-a-valid-dsn"
	ctx := context.Background()

	if err := Up(ctx, badDSN); err == nil || !strings.Contains(err.Error(), "building migrator") {
		t.Errorf("Up(%q) error = %v, want an error naming \"building migrator\"", badDSN, err)
	}
	if err := Down(ctx, badDSN); err == nil || !strings.Contains(err.Error(), "building migrator") {
		t.Errorf("Down(%q) error = %v, want an error naming \"building migrator\"", badDSN, err)
	}
}
