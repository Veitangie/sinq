# sinq Lua API & Type Translation

Because `sinq` bridges a Go runtime with a Lua Virtual Machine, data must be translated back and forth across the boundary. `sinq` provides a sandboxed Lua environment to give you scriptable control over the HTTP request lifecycle.

To ensure thread safety and prevent side-effect leaks between concurrent scenarios, **the API is strictly scoped**. Certain functions are only available during specific lifecycle hooks.

---

## 1. Global State & Environment

These variables and functions are available globally in **all** script blocks.

### `env`
A table containing the environment variables configured for the current scenario. Modifications made to this table from a user script persist for the lifetime of the scenario.
```lua
-- Example usage in a script or inline string interpolation
local host = env.BASE_URL
```

### `secrets`
A table containing sensitive values passed to `sinq` via the `-S` / `--secrets` CLI flag.

### `req` and `res` (Current Request Context)
Shorthands for the *current* request and response being processed. 
* `req`: Used in `$PRE` to modify the outgoing request (e.g., `req.attach()`).
* `res`: A direct reference to `sinq.responses[%current%]`.

### Flow Control
* **`sinq.setNextRequest(index)`**: Alters execution flow. The next request executed will be the one at the specified `index` (1-based). Useful for building loops or conditional skips within a scenario.

---

## 2. Variable Scoping (Local vs. Global)

To prevent cache poisoning and unintended side effects, you should strictly control your variable scoping.

* **Temporary Math & Logic:** Use the `local` keyword. This ensures the variable is garbage-collected immediately after the script block finishes.
  ```lua
  local data = res.json()
  local id = data.id
  ```
* **Passing State Across Files:** If you need a value from `01_login.sinq` to be accessible in `02_action.sinq`, you must declare it globally (without `local`). It will be attached to the Lua sandbox for the lifespan of that specific scenario (`_G`).
  ```lua
  AUTH_TOKEN = res.json().token
  -- Equivalent to: _G.AUTH_TOKEN = ...
  ```

---

## 3. Inline Scripts (Request Templating)

Aside from lifecycle hooks, you can use General/Inline scripts to dynamically build your HTTP requests. These are evaluated *after* `$PRE` but *before* the request is sent. The return value of these scripts is injected directly into the raw HTTP text.

You can name them (e.g., `$MY_SCRIPT{...}`) or leave them anonymous (`${...}`).

**Single-line Interpolation:**
If an inline script fails to compile, `sinq` automatically prepends `return ` and retries. This allows for clean, single-line variable interpolation:
```text
GET ${env.BASE_URL}/users/${CREATED_USER_ID}
Authorization: Bearer ${secrets.API_KEY}
```

**Multi-line Dynamic Generation:**
For complex logic, use explicit returns:
```text
POST ${env.BASE_URL}/users
Content-Type: application/json

{
    "email": "$GENERATE_EMAIL{
        local random_num = math.random(1000, 9999)
        return 'testuser_' .. random_num .. '@example.com'
    }",
    "role": "admin"
}
```
*Note: Inline scripts must return a value. Returning nothing will fail the request materialization.*

---

## 4. Lifecycle-Specific APIs

The following APIs are dynamically injected and destroyed depending on the execution phase of the request.

### `$PRE` (Setup Phase)
Executes before the HTTP request is materialized. Used for file I/O operations.
* **`req.attach(filepath)`**: Replaces the request body with the contents of the specified file. *Note: Fails if a textual body is already defined in the `.sinq` file.*
* **`res.saveTo(filepath)`**: Streams the upcoming response body directly to disk, bypassing the Lua memory buffer. Ideal for downloading large files. If used, `bodyRaw` and JSON methods will not be available in subsequent hooks.
> *Both of these functions expect the path to be relative to the filesystem root passed to sinq on startup.*

### `$RETRY` (Polling Phase)
Executes after receiving a response. The script **must** return a number indicating how many milliseconds to wait before retrying, or a negative number to stop.

* **`sinq.retry.stop`**: A constant (`-1`) indicating the retry loop should break immediately.
* **`sinq.retry.when(condition, delay)`**
  * Retries if `condition` is true. `delay` defaults to `1000ms`.
* **`sinq.retry.whenExponential(condition, base, constant)`**
  * Retries if `condition` is true, using exponential backoff (`base ^ attempt * constant`).
  * `base` defaults to `2` (Max `10`). `constant` defaults to `500ms`.
