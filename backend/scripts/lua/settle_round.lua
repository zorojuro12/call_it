-- Applies a settlement Go already computed (domain.Settle) — this
-- script does not compute payouts, only applies them atomically:
-- CAS the round to its terminal status, credit each payout, and emit
-- one outbox event.
--
-- KEYS[1] round:{roundID}   KEYS[2] room:{roomID}:wallets   KEYS[3] <outbox>
-- ARGV[1] terminalStatus ('resolved' | 'refunded')
-- ARGV[2] resolvedOutcome  ('' when refunded — never nil, see Lua conventions)
-- ARGV[3] dust             ARGV[4] idempotencyKey   ARGV[5] roundID
-- ARGV[6] roomID           ARGV[7] total            ARGV[8] payoutsJSON
--   (payoutsJSON is authored by Go from the same settlement.Payouts slice
--   that produces the ARGV[9..] tail below — Amendment E1. This script
--   never parses or builds it, only echoes it into the outbox event.)
-- ARGV[9..] alternating userID, amount
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
local roomID = ARGV[6]
local total = ARGV[7]
local payoutsJSON = ARGV[8]

local currentStatus = redis.call('HGET', roundKey, 'status')
if currentStatus == 'resolved' or currentStatus == 'refunded' then
  return {'ALREADY_RESOLVED', currentStatus}
end
if currentStatus ~= 'locked' then
  return {'NOT_LOCKED', currentStatus}
end

local creditedCount = 0
local i = 9
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
  'room_id', roomID,
  'total', total,
  'dust', dust,
  'winning_outcome', resolvedOutcome,
  'payouts', payoutsJSON,
  'idempotency_key', idempotencyKey)

return {'OK', tostring(creditedCount)}
