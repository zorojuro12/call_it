-- Atomically accept a wager: debit the wallet, credit the outcome pool
-- and total, record the wager, add the bettor to the distinct-bettor
-- set, and emit an outbox event — one atomic unit.
--
-- KEYS[1] room:{roomID}            KEYS[5] round:{roundID}:wagers
-- KEYS[2] room:{roomID}:wallets    KEYS[6] round:{roundID}:bettors
-- KEYS[3] round:{roundID}          KEYS[7] idem:{idempotencyKey}
-- KEYS[4] round:{roundID}:pools    KEYS[8] <outbox stream>
--
-- ARGV[1] userID   ARGV[2] outcomeIdx   ARGV[3] amount
-- ARGV[4] idempotencyKey              ARGV[5] roomID   ARGV[6] roundID
--
-- Reply on accept: {'OK', balance, bettorCount, total, pool_0, ..., pool_{n-1}}
-- Every element a string. A nil must never appear in the returned table —
-- Lua-to-Redis conversion truncates the reply at the first nil.

local roomKey = KEYS[1]
local walletsKey = KEYS[2]
local roundKey = KEYS[3]
local poolsKey = KEYS[4]
local wagersKey = KEYS[5]
local bettorsKey = KEYS[6]
local idemKey = KEYS[7]
local outboxKey = KEYS[8]

local userID = ARGV[1]
local outcome = ARGV[2]
local amount = tonumber(ARGV[3])
local idempotencyKey = ARGV[4]
local roomID = ARGV[5]
local roundID = ARGV[6]

-- Idempotency check first, before any mutation: a replayed key returns
-- the cached reply verbatim.
local cached = redis.call('GET', idemKey)
if cached then
  return cjson.decode(cached)
end

local status = redis.call('HGET', roundKey, 'status')
if status ~= 'open' then
  return {'POOL_LOCKED'}
end

-- Lockout is judged by Redis's own clock, never a timestamp the caller
-- supplies — this is what makes the client-latency exploit structurally
-- impossible rather than merely discouraged (spec §4).
local lockAtMS = tonumber(redis.call('HGET', roundKey, 'lock_at_ms'))
local t = redis.call('TIME')
local nowMS = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
if nowMS >= lockAtMS then
  return {'POOL_LOCKED'}
end

local outcomeCount = tonumber(redis.call('HGET', roundKey, 'outcome_count'))
local outcomeNum = tonumber(outcome)
if outcomeNum == nil or outcomeNum < 0 or outcomeNum >= outcomeCount then
  return {'INVALID_OUTCOME'}
end

local newBalance = redis.call('HINCRBY', walletsKey, userID, -amount)
redis.call('HINCRBY', poolsKey, outcome, amount)
local total = redis.call('HINCRBY', poolsKey, 'total', amount)

local wagerField = userID .. ':' .. outcome
redis.call('HINCRBY', wagersKey, wagerField, amount)

redis.call('SADD', bettorsKey, userID)
local bettorCount = redis.call('SCARD', bettorsKey)

redis.call('XADD', outboxKey, '*',
  'type', 'wager_placed',
  'user', userID,
  'outcome', outcome,
  'amount', tostring(amount),
  'balance', tostring(newBalance),
  'idempotency_key', idempotencyKey,
  'room_id', roomID,
  'round_id', roundID)

local reply = {'OK', tostring(newBalance), tostring(bettorCount), tostring(total)}
for i = 0, outcomeCount - 1 do
  table.insert(reply, redis.call('HGET', poolsKey, tostring(i)))
end

redis.call('SET', idemKey, cjson.encode(reply), 'EX', 86400)

return reply
