# sinq Lua API & Type Translation

`sinq` runs your Lua in a sandbox and translates values across the Go/Lua boundary. The API is **scoped**: some functions are only available inside specific lifecycle hooks, which keeps concurrent scenarios from leaking state into each other.

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
* **`sinq.setNextRequest(index)`**: Alters execution flow. The next request executed will be the one at the specified `index` (1-based). Useful for building loops or conditional skips within a scenario. It can be called from any hook of the current request, including `$PRE`, which is the place to drive a loop whose runs may call `req.skip()`, since `$POST` doesn't run for a skipped request. A request that runs more than once is reported once. Its status is the most severe across its runs (error, then failure, then aborted, then success, then skipped). It keeps the assertion failures of every run, its timings and attempt counts add up, and `--dump-on-failure` shows the first failing run.
* **`sinq.finishScenario()`**: Alters execution flow. Tells `sinq` to finish the scenario once the current request completes its life cycle. Useful for gracefully finishing loops or conditional scenario shutdowns.

### Standard Output (`print` & `io.write`)
`print` (and `io.write`, if `io` is enabled via `--unrestricted`) works in every script. Because scenarios run in parallel, printing straight to the terminal would interleave badly, so this output is discarded by default. Pass `-p` / `--print` to buffer it and attach it to the scenario result instead. Like in standard Lua, `print` converts values with `tostring`, so a table with a `__tostring` metamethod (such as `res.error`) prints as its text.

---

## 2. Variable Scoping (Local vs. Global)

Control variable scoping carefully to avoid leaking state between requests.

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

