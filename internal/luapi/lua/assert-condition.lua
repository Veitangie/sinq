local condition, message = ...
if type(message) ~= "string" then error("lua.assert.true: Call with non-string message", 1) end

if not condition then sinq.assert.fail(message) end
