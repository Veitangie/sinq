local cond, range, delegate = ...
if type(range) ~= "number" then range = 50 end
if range < 0 then range = -range end
if type(delegate) ~= "function" then delegate = sinq.retry.when end
local jitter = 0
if cond then jitter = math.random(-range, range) end
return delegate(cond, select(4, ...)) + jitter
