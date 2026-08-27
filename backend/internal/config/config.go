// Package config loads and validates process configuration from
// environment variables, failing fast at startup rather than at the
// point of first use.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// minJWTSecretLen is the floor on the HMAC signing key. HS256's key
// should be at least as long as the hash output it produces, or the
// signature's effective strength drops below the algorithm's.
const minJWTSecretLen = 32

const (
	defaultJWTTTL = 2 * time.Hour
	minJWTTTL     = time.Minute
	maxJWTTTL     = 24 * time.Hour
)

// Config holds cmd/api's configuration surface. Postgres and Kafka never
// appear here — cmd/api never talks to either directly (CLAUDE.md: the
// WebSocket server never writes PostgreSQL directly); those surfaces
// belong to MigrateConfig and RelayConfig, the binaries that do.
type Config struct {
	Port      int
	Env       string
	LogLevel  string
	RedisAddr string
	RedisDB   int
	JWTSecret string        // REQUIRED — no default, min 32 bytes
	JWTTTL    time.Duration // default 2h, valid 1m..24h
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
		Port:      8080,
		Env:       "development",
		LogLevel:  "info",
		RedisAddr: "localhost:6379",
		RedisDB:   0,
		JWTTTL:    defaultJWTTTL,
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

	if v, ok := lookup("REDIS_ADDR"); ok {
		if v == "" {
			return Config{}, fmt.Errorf("config: REDIS_ADDR must not be empty")
		}
		cfg.RedisAddr = v
	}

	if v, ok := lookup("REDIS_DB"); ok {
		db, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("config: REDIS_DB %q is not a valid integer: %w", v, err)
		}
		if db < 0 || db > 15 {
			return Config{}, fmt.Errorf("config: REDIS_DB %d out of valid range 0-15", db)
		}
		cfg.RedisDB = db
	}

	v, ok := lookup("JWT_SECRET")
	if !ok || v == "" {
		return Config{}, fmt.Errorf("config: JWT_SECRET is required")
	}
	if len(v) < minJWTSecretLen {
		return Config{}, fmt.Errorf("config: JWT_SECRET must be at least %d bytes, got %d", minJWTSecretLen, len(v))
	}
	cfg.JWTSecret = v

	if v, ok := lookup("JWT_TTL"); ok {
		ttl, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("config: JWT_TTL %q is not a valid duration: %w", v, err)
		}
		if ttl < minJWTTTL || ttl > maxJWTTTL {
			return Config{}, fmt.Errorf("config: JWT_TTL %s out of valid range 1m-24h", ttl)
		}
		cfg.JWTTTL = ttl
	}

	return cfg, nil
}

// MigrateConfig holds the configuration surface for cmd/migrate. A
// migration runner has no business demanding a JWT signing key, so it
// does not embed or require Config.
type MigrateConfig struct {
	PostgresDSN string
	LogLevel    string
}

// LoadMigrate reads the migration runner's configuration via lookup. It
// does not require JWT_SECRET.
func LoadMigrate(lookup LookupFunc) (MigrateConfig, error) {
	cfg := MigrateConfig{
		LogLevel: "info",
	}

	dsn, ok := lookup("POSTGRES_DSN")
	if !ok || dsn == "" {
		return MigrateConfig{}, fmt.Errorf("config: POSTGRES_DSN is required")
	}
	cfg.PostgresDSN = dsn

	if v, ok := lookup("LOG_LEVEL"); ok {
		if !validLogLevels[v] {
			return MigrateConfig{}, fmt.Errorf("config: LOG_LEVEL %q is not one of debug|info|warn|error", v)
		}
		cfg.LogLevel = v
	}

	return cfg, nil
}

// RelayConfig holds cmd/relay's configuration surface. Like
// MigrateConfig, it does not require JWT_SECRET — the relay never
// issues or verifies a token, and requiring one would hand a non-auth
// binary a credential it has no use for.
type RelayConfig struct {
	RedisAddr    string
	RedisDB      int
	KafkaBrokers []string
	LogLevel     string
	Env          string
}

// LoadRelay reads the relay's configuration via lookup, reusing Load's
// validation helpers for the fields the two binaries share.
func LoadRelay(lookup LookupFunc) (RelayConfig, error) {
	cfg := RelayConfig{
		RedisAddr:    "localhost:6379",
		RedisDB:      0,
		KafkaBrokers: []string{"localhost:9092"},
		LogLevel:     "info",
		Env:          "development",
	}

	if v, ok := lookup("REDIS_ADDR"); ok {
		if v == "" {
			return RelayConfig{}, fmt.Errorf("config: REDIS_ADDR must not be empty")
		}
		cfg.RedisAddr = v
	}

	if v, ok := lookup("REDIS_DB"); ok {
		db, err := strconv.Atoi(v)
		if err != nil {
			return RelayConfig{}, fmt.Errorf("config: REDIS_DB %q is not a valid integer: %w", v, err)
		}
		if db < 0 || db > 15 {
			return RelayConfig{}, fmt.Errorf("config: REDIS_DB %d out of valid range 0-15", db)
		}
		cfg.RedisDB = db
	}

	if v, ok := lookup("KAFKA_BROKERS"); ok {
		if v == "" {
			return RelayConfig{}, fmt.Errorf("config: KAFKA_BROKERS must not be empty")
		}
		brokers := strings.Split(v, ",")
		for _, b := range brokers {
			if b == "" {
				return RelayConfig{}, fmt.Errorf("config: KAFKA_BROKERS %q contains an empty element", v)
			}
		}
		cfg.KafkaBrokers = brokers
	}

	if v, ok := lookup("LOG_LEVEL"); ok {
		if !validLogLevels[v] {
			return RelayConfig{}, fmt.Errorf("config: LOG_LEVEL %q is not one of debug|info|warn|error", v)
		}
		cfg.LogLevel = v
	}

	if v, ok := lookup("ENV"); ok {
		if !validEnvs[v] {
			return RelayConfig{}, fmt.Errorf("config: ENV %q is not one of development|production|test", v)
		}
		cfg.Env = v
	}

	return cfg, nil
}
