// Package lua embeds the Redis Lua scripts that make every
// balance-mutating operation atomic. Kept as real .lua files (not Go
// string literals) so they retain editor syntax highlighting and
// external linting; go:embed cannot reach outside this package's
// directory, which is why the scripts live here rather than in
// internal/redisstore alongside their Go wrappers.
package lua

import _ "embed"

//go:embed place_wager.lua
var PlaceWager string

//go:embed lock_round.lua
var LockRound string

//go:embed settle_round.lua
var SettleRound string

//go:embed refund_round.lua
var RefundRound string
