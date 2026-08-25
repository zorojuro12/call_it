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

// JoinRequest is what a caller supplies to Join. UserID is empty for a
// guest — the service generates one. AccountBalance is ignored when
// Guest is true.
type JoinRequest struct {
	UserID         string
	DisplayName    string
	Guest          bool
	AccountBalance domain.Tokens
}

// Joined is what a successful Join reports back.
type Joined struct {
	RoomID         string
	Code           string
	BuyIn          domain.Tokens
	SessionBalance domain.Tokens
	PartialBuyIn   bool
	Guest          bool
	Token          string
}

// Join resolves code to a room, seeds or reads back the caller's
// session wallet, and issues a room-scoped token.
//
// Guests hold no user:{id} hash at all — their whole identity is the
// signed token, which is exactly what spec §3 describes as
// session-scoped and wiped when the session ends.
func (s *Service) Join(ctx context.Context, code string, c JoinRequest) (Joined, error) {
	roomID, err := s.store.RoomByCode(ctx, code)
	if err != nil {
		return Joined{}, err
	}

	rm, err := s.store.Room(ctx, roomID)
	if err != nil {
		return Joined{}, err
	}
	if rm.Status != "open" {
		return Joined{}, ErrNotJoinable
	}

	userID := c.UserID
	if userID == "" {
		userID = uuid.NewString()
	}

	sessionBalance := domain.GuestSessionBalance(rm.BuyIn)
	partial := false
	if !c.Guest {
		sessionBalance = domain.AccountSessionBalance(c.AccountBalance, rm.BuyIn)
		partial = domain.IsPartialBuyIn(c.AccountBalance, rm.BuyIn)
	}

	effective, err := s.store.JoinRoom(ctx, roomID, userID, sessionBalance)
	if err != nil {
		return Joined{}, err
	}

	token, err := s.issuer.Issue(auth.Claims{UserID: userID, DisplayName: c.DisplayName, RoomID: roomID, Guest: c.Guest})
	if err != nil {
		return Joined{}, fmt.Errorf("room: join: %w", err)
	}

	return Joined{
		RoomID:         roomID,
		Code:           code,
		BuyIn:          rm.BuyIn,
		SessionBalance: effective,
		PartialBuyIn:   partial,
		Guest:          c.Guest,
		Token:          token,
	}, nil
}
