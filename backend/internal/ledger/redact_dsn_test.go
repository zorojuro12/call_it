package ledger

import (
	"strings"
	"testing"
)

// TestRedactDSN pins the helper TestReconcileAfterLoad uses to report a
// failed connection: deploy/k8s/chaos.sh passes it the real Postgres
// password, so a failure message must name the host, never the password.
func TestRedactDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "password replaced, host and database kept",
			dsn:  "postgres://callit:s3cret-canary@localhost:15432/callit?sslmode=disable",
			want: "postgres://callit:xxxxx@localhost:15432/callit?sslmode=disable",
		},
		{
			name: "no password left as is",
			dsn:  "postgres://callit@localhost:5432/callit",
			want: "postgres://callit@localhost:5432/callit",
		},
		{
			name: "unparseable DSN not echoed",
			dsn:  "postgres://callit:s3cret-canary@[bad",
			want: "<unparseable DSN>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactDSN(tt.dsn)

			if got != tt.want {
				t.Errorf("redactDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
			if strings.Contains(got, "s3cret-canary") {
				t.Errorf("redactDSN(%q) leaks the password: %q", tt.dsn, got)
			}
		})
	}
}
