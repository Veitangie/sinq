-- sinq - A concurrent integration testing tool
-- Copyright (C) 2026 Veitangie
-- SPDX-License-Identifier: GPL-3.0-or-later
local source, expected, message = ...
if type(message) ~= "string" then
  message = "sinq.assert.contains: " .. tostring(source) .. " did not contain " .. tostring(expected)
end

if type(expected) ~= "string" or type(source) ~= "string" or not string.find(source, expected, 1, true) then
  sinq.assert.fail(message)
end
