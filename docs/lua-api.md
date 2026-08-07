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

### Standard Output (`print` & `io.write`)
The standard Lua `print` function (and `io.write` if the `io` library is enabled via `--unrestricted`) is available in all scripts. However, since the runner executes all scenarios in parallel, just printing everything out results in a poor experience. To accommodate the best practices of debugging with `print` statements `sinq` provides a `-p` / `--print` flag, which buffers all the output in memory and reports it as part of the scenario result. Without the flag all the calls to these functions are discarded.

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
```http
GET ${env.BASE_URL}/users/${CREATED_USER_ID}
Authorization: Bearer ${secrets.API_KEY}
```

!!! note
    Calling functions inside an inline script can make it ambiguous for the compiler to determine if a `return` should be prepended. In those cases, you must explicitly write `return`.

**Multi-line Dynamic Generation:**
For complex logic, use explicit returns:
```http
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

!!! note
    Inline scripts must return a value. Returning nothing will fail the request materialization.

---

## 4. Lifecycle-Specific APIs

The following APIs are dynamically injected and destroyed depending on the execution phase of the request.

### `$PRE` (Setup Phase)
Executes before the HTTP request is materialized. Used for file I/O operations.

* **`req.attach(filepath string)`**: Replaces the request body with the contents of the specified file. *Note: Fails if a textual body is already defined in the `.sinq` file.*
* **`req.saveResponseTo(filepath string)`**: Streams the upcoming response body directly to disk, bypassing the Lua memory buffer. Ideal for downloading large files. Automatically creates missing directories in the filepath. If used, `bodyRaw` and JSON methods will not be available in subsequent hooks.
* **`req.cache(enable bool?)`**: Turns on/off client-side request caching. The cache is based on the data sent over the wire and any attached filenames (attach, saveResponseTo). The parameter defaults to `true` if omitted.
* **`req.skip(enable bool?)`**: Marks the request to be skipped. Parameter defaults to `true` if omitted. The `$PRE` script will finish executing, but the HTTP request will not be fired and subsequent hooks are bypassed. The request is marked as `Aborted` in the reporter without throwing a test failure.

!!! note
    Both of the file functions expect the path to be relative to the current file. Passing in an absolute path will fail.

### `$RETRY` (Polling Phase)
Executes after receiving a response. The script **must** return a number indicating how many milliseconds to wait before retrying, or a negative number to stop.

* **`sinq.retry.stop`**: A constant (`-1`) indicating the retry loop should break immediately.
* **`sinq.retry.when(condition boolean, delay number?)`**
    * Retries if `condition` is true. `delay` defaults to `500ms`.
* **`sinq.retry.whenExponential(condition boolean, base number?, constant number?)`**
    * Retries if `condition` is true, using exponential backoff (`base ^ attempt * constant`).
    * `base` defaults to `2` (Max `10`). `constant` defaults to `500ms`.
* **`sinq.retry.withJitter(condition boolean, range number?, delegate function?, delegate_args any...)`**
    * Adds randomized jitter to a retry calculation to prevent thundering herd problems.
    * `range` defaults to `50` (±50ms jitter). `delegate` defaults to `sinq.retry.when`, delegate will be passed condition and delegate_args when called.
    * Usage is: `sinq.retry.withJitter(res.code ~= 200, 100, sinq.retry.when, 2 * sinq.second)` - jitter conditional retry with range of [-100:100]

### `$ASSERT` (Validation Phase)
Executes after the retry loop finishes. Used to validate the final state of the response.

* **`sinq.assert.fail(reason string)`**: Marks the test as failed with the provided reason. **Note: This does not halt Lua execution.** The rest of the `$ASSERT` block will continue to run, allowing you to collect multiple failure reasons for a single request.
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
* **`sinq.assert.code(expectedHttpCode number, message string?)`**: Fails if the actual status code does not match.
* **`sinq.assert.equals(actual any, expected any)`**: Fails if `actual` does not equal `expected`. When comparing tables, checks that every key-value pair in `expected` recursively matches those in `actual`, but ignores pairs from `actual` not present in `expected`.
* **`sinq.assert.contains(source string, substring string, message string?)`**: Fails if the string does not contain the specified substring.
* **`sinq.assert.isTrue(condition boolean, message string?)`**: Fails if the condition resolves to `false` or `nil`.
* **`sinq.assert.fileMatches(filepath string)`**: Fails if the response previously saved using `req.saveResponseTo()` does not exactly match the contents of `filepath`. Fails immediately if `req.saveResponseTo()` was not called.

### `$POST` (State Extraction Phase)
Executes after a successful `$ASSERT` phase. Typically used to parse the final response payload and store relevant data in the global sandbox for subsequent requests. No special scoped APIs are injected here.

---

## 5. The Responses Table (`sinq.responses`)

When an HTTP request completes, `sinq` parses the response and injects it into the `sinq.responses` table at the index corresponding to the request number. Lua is 1-indexed, meaning the response to the first request in your scenario is accessed via `sinq.responses[1]`.

!!! note
    A response object only exists *after* the request has been executed. Accessing `sinq.responses[2]` or the alias `res` during the `$PRE` hook of the second request will return nil.

### Response Object Structure
* `attempt` *(number)*: The current execution attempt (useful during `$RETRY`).
* `code` *(number)*: The HTTP status code (e.g., `200`, `404`).
* `oversized` *(boolean | nil)*: `true` if the payload exceeded the scenario's `max_body` limit and was safely truncated.

### Body Access Methods

!!! note
    These are only available if `req.saveResponseTo()` was NOT used in the `$PRE` hook.

* `bodyRaw` *(string)*: The raw string of the response payload.
* `extractBodyJson()` *(function)*: Safely attempts to parse `bodyRaw` into a Lua table.
    * **Returns:** `(result table, error string)`
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

!!! note "JSON Blindspot (1-Indexed Arrays)"
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

* [`sinq.time.ms`](#constants) / [`sinq.time.second`](#constants) / [`sinq.time.minute`](#constants) / [`sinq.time.hour`](#constants)
* [`sinq.time.now()`](#functions)
* [`sinq.time.fromString(str string, format string?)`](#functions)
* [`sinq.time.toString(ms number, format string?)`](#functions)
* [`sinq.crypto.base64Encode(source string)`](#encoding)
* [`sinq.crypto.base64Decode(source string)`](#encoding)
* [`sinq.crypto.base64UrlEncode(source string)`](#encoding)
* [`sinq.crypto.base64UrlDecode(source string)`](#encoding)
* [`sinq.crypto.hexEncode(source string)`](#encoding)
* [`sinq.crypto.hexDecode(source string)`](#encoding)
* [`sinq.crypto.md5(source string, encoding string?)`](#hashing)
* [`sinq.crypto.sha1(source string, encoding string?)`](#hashing)
* [`sinq.crypto.sha256(source string, encoding string?)`](#hashing)
* [`sinq.crypto.sha512(source string, encoding string?)`](#hashing)
* [`sinq.crypto.hmac(source string, algo string?, key string?, encoding string?)`](#hashing)
* [`sinq.jwt.decode(token string)`](#9-jwt-api-sinqjwt)
* [`sinq.jwt.verify(token string, key string, algo string?)`](#9-jwt-api-sinqjwt)
* [`sinq.jwt.sign(claimsTable table, key string, method string?)`](#9-jwt-api-sinqjwt)
* [`sinq.json.parse(source string)`](#10-json-utilities-sinqjson)
* [`sinq.json.serialize(tbl table, indent string?)`](#10-json-utilities-sinqjson)

---

## 7. Time API (`sinq.time.*`)

Built-in constants and functions to make time-based logic and parsing possible.

### Constants
* **`sinq.time.ms`** (1)
* **`sinq.time.second`** (1000)
* **`sinq.time.minute`** (60000)
* **`sinq.time.hour`** (3600000)

!!! note
    Lua uses `float64` for numbers. When converting a timestamp from milliseconds to another unit (e.g., seconds) using division, use `math.floor` to ensure a clean integer: `math.floor(sinq.time.now() / sinq.time.second)`.

### Functions
* **`sinq.time.now()`**: Returns the current UNIX timestamp.
    * **Returns:** `number` (milliseconds since epoch).
* **`sinq.time.fromString(str string, format string?)`**: Parses a time string into a UNIX timestamp (milliseconds).
    * **Returns:** `(result number, error string)`
    * **Format Rules:** Uses [Go's time layout rules](https://pkg.go.dev/time#pkg-constants). If omitted, defaults to ISO8601 (`2006-01-02T15:04:05.000Z07:00`).
* **`sinq.time.toString(ms number, format string?)`**: Formats a UNIX timestamp (milliseconds) into a time string.
    * **Returns:** `string`
    * **Format Rules:** Uses [Go's time layout rules](https://pkg.go.dev/time#pkg-constants). If omitted, defaults to ISO8601.

---

## 8. Crypto API (`sinq.crypto.*`)

Provides standard cryptographic encoding and hashing functions.

### Encoding
* **`sinq.crypto.base64Encode(source string)`**: Encodes a string into standard Base64.
    * **Returns:** `string`
* **`sinq.crypto.base64Decode(source string)`**: Decodes a standard Base64 string.
    * **Returns:** `(result string, error string)`
* **`sinq.crypto.base64UrlEncode(source string)`**: Encodes a string into URL-safe Base64.
    * **Returns:** `string`
* **`sinq.crypto.base64UrlDecode(source string)`**: Decodes a URL-safe Base64 string.
    * **Returns:** `(result string, error string)`
* **`sinq.crypto.hexEncode(source string)`**: Encodes a string into a hexadecimal representation.
    * **Returns:** `string`
* **`sinq.crypto.hexDecode(source string)`**: Decodes a hexadecimal string.
    * **Returns:** `(result string, error string)`

### Hashing
* **`sinq.crypto.md5(source string, encoding string?)`**, **`sinq.crypto.sha1(source string, encoding string?)`**, **`sinq.crypto.sha256(source string, encoding string?)`**, **`sinq.crypto.sha512(source string, encoding string?)`**: Computes the cryptographic hash of the input string.
    * **Returns:** `(result string, error string)`
    * **Parameters:** `encoding` string defaults to `"hex"`. Supported values are `"hex"`, `"base64"`, `"base64url"`, and `"raw"`.
    
    !!! note
        Since it defaults to `"hex"`, the output is safe to print and transmit. If `"raw"` is used, the function returns the raw bytes.

* **`sinq.crypto.hmac(source string, algo string?, key string?, encoding string?)`**: Computes the HMAC of the source string.
    * **Returns:** `(result string, error string)`
    * **Parameters:** `algo` string defaults to `"sha256"`. Supported values are `"sha256"`, `"sha1"`, `"sha512"`, and `"md5"`. `key` string defaults to `""`. `encoding` string defaults to `"hex"`. Supported values are `"hex"`, `"base64"`, `"base64url"`, and `"raw"`.

---

## 9. JWT API (`sinq.jwt.*`)

Allows for generation, decoding, and validation of JSON Web Tokens natively within your scenario flow.

* **`sinq.jwt.decode(token string)`**: Decodes a JWT token without validating its signature. 
    * **Returns:** `(result table, error string)`
    * **Table Structure:** Contains `header`(table), `claims` (table), `signature` (string), and `method` (string).
* **`sinq.jwt.verify(token string, key string, algo string?)`**: Verifies the token using the provided key and optional algorithm constraint.
    * **Returns:** `(result table, error string)`
    
    !!! note
        Symmetric algorithms (`HS*`) use raw string keys. Asymmetric algorithms (`RS*`, `ES*`, `EdDSA`) require PEM-encoded public keys.

* **`sinq.jwt.sign(claimsTable table, key string, method string?)`**: Creates a signed JWT string.
    * **Returns:** `(result string, error string)`
    * `claimsTable`: A Lua table representing the JWT payload.
    * `key` string: The signing key string.
    * `method` string?: The signing algorithm. Defaults to `HS256`. 
    
    !!! note
        The `claimsTable` must have strictly string keys. Mixing list-style (integer) indices with string keys in Lua will cause parsing to fail and return an error. Asymmetric algorithms require PEM-encoded private keys.
    
    !!! warning
        Passing a cyclic table as the `claimsTable` will result in a serialization error being returned as a second return value (`nil, "Failed to serialize..."`). It is safe and will not crash the runner, but the token will not be generated.

---

## 10. JSON Utilities (`sinq.json.*`)

The `sinq.json` table provides explicit methods to parse and serialize JSON data from Lua.

* **`sinq.json.parse(source string)`**: Parses a JSON string into a Lua table.
    * **Returns:** `(result table, error string)`
* **`sinq.json.serialize(tbl table, indent string?)`**: Serializes a Lua table into a JSON string.
    * **Returns:** `(result string, error string)`
    * `indent` string?: Optional string used for formatting (e.g., `"  "`). If omitted, produces compact JSON. If present, also introduces newlines between object and array entries.
    
    !!! note
        Passing a cyclic table will immediately return an error (`"Cycle detected, unable to serialize"`).

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
* **`sinq.fake.timestamp(fromMs, toMs?)`**: Returns a random UNIX timestamp (integer milliseconds) between `fromMs` and `toMs`. If `toMs` is omitted, it defaults to the current time.
* **`sinq.fake.setSeed(int64)`**: Seeds the fake data generator to ensure deterministic output across runs.

#### Additional Randomness
* **`math.random(max?)`, `math.random(min, max)`**: Lua's standard way of generating pseudo-random data is present in `sinq` and always available.

---

## 12. Libraries

`sinq` does not load two of common core Lua libraries - `io` and `os` by default. This is done in order to prevent `.sinq` scripts from becoming a safety concern when run without due diligence. To enable these libraries in Lua scripts use `--unrestricted` flag, and only run trusted scripts with this flag.

`sinq` allows you to import external Lua packages. To make them accessible via `require("package")`, you must provide the directory paths containing those packages.

You can do this using the `SINQ_LUA_PATH` environment variable or the `--plugins` CLI flag (which takes precedence). Multiple paths should be separated by a colon (`:`) on macOS/Linux, or a semicolon (`;`) on Windows. You can also pass the `--plugins` flag multiple times to aggregate paths.

!!! note
    All paths passed to `sinq` as positional arguments and the current working directory also get appended to the end of path for the purposes of searching for Lua plugins. So if you run `sinq` from a directory containing a file `my-module.lua`, `require("my-module")` will work for all `.sinq` files.
