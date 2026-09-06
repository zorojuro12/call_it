// Command relay reads the wager-outbox Redis Stream and produces every
// entry to Kafka — a separate binary from cmd/api by design, so the
// WebSocket server never writes PostgreSQL (or, transitively, Kafka)
// directly.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/zorojuro12/call_it/backend/internal/config"
	"github.com/zorojuro12/call_it/backend/internal/events"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/relay"
)

func main() {
	if err := run(); err != nil {
		slog.Error("relay exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadRelay(os.LookupEnv)
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	client := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
		DB:   cfg.RedisDB,
	})
	defer func() {
		if err := client.Close(); err != nil {
			logger.Error("closing redis client", "error", err)
		}
	}()

	producer := events.NewKafkaProducer(cfg.KafkaBrokers)
	defer func() {
		if err := producer.Close(); err != nil {
			logger.Error("closing kafka producer", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := producer.EnsureTopics(ctx, events.Partitions); err != nil {
		return err
	}

	consumerID := uuid.NewString()
	r := relay.New(client, redisstore.OutboxStream, redisstore.OutboxGroup, consumerID, producer)

	logger.Info("relay starting", "consumer", consumerID)
	return r.Run(ctx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
