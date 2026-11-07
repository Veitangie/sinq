# Writing & Debugging Scenarios

A scenario is a directory of `.sinq` files. `sinq` runs the files in natural-sorted order and they share state through Lua globals. This page covers the shapes that come up most and how to work out why one is failing.

## A complete scenario

Two leaf directories that share a setup file, four requests each.

```
api/
  setup.sinq          # health check + login, shared by both scenarios
  users/
    users.scenario       # tags this scenario "users"
    users.sinq        # create a user, then read it back
  orders/
    orders.scenario      # tags this scenario "orders"
    orders.sinq       # create an order, then cancel it
```

`api/` has subdirectories, so it is not a scenario itself. `api/users/` and `api/orders/` are leaf directories, so each one is a scenario. Both inherit `setup.sinq` and run it first:

- **`users`**: Health check &rarr; Log in &rarr; Create user &rarr; Fetch user
- **`orders`**: Health check &rarr; Log in &rarr; Create order &rarr; Cancel order

### The shared setup file

**`api/setup.sinq`** holds two requests, separated by `###`. The text after `###` names the request in `--list` output and in reports.

```text
### Health check
GET ${env.BASE_URL}/health

$ASSERT{ sinq.assert.code(200, "API is not up") }

### Log in
POST ${env.BASE_URL}/auth/login
Content-Type: application/json

{ "user": "e2e", "pass": "${secrets.E2E_PASSWORD}" }

$ASSERT{ sinq.assert.code(200, "login failed") }

$POST{ AUTH_TOKEN = res.json().token }
```

- Each request is literal HTTP. `${env.BASE_URL}` and `${secrets.E2E_PASSWORD}` are inline scripts, evaluated just before the request is sent.
- Blocks run in lifecycle order (`$PRE`, the request, `$RETRY`, `$ASSERT`, `$POST`) no matter where you write them in the file.
- `$POST` runs last. `AUTH_TOKEN` has no `local`, so it is a scenario global that every later file can read.
- `BASE_URL` and `E2E_PASSWORD` are not set anywhere in the tree. Pass them at run time (`-e BASE_URL=... -s E2E_PASSWORD=...`, or `--secrets-file`), or add an `api/config.scenario`.

### The `users` scenario

**`api/users/users.scenario`** adds a tag so this scenario can be run on its own:

```json
{ "tags": ["users"] }
```

**`api/users/users.sinq`**:

```text
### Create user
POST ${env.BASE_URL}/users
Authorization: Bearer ${AUTH_TOKEN}
Content-Type: application/json

{ "name": "Ada", "email": "ada@example.test" }

$ASSERT{ sinq.assert.code(201, "user not created") }

$POST{ USER_ID = res.json().id }

### Fetch user
GET ${env.BASE_URL}/users/${USER_ID}
Authorization: Bearer ${AUTH_TOKEN}

$ASSERT{
  sinq.assert.code(200)
  sinq.assert.equals(res.json(), { name = "Ada", email = "ada@example.test" })
}
```

- The first request's `$POST` saves `USER_ID`; the second interpolates it into the URL with `${USER_ID}`.
- `sinq.assert.equals` with a table is a partial match: the listed keys must be present and equal, and other fields in the response are ignored.

### The `orders` scenario

Same shape, different resource.

**`api/orders/orders.scenario`**:

```json
{ "tags": ["orders"] }
```

**`api/orders/orders.sinq`**:

```text
### Create order
POST ${env.BASE_URL}/orders
Authorization: Bearer ${AUTH_TOKEN}
Content-Type: application/json

{ "sku": "WIDGET-1", "qty": 3 }

$ASSERT{ sinq.assert.code(201, "order not created") }

$POST{ ORDER_ID = res.json().id }

### Cancel order
DELETE ${env.BASE_URL}/orders/${ORDER_ID}
Authorization: Bearer ${AUTH_TOKEN}

$ASSERT{ sinq.assert.code(204, "order not cancelled") }
```

Run both scenarios with `sinq ./api`, or just one by tag with `sinq -t orders ./api`.

## Patterns

### Run once per data set

Put an `env_matrix` in a `.scenario` file. Each object is an axis; `sinq` runs the scenario once per combination and merges the chosen values into `env`.

```json
{
  "env_matrix": [
    { "admin": { "role": "admin" }, "guest": { "role": "guest" } },
    { "card":  { "expect": 200 },   "crypto": { "expect": 202 } }
  ]
}
```

```text
POST ${env.BASE_URL}/checkout
Content-Type: application/json

{ "role": "${env.role}" }

$ASSERT{ sinq.assert.code(env.expect, "wrong status for " .. env.role) }
```

Four runs: `admin`+`card`, `admin`+`crypto`, `guest`+`card`, `guest`+`crypto`. The console report lists each run's labels under the scenario name, and JUnit reports append them to the suite name (`checkout_admin_card`).

