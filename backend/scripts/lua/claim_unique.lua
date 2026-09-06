-- Claim a unique secondary index and create the entity it points at,
-- atomically. Serves both email claiming (registration) and room-code
-- claiming (room creation) -- both are the same operation, so one
-- script serves both call sites.
--
-- KEYS[1] unique index key    (email:{email} | code:{code})
-- KEYS[2] entity hash key     (user:{userID} | room:{roomID})
-- ARGV[1] the id the index points at
-- ARGV[2..] field/value pairs written to the entity hash
-- reply: {'OK'} | {'TAKEN', existingID}

local indexKey = KEYS[1]
local entityKey = KEYS[2]
local id = ARGV[1]

local claimed = redis.call('SETNX', indexKey, id)

if claimed == 0 then
  local existingID = redis.call('GET', indexKey)
  return {'TAKEN', existingID}
end

local fields = {}
for i = 2, #ARGV do
  fields[#fields + 1] = ARGV[i]
end
redis.call('HSET', entityKey, unpack(fields))

return {'OK'}
