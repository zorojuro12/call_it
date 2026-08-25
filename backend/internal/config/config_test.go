package config

import (
	"strings"
	"testing"
	"time"
)

const validJWTSecret = "01234567890123456789012345678901" // 34 bytes, comfortably >= 32

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
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour},
		},
		{
			name: "explicit values override defaults",
			env: map[string]string{
				"PORT":      "9090",
				"ENV":       "production",
				"LOG_LEVEL": "debug",
			},
			want: Config{Port: 9090, Env: "production", LogLevel: "debug", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour},
		},
		{
			name: "explicit redis addr overrides default",
			env:  map[string]string{"REDIS_ADDR": "redis:6379"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "redis:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour},
		},
		{
			name:    "empty redis addr fails fast",
			env:     map[string]string{"REDIS_ADDR": ""},
			wantErr: true,
		},
		{
			name: "redis db at top of valid range",
			env:  map[string]string{"REDIS_DB": "15"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 15, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour},
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
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: strings.Repeat("b", 32), JWTTTL: 2 * time.Hour},
		},
		{
			name: "no JWT TTL defaults to two hours",
			env:  map[string]string{},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 2 * time.Hour},
		},
		{
			name: "explicit JWT TTL overrides default",
			env:  map[string]string{"JWT_TTL": "45m"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0, JWTSecret: validJWTSecret, JWTTTL: 45 * time.Minute},
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
			if got != tt.want {
				t.Errorf("Load() with env %v = %+v, want %+v", tt.env, got, tt.want)
			}
		})
	}
}
