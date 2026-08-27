// Package events: message.go decodes Kafka messages into typed events.
// The concrete type is determined by the topic alone, because the wire
// payload carries no type discriminator. Refunded distinguishes a refund
// from a resolution within RoundSettled, it does not distinguish the
// two structs.
package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrInvalidEvent = errors.New("events: invalid event")

// DecodeMessage decodes a Kafka message value into a typed Event by routing
// on topic. Returns the concrete event type and any error.
func DecodeMessage(topic string, value []byte) (Event, error) {
	switch topic {
	case TopicWagersPlaced:
		var w WagerPlaced
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&w); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidEvent, err)
		}
		if err := validateWagerPlaced(&w); err != nil {
			return nil, err
		}
		return w, nil
	case TopicRoundsSettled:
		var s RoundSettled
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidEvent, err)
		}
		if err := validateRoundSettled(&s); err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, fmt.Errorf("%w: topic %q", ErrUnknownEventType, topic)
	}
}

func validateWagerPlaced(w *WagerPlaced) error {
	if w.RoomID == "" {
		return fmt.Errorf("%w: room_id is required", ErrInvalidEvent)
	}
	if w.RoundID == "" {
		return fmt.Errorf("%w: round_id is required", ErrInvalidEvent)
	}
	if w.UserID == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalidEvent)
	}
	if w.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency_key is required", ErrInvalidEvent)
	}
	if w.Amount <= 0 {
		return fmt.Errorf("%w: amount must be positive, got %d", ErrInvalidEvent, w.Amount)
	}
	if w.Outcome < 0 {
		return fmt.Errorf("%w: outcome must be non-negative, got %d", ErrInvalidEvent, w.Outcome)
	}
	if w.Balance < 0 {
		return fmt.Errorf("%w: balance must be non-negative, got %d", ErrInvalidEvent, w.Balance)
	}
	return nil
}

func validateRoundSettled(s *RoundSettled) error {
	if s.RoomID == "" {
		return fmt.Errorf("%w: room_id is required", ErrInvalidEvent)
	}
	if s.RoundID == "" {
		return fmt.Errorf("%w: round_id is required", ErrInvalidEvent)
	}
	if s.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency_key is required", ErrInvalidEvent)
	}
	if s.Total < 0 {
		return fmt.Errorf("%w: total must be non-negative, got %d", ErrInvalidEvent, s.Total)
	}
	if s.Dust < 0 {
		return fmt.Errorf("%w: dust must be non-negative, got %d", ErrInvalidEvent, s.Dust)
	}
	for _, p := range s.Payouts {
		if p.UserID == "" {
			return fmt.Errorf("%w: payout user_id is required", ErrInvalidEvent)
		}
		if p.Amount <= 0 {
			return fmt.Errorf("%w: payout amount must be positive, got %d", ErrInvalidEvent, p.Amount)
		}
	}
	if s.Refunded {
		if s.WinningOutcome != -1 {
			return fmt.Errorf("%w: refunded round must have winning_outcome -1, got %d", ErrInvalidEvent, s.WinningOutcome)
		}
		if s.Dust != 0 {
			return fmt.Errorf("%w: refunded round must have zero dust, got %d", ErrInvalidEvent, s.Dust)
		}
	} else {
		if s.WinningOutcome < 0 {
			return fmt.Errorf("%w: resolved round must have non-negative winning_outcome, got %d", ErrInvalidEvent, s.WinningOutcome)
		}
	}
	return nil
}
