# Lua API & Type Translation

Because `sinq` bridges a Go runtime with a Lua Virtual Machine, data must be translated back and forth across the boundary. 

## Variable Scoping (Local vs. Global)

To prevent cache poisoning and unintended side effects, you should strictly control your variable scoping.

* **Temporary Math & Logic:** Use the `local` keyword. This ensures the variable is garbage-collected immediately after the script block finishes.
  ```lua
  local id = sinq.responses[1].body.id
  ```
* **Passing State Across Files:** If you need a value from `01_login.sinq` to be accessible in `02_action.sinq`, you must declare it globally (without `local`). It will be attached to the Lua sandbox for the lifespan of that specific scenario.
  ```lua
  AUTH_TOKEN = sinq.responses[1].body.token
  -- Equivalent to: _G.AUTH_TOKEN = ...
  ```

## HTTP Response Translation

When an HTTP request completes, `sinq` parses the response and injects it into the `sinq.responses` table at the index corresponding to the request number.

### 1. The JSON Blindspot (1-Indexed Arrays)
In Go and in general, arrays are `0-indexed`. In Lua, tables are `1-indexed`. 
If your API returns a top-level JSON array, `sinq` translates it into a Lua table starting at index 1.

**API Response:**
```json
[
  {"id": 42},
  {"id": 99}
]
```

**Lua Assertion:**
```lua
$ASSERT{
    -- Correct: Access the first element at index 1
    local first_id = sinq.responses[1].body[1].id
    if first_id ~= 42 then sinq.test.fail("ID mismatch") end
}
```

### 2. HTTP Headers
HTTP headers are complex because a single key can have multiple values (e.g., multiple `Set-Cookie` headers). `sinq` handles this translation automatically.

* **Single Value Headers:** Translated to a standard Lua string.
  ```lua
  local contentType = sinq.responses[1].headers["Content-Type"]
  ```
* **Multi-Value Headers:** Translated to a 1-indexed Lua table (array) of strings.
  ```lua
  local firstCookie = sinq.responses[1].headers["Set-Cookie"][1]
  ```

## Standard Library Integrity
By default, `sinq` reuses the Lua `LState` to maximize performance. Do not mutate core Lua functions (e.g., overwriting `table.insert` or `string.sub`). If a test suite requires core library mutation, you must run `sinq` with the `--safe` (`-s`) flag to force a hard VM reset on every request. Also, you should probably reconsider if whatever you're doing **really** requires core library mutation.
