package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const validJWTSecret = "01234567890123456789012345678901" // 34 bytes, comfortably >= 32

var defaultOrigins = []string{"http://localhost:3000"}

func TestLoad(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		unset         []string // keys to omit entirely from the merged environment
		want          Config
		wantErr       bool
		wantErrSubstr string // when set, the error message must contain this
	}{
		{
			name: "all defaults when nothing set",
			env:  map[string]string{},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: defaultOrigins},
		},
		{
			name: "explicit values override defaults",
			env: map[string]string{
				"PORT":                 "9090",
				"ENV":                  "production",
				"LOG_LEVEL":            "debug",
				"CORS_ALLOWED_ORIGINS": "https://callit.example",
			},
			want: Config{Port: 9090, Env: "production", LogLevel: "debug", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: []string{"https://callit.example"}},
		},
		{
			name: "explicit redis addr overrides default",
			env:  map[string]string{"REDIS_ADDR": "redis:6379"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "redis:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: defaultOrigins},
		},
		{
			name:    "empty redis addr fails fast",
			env:     map[string]string{"REDIS_ADDR": ""},
			wantErr: true,
		},
		{
			name: "redis db at top of valid range",
			env:  map[string]string{"REDIS_DB": "15"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 15, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: defaultOrigins},
		},
		{
			name:    "redis db above valid range fails fast",
			env:     map[string]string{"REDIS_DB": "16"},
			wantErr: true,
		},
		{
			name:    "redis db below valid range fails fast",
			env:     map[string]string{"REDIS_DB": "-1"},
			wantErr: true,
		},
		{
			name:    "non-numeric redis db fails fast",
			env:     map[string]string{"REDIS_DB": "notanumber"},
			wantErr: true,
		},
		{
			name:    "non-numeric port fails fast",
			env:     map[string]string{"PORT": "not-a-number"},
			wantErr: true,
		},
		{
			name:    "port out of valid range fails fast",
			env:     map[string]string{"PORT": "70000"},
			wantErr: true,
		},
		{
			name:    "port zero fails fast",
			env:     map[string]string{"PORT": "0"},
			wantErr: true,
		},
		{
			name:    "unrecognized log level fails fast",
			env:     map[string]string{"LOG_LEVEL": "verbose"},
			wantErr: true,
		},
		{
			name:    "unrecognized env fails fast",
			env:     map[string]string{"ENV": "staging-typo"},
			wantErr: true,
		},
		{
			name:          "missing JWT secret fails fast",
			unset:         []string{"JWT_SECRET"},
			wantErr:       true,
			wantErrSubstr: "JWT_SECRET",
		},
		{
			name:          "empty JWT secret fails fast",
			env:           map[string]string{"JWT_SECRET": ""},
			wantErr:       true,
			wantErrSubstr: "JWT_SECRET",
		},
		{
			name:          "JWT secret under 32 bytes fails fast",
			env:           map[string]string{"JWT_SECRET": strings.Repeat("a", 31)},
			wantErr:       true,
			wantErrSubstr: "32",
		},
		{
			name: "JWT secret at exactly 32 bytes is accepted",
			env:  map[string]string{"JWT_SECRET": strings.Repeat("b", 32)},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: strings.Repeat("b", 32), JWTTTL: 2 * time.Hour, AllowedOrigins: defaultOrigins},
		},
		{
			name: "no JWT TTL defaults to two hours",
			env:  map[string]string{},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: defaultOrigins},
		},
		{
			name: "explicit JWT TTL overrides default",
			env:  map[string]string{"JWT_TTL": "45m"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 45 * time.Minute, AllowedOrigins: defaultOrigins},
		},
		{
			name:          "JWT TTL below one minute fails fast",
			env:           map[string]string{"JWT_TTL": "30s"},
			wantErr:       true,
			wantErrSubstr: "1m-24h",
		},
		{
			name:          "JWT TTL above twenty-four hours fails fast",
			env:           map[string]string{"JWT_TTL": "25h"},
			wantErr:       true,
			wantErrSubstr: "1m-24h",
		},
		{
			name:          "unparseable JWT TTL fails fast",
			env:           map[string]string{"JWT_TTL": "notaduration"},
			wantErr:       true,
			wantErrSubstr: "JWT_TTL",
		},
		{
			name: "allowed origins default to localhost:3000 outside production",
			env:  map[string]string{},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: defaultOrigins},
		},
		{
			name: "allowed origins parse a comma-separated list",
			env:  map[string]string{"CORS_ALLOWED_ORIGINS": "http://a.test,http://b.test"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: []string{"http://a.test", "http://b.test"}},
		},
		{
			name: "allowed origins trim whitespace around each entry",
			env:  map[string]string{"CORS_ALLOWED_ORIGINS": "http://a.test , http://b.test"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour, AllowedOrigins: []string{"http://a.test", "http://b.test"}},
		},
		{
			name:          "production requires allowed origins",
			env:           map[string]string{"ENV": "production"},
			wantErr:       true,
			wantErrSubstr: "CORS_ALLOWED_ORIGINS",
		},
		{
			name:          "production rejects an empty allowed-origins value",
			env:           map[string]string{"ENV": "production", "CORS_ALLOWED_ORIGINS": ""},
			wantErr:       true,
			wantErrSubstr: "CORS_ALLOWED_ORIGINS",
		},
		{
			name:    "a bare wildcard is rejected in every env",
			env:     map[string]string{"CORS_ALLOWED_ORIGINS": "*"},
			wantErr: true,
		},
		{
			name:    "a wildcard mixed into a list is still rejected",
			env:     map[string]string{"CORS_ALLOWED_ORIGINS": "http://a.test,*"},
			wantErr: true,
		},
		{
			name:    "an entry that is not an absolute URL is rejected",
			env:     map[string]string{"CORS_ALLOWED_ORIGINS": "not-a-url"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := map[string]string{"JWT_SECRET": validJWTSecret}
			for _, k := range tt.unset {
				delete(base, k)
			}

			lookup := func(key string) (string, bool) {
				for _, u := range tt.unset {
					if u == key {
						return "", false
					}
				}
				if v, ok := tt.env[key]; ok {
					return v, true
				}
				v, ok := base[key]
				return v, ok
			}

			got, err := Load(lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() with env %v: got nil error, want an error", tt.env)
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("Load() with env %v: error %q does not contain %q", tt.env, err.Error(), tt.wantErrSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load() with env %v: unexpected error: %v", tt.env, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Load() with env %v = %+v, want %+v", tt.env, got, tt.want)
			}
		})
	}
}

