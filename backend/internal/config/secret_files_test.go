package config

import (
	"errors"
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

func TestResolveSecretFilesErrors(t *testing.T) {
	const canary = "s3cret-canary"
	tests := []struct {
		name          string
		env           map[string]string
		files         map[string]string
		wantErrSubstr string
		wantNotExist  bool
	}{
		{
			name:          "both NAME and NAME_FILE set is ambiguous",
			env:           map[string]string{"JWT_SECRET": "x", "JWT_SECRET_FILE": "/s/jwt"},
			files:         map[string]string{"/s/jwt": canary},
			wantErrSubstr: "set JWT_SECRET or JWT_SECRET_FILE, not both",
		},
		{
			name:          "an empty NAME still conflicts with NAME_FILE",
			env:           map[string]string{"JWT_SECRET": "", "JWT_SECRET_FILE": "/s/jwt"},
			files:         map[string]string{"/s/jwt": canary},
			wantErrSubstr: "set JWT_SECRET or JWT_SECRET_FILE, not both",
		},
		{
			name:          "an empty NAME_FILE is rejected",
			env:           map[string]string{"JWT_SECRET_FILE": ""},
			wantErrSubstr: "JWT_SECRET_FILE must not be empty",
		},
		{
			name:          "an unreadable file is reported and wrapped",
			env:           map[string]string{"JWT_SECRET_FILE": "/s/missing"},
			wantErrSubstr: "reading JWT_SECRET_FILE",
			wantNotExist:  true,
		},
		{
			name:          "an empty file is rejected",
			env:           map[string]string{"JWT_SECRET_FILE": "/s/jwt"},
			files:         map[string]string{"/s/jwt": ""},
			wantErrSubstr: "JWT_SECRET_FILE /s/jwt is empty",
		},
		{
			name:          "a file holding only a newline is rejected",
			env:           map[string]string{"JWT_SECRET_FILE": "/s/jwt"},
			files:         map[string]string{"/s/jwt": "\n"},
			wantErrSubstr: "JWT_SECRET_FILE /s/jwt is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup, err := ResolveSecretFiles(fakeEnv(tt.env), fakeFiles(tt.files), "JWT_SECRET")

			if err == nil {
				t.Fatalf("ResolveSecretFiles() error = nil, want error containing %q", tt.wantErrSubstr)
			}
			if lookup != nil {
				t.Errorf("ResolveSecretFiles() lookup is non-nil on error")
			}
			if !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErrSubstr)
			}
			if tt.wantNotExist && !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("error = %v, want it to wrap fs.ErrNotExist", err)
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("error %q leaks the secret file's content", err)
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
