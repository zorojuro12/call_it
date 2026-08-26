package wager

// OddsEvent is the odds_updated broadcast payload — pool totals,
// multipliers, and counts only. No field here may ever carry a
// per-user stake; wagers stay anonymous until the round resolves
// (CLAUDE.md).
type OddsEvent struct {
	RoundID     string    `json:"round_id"`
	Pools       []int64   `json:"pools"`
	Total       int64     `json:"total"`
	Multipliers []float64 `json:"multipliers"`
	Bettors     int       `json:"bettors"`
	Players     int       `json:"players"`
}
