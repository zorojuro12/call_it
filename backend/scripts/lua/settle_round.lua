-- Applies a settlement Go already computed (domain.Settle) — this
-- script does not compute payouts, only applies them atomically:
-- CAS the round to its terminal status, credit each payout, and emit
-- one outbox event.
--
-- KEYS[1] round:{roundID}   KEYS[2] room:{roomID}:wallets   KEYS[3] <outbox>
-- ARGV[1] terminalStatus ('resolved' | 'refunded')
-- ARGV[2] resolvedOutcome  ('' when refunded — never nil, see Lua conventions)
-- ARGV[3] dust             ARGV[4] idempotencyKey   ARGV[5] roundID
-- ARGV[6..] alternating userID, amount
--
-- reply: {'OK', creditedCount} | {'NOT_LOCKED', status} | {'ALREADY_RESOLVED', status}

local roundKey = KEYS[1]
local walletsKey = KEYS[2]
local outboxKey = KEYS[3]

local terminalStatus = ARGV[1]
local resolvedOutcome = ARGV[2]
local dust = ARGV[3]
local idempotencyKey = ARGV[4]
local roundID = ARGV[5]

local creditedCount = 0
local i = 6
while ARGV[i] ~= nil do
  local userID = ARGV[i]
  local amount = tonumber(ARGV[i + 1])
  redis.call('HINCRBY', walletsKey, userID, amount)
  creditedCount = creditedCount + 1
  i = i + 2
end

redis.call('HSET', roundKey, 'status', terminalStatus)
if resolvedOutcome ~= '' then
  redis.call('HSET', roundKey, 'resolved_outcome', resolvedOutcome)
end

local eventType = 'round_settled'
if terminalStatus == 'refunded' then
  eventType = 'round_refunded'
end

redis.call('XADD', outboxKey, '*',
  'type', eventType,
  'round_id', roundID,
  'dust', dust,
  'winning_outcome', resolvedOutcome,
  'idempotency_key', idempotencyKey)

return {'OK', tostring(creditedCount)}
