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
		return w, nil
	case TopicRoundsSettled:
		var s RoundSettled
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidEvent, err)
		}
		return s, nil
	default:
		return nil, fmt.Errorf("%w: topic %q", ErrUnknownEventType, topic)
	}
}
