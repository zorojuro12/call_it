package config

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// fakeEnv returns a LookupFunc over env, and fakeFiles a readFile over
// files, so ResolveSecretFiles is tested without touching the process
// environment or the filesystem.
func fakeEnv(env map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func fakeFiles(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		v, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
		}
		return []byte(v), nil
	}
}

func TestResolveSecretFiles(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		files     map[string]string
		key       string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "file content supplies the variable, one trailing newline stripped",
			env:       map[string]string{"JWT_SECRET_FILE": "/s/jwt"},
			files:     map[string]string{"/s/jwt": "abc\n"},
			key:       "JWT_SECRET",
			wantValue: "abc",
			wantOK:    true,
		},
		{
			name:      "file without a trailing newline is used as is",
			env:       map[string]string{"JWT_SECRET_FILE": "/s/jwt"},
			files:     map[string]string{"/s/jwt": "abc"},
			key:       "JWT_SECRET",
			wantValue: "abc",
			wantOK:    true,
		},
		{
			name:      "only one trailing newline is stripped",
			env:       map[string]string{"JWT_SECRET_FILE": "/s/jwt"},
			files:     map[string]string{"/s/jwt": "abc\n\n"},
			key:       "JWT_SECRET",
			wantValue: "abc\n",
			wantOK:    true,
		},
		{
			name:      "no _FILE variable passes the plain variable through",
			env:       map[string]string{"JWT_SECRET": "xyz"},
			key:       "JWT_SECRET",
			wantValue: "xyz",
			wantOK:    true,
		},
		{
			name:   "neither set stays unset",
			env:    map[string]string{},
			key:    "JWT_SECRET",
			wantOK: false,
		},
		{
			name:      "keys not named pass through untouched",
			env:       map[string]string{"JWT_SECRET_FILE": "/s/jwt", "REDIS_ADDR": "redis:6379"},
			files:     map[string]string{"/s/jwt": "abc"},
			key:       "REDIS_ADDR",
			wantValue: "redis:6379",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup, err := ResolveSecretFiles(fakeEnv(tt.env), fakeFiles(tt.files), "JWT_SECRET")
			if err != nil {
				t.Fatalf("ResolveSecretFiles() error = %v, want nil", err)
			}

			got, ok := lookup(tt.key)

			if ok != tt.wantOK || got != tt.wantValue {
				t.Errorf("lookup(%q) = (%q, %v), want (%q, %v)", tt.key, got, ok, tt.wantValue, tt.wantOK)
			}
		})
	}
}

func TestResolveSecretFilesFeedsLoad(t *testing.T) {
	// Arrange
	key := strings.Repeat("a", 64)
	env := map[string]string{"JWT_SECRET_FILE": "/run/secrets/jwt_secret"}
	files := map[string]string{"/run/secrets/jwt_secret": key}

	// Act
	lookup, err := ResolveSecretFiles(fakeEnv(env), fakeFiles(files), "JWT_SECRET")
	if err != nil {
		t.Fatalf("ResolveSecretFiles() error = %v", err)
	}
	cfg, err := Load(lookup)

	// Assert
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JWTSecret != key {
		t.Errorf("cfg.JWTSecret = %q, want the file's content", cfg.JWTSecret)
	}
}