* **`sinq.retry.withJitter(condition, range, delegate, delegate_args...)`**
  * Adds randomized jitter to a retry calculation to prevent thundering herd problems.
  * `range` defaults to `50` (±50ms jitter). `delegate` defaults to `sinq.retry.when`, delegate will be passed condition and delegate_args when called.
  * Usage is: `sinq.retry.withJitter(res.code ~= 200, 100, sinq.retry.when, 2 * sinq.second)` - jitter conditional retry with range of [-200:200]


### `$ASSERT` (Validation Phase)
Executes after the retry loop finishes. Used to validate the final state of the response.

* **`sinq.assert.fail(reason)`**: Marks the test as failed with the provided reason. **Note: This does not halt Lua execution.** The rest of the `$ASSERT` block will continue to run, allowing you to collect multiple failure reasons for a single request.
  ```lua
  $ASSERT{
      local data = res.json()
      if data.id == nil then
          sinq.assert.fail("ID is missing")
      end
      if data.status ~= "active" then
          sinq.assert.fail("User is not active") 
      end
      -- If both conditions are met, the report will show TWO failures for this request.
  }
  ```
* **`sinq.assert.code(expectedHttpCode)`**: Fails if the actual status code does not match.
* **`sinq.assert.equals(actual, expected)`**: Fails if `actual` does not strictly equal `expected`. Recursively compares nested tables.
* **`sinq.assert.contains(string, substring)`**: Fails if the string does not contain the specified substring.
* **`sinq.assert.isTrue(condition)`**: Fails if the condition resolves to `false` or `nil`.

### `$POST` (State Extraction Phase)
Executes after a successful `$ASSERT` phase. Typically used to parse the final response payload and store relevant data in the global sandbox for subsequent requests. No special scoped APIs are injected here.

---

## 5. The Responses Table (`sinq.responses`)

When an HTTP request completes, `sinq` parses the response and injects it into the `sinq.responses` table at the index corresponding to the request number. Lua is 1-indexed, meaning the response to the first request in your scenario is accessed via `sinq.responses[1]`.

> **Note:** A response object only exists *after* the request has been executed. Accessing `sinq.responses[2]` or the alias `res` during the `$PRE` hook of the second request will return a table with a single method - `saveTo`.

### Response Object Structure
* `attempt` *(number)*: The current execution attempt (useful during `$RETRY`).
* `code` *(number)*: The HTTP status code (e.g., `200`, `404`).
* `oversized` *(boolean | nil)*: `true` if the payload exceeded the scenario's `max_body_size` limit and was safely truncated.

### Body Access Methods
*Note: These are only available if `res.saveTo()` was NOT used in the `$PRE` hook.*

* `bodyRaw` *(string)*: The raw string of the response payload.
* `extractBodyJson()` *(function)*: Safely attempts to parse `bodyRaw` into a Lua table.
  * **Returns:** `(table, error)`
* `json()` *(function)*: An unsafe convenience wrapper around `extractBodyJson`. 
  * **Returns:** `table` directly. 
  * **Throws:** Calls a fatal `error()` if the body is not valid JSON, failing the scenario immediately.

### HTTP Headers Translation
HTTP headers are complex because a single key can have multiple values. `sinq` handles this translation automatically.

* **Single Value Headers:** Translated to a standard Lua string.
  ```lua
  local contentType = res.headers["Content-Type"]
  ```
* **Multi-Value Headers:** Translated to a 1-indexed Lua table (array) of strings.
  ```lua
  local firstCookie = res.headers["Set-Cookie"][1]
  ```

### The JSON Blindspot (1-Indexed Arrays)
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
    local data = res.json()
    local first_id = data[1].id
    
    if first_id ~= 42 then 
        sinq.assert.fail("ID mismatch") 
    end
}
```

---

## 6. Time Constants

Built-in constants to make time-based logic highly readable, especially during `$RETRY` phases.

* `sinq.ms` = 1
* `sinq.second` = 1000
* `sinq.minute` = 60000
* `sinq.hour` = 3600000

---

## 7. Standard Library Integrity

By default, `sinq` reuses the Lua `LState` to maximize performance. Do not mutate core Lua functions (e.g., overwriting `table.insert` or `string.sub`). 

If a test suite requires core library mutation, you must run `sinq` with the `--safe` (`-s`) flag to force a hard VM reset on every request. Also, you should probably reconsider if whatever you're doing **really** requires core library mutation.
