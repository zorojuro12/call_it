-- Top an account balance up to a target, atomically. Setting to the
-- target rather than incrementing by a Go-computed delta is what makes
-- a concurrent double-claim safe: the second call reads the
-- already-topped balance and credits nothing, instead of adding a
-- second delta computed from a stale read.
--
-- KEYS[1] user:{userID}
-- ARGV[1] target balance
-- reply: {'OK', credited, newBalance} | {'NOT_FOUND'}

local userKey = KEYS[1]
local target = tonumber(ARGV[1])

local balanceStr = redis.call('HGET', userKey, 'balance')
if balanceStr == false then
  return {'NOT_FOUND'}
end

local balance = tonumber(balanceStr)

if balance >= target then
  return {'OK', '0', tostring(balance)}
end

redis.call('HSET', userKey, 'balance', target)

return {'OK', tostring(target - balance), tostring(target)}
