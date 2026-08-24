-- Refunds every stake on a round's timeout/disconnect path. Unlike
-- settlement there is nothing to compute — refunding is the identity
-- function on stakes — so this script reads the wagers hash inside its
-- own atomic unit rather than taking amounts from Go.
--
-- KEYS[1] round:{roundID}  KEYS[2] room:{roomID}:wallets
-- KEYS[3] round:{roundID}:wagers   KEYS[4] <outbox>
-- ARGV[1] idempotencyKey   ARGV[2] roundID
-- reply: {'OK', totalRefunded} | {'NOT_LOCKED', status} | {'ALREADY_RESOLVED', status}

local roundKey = KEYS[1]
local walletsKey = KEYS[2]
local wagersKey = KEYS[3]
local outboxKey = KEYS[4]

local idempotencyKey = ARGV[1]
local roundID = ARGV[2]

local status = redis.call('HGET', roundKey, 'status')
if status == 'resolved' or status == 'refunded' then
  return {'ALREADY_RESOLVED', status}
end
if status ~= 'locked' then
  return {'NOT_LOCKED', status}
end

local wagers = redis.call('HGETALL', wagersKey)
local total = 0
for i = 1, #wagers, 2 do
  local field = wagers[i]
  local amount = tonumber(wagers[i + 1])
  local colonIdx = nil
  for j = #field, 1, -1 do
    if field:sub(j, j) == ':' then
      colonIdx = j
      break
    end
  end
  local userID = field:sub(1, colonIdx - 1)
  redis.call('HINCRBY', walletsKey, userID, amount)
  total = total + amount
end

redis.call('HSET', roundKey, 'status', 'refunded')

redis.call('XADD', outboxKey, '*',
  'type', 'round_refunded',
  'round_id', roundID,
  'total', tostring(total),
  'idempotency_key', idempotencyKey)

return {'OK', tostring(total)}
