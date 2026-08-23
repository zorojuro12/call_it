// Package config loads and validates process configuration from
// environment variables, failing fast at startup rather than at the
// point of first use.
package config

import (
	"fmt"
	"strconv"
)

// Config holds Phase 0's configuration surface. Fields for services this
// binary doesn't talk to yet (Redis, Postgres, Kafka, JWT) are added in
// the phase that introduces that integration, not speculatively here.
type Config struct {
	Port     int
	Env      string
	LogLevel string
}

var validEnvs = map[string]bool{
	"development": true,
	"production":  true,
	"test":        true,
}

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// LookupFunc mirrors os.LookupEnv's signature, allowing tests to supply
// an in-memory environment instead of mutating the process's real one.
type LookupFunc func(key string) (string, bool)

// Load reads configuration via lookup, applying defaults for anything
// unset and rejecting invalid values immediately.
func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		Port:     8080,
		Env:      "development",
		LogLevel: "info",
	}

	if v, ok := lookup("PORT"); ok {
		port, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("config: PORT %q is not a valid integer: %w", v, err)
		}
		if port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("config: PORT %d out of valid range 1-65535", port)
		}
		cfg.Port = port
	}

	if v, ok := lookup("ENV"); ok {
		if !validEnvs[v] {
			return Config{}, fmt.Errorf("config: ENV %q is not one of development|production|test", v)
		}
		cfg.Env = v
	}

	if v, ok := lookup("LOG_LEVEL"); ok {
		if !validLogLevels[v] {
			return Config{}, fmt.Errorf("config: LOG_LEVEL %q is not one of debug|info|warn|error", v)
		}
		cfg.LogLevel = v
	}

	return cfg, nil
}
