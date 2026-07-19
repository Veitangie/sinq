# sinq Lua API & Type Translation

Because `sinq` bridges a Go runtime with a Lua Virtual Machine, data must be translated back and forth across the boundary. `sinq` provides a sandboxed Lua environment to give you scriptable control over the HTTP request lifecycle.

To ensure thread safety and prevent side-effect leaks between concurrent scenarios, **the API is strictly scoped**. Certain functions are only available during specific lifecycle hooks.

---

## 1. Global State & Environment

These variables and functions are available globally in **all** script blocks.

### `env`
A table containing the environment variables configured for the current scenario merged with all the values passed via the `-e` / `--env` flags. Modifications made to this table from a user script persist for the lifetime of the scenario.
```lua
-- Example usage in a script or inline string interpolation
local host = env.BASE_URL
```

### `secrets`
A table containing sensitive values passed to `sinq` via the `-s` / `--secret` / `--secrets-file` CLI flags.

### `req` and `res` (Current Request Context)
Shorthands for the *current* request and response being processed. 
* `req`: Used in `$PRE` to modify the outgoing request (e.g., `req.attach()`).
* `res`: A direct reference to `sinq.responses[%current%]`.

### Flow Control
* **`sinq.setNextRequest(index)`**: Alters execution flow. The next request executed will be the one at the specified `index` (1-based). Useful for building loops or conditional skips within a scenario.
* **`sinq.finishScenario()`**: Alters execution flow. Tells `sinq` to finish the scenario once the current request completes its life cycle. Useful for gracefully finishing loops or conditional scenario shutdowns.

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
* **`req.saveResponseTo(filepath)`**: Streams the upcoming response body directly to disk, bypassing the Lua memory buffer. Ideal for downloading large files. If used, `bodyRaw` and JSON methods will not be available in subsequent hooks.
* **`req.cache(bool?)`**: Turns on/off client-side request caching. The cache is based on the data sent over the wire and any attached filenames (attach, saveResponseTo). The parameter defaults to `true` if omitted.
* **`req.skip(bool?)`**: If `true` (default), marks the request to be skipped. The `$PRE` script will finish executing, but the HTTP request will not be fired and subsequent hooks are bypassed. The request is marked as `Aborted` in the reporter without throwing a test failure.

> *Both of the file functions expect the path to be relative to the current file. Passing in an absolute path will fail*

> **Stupid Stuff**: Due to the caching being based on go's `singleflight` package, there exists at least one edge case that I'm aware of that can break tests. If multiple scenarios get to the cached request at the same time, and the scenario with the lowest timeout picks up the request, it's possible that it times out, and every other scenario will fail with a timeout error despite having time. To prevent this, make sure all the scenarios that have cached requests in their path have roughly the same timeouts, or disable caching dynamically for the scenarios with significantly different timeouts.

### `$RETRY` (Polling Phase)
Executes after receiving a response. The script **must** return a number indicating how many milliseconds to wait before retrying, or a negative number to stop.

* **`sinq.retry.stop`**: A constant (`-1`) indicating the retry loop should break immediately.
* **`sinq.retry.when(condition, delay?)`**
  * Retries if `condition` is true. `delay` defaults to `1000ms`.
* **`sinq.retry.whenExponential(condition, base?, constant?)`**
  * Retries if `condition` is true, using exponential backoff (`base ^ attempt * constant`).
  * `base` defaults to `2` (Max `10`). `constant` defaults to `500ms`.
* **`sinq.retry.withJitter(condition, range?, delegate?, delegate_args...)`**
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
* **`sinq.assert.code(expectedHttpCode, message?)`**: Fails if the actual status code does not match.
* **`sinq.assert.equals(actual, expected, message?)`**: Fails if `actual` does not strictly equal `expected`. Recursively compares nested tables.
* **`sinq.assert.contains(string, substring, message?)`**: Fails if the string does not contain the specified substring.
* **`sinq.assert.isTrue(condition, message?)`**: Fails if the condition resolves to `false` or `nil`.
* **`sinq.assert.fileMatches(filepath)`**: Fails if the response previously saved using `req.saveResponseTo()` does not exactly match the contents of `filepath`. Fails immediately if `req.saveResponseTo()` was not called.

