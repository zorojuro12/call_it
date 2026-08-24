package config

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "all defaults when nothing set",
			env:  map[string]string{},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 0},
		},
		{
			name: "explicit values override defaults",
			env: map[string]string{
				"PORT":      "9090",
				"ENV":       "production",
				"LOG_LEVEL": "debug",
			},
			want: Config{Port: 9090, Env: "production", LogLevel: "debug", RedisAddr: "localhost:6379", RedisDB: 0},
		},
		{
			name: "explicit redis addr overrides default",
			env:  map[string]string{"REDIS_ADDR": "redis:6379"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "redis:6379", RedisDB: 0},
		},
		{
			name:    "empty redis addr fails fast",
			env:     map[string]string{"REDIS_ADDR": ""},
			wantErr: true,
		},
		{
			name: "redis db at top of valid range",
			env:  map[string]string{"REDIS_DB": "15"},
			want: Config{Port: 8080, Env: "development", LogLevel: "info", RedisAddr: "localhost:6379", RedisDB: 15},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				v, ok := tt.env[key]
				return v, ok
			}

			got, err := Load(lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() with env %v: got nil error, want an error", tt.env)
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
