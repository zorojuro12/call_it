// Command ledger-worker reads the wagers-placed and rounds-settled Kafka
// topics and writes their balance-changing events to the PostgreSQL
// double-entry ledger — a separate binary from cmd/api by design, so the
// WebSocket server never writes PostgreSQL directly.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zorojuro12/call_it/backend/internal/config"
	"github.com/zorojuro12/call_it/backend/internal/events"
	"github.com/zorojuro12/call_it/backend/internal/ledger"
)

func main() {
	if err := run(); err != nil {
		slog.Error("ledger worker exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadLedger(os.LookupEnv)
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	producer := events.NewKafkaProducer(cfg.KafkaBrokers)
	if err := producer.EnsureTopics(ctx, events.Partitions); err != nil {
		return err
	}
	producer.Close()

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return err
	}
	defer pool.Close()

	consumer := events.NewKafkaConsumer(cfg.KafkaBrokers, cfg.ConsumerGroup, []string{events.TopicWagersPlaced, events.TopicRoundsSettled}, true)
	defer func() {
		if err := consumer.Close(); err != nil {
			logger.Error("closing kafka consumer", "error", err)
		}
	}()

	repo := ledger.New(pool)
	worker := ledger.NewWorker(consumer, repo)

	logger.Info("ledger worker starting", "group", cfg.ConsumerGroup)
	return worker.Run(ctx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