### `$POST` (State Extraction Phase)
Executes after a successful `$ASSERT` phase. Typically used to parse the final response payload and store relevant data in the global sandbox for subsequent requests. No special scoped APIs are injected here.

---

## 5. The Responses Table (`sinq.responses`)

When an HTTP request completes, `sinq` parses the response and injects it into the `sinq.responses` table at the index corresponding to the request number. Lua is 1-indexed, meaning the response to the first request in your scenario is accessed via `sinq.responses[1]`.

> **Note:** A response object only exists *after* the request has been executed. Accessing `sinq.responses[2]` or the alias `res` during the `$PRE` hook of the second request will return nil.

### Response Object Structure
* `attempt` *(number)*: The current execution attempt (useful during `$RETRY`).
* `code` *(number)*: The HTTP status code (e.g., `200`, `404`).
* `oversized` *(boolean | nil)*: `true` if the payload exceeded the scenario's `max_body_size` limit and was safely truncated.

### Body Access Methods
*Note: These are only available if `req.saveResponseTo()` was NOT used in the `$PRE` hook.*

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

## 6. Extensions Quick Reference

* `sinq.time.ms`, `sinq.time.second`, `sinq.time.minute`, `sinq.time.hour`
* `sinq.time.now()`
* `sinq.time.fromString(str, format?)`
* `sinq.time.toString(ms, format?)`
* `sinq.crypto.base64Encode(string)`
* `sinq.crypto.base64Decode(string)`
* `sinq.crypto.base64UrlEncode(string)`
* `sinq.crypto.base64UrlDecode(string)`
* `sinq.crypto.hexEncode(string)`
* `sinq.crypto.hexDecode(string)`
* `sinq.crypto.md5(string, encoding?)`
* `sinq.crypto.sha1(string, encoding?)`
* `sinq.crypto.sha256(string, encoding?)`
* `sinq.crypto.sha512(string, encoding?)`
* `sinq.crypto.hmac(source, algo?, key?, encoding?)`
* `sinq.jwt.decode(token)`
* `sinq.jwt.verify(token, key, algo?)`
* `sinq.jwt.sign(claimsTable, key, method?)`
* `sinq.json.parse(string)`
* `sinq.json.serialize(table, indent?)`

---

## 7. Time API (`sinq.time.*`)

Built-in constants and functions to make time-based logic and parsing possible.

### Constants
* **`sinq.time.ms`** (1)
* **`sinq.time.second`** (1000)
* **`sinq.time.minute`** (60000)
* **`sinq.time.hour`** (3600000)

> **Note:** Lua uses `float64` for numbers. When converting a timestamp from milliseconds to another unit (e.g., seconds) using division, use `math.floor` to ensure a clean integer: `math.floor(sinq.time.now() / sinq.time.second)`.

### Functions
* **`sinq.time.now()`**: Returns the current UNIX timestamp.
  * **Returns:** `number` (milliseconds since epoch).
