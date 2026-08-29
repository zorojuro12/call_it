package ledger

import "github.com/google/uuid"

// AccountKind identifies a category of ledger account.
type AccountKind string

const (
	KindUserWallet AccountKind = "user_wallet"
	KindRoomEscrow AccountKind = "room_escrow"
	KindRoundPool  AccountKind = "round_pool"
	KindSystemMint AccountKind = "system_mint"
	KindSystemDust AccountKind = "system_dust"
)

// Direction is the sign of an entry: credit (in) or debit (out).
// Balance for an account is Σcredits − Σdebits.
type Direction string

const (
	Debit  Direction = "debit"
	Credit Direction = "credit"
)

// AccountRef is a deterministic identity for a ledger account.
// The ID is a UUIDv5 computed from the account's natural key.
type AccountRef struct {
	Kind   AccountKind
	UserID string // "" when the kind does not scope by user
	RoomID string // "" when the kind does not scope by room
}

// accountNamespace is the UUID namespace for ledger account IDs (D5).
var accountNamespace = uuid.MustParse("9b1d4f6a-3c2e-4a58-8f7b-1e0d2c3b4a59")

// ID returns the deterministic UUIDv5 this account is stored under.
// The ID combines kind, user_id, and room_id so a room and a user
// cannot collide onto the same account even if they share an ID string.
func (a AccountRef) ID() uuid.UUID {
	key := string(a.Kind) + ":" + a.UserID + ":" + a.RoomID
	return uuid.NewSHA1(accountNamespace, []byte(key))
}
