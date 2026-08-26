// Package round owns round lifecycle: opening, the two server-side
// timers (lock, auto-refund), host resolution, and session-end
// persistence. It never writes Redis directly — every mutation goes
// through internal/redisstore's existing, tested writers, and
// settlement math is never recomputed here (CLAUDE.md).
package round

import (
	"encoding/json"
	"fmt"
	"time"
)

// Spec is what the host supplies to open a round.
type Spec struct {
	Question string
	Outcomes []string
	LockIn   time.Duration
}

// Opened is what a successful Open reports back, and also the payload
// of the round_opened broadcast.
type Opened struct {
	RoundID  string   `json:"round_id"`
	Question string   `json:"question"`
	Outcomes []string `json:"outcomes"`
	LockAtMS int64    `json:"lock_at_ms"`
}

// LockedEvent is the round_locked broadcast payload.
type LockedEvent struct {
	RoundID string `json:"round_id"`
}

// Broadcaster is how this package reaches a room's connected clients
// without importing internal/ws — internal/ws imports round (for the
// message router, Task 9), so round cannot import ws without a build
// cycle. internal/ws.Hub satisfies this interface.
type Broadcaster interface {
	Broadcast(roomID string, payload []byte)

	// Names resolves connected clients' display names by user ID, for
	// the round_resolved reveal (a player who disconnected before
	// resolution has no name available here).
	Names(roomID string) map[string]string
}

// ResultRow is one player's revealed result — the first and only
// moment a per-player stake is disclosed (CLAUDE.md).
type ResultRow struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Staked      int64  `json:"staked"`
	Returned    int64  `json:"returned"`
	Net         int64  `json:"net"`
}

// ResolvedEvent is the round_resolved broadcast payload.
type ResolvedEvent struct {
	RoundID        string      `json:"round_id"`
	WinningOutcome int         `json:"winning_outcome"`
	Results        []ResultRow `json:"results"`
	Dust           int64       `json:"dust"`
	Refunded       bool        `json:"refunded"`
}

// envelope mirrors internal/ws.Envelope's wire format ({"type":...,
// "data":...}) so every client-side ws.Decode call reads a broadcast
// from this package the same as one built by internal/ws itself. It is
// defined locally rather than imported, for the reason Broadcaster's
// doc comment gives.
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// EncodeEnvelope marshals data and wraps it in the same envelope shape
// internal/ws uses. internal/wager reuses this rather than defining its
// own, so there is exactly one envelope encoder on the round/wager side
// of the boundary.
func EncodeEnvelope(msgType string, data any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("round: encode %s: %w", msgType, err)
	}
	return json.Marshal(envelope{Type: msgType, Data: raw})
}
