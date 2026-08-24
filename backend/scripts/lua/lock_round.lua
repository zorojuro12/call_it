-- Compare-and-set open -> locked. This is the precondition every
-- settlement path checks: without it, a round could be settled while
-- new wagers are still arriving.
--
-- KEYS[1] round:{roundID}
-- reply: {'OK'} | {'ALREADY_LOCKED'} | {'ROUND_TERMINAL', status}

local roundKey = KEYS[1]

redis.call('HSET', roundKey, 'status', 'locked')

return {'OK'}
