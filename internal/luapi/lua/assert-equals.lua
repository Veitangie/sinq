-- sinq - A concurrent integration testing tool
-- Copyright (C) 2026 Veitangie
-- SPDX-License-Identifier: GPL-3.0-or-later
local actual, expected, message = ...
local function messageOr(otherMessage)
  if type(message) ~= "string" then return otherMessage end
  return message
end

if type(actual) ~= type(expected) then
  sinq.assert.fail(
    messageOr("sinq.assert.equals: Expected type " .. type(expected) .. ", got type " .. type(actual) .. " instead.")
  )
  return
end

if type(expected) == "table" then
  local function compareTables(have, want, prefix)
    if prefix == nil then prefix = "" end

    for k, v in pairs(want) do
      local haveV = have[k]
      if type(v) ~= type(haveV) then
        sinq.assert.fail(
          messageOr(
            "sinq.assert.equals: Expected type "
              .. type(v)
              .. ", got type "
              .. type(haveV)
              .. " in field "
              .. prefix
              .. k
          )
        )
        goto continue
      end

      if type(v) == "table" then
        compareTables(haveV, v, prefix .. k .. ".")
        goto continue
      end

      if v ~= haveV then
        sinq.assert.fail(
          messageOr(
            "sinq.assert.equals: Expected value "
              .. tostring(v)
              .. ", got "
              .. tostring(haveV)
              .. " in field "
              .. prefix
              .. k
          )
        )
      end
      ::continue::
    end
  end

  compareTables(actual, expected, "")
  return
end

if actual ~= expected then
  sinq.assert.fail(
    messageOr("sinq.assert.equals: Expected " .. tostring(expected) .. ", got " .. tostring(actual) .. " instead")
  )
end
