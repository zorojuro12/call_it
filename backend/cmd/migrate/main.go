// Command migrate applies or reverts the PostgreSQL ledger schema.
// Usage: migrate [down]
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/zorojuro12/call_it/backend/internal/config"
	"github.com/zorojuro12/call_it/backend/internal/migrate"
)

func main() {
	cfg, err := config.LoadMigrate(os.LookupEnv)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "down" {
		if err := migrate.Down(ctx, cfg.PostgresDSN); err != nil {
			logger.Error("migrate down failed", "error", err)
			os.Exit(1)
		}
		logger.Info("migrate down succeeded")
		return
	}

	if err := migrate.Up(ctx, cfg.PostgresDSN); err != nil {
		logger.Error("migrate up failed", "error", err)
		os.Exit(1)
	}
	logger.Info("migrate up succeeded")
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