// lookupFrom builds a LookupFunc over env, with base's JWT_SECRET as a
// fallback and unset keys forced absent regardless of either map — the
// same pattern TestLoad uses inline, factored out for the metrics tests.
func lookupFrom(env map[string]string, unset []string) LookupFunc {
	base := map[string]string{"JWT_SECRET": validJWTSecret}
	return func(key string) (string, bool) {
		for _, u := range unset {
			if u == key {
				return "", false
			}
		}
		if v, ok := env[key]; ok {
			return v, true
		}
		v, ok := base[key]
		return v, ok
	}
}

func TestLoadMetricsAddr(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		want          string
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "unset defaults to disabled",
			env:  map[string]string{},
			want: "",
		},
		{
			name: "loopback host:port accepted",
			env:  map[string]string{"METRICS_ADDR": "127.0.0.1:9090"},
			want: "127.0.0.1:9090",
		},
		{
			name: "localhost accepted",
			env:  map[string]string{"METRICS_ADDR": "localhost:9090"},
			want: "localhost:9090",
		},
		{
			name:          "no host, no colon, is rejected",
			env:           map[string]string{"METRICS_ADDR": "9090"},
			wantErr:       true,
			wantErrSubstr: "METRICS_ADDR",
		},
		{
			name:          "non-numeric port is rejected",
			env:           map[string]string{"METRICS_ADDR": "127.0.0.1:notaport"},
			wantErr:       true,
			wantErrSubstr: "METRICS_ADDR",
		},
		{
			name:          "port out of range is rejected",
			env:           map[string]string{"METRICS_ADDR": "127.0.0.1:99999"},
			wantErr:       true,
			wantErrSubstr: "METRICS_ADDR",
		},
		{
			name: "explicitly empty is treated as unset",
			env:  map[string]string{"METRICS_ADDR": ""},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(lookupFrom(tt.env, nil))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() with env %v: got nil error, want an error", tt.env)
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("Load() with env %v: error %q does not contain %q", tt.env, err.Error(), tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() with env %v: unexpected error: %v", tt.env, err)
			}
			if got.MetricsAddr != tt.want {
				t.Errorf("Load().MetricsAddr = %q, want %q", got.MetricsAddr, tt.want)
			}
		})
	}
}

func TestLoadMetricsAddrProductionLoopback(t *testing.T) {
	prodEnv := map[string]string{
		"ENV":                  "production",
		"CORS_ALLOWED_ORIGINS": "https://callit.example",
	}
	withMetrics := func(addr string) map[string]string {
		env := map[string]string{"METRICS_ADDR": addr}
		for k, v := range prodEnv {
			env[k] = v
		}
		return env
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{name: "production loopback IPv4 accepted", env: withMetrics("127.0.0.1:9090")},
		{name: "production localhost accepted", env: withMetrics("localhost:9090")},
		{name: "production loopback IPv6 accepted", env: withMetrics("[::1]:9090")},
		{name: "production 0.0.0.0 rejected", env: withMetrics("0.0.0.0:9090"), wantErr: true},
		{name: "production non-loopback IP rejected", env: withMetrics("10.0.0.5:9090"), wantErr: true},
		{name: "production empty host rejected", env: withMetrics(":9090"), wantErr: true},
		{
			name: "development 0.0.0.0 accepted",
			env:  map[string]string{"METRICS_ADDR": "0.0.0.0:9090"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(lookupFrom(tt.env, nil))
			if tt.wantErr && err == nil {
				t.Fatalf("Load() with env %v: got nil error, want an error naming METRICS_ADDR and loopback", tt.env)
			}
			if tt.wantErr && err != nil {
				if !strings.Contains(err.Error(), "METRICS_ADDR") || !strings.Contains(err.Error(), "loopback") {
					t.Errorf("Load() error = %q, want it to contain both %q and %q", err.Error(), "METRICS_ADDR", "loopback")
				}
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Load() with env %v: unexpected error: %v", tt.env, err)
			}
		})
	}
}
