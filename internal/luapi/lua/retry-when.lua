local cond, delay = ...
if type(delay) ~= "number" or delay < 0 then delay = 500 end
if cond then return delay end
return -1
