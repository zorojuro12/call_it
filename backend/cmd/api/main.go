// Command api runs CallIt's HTTP/WebSocket server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/config"
	"github.com/zorojuro12/call_it/backend/internal/httpapi"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/room"
	"github.com/zorojuro12/call_it/backend/internal/ws"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("api exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	store, err := redisstore.New(cfg.RedisAddr, cfg.RedisDB)
	if err != nil {
		return fmt.Errorf("connecting to redis: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("closing redis store", "error", err)
		}
	}()

	issuer, err := auth.NewIssuer([]byte(cfg.JWTSecret), cfg.JWTTTL)
	if err != nil {
		return fmt.Errorf("constructing token issuer: %w", err)
	}

	accounts := account.NewService(store, issuer)
	rooms := room.NewService(store, issuer)
	hub := ws.NewHub()

	server := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port),
		Handler: httpapi.NewMux(httpapi.Deps{
			Accounts: accounts,
			Rooms:    rooms,
			Store:    store,
			Issuer:   issuer,
			Hub:      hub,
		}),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	hub.Shutdown()

	logger.Info("server stopped cleanly")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