### Upload a file

When the file *is* the whole request body, use `req.attach`:

```text
### Upload raw
POST ${env.BASE_URL}/documents
Authorization: Bearer ${AUTH_TOKEN}
Content-Type: application/pdf

$PRE{ req.attach("fixtures/report.pdf") }

$ASSERT{ sinq.assert.code(201) }
```

When it is one field among several (a real HTML-form upload), use `req.multipart`:

```text
### Upload avatar
POST ${env.BASE_URL}/avatar
Authorization: Bearer ${AUTH_TOKEN}

$PRE{
  req.multipart(
    { name = "caption", data = "hello" },
    { name = "file", source = "fixtures/a.png", filename = true }
  )
}

$ASSERT{ sinq.assert.code(201) }
```

In both, the path is relative to this `.sinq` file. `req.attach` fails if the file also has a literal body; `req.multipart` sets `Content-Type: multipart/form-data; boundary=...` itself unless you set a `Content-Type` yourself, and `filename = true` advertises `basename(source)`.

### Negative test

```text
POST ${env.BASE_URL}/admin/wipe
Content-Type: application/json

{ "confirm": true }

$ASSERT{ sinq.assert.code(401) }
```

A 401 here is a pass, because that is what the assertion checks for.

### Change the flow

```text
GET ${env.BASE_URL}/status

$POST{
  if res.json().done then
    sinq.finishScenario()      -- end the scenario after this request
  else
    sinq.setNextRequest(2)     -- jump back to the 2nd request
  end
}
```

A request that runs several times shows up once in the report. It is marked failed if any of its runs failed, lists the failures of all runs, and adds up its attempts and timings. The summary's "requests sent" counts every run and retry, including ones answered from the response cache (`req.cache`) without reaching the server.

## Lua API cheat-sheet

Available in every script block:

```
env.KEY                      scenario environment (also -e / --env overrides)
secrets.KEY                  values from -s / --secret / --secrets-file
res                          current response (see below)
req                          current request (mutate only in $PRE)
sinq.responses[n]            the nth response, 1-indexed
sinq.setNextRequest(index)   jump execution to request `index` (1-indexed)
sinq.finishScenario()        stop after the current request finishes
print(...)                   shown only with -p / --print, otherwise discarded
```

Response table:

```
res.code                     status code
res.headers["Name"]          string, or array of strings if the header repeats
res.bodyRaw                  raw body string
res.json()                   parse bodyRaw; fails the request on invalid JSON
res.extractBodyJson()        -> (table, err); does not throw
res.attempt                  current attempt, from 1
res.oversized                true if the body was truncated at max_body
res.size                     bytes written (only set after req.saveResponseTo)
res.error                    nil, or { kind, message } when the attempt got no response
```

`$PRE` (setup and file I/O):

```
req.attach(path)             use a file's contents as the whole request body
req.saveResponseTo(path)     stream the response to disk instead of buffering it
req.cache(enable? | key)     opt in to the response cache; a string key is shared by every request using it
req.skip(enable?)            do not send this request (enable defaults to true)
req.multipart(entry, ...)    build a multipart/form-data body
req.multipartBoundary(str)   pin the multipart boundary instead of a random one
```

`$RETRY` (must return a number):

```
sinq.retry.stop                                   -1
sinq.retry.when(cond, delay?)                     delay while cond (delay defaults to 500ms)
sinq.retry.whenExponential(cond, base?, const?)   base^attempt * const
sinq.retry.withJitter(cond, range?, fn?, args...) add +/- jitter to a retry
```

`$ASSERT`:

```
sinq.assert.code(want, msg?)
sinq.assert.equals(actual, expected, msg?)        partial match when expected is a table
sinq.assert.contains(str, substr, msg?)
sinq.assert.isTrue(cond, msg?)
sinq.assert.fail(msg)                             records a failure but does not stop the block
sinq.assert.fileMatches(path)                     needs a prior req.saveResponseTo
```

Helpers (all script blocks):

```
sinq.time.ms | .second | .minute | .hour         millisecond constants
sinq.time.now()                                  unix milliseconds
sinq.time.fromString(s, fmt?) | toString(ms, fmt?)
sinq.time.sleep(ms)                              unconditional delay, bounded by script_timeout
sinq.encoding.base64Encode/Decode, base64UrlEncode/Decode, hexEncode/Decode
sinq.encoding.urlEncode/urlDecode               query-string and form-body escaping
sinq.crypto.md5/sha1/sha256/sha512(s, enc?) | hmac(s, algo?, key?, enc?)
sinq.jwt.decode(t) | verify(t, key, algo?) | sign(claims, key, method?)
sinq.json.parse(s) | serialize(t, indent?) | array(t?) | sinq.json.null
sinq.fake.uuid/email/name/int/oneOf/...          see the Lua API page for the full set
```

