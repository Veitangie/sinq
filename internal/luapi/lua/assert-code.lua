local code, message = ...
if type(message) ~= "string" then
  message = "sinq.assert.code: Expected code " .. tostring(code) .. ", got " .. tostring(res.code) .. " instead"
end

if res.code ~= code then sinq.assert.fail(message) end
