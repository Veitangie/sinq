-- sinq - A concurrent integration testing tool
-- Copyright (C) 2026 Veitangie
-- SPDX-License-Identifier: GPL-3.0-or-later
local cond, delay = ...
if type(delay) ~= "number" or delay < 0 then delay = 500 end
if cond then return delay end
return -1
