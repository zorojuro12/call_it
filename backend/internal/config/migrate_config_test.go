package config

import (
	"strings"
	"testing"
)

func TestLoadMigrateRequiresDSN(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		unset         []string
		want          MigrateConfig
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:          "unset POSTGRES_DSN fails fast",
			unset:         []string{"POSTGRES_DSN"},
			wantErr:       true,
			wantErrSubstr: "POSTGRES_DSN is required",
		},
		{
			name:          "empty POSTGRES_DSN fails fast",
			env:           map[string]string{"POSTGRES_DSN": ""},
			wantErr:       true,
			wantErrSubstr: "POSTGRES_DSN is required",
		},
		{
			name: "non-empty POSTGRES_DSN is accepted",
			env:  map[string]string{"POSTGRES_DSN": "postgres://callit:callit@localhost:5432/callit?sslmode=disable"},
			want: MigrateConfig{PostgresDSN: "postgres://callit:callit@localhost:5432/callit?sslmode=disable", LogLevel: "info"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				for _, u := range tt.unset {
					if u == key {
						return "", false
					}
				}
				v, ok := tt.env[key]
				return v, ok
			}

			got, err := LoadMigrate(lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadMigrate() error = nil, want error")
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("LoadMigrate() error = %v, want it to contain %q", err, tt.wantErrSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadMigrate() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("LoadMigrate() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadMigrateDoesNotRequireJWTSecret(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "POSTGRES_DSN" {
			return "postgres://callit:callit@localhost:5432/callit?sslmode=disable", true
		}
		return "", false
	}

	if _, err := LoadMigrate(lookup); err != nil {
		t.Fatalf("LoadMigrate() error = %v, want nil (must not require JWT_SECRET)", err)
	}
}
