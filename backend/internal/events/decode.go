package events

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Decode turns a Redis Stream entry's field map into a typed Event,
// switching on fields["type"]. A missing required field is an error,
// not a silently-substituted zero value — a relay that guessed at a
// missing field would risk misrouting or miscrediting a money movement.
func Decode(fields map[string]string) (Event, error) {
	switch fields["type"] {
	case "wager_placed":
		return decodeWagerPlaced(fields)
	case "round_settled":
		return decodeRoundSettled(fields, false)
	case "round_refunded":
		return decodeRoundSettled(fields, true)
	default:
		return nil, fmt.Errorf("events: unrecognized type %q", fields["type"])
	}
}

func requireField(fields map[string]string, name string) (string, error) {
	v, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("events: missing required field %q", name)
	}
	return v, nil
}

func parseInt(fields map[string]string, name string) (int64, error) {
	v, err := requireField(fields, name)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("events: field %q is not an integer: %w", name, err)
	}
	return n, nil
}

func decodeWagerPlaced(fields map[string]string) (Event, error) {
	roomID, err := requireField(fields, "room_id")
	if err != nil {
		return nil, err
	}
	roundID, err := requireField(fields, "round_id")
	if err != nil {
		return nil, err
	}
	userID, err := requireField(fields, "user")
	if err != nil {
		return nil, err
	}
	idempotencyKey, err := requireField(fields, "idempotency_key")
	if err != nil {
		return nil, err
	}
	outcome, err := parseInt(fields, "outcome")
	if err != nil {
		return nil, err
	}
	amount, err := parseInt(fields, "amount")
	if err != nil {
		return nil, err
	}
	balance, err := parseInt(fields, "balance")
	if err != nil {
		return nil, err
	}

	return WagerPlaced{
		RoomID:         roomID,
		RoundID:        roundID,
		UserID:         userID,
		IdempotencyKey: idempotencyKey,
		Outcome:        int(outcome),
		Amount:         amount,
		Balance:        balance,
	}, nil
}

func decodeRoundSettled(fields map[string]string, refunded bool) (Event, error) {
	roomID, err := requireField(fields, "room_id")
	if err != nil {
		return nil, err
	}
	roundID, err := requireField(fields, "round_id")
	if err != nil {
		return nil, err
	}
	idempotencyKey, err := requireField(fields, "idempotency_key")
	if err != nil {
		return nil, err
	}

	// -1 is the sentinel for "no winning outcome": 0 is a valid outcome
	// index and would be indistinguishable from a real outcome-0 win.
	winningOutcome := int64(-1)
	if refunded {
		if fields["winning_outcome"] != "" {
			return nil, fmt.Errorf("events: a refund event cannot carry a winning_outcome (got %q)", fields["winning_outcome"])
		}
	} else {
		winningOutcome, err = parseInt(fields, "winning_outcome")
		if err != nil {
			return nil, err
		}
	}

	total, err := parseInt(fields, "total")
	if err != nil {
		return nil, err
	}
	dust, err := parseInt(fields, "dust")
	if err != nil {
		return nil, err
	}
	payoutsRaw, err := requireField(fields, "payouts")
	if err != nil {
		return nil, err
	}
	var payouts []Payout
	if err := json.Unmarshal([]byte(payoutsRaw), &payouts); err != nil {
		return nil, fmt.Errorf("events: field %q is not valid JSON: %w", "payouts", err)
	}

	return RoundSettled{
		RoomID:         roomID,
		RoundID:        roundID,
		IdempotencyKey: idempotencyKey,
		WinningOutcome: int(winningOutcome),
		Total:          total,
		Dust:           dust,
		Payouts:        payouts,
		Refunded:       refunded,
	}, nil
}