Full signatures and behaviour: [Lua API](lua-api.md).

## Common mistakes

- **Missing `return` in an inline script.** `${PLAYER_ID}` works because `sinq` retries a bare expression with `return ` prepended. A call like `${tostring(x)}` is ambiguous; write `${return tostring(x)}`.
- **Multiple return values in an inline script.** Only the first one is inserted, so `${return sinq.json.serialize(t)}` works as is. A `nil` first value fails the request instead of inserting the text `nil`.
- **Quoting a table.** An inline script that returns a table inserts JSON, so write `"address": ${env.pointA}`, not `"address": "${env.pointA}"`. Strings are the opposite: they're inserted raw, so they need the quotes (`"${env.NAME}"`).
- **Empty arrays become `{}`.** Lua has no empty-array value, so an empty table serializes as an object. Arrays parsed from JSON keep their shape; for one built in Lua, use `sinq.json.array()` (or `sinq.json.array(t)` on a list that might end up empty).
- **`local` on a value you need later.** State passed between files must be a global: `AUTH_TOKEN = ...`, not `local AUTH_TOKEN = ...`.
- **Lua tables are 1-indexed.** The first element of a top-level JSON array is `res.json()[1]`.
- **`res.json()` fails the request on non-JSON.** Use `res.extractBodyJson()` when the body might not be JSON.
- **`$RETRY` must return a number.** Returning nothing, a boolean, or a table errors the request.
- **`$PRE` cannot read the body.** It is not built yet. Read it in an inline script or in `$POST` of an earlier request.
- **One body source per request.** A literal body together with `req.attach` or `req.multipart` is an error.
- **`$POST` is skipped after a failed assertion** when `fail_fast` is true (the default).

## Debugging a failing scenario

Reach for these flags, roughly in order:

| Flag | Use |
| --- | --- |
| `--list` | Confirm which files make up the scenario and in what order. |
| `-V` / `--verbose` | Per-stage timings, so you can see which stage is slow or hanging. |
| `--dump-on-failure` | Print the full request and response of a request that failed, errored or was aborted. |
| `-p` / `--print` | Attach your Lua `print` output to the report. |
| `-L debug` | Everything, including request and response bodies. Avoid in CI with retained logs; it can expose secrets. |

### Failure vs error

- **Failure**: an assertion said no (`sinq.assert.*`). The service responded and the check did not pass. JUnit `<failure>`.
- **Error**: something broke before an assertion could run, such as a Lua exception, a malformed request, or a request that still got no response after `$RETRY` (refused, timed out, ...; see [`res.error`](lua-api.md#send-failures-reserror)). JUnit `<error>`.

### Error messages

| Message | Cause |
| --- | --- |
| `Empty request (did you add ### ...)` | The file, or the part after a `###`, has no request line. |
| `<line>: URL is not absolute, unable to derive scheme` | The URL on the request line has no scheme (`/api/users` instead of `${env.BASE_URL}/api/users`). |
| `<line>: Unsupported scheme: ...` | The URL's scheme is not `http` or `https`. |
| `<line>: Empty host in URL` | The URL has a scheme but no host (`https:///path`). |
| `<line>: Malformed header field, ...` | A header line has no `:` right after its name. |
| `<file>:<line>:<col> Failed to parse lua script` | Lua syntax error in a hook or inline script. |
| `Error occurred while executing lua script` | Lua runtime error: a nil index, an explicit `error(...)`, and so on. |
| `Pre script is defined more than once` | Two `$PRE` (or `$ASSERT`, or `$POST`) blocks in one request. |
| `Too many retries` | `$RETRY` kept returning a non-negative number past `max_retries`. |
| `Request has both body and a file source` | A literal body plus `req.attach`. |
| `Request has more than one body ...` | `req.multipart` together with a literal body or `req.attach`. |
| `req.attach: invalid file path ...` / `req.multipart: invalid source path ...` | The path is absolute, missing, or a directory. Paths are relative to the `.sinq` file. |
| `Failed to parse scenario config` | Invalid JSON in a `.scenario` file. |
| `Failed to parse env matrix: keys ... have non-object values` | An `env_matrix` axis value is not an object. |
| `Non positive request timeout` (or `script` / `scenario`) | A timeout in a `.scenario` file is `0` or negative. |
| `Failed to extract body as json` | `res.json()` on a body that is not valid JSON. |
| `request timeout` | The last attempt ran out of `req_timeout`. `$RETRY` saw it as `res.error.kind == "timeout"`. |
| `... connection refused` and other network messages | The last attempt got no response. `$RETRY` saw it on `res.error`. |
| `Context cancelled` / `context deadline exceeded` | The run was interrupted, or the scenario's `timeout` ran out. These skip `$RETRY`. |
