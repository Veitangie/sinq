-- sinq - A concurrent integration testing tool
-- Copyright (C) 2026 Veitangie
-- SPDX-License-Identifier: GPL-3.0-or-later
local condition, message = ...
if type(message) == "nil" then message = "sinq.assert.isTrue: Assertion failed" end

if type(message) ~= "string" then error("sinq.assert.isTrue: Call with non-string message", 1) end

if not condition then sinq.assert.fail(message) end
