package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadRelay(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		unset         []string
		want          RelayConfig
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "all defaults when nothing set",
			want: RelayConfig{
				RedisAddr:    "localhost:6379",
				RedisDB:      0,
				KafkaBrokers: []string{"localhost:9092"},
				LogLevel:     "info",
				Env:          "development",
			},
		},
		{
			name: "KAFKA_BROKERS splits on commas into multiple entries",
			env:  map[string]string{"KAFKA_BROKERS": "a:9092,b:9092"},
			want: RelayConfig{
				RedisAddr:    "localhost:6379",
				RedisDB:      0,
				KafkaBrokers: []string{"a:9092", "b:9092"},
				LogLevel:     "info",
				Env:          "development",
			},
		},
		{
			name:          "empty KAFKA_BROKERS fails fast",
			env:           map[string]string{"KAFKA_BROKERS": ""},
			wantErr:       true,
			wantErrSubstr: "KAFKA_BROKERS must not be empty",
		},
		{
			name:          "an empty broker element is named",
			env:           map[string]string{"KAFKA_BROKERS": "a:9092,,b:9092"},
			wantErr:       true,
			wantErrSubstr: "KAFKA_BROKERS",
		},
		{
			name:          "invalid REDIS_DB rejects exactly as Load does",
			env:           map[string]string{"REDIS_DB": "16"},
			wantErr:       true,
			wantErrSubstr: "REDIS_DB",
		},
		{
			name:          "invalid LOG_LEVEL rejects exactly as Load does",
			env:           map[string]string{"LOG_LEVEL": "verbose"},
			wantErr:       true,
			wantErrSubstr: "LOG_LEVEL",
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

			got, err := LoadRelay(lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadRelay() error = nil, want error")
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("LoadRelay() error = %v, want it to contain %q", err, tt.wantErrSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadRelay() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoadRelay() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadRelayDoesNotRequireJWTSecret(t *testing.T) {
	lookup := func(key string) (string, bool) { return "", false }

	if _, err := LoadRelay(lookup); err != nil {
		t.Fatalf("LoadRelay() error = %v, want nil (must not require JWT_SECRET)", err)
	}
}
