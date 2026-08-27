-- Applies a refund Go already computed by aggregating stakes
-- (Amendment E2) — this script does not read the wagers hash or compute
-- amounts, only applies them atomically: CAS the round to 'refunded',
-- credit each payout, and emit one outbox event. Amounts now come from
-- Go, symmetric with settle_round.lua, so a ledger can be written from
-- the event alone without re-reading Redis state that may already be
-- gone by the time it is consumed.
--
-- KEYS[1] round:{roundID}   KEYS[2] room:{roomID}:wallets   KEYS[3] <outbox>
-- ARGV[1] idempotencyKey   ARGV[2] roundID   ARGV[3] roomID
-- ARGV[4] total            ARGV[5] payoutsJSON
-- ARGV[6..] alternating userID, amount
--
-- reply: {'OK', totalRefunded} | {'NOT_LOCKED', status} | {'ALREADY_RESOLVED', status}

local roundKey = KEYS[1]
local walletsKey = KEYS[2]
local outboxKey = KEYS[3]

local idempotencyKey = ARGV[1]
local roundID = ARGV[2]
local roomID = ARGV[3]
local total = ARGV[4]
local payoutsJSON = ARGV[5]

-- The Go read that produced this ARGV list is not atomic with this
-- script, so the script keeps its own status CAS regardless of the
-- Go-side check in Store.RefundRound.
local status = redis.call('HGET', roundKey, 'status')
if status == 'resolved' or status == 'refunded' then
  return {'ALREADY_RESOLVED', status}
end
if status ~= 'locked' then
  return {'NOT_LOCKED', status}
end

local i = 6
while ARGV[i] ~= nil do
  local userID = ARGV[i]
  local amount = tonumber(ARGV[i + 1])
  redis.call('HINCRBY', walletsKey, userID, amount)
  i = i + 2
end

redis.call('HSET', roundKey, 'status', 'refunded')

redis.call('XADD', outboxKey, '*',
  'type', 'round_refunded',
  'round_id', roundID,
  'room_id', roomID,
  'total', total,
  'dust', '0',
  'winning_outcome', '',
  'payouts', payoutsJSON,
  'idempotency_key', idempotencyKey)

return {'OK', total}
