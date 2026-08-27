package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadLedger(t *testing.T) {
	tests := []struct {
		name    string
		lookup  LookupFunc
		want    LedgerConfig
		wantErr string
	}{
		{
			name: "missing POSTGRES_DSN",
			lookup: func(key string) (string, bool) {
				return "", false
			},
			wantErr: "POSTGRES_DSN is required",
		},
		{
			name: "empty POSTGRES_DSN",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "",
				}
				v, ok := m[key]
				return v, ok
			},
			wantErr: "POSTGRES_DSN is required",
		},
		{
			name: "minimal config with defaults",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
				}
				v, ok := m[key]
				return v, ok
			},
			want: LedgerConfig{
				PostgresDSN:   "postgres://x",
				KafkaBrokers:  []string{"localhost:9092"},
				ConsumerGroup: "ledger-writer",
				LogLevel:      "info",
				Env:           "development",
			},
		},
		{
			name: "custom KAFKA_BROKERS",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN":  "postgres://x",
					"KAFKA_BROKERS": "a:9092,b:9092",
				}
				v, ok := m[key]
				return v, ok
			},
			want: LedgerConfig{
				PostgresDSN:   "postgres://x",
				KafkaBrokers:  []string{"a:9092", "b:9092"},
				ConsumerGroup: "ledger-writer",
				LogLevel:      "info",
				Env:           "development",
			},
		},
		{
			name: "empty KAFKA_BROKERS",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN":  "postgres://x",
					"KAFKA_BROKERS": "",
				}
				v, ok := m[key]
				return v, ok
			},
			wantErr: "KAFKA_BROKERS must not be empty",
		},
		{
			name: "KAFKA_BROKERS with empty element",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN":  "postgres://x",
					"KAFKA_BROKERS": "a:9092,",
				}
				v, ok := m[key]
				return v, ok
			},
			wantErr: "contains an empty element",
		},
		{
			name: "custom LEDGER_GROUP",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
					"LEDGER_GROUP": "alt",
				}
				v, ok := m[key]
				return v, ok
			},
			want: LedgerConfig{
				PostgresDSN:   "postgres://x",
				KafkaBrokers:  []string{"localhost:9092"},
				ConsumerGroup: "alt",
				LogLevel:      "info",
				Env:           "development",
			},
		},
		{
			name: "empty LEDGER_GROUP",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
					"LEDGER_GROUP": "",
				}
				v, ok := m[key]
				return v, ok
			},
			wantErr: "LEDGER_GROUP must not be empty",
		},
		{
			name: "custom LOG_LEVEL",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
					"LOG_LEVEL":    "debug",
				}
				v, ok := m[key]
				return v, ok
			},
			want: LedgerConfig{
				PostgresDSN:   "postgres://x",
				KafkaBrokers:  []string{"localhost:9092"},
				ConsumerGroup: "ledger-writer",
				LogLevel:      "debug",
				Env:           "development",
			},
		},
		{
			name: "invalid LOG_LEVEL",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
					"LOG_LEVEL":    "shout",
				}
				v, ok := m[key]
				return v, ok
			},
			wantErr: "not one of debug|info|warn|error",
		},
		{
			name: "custom ENV",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
					"ENV":          "production",
				}
				v, ok := m[key]
				return v, ok
			},
			want: LedgerConfig{
				PostgresDSN:   "postgres://x",
				KafkaBrokers:  []string{"localhost:9092"},
				ConsumerGroup: "ledger-writer",
				LogLevel:      "info",
				Env:           "production",
			},
		},
		{
			name: "invalid ENV",
			lookup: func(key string) (string, bool) {
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
					"ENV":          "staging",
				}
				v, ok := m[key]
				return v, ok
			},
			wantErr: "not one of development|production|test",
		},
		{
			name: "no JWT_SECRET required",
			lookup: func(key string) (string, bool) {
				// Deliberately omit JWT_SECRET to verify it's not required
				m := map[string]string{
					"POSTGRES_DSN": "postgres://x",
				}
				v, ok := m[key]
				return v, ok
			},
			want: LedgerConfig{
				PostgresDSN:   "postgres://x",
				KafkaBrokers:  []string{"localhost:9092"},
				ConsumerGroup: "ledger-writer",
				LogLevel:      "info",
				Env:           "development",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadLedger(tt.lookup)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("LoadLedger() got nil error, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("LoadLedger() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("LoadLedger() error = %v, want nil", err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoadLedger() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