Aside from lifecycle hooks, you can use General/Inline scripts to dynamically build your HTTP requests. These are evaluated *after* `$PRE` but *before* the request is sent. The return value of these scripts is injected directly into the raw HTTP text, converted as described in [Returned Values](#returned-values).

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

### Returned Values

Inline scripts must return a value. Only the **first** returned value is used, so helpers that return `(result, error)` can be used directly: `${ return sinq.json.serialize(t) }` inserts the JSON, not the error. Returning nothing, or `nil` as the first value, fails the request materialization.

The value is inserted according to its type:

| Returned value | Inserted as | Example |
|---|---|---|
| string | the string itself, as is | `return 'abc'` → `abc` |
| number | its Lua text | `return 3.5` → `3.5` |
| boolean | `true` or `false` | `return true` → `true` |
| table | compact JSON, using the same rules as [`sinq.json.serialize`](#10-json-utilities-sinqjson) | `return { id = 7 }` → `{"id":7}` |
| `sinq.json.null` | `null` | `return sinq.json.null` → `null` |
| `nil` or nothing | an error, the request fails | |
| function, channel or coroutine | an error (`unexpected type …`), the request fails | `return print` |

Tables let you pass a JSON sub-document straight through, for example `"address": ${ env.pointA }`. If the table can't be serialized (a mixed or sparse table, a cycle), the request fails with the serialization error. An empty table becomes `{}`; use [`sinq.json.array()`](#10-json-utilities-sinqjson) for `[]`.

!!! warning
    Strings are inserted raw, with no quoting or escaping, because the template supplies the quotes (`"${ name }"`). Don't wrap a table in quotes: `"${ { a = 1 } }"` produces `"{"a":1}"`, which is invalid JSON.

---

## 4. Lifecycle-Specific APIs

The following APIs are dynamically injected and destroyed depending on the execution phase of the request.

### `$PRE` (Setup Phase)
Executes before the HTTP request is materialized. Used for file I/O operations.

* **`req.attach(filepath string)`**: Replaces the request body with the contents of the specified file. *Note: Fails if a textual body is already defined in the `.sinq` file.*
* **`req.saveResponseTo(filepath string)`**: Streams the upcoming response body directly to disk, bypassing the Lua memory buffer. Ideal for downloading large files. Automatically creates missing directories in the filepath. If used, `bodyRaw` and JSON methods will not be available in subsequent hooks.
* **`req.cache(enable bool?)`** / **`req.cache(key string)`**: Turns on client-side response caching for this request, or off with `false`. With no argument, the cache key is derived from the request: the bytes sent over the wire, the cookies sent with it, attached file names (`attach`, `saveResponseTo`), multipart parts and the scenario directory. With a string, that string is the key, used as is: every request that calls `req.cache` with the same key shares one cached response, whatever its method, URL or body. Any other argument (a number, `nil`, a table) raises an error. Calling it again replaces the earlier setting.
    * Each retry attempt is cached separately, so `$RETRY` polling still sends a new request on every attempt.
    * A response served from the cache still counts toward "requests sent" in the summary, even though nothing reached the server.
    * A custom key is not scoped for you. To keep two requests apart, put a prefix in the key yourself: `req.cache("auth:" .. env.phoneNumber)`.

    !!! warning
        The first request to fill a key decides what is stored, including where its body went. If that request used `req.saveResponseTo`, later requests sharing the key get an empty body and their own `saveResponseTo` file isn't written. An error is cached the same way, and nothing is evicted until the run ends. Share a key only between requests that expect the same response.
* **`req.skip(enable bool?)`**: Marks the request to be skipped. Parameter defaults to `true` if omitted. The `$PRE` script will finish executing, but the HTTP request will not be fired and subsequent hooks are bypassed. The request is reported as skipped, without a test failure.
* **`req.multipart(entry table, ...)`**: Builds a `multipart/form-data` request body from one or more part definitions. See [Multipart Bodies](#multipart-bodies) below.
* **`req.multipartBoundary(boundary string)`**: Overrides the randomly generated multipart boundary. See [Multipart Bodies](#multipart-bodies).

!!! note
    `req.attach`, `req.saveResponseTo`, and a multipart entry's `source` all expect a path relative to the current `.sinq` file. Passing an absolute path fails.

#### Multipart Bodies

**`req.multipart(entry table, ...)`** assembles a `multipart/form-data` body. Parts may be passed as positional arguments (`req.multipart(a, b)`) or as a single contiguous array table (`req.multipart{ a, b }`); both forms are equivalent. Repeated calls **accumulate** parts in order, and duplicate field names are allowed.

Each `entry` is a table with these keys (all optional unless noted):

| Key | Type | Meaning |
| --- | --- | --- |
| `name` | string | Form field name. **Required** unless `contentDisposition` is set. |
| `data` | string | Literal part body. Mutually exclusive with `source`. |
| `source` | string | Path to a file whose contents become the part body. Mutually exclusive with `data`. Resolved relative to the current `.sinq` file; absolute paths and paths that do not resolve to a readable file are rejected. |
| `filename` | string \| boolean | A string sets the advertised filename and makes this a *file* part. `true` uses `basename(source)`. Omitted (or `false`) produces a plain *field* part with no `filename` parameter. |
| `contentType` | string | The part's `Content-Type`. If omitted **and** the part has a filename, it is sniffed from the filename extension, then the `source` extension, falling back to `application/octet-stream`. Field parts get no `Content-Type` unless you set one. |
| `contentDisposition` | string | Sets the raw `Content-Disposition` header verbatim. When present, `name` and `filename` are ignored. |
| `headers` | table | Extra per-part headers, e.g. `{ ["X-Chunk"] = "3" }`. Keys are canonicalised; values may be string, number, or boolean. An explicit `contentType` / `contentDisposition` wins over a same-named `headers` entry. |

A non-string value for any of the string-typed keys is an error, as is providing both `data` and `source`, or an entry with neither `name` nor `contentDisposition`. A table that mixes array entries with named keys uses only the array entries.

`name` and `filename` only apply when `sinq` builds the `Content-Disposition` header for you. If you set `contentDisposition` yourself, it is used verbatim and `name` / `filename` on that entry are ignored entirely (not even type-checked).

**`req.multipartBoundary(boundary string)`** replaces the random boundary with a fixed one. It is validated (RFC 2046) when the body is assembled; an invalid value fails the request. It is a no-op without `req.multipart`, and you are responsible for ensuring the boundary does not occur in any part's content.

!!! note
    `req.multipart` sets `Content-Type: multipart/form-data; boundary=…` on the request **only if you have not already set a `Content-Type` header yourself**. A header written in the `.sinq` file (even one with a deliberately mismatched boundary) is left untouched. `req.multipart` is mutually exclusive with `req.attach` and with a literal body in the `.sinq` file. The assembled body is buffered in memory, so it is meant for form fields and modestly sized files rather than multi-gigabyte uploads.

```text
POST ${env.BASE_URL}/avatar
Authorization: Bearer ${AUTH_TOKEN}

$PRE{
    req.multipart(
        { name = "caption", data = "vacation 2026" },
        { name = "photo", source = "fixtures/beach.jpg", filename = true },
        { name = "meta", data = sinq.json.serialize({ album = "trips" }), contentType = "application/json" }
    )
}

$ASSERT{ sinq.assert.code(201) }
```

### `$RETRY` (Polling Phase)
Executes after every attempt, including one that got no response (then `res.error` is set, see [Send Failures](#send-failures-reserror)). The script **must** return a number indicating how many milliseconds to wait before retrying, or a negative number to stop.

* **`sinq.retry.stop`**: A constant (`-1`) indicating the retry loop should break immediately.
* **`sinq.retry.when(condition boolean, delay number?)`**
    * Retries if `condition` is true. `delay` defaults to `500ms`.
* **`sinq.retry.whenExponential(condition boolean, base number?, constant number?)`**
    * Retries if `condition` is true, using exponential backoff (`base ^ attempt * constant`).
    * `base` defaults to `2`; a `base` that isn't a number in `(0, 10]` falls back to `2`. `constant` defaults to `500ms`.
* **`sinq.retry.withJitter(condition boolean, range number?, delegate function?, delegate_args any...)`**
    * Adds randomized jitter to a retry calculation to prevent thundering herd problems.
    * `range` defaults to `50` (±50ms jitter). `delegate` defaults to `sinq.retry.when`, delegate will be passed condition and delegate_args when called.
    * Usage is: `sinq.retry.withJitter(res.code ~= 200, 100, sinq.retry.when, 2 * sinq.time.second)` - jitter conditional retry with range of [-100:100]

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
* **`sinq.assert.equals(actual any, expected any, message string?)`**: Fails if `actual` does not equal `expected`. When comparing tables, checks that every key-value pair in `expected` recursively matches those in `actual`, but ignores pairs from `actual` not present in `expected`. Without `message`, the failure text never includes the compared values. It reports a type mismatch (`Expected type string, got type nil`) or an incorrect value, and for tables the path of the mismatching field (`Incorrect value in field auth.token`). A `message` replaces that text.
* **`sinq.assert.contains(source string, substring string, message string?)`**: Fails if the string does not contain the specified substring. Without `message`, the failure text includes neither `source` nor `substring`. A `message` replaces the default text.
* **`sinq.assert.isTrue(condition boolean, message string?)`**: Fails if the condition resolves to `false` or `nil`.
* **`sinq.assert.fileMatches(filepath string)`**: Fails if the response previously saved using `req.saveResponseTo()` does not exactly match the contents of `filepath`. Fails immediately if `req.saveResponseTo()` was not called.

### `$POST` (State Extraction Phase)
Executes after `$ASSERT`, unless `$ASSERT` raised an error. After failed assertions it runs only if `fail_fast` is off (it's on by default). Typically used to parse the final response payload and store relevant data in the global sandbox for subsequent requests. No special scoped APIs are injected here.

---

## 5. The Responses Table (`sinq.responses`)

When an HTTP request completes, `sinq` parses the response and injects it into the `sinq.responses` table at the index corresponding to the request number. Lua is 1-indexed, meaning the response to the first request in your scenario is accessed via `sinq.responses[1]`.

!!! note
    A response object only exists *after* the request has been executed. Accessing `sinq.responses[2]` or the alias `res` during the `$PRE` hook of the second request will return nil.

### Response Object Structure
* `attempt` *(number)*: The current execution attempt, counting from 1 (useful during `$RETRY`).
* `code` *(number)*: The HTTP status code (e.g., `200`, `404`).
* `headers` *(table)*: The response headers, see [below](#http-headers-translation).
* `oversized` *(boolean | nil)*: `true` if the payload exceeded the scenario's `max_body` limit and was safely truncated.
* `size` *(number | nil)*: Bytes written to disk, if `req.saveResponseTo()` was used.
* `error` *(table | nil)*: Set when the attempt got no response at all; `nil` otherwise. See [Send Failures](#send-failures-reserror).

### Body Access Methods

!!! note
    These are only available if `req.saveResponseTo()` was NOT used in the `$PRE` hook.

* `bodyRaw` *(string)*: The raw string of the response payload.
* `extractBodyJson()` *(function)*: Safely attempts to parse `bodyRaw` into a Lua table.
    * **Returns:** `(result table, error string)`
* `json()` *(function)*: An unsafe convenience wrapper around `extractBodyJson`. 
    * **Returns:** `table` directly. 
    * **Throws:** Calls a fatal `error()` if the body is not valid JSON, failing the scenario immediately.

### Send Failures (`res.error`)
When an attempt fails without a response (the connection is refused, DNS fails, TLS fails, `req_timeout` runs out, and so on), `res.error` describes it:

* `res.error.kind` *(string)*: what went wrong, one of the kinds below.
* `res.error.message` *(string)*: a human-readable description. Its wording comes from Go and may change between releases; don't match on it.

`tostring(res.error)` and `print(res.error)` give `kind: message`, for example `refused: Get "http://localhost:8080/": dial tcp [::1]:8080: connect: connection refused`.

For that attempt, the response fields (`code`, `headers`, `bodyRaw`, `size`, `oversized`) are `nil`, and so are `json` and `extractBodyJson`, so calling `res.json()` raises an error. `res.attempt` is still set. The next attempt starts clean: after a successful one, `res.error` is `nil` again.

| `kind` | Meaning |
| --- | --- |
| `timeout` | `req_timeout` (or `--cache-timeout` for a cached request) ran out, or the connection or TLS handshake timed out. |
| `dns` | The host name could not be resolved. |
| `refused` | The server refused the connection. |
| `reset` | The connection was closed or reset before or while the response arrived. |
| `tls` | The TLS handshake failed: untrusted or expired certificate, wrong host name, a TLS alert, or a server that doesn't speak TLS (including `https://` to a plain HTTP server). |
| `redirects` | The server redirected more than `max_redirects` times. |
| `other` | Anything else. |

The named kinds are stable within a major version. `other` is not: a minor release may move some of its errors into a new, more specific kind (see [Versioning](versioning.md)). Branch on the named kinds and treat `other` as "everything else", rather than checking `kind == "other"`.

!!! warning "`res.error` is a table, not a string"
    `res.error == "timeout"` is always `false`. Compare the kind instead: `res.error.kind == "timeout"`. Functions that take a string, such as `sinq.assert.fail`, need a string too: pass `res.error.kind`, `res.error.message` or `tostring(res.error)`.

**What happens to the request.** `$RETRY` runs after a failed attempt like after any other, so it can retry. Policies written for status codes usually cover failures already, since `res.code` is `nil`: `sinq.retry.when(res.code ~= 200)` retries a refused connection as well. A policy that reads the body has to check `res.error` first:

```lua
$RETRY{
    if res.error then
        return sinq.retry.when(res.error.kind == "timeout" or res.error.kind == "refused")
    end
    return sinq.retry.when(res.json().status == "pending")
}
```

If the last attempt still failed, the request ends as an **Error** with the failure's message, and its `$ASSERT` and `$POST` don't run. Retrying past `max_retries` ends it with `Too many retries` instead.

Not every error reaches `res.error`. These end the request right away, without running `$RETRY`:

* the scenario's own `timeout` running out (Error), or `Ctrl+C` (Aborted),
* mistakes in the request itself, such as two bodies, a URL scheme other than `http` or `https`, an `req.attach` or `req.multipart` file that can't be opened, or a `req.saveResponseTo` file that can't be created.

A cached request that joins another request's network call (see [`req.cache`](#pre-setup-phase)) gets that call's error, including a `timeout` from `--cache-timeout`.

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
* [`sinq.time.sleep(ms number)`](#functions)
* [`sinq.encoding.base64Encode(source string)`](#encoding)
* [`sinq.encoding.base64Decode(source string)`](#encoding)
* [`sinq.encoding.base64UrlEncode(source string)`](#encoding)
* [`sinq.encoding.base64UrlDecode(source string)`](#encoding)
* [`sinq.encoding.hexEncode(source string)`](#encoding)
* [`sinq.encoding.hexDecode(source string)`](#encoding)
* [`sinq.encoding.urlEncode(source string)`](#encoding)
* [`sinq.encoding.urlDecode(source string)`](#encoding)
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
* [`sinq.json.array(tbl table?)`](#10-json-utilities-sinqjson)

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
* **`sinq.time.sleep(ms number)`**: Blocks the current script for `ms` milliseconds, then returns nothing. Useful as an unconditional delay before a request fires (e.g. `$PRE{ sinq.time.sleep(2 * sinq.time.second) }` to wait out a fixed propagation delay).
    * The sleep is interrupted immediately by `Ctrl+C` or the scenario `timeout`.
    * It counts against `script_timeout` like any other script time. A sleep longer than `script_timeout` will time the script out (or, if it is the last statement in the block, be cut short at the `script_timeout` mark). Raise `script_timeout` in the `.scenario` file for longer waits.
    * A worker running a sleep cannot pick up another scenario, exactly like a `$RETRY` backoff.
    * Prefer a `$RETRY` policy (`sinq.retry.when(condition, delay)`) whenever the endpoint gives you a readiness signal; reach for `sleep` only for genuinely unconditional waits.

    !!! note
        A non-numeric argument is an error. A negative or zero `ms` returns immediately.

---

## 8. Encoding & Crypto APIs

`sinq.encoding.*` holds reversible, non-cryptographic transforms (Base64, hex, URL
escaping). `sinq.crypto.*` holds hashing and message authentication. They are
separate namespaces, and the encoding helpers are **not** reachable via `sinq.crypto`.

### Encoding

`sinq.encoding.*`

* **`sinq.encoding.base64Encode(source string)`**: Encodes a string into standard (padded) Base64.
    * **Returns:** `string`
* **`sinq.encoding.base64Decode(source string)`**: Decodes a standard Base64 string.
    * **Returns:** `(result string, error string)`
* **`sinq.encoding.base64UrlEncode(source string)`**: Encodes a string into URL-safe Base64.
    * **Returns:** `string`
* **`sinq.encoding.base64UrlDecode(source string)`**: Decodes a URL-safe Base64 string.
    * **Returns:** `(result string, error string)`
* **`sinq.encoding.hexEncode(source string)`**: Encodes a string into a hexadecimal representation.
    * **Returns:** `string`
* **`sinq.encoding.hexDecode(source string)`**: Decodes a hexadecimal string.
    * **Returns:** `(result string, error string)`
* **`sinq.encoding.urlEncode(source string)`**: Percent-encodes a string for use in a query string or an `application/x-www-form-urlencoded` body. Spaces become `+`.
    * **Returns:** `string`
* **`sinq.encoding.urlDecode(source string)`**: Reverses `urlEncode` (`+` is decoded back to a space).
    * **Returns:** `(result string, error string)`. The error is set for malformed percent escapes.

!!! note "Building an `application/x-www-form-urlencoded` body"
    There is no dedicated form helper, so assemble the body yourself and set the
    header:
    ```text
    POST ${env.BASE_URL}/login
    Content-Type: application/x-www-form-urlencoded

    ${ "grant_type=password&username=" .. sinq.encoding.urlEncode(env.USER) .. "&password=" .. sinq.encoding.urlEncode(secrets.PASS) }
    ```

### Hashing

`sinq.crypto.*`

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
* **`sinq.jwt.verify(token string, key string, algo string?)`**: Verifies the token using the provided key. Without `algo`, the algorithm named in the token's own header is used; pass `algo` to require a specific one, so a token can't pick an algorithm that misreads your key.
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
    * Every JSON array is returned as a table marked as an array (the mark is kept in its metatable), so an array that is empty, or becomes empty later, still serializes as `[]`. The table is otherwise an ordinary Lua table: `#`, `ipairs` and `table.insert` work as usual. Replacing its metatable with `setmetatable` removes the mark.
* **`sinq.json.serialize(tbl table, indent string?)`**: Serializes a Lua table into a JSON string.
    * **Returns:** `(result string, error string)`
    * `indent` string?: Optional string used for formatting (e.g., `"  "`). If omitted, produces compact JSON. If present, also introduces newlines between object and array entries.
    * How a table is serialized:
        * Keys `1..n` with no gaps and no other keys: a JSON array.
        * Only string keys: a JSON object.
        * Empty: `[]` if it's marked as an array (parsed from a JSON array, or passed through `sinq.json.array`), `{}` otherwise.
        * Anything else is an error: integer and string keys mixed together, gaps in the integer keys (`{ 1, nil, 3 }`), number keys that aren't whole or fall outside `1..n`, or keys of other types.
    
    !!! note
        Passing a cyclic table will immediately return an error (`"Cycle detected, unable to serialize"`).

    !!! note
        Lua can't store `nil` in a table, so a hole in an array is a gap and fails serialization. To send `[1, null, 3]`, use `{ 1, sinq.json.null, 3 }`.

* **`sinq.json.array(tbl table?)`**: Marks a table as a JSON array, so it serializes as `[]` when empty instead of `{}`.
    * **Returns:** `(result table, error string)`
    * Without an argument (or with `nil`), returns a new empty array: `"tags": ${ return sinq.json.array() }` sends `"tags": []`.
    * With a table, marks that table in place and returns it. The table must be array-shaped: empty, or keys `1..n` with no gaps and nothing else. A non-empty array already serializes as an array; marking it matters when it may end up empty, such as a list that is filtered down later.
    * Returns `nil` and an error for a table that isn't array-shaped (in which case the table isn't marked), for an argument that isn't a table, or for more than one argument.
    
    !!! note
        If the table already has a metatable, the mark is added to that metatable and nothing else changes. If several tables share that metatable, they are all marked.

* **`sinq.json.null`**: A special constant representing a JSON `null` value, allowing Lua tables to explicitly serialize `null` properties (since standard Lua drops `nil` table keys). Tables, parsed from JSON will also include this constant to represent explicit `null`. Can be compared with standard `==` operator (`sinq.assert.isTrue(res.json().myNull == sinq.json.null)`)

---

## 11. Fake Data Generation (`sinq.fake.*`)

The `sinq.fake` table exposes deterministic fake data generators. All generators respect the current seed.

#### Primitives & Core Data
* **`sinq.fake.uuid()`** (alias: **`sinq.fake.uuidv4()`**): Returns a random UUIDv4 string.
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

`sinq` does not load two common core Lua libraries - `io` and `os` - by default, so that a `.sinq` script cannot touch the process, spawn a shell, or reach the filesystem outside its scenario workspace.

!!! danger "`--unrestricted` disables the sandbox"
    Passing `--unrestricted` (`-u`) loads the full `os` and `io` libraries. Scripts then gain `os.execute` (run arbitrary shell commands), `os.exit` (terminate `sinq` immediately, skipping all reporting and cleanup), `os.remove` / `os.rename` / `os.setenv`, and `io.open` (read and write any path the `sinq` process can, not just files under the scenario directory). Only pass `-u` for scenario files you fully trust - treat it the way you would treat piping a script straight into your shell.

`sinq` allows you to import external Lua packages. To make them accessible via `require("package")`, you must provide the directory paths containing those packages.

You can do this using the `SINQ_LUA_PATH` environment variable or the `--plugins` CLI flag (which takes precedence). Multiple paths should be separated by a colon (`:`) on macOS/Linux, or a semicolon (`;`) on Windows. You can also pass the `--plugins` flag multiple times to aggregate paths.

!!! note
    All paths passed to `sinq` as positional arguments and the current working directory also get appended to the end of path for the purposes of searching for Lua plugins. So if you run `sinq` from a directory containing a file `my-module.lua`, `require("my-module")` will work for all `.sinq` files.
