-- Sliding-window rate limiter. Check-then-record must be atomic, or the
-- limit is advisory rather than enforced.
--
-- KEYS[1] ratelimit:{scope}:{id}
-- ARGV[1] window in milliseconds
-- ARGV[2] limit
-- ARGV[3] member id, unique per attempt
-- reply: {'ALLOWED', remaining, member, resetAtMs}
--      | {'DENIED', '0', '', retryAfterMs, resetAtMs}

local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local member = ARGV[3]

local timeParts = redis.call('TIME')
local now = tonumber(timeParts[1]) * 1000 + math.floor(tonumber(timeParts[2]) / 1000)

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

local count = redis.call('ZCARD', key)

redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window)

local remaining = limit - count - 1

local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
local resetAt = now + window
if #oldest > 0 then
  resetAt = tonumber(oldest[2]) + window
end

return {'ALLOWED', tostring(remaining), member, tostring(resetAt)}