* **`sinq.time.fromString(str, format?)`**: Parses a time string into a UNIX timestamp (milliseconds). The `format` string is optional.
  * **Returns:** `(number, error)`
  * **Format Rules:** Uses [Go's time layout rules](https://pkg.go.dev/time#pkg-constants). If omitted, defaults to ISO8601 (`2006-01-02T15:04:05.000Z07:00`).
* **`sinq.time.toString(ms, format?)`**: Formats a UNIX timestamp (milliseconds) into a time string. The `format` string is optional.
  * **Returns:** `string`
  * **Format Rules:** Uses [Go's time layout rules](https://pkg.go.dev/time#pkg-constants). If omitted, defaults to ISO8601.

---

## 8. Crypto API (`sinq.crypto.*`)

Provides standard cryptographic encoding and hashing functions.

### Encoding
* **`sinq.crypto.base64Encode(string)`**: Encodes a string into standard Base64.
  * **Returns:** `string`
* **`sinq.crypto.base64Decode(string)`**: Decodes a standard Base64 string.
  * **Returns:** `(string, error)`
* **`sinq.crypto.base64UrlEncode(string)`**: Encodes a string into URL-safe Base64.
  * **Returns:** `string`
* **`sinq.crypto.base64UrlDecode(string)`**: Decodes a URL-safe Base64 string.
  * **Returns:** `(string, error)`
* **`sinq.crypto.hexEncode(string)`**: Encodes a string into a hexadecimal representation.
  * **Returns:** `string`
* **`sinq.crypto.hexDecode(string)`**: Decodes a hexadecimal string.
  * **Returns:** `(string, error)`

### Hashing
* **`sinq.crypto.md5(string, encoding?)`**, **`sinq.crypto.sha1(string, encoding?)`**, **`sinq.crypto.sha256(string, encoding?)`**, **`sinq.crypto.sha512(string, encoding?)`**: Computes the cryptographic hash of the input string.
  * **Returns:** `(string, error)`
  * **Parameters:** `encoding` defaults to `"hex"`. Supported values are `"hex"`, `"base64"`, `"base64url"`, and `"raw"`.
  * **Note:** Since it defaults to `"hex"`, the output is safe to print and transmit. If `"raw"` is used, the function returns the raw bytes.
  * **Throws:** Returns an error string as the second value if an unknown encoding string is provided.
* **`sinq.crypto.hmac(source, algo?, key?, encoding?)`**: Computes the HMAC of the source string.
  * **Returns:** `(string, error)`
  * **Parameters:** `algo` defaults to `"sha256"`. Supported values are `"sha256"`, `"sha1"`, `"sha512"`, and `"md5"`. `key` defaults to `""`. `encoding` defaults to `"hex"`. Supported values are `"hex"`, `"base64"`, `"base64url"`, and `"raw"`.
  * **Throws:** Returns an error string as the second value if an unknown algorithm or encoding string is provided.

---

## 9. JWT API (`sinq.jwt.*`)

Allows for generation, decoding, and validation of JSON Web Tokens natively within your scenario flow.

* **`sinq.jwt.decode(token)`**: Decodes a JWT token without validating its signature. 
  * **Returns:** `(table, error)`
  * **Table Structure:** Contains `header`(table), `claims` (table), `signature` (string), and `method` (string).
* **`sinq.jwt.verify(token, key, algo?)`**: Verifies the token using the provided key and optional algorithm constraint.
  * **Returns:** `(table, error)`
  > **Note:** Symmetric algorithms (`HS*`) use raw string keys. Asymmetric algorithms (`RS*`, `ES*`, `EdDSA`) require PEM-encoded public keys.
* **`sinq.jwt.sign(claimsTable, key, method?)`**: Creates a signed JWT string. Returns `string, error`.
  * `claimsTable`: A Lua table representing the JWT payload.
  * `key`: The signing key string.
  * `method?`: The signing algorithm. Defaults to `HS256`. 
  > **Note:** The `claimsTable` must have strictly string keys. Mixing list-style (integer) indices with string keys in Lua will cause parsing to fail and return an error. Asymmetric algorithms require PEM-encoded private keys.
  > **Warning:** Passing a cyclic table as the `claimsTable` results in Undefined Behavior (UB). Currently, recursive references are safely dropped during conversion, but this behavior is not guaranteed and may change in future versions.

---

## 10. JSON Utilities (`sinq.json.*`)

The `sinq.json` table provides explicit methods to parse and serialize JSON data from Lua.

* **`sinq.json.parse(string)`**: Parses a JSON string into a Lua table. Returns `table, error`.
* **`sinq.json.serialize(table, indent?)`**: Serializes a Lua table into a JSON string. Returns `string, error`.
  * `indent?`: Optional string used for formatting (e.g., `"  "`). If omitted, produces compact JSON. If present, also introduces newlines between object and array entries.
  > **Note:** Passing a cyclic table will immediately return an error (`"Cycle detected, unable to serialize"`).
* **`sinq.json.null`**: A special constant representing a JSON `null` value, allowing Lua tables to explicitly serialize `null` properties (since standard Lua drops `nil` table keys). Tables, parsed from JSON will also include this constant to represent explicit `null`. Can be compared with standard `==` operator (`sinq.assert.isTrue(res.json().myNull == sinq.json.null)`)

---

## 11. Fake Data Generation (`sinq.fake.*`)

The `sinq.fake` table exposes deterministic fake data generators. All generators respect the current seed.

#### Primitives & Core Data
* **`sinq.fake.uuid()`**: Returns a random UUIDv4 string.
* **`sinq.fake.int(min?, max?)`**: Returns a random integer.
* **`sinq.fake.float(min?, max?)`**: Returns a random float.
* **`sinq.fake.shakespeare()`**: Returns a random boolean (`true` or `false`).
* **`sinq.fake.oneOf(array)`**: Accepts a Lua array (table with integer keys) and returns a random element.

#### Networking & Web
* **`sinq.fake.email()`**: Returns a random email address.
* **`sinq.fake.ipv4()`**: Returns a random IPv4 address.
* **`sinq.fake.ipv6()`**: Returns a random IPv6 address.
* **`sinq.fake.url()`**: Returns a random URL string.
* **`sinq.fake.userAgent()`**: Returns a random User-Agent string.
* **`sinq.fake.trace()`**: Returns a random W3C traceparent header string.
* **`sinq.fake.username()`**: Returns a random username.
* **`sinq.fake.password()`**: Returns a random password.

#### Identity & Text
* **`sinq.fake.name()`**: Returns a full name.
* **`sinq.fake.firstName()`**: Returns a first name.
* **`sinq.fake.lastName()`**: Returns a last name.
* **`sinq.fake.phone()`**: Returns a random phone number.
* **`sinq.fake.address()`**: Returns a full address.
* **`sinq.fake.company()`**: Returns a company name.
* **`sinq.fake.word()`**: Returns a single random word.


#### Time & Configuration
* **`sinq.fake.timestamp(timeMs)`**: Returns a UNIX timestamp (integer milliseconds) before the given timestamp.
* **`sinq.fake.setSeed(int64)`**: Seeds the fake data generator to ensure deterministic output across runs.

#### Additional Randomness
* **`math.random(max?)`, `math.random(min, max)`**: Lua's standard way of generating pseudo-random data is present in `sinq` and always available.

---

## 12. Libraries

`sinq` does not load two of common core Lua libraries - `io` and `os` by default. This is done in order to prevent `.sinq` scripts from becoming a safety concern when ran without due diligence. To enable these libraries in Lua scripts use `--unrestricted` flag, and only run trusted scripts with this flag.

`sinq` allows users to import external Lua packages. For them to be accessible via the `require("package")` calls, path to the directory containing the files for the package should be passed to `sinq` via the environment variable `SINQ_LUA_PATH` or via the `--plugins` flag. If both present, the flag takes precedence. The path is expected to consist of plain paths to the directories joined with `;`. Several `--plugins` flags can be passed on startup, which will result in an aggregated list of all paths within them.

### State Isolation
By default, `sinq` reuses the Lua `LState` to maximize performance. Do not mutate core Lua functions (e.g., overwriting `table.insert` or `string.sub`) or imported package data.

If a test suite requires core or external library mutation, you must run `sinq` with the `--safe` (`-s`) flag to force a hard VM reset on every request. Also, you should probably reconsider if whatever you're doing **really** requires core library mutation. **Note, that passing modified state between scenarios via library mutation is considered UB, and may change in any future release**
