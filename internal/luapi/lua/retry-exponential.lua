local condition, base, constant = ...
if type(base) ~= "number" or base <= 0 or base > 10 then base = 2 end
if type(constant) ~= "number" or constant <= 0 then constant = 500 end
if condition then return base ^ res.attempt * constant end
return -1
