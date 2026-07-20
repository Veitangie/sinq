-- sinq - A concurrent integration testing tool
-- Copyright (C) 2026 Veitangie
-- SPDX-License-Identifier: GPL-3.0-or-later
local cond, range, delegate = ...
if type(range) ~= "number" then range = 50 end
if range < 0 then range = -range end
if type(delegate) ~= "function" then delegate = sinq.retry.when end
local jitter = 0
if cond then jitter = math.random(-range, range) end
return delegate(cond, select(4, ...)) + jitter
