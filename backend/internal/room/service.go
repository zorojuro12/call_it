package room

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
)

// maxCodeAttempts bounds the code-collision retry loop. Five attempts
// against a 31^6 ~= 887-million-code space is far beyond what live room
// counts need; the bound exists so a pathological rand cannot spin
// forever.
const maxCodeAttempts = 5

// Service orchestrates room lifecycle: creation with a generated code,
// and joining by code as a guest or account holder.
type Service struct {
	store  *redisstore.Store
	issuer *auth.Issuer
	rand   io.Reader
}

// NewService constructs a Service bound to store and issuer.
func NewService(store *redisstore.Store, issuer *auth.Issuer) *Service {
	return &Service{store: store, issuer: issuer, rand: rand.Reader}
}

// Created is what a successful Create reports back.
type Created struct {
	RoomID string
	Code   string
	BuyIn  domain.Tokens
	Token  string
}

// Create generates a room code, creates the room, seeds the host's
// wallet, and issues a room-scoped token for the host.
//
// Seeding the host's wallet is not cosmetic:
// redisstore.PlayerCount is HLEN room:{roomID}:wallets - 1, written on
// the assumption that the host holds a wallet. Skipping this would make
// the "N/M players wagered" denominator short by one for the room's
// entire life. The host still cannot wager — that guard lives in
// place_wager.lua and is keyed on host_id, not on wallet absence.
func (s *Service) Create(ctx context.Context, hostID, hostName string, buyIn domain.Tokens) (Created, error) {
	roomID := uuid.NewString()

	var code string
	for attempt := 0; ; attempt++ {
		if attempt >= maxCodeAttempts {
			return Created{}, ErrCodeExhausted
		}

		c, err := GenerateCode(s.rand)
		if err != nil {
			return Created{}, fmt.Errorf("room: create: %w", err)
		}

		err = s.store.CreateRoom(ctx, roomID, c, hostID, buyIn)
		if err == nil {
			code = c
			break
		}
		if errors.Is(err, redisstore.ErrAlreadyExists) {
			continue
		}
		return Created{}, err
	}

	if _, err := s.store.JoinRoom(ctx, roomID, hostID, buyIn); err != nil {
		return Created{}, fmt.Errorf("room: create: seed host wallet: %w", err)
	}

	token, err := s.issuer.Issue(auth.Claims{UserID: hostID, DisplayName: hostName, RoomID: roomID, Guest: false})
	if err != nil {
		return Created{}, fmt.Errorf("room: create: %w", err)
	}

	return Created{RoomID: roomID, Code: code, BuyIn: buyIn, Token: token}, nil
}
