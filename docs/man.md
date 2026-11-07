# sinq 1 "November 2026" "sinq" "Sinq Manual"

## NAME

sinq - The Spanish Inquisition

## SYNOPSIS

**sinq** [flags] [directories...]

## DESCRIPTION

**sinq** is a concurrent HTTP functional and integration testing tool. It treats your filesystem as a workflow definition, executing sequences of requests to walk through different workflow scenarios. Every leaf directory becomes an isolated execution scenario with its own state, configuration, and request chain. The environment matrix mechanism allows for combinatorial/matrix/parametrized testing, reusing the same workflow for different datasets.

## CONCEPTS
### Scenarios and Treewalker
The unit of execution is a scenario. Each directory inherits the naturally sorted `.scenario` and `.sinq` files of its parents and appends its own. A leaf directory (no subdirectories, at least one `.sinq` or `.scenario` file) becomes one scenario.

### Concurrency
Requests within a scenario run in order. Scenarios run concurrently in a pool of `--workers` workers.

## FILE FORMAT

A `.sinq` file is a standard HTTP request with optional embedded Lua scripts:
```
GET ${env.baseUrl}/ping HTTP/1.1
My-Header: ${ return math.random(5, 10) }

{
  "myCustomBodyField": "${ 'hello world' }"
}

$ASSERT{
  sinq.assert.code(200)
}
```
Every `$NAME?{ ... }` block is treated as Lua script. Scripts with non-reserved names are executed before sending the request. Their return values get substituted into the final request. Only the first returned value is used; `nil`, functions, channels and coroutines fail the request. Strings are inserted as is (no quoting or escaping), numbers and booleans as text, tables as compact JSON (same rules as **sinq.json.serialize**), and **sinq.json.null** as `null`.

Multiple requests can be defined in a single file by separating them with a `###` delimiter on its own line. Text after `###` on the same line names the request that follows it (shown in `--list` output and in reports); a leading `###` before any request content names the file's first request the same way.

Lifecycle scripts have reserved names, and are executed at different times during the request lifecycle:

* `$PRE`: Executes immediately when a worker picks up the request, before it is materialized.

* `$RETRY`: Executes immediately after every attempt, including one that got no response (then `res.error` is set). It must return a Lua number representing milliseconds to wait before retrying, or a negative number to stop.

* `$ASSERT`: Executes after the retry loop completes. Used to fail tests.

* `$POST`: Executes after assertions (after failed ones only if `fail_fast` is off). Used to extract data from the response and save it to the global sandbox.

## CONFIGURATION

`sinq` uses JSON-formatted `.scenario` files along the scenario path to manage environments, timeouts, and other configurations. When a leaf directory inherits a `.scenario` file from a parent, the configurations are deep merged (the only exclusion being the `env_matrix` and `tags` lists, which all get combined into flat lists), with the deeper (child) configuration taking precedence. Unmentioned default values are preserved. 

Available keys include `name`, `description`, `env`, `req_timeout`, `script_timeout`, `timeout`, `fail_fast`, `max_retries`, `max_redirects`, `max_body`, `env_matrix` and `tags`. The `env` object is parsed into a global Lua table and can be accessed directly in any Lua script. `env_matrix` is accumulated from all the `.scenario` files in the scenario. Every entry should be a json object, every key being a label for a particular dataset, and the value for the key being another object. Then, treating every entry as a matrix or axis, `sinq` creates a Cartesian product of all of them, and the scenario is run multiple times for every unique resulting combination, the combinations being deep merged with the `env` data.

## OPTIONS

**-v**, **--version**
: Print the current sinq version and exit.

**-h**, **--help**
: Print help message and exit.

**-i**, **--insecure**
: Disable SSL/TLS certificate verification.

**-V**, **--verbose**
: Enable verbose reporting (reports each stage duration, only affects "std" format).

**-l**, **--list**
: Parse and list scenarios at specified directories.

**-u**, **--unrestricted**
: Load the full Lua **os** and **io** libraries for scripts. This disables the sandbox: scripts can run shell commands (`os.execute`), access files outside the scenario directory (`io.open`), and terminate sinq (`os.exit`). Only use with fully trusted scenario files.

**-p**, **--print**
: Capture lua output and show it in the report.

**-w**, **--workers** *int*
: Number of concurrent workers (default 10).

**-c**, **--count** *int*
: Number of launches for every scenario (default 1). Must be positive. With more than one launch, each is reported with its run number, counting from 1, after the scenario name (for example *checkout#2*).

**-s**, **--secret** *string*
: Key=value pair overrides for scenario secrets.

**-e**, **--env** *string*
: Key=value pair overrides for all scenario environments.

**-o**, **--out** *path*
: Path to write the output file (prints to stdout if omitted).

**-L**, **--log-level** *string*
: Log level to use: debug, info, warn or error (default "warn").

**-f**, **--format** *string*
: Output format: std or junit (default "std").

**-C**, **--color** *string*
: Terminal colors: always, never, auto (default "auto").

**-S**, **--show** *string*
: Which results to show in the output: all, no-skip, failed, none (default "no-skip"). Only affects the "std" format; "junit" output always includes every result.

**-t**, **--tag** *string*
: Execute only scenarios that have at least one of passed tags.

**-n**, **--name** *string*
: Execute only scenarios which names match at least one of passed regular expressions.

**--no-spinner**
: Disable spinner animation.

**--dump-on-failure**
: Print full request and response data of a request that failed, errored or was aborted.

**--secrets-file** *path*
: Path to JSON-formatted secrets file.

**--no-tag** *string*
: Do not execute scenarios that have the tag.

**--no-name** *string*
: Do not execute scenarios which names match the regular expression.

**--plugins** *string*
: Paths to lua plugin directory entries, joined with ':' on Linux and MacOS, ';' on Windows.

**--max-cache-size** *string*
: Global maximum response size for cached requests, default 5MiB.

**--cache-timeout** *string*
: Global timeout for the cached requests, default 10s.

**--completion**
: Output shell completion script for the detected shell (bash, zsh, fish, or powershell).

## ENVIRONMENT

`sinq` reads the following environment variables:

**SINQ_LUA_PATH**
: Defines a list of directories that `sinq` will look for plugins in, joined with ':' on Linux and MacOS, ';' on Windows. Is ignored if **--plugins** flag is used.

**NO_COLOR**, **SINQ_NO_COLOR**
: Any non-empty value disables colored output and the spinner under `--color auto`.

**CI**
: Any non-empty value has the same effect as **NO_COLOR**, and makes `sinq` warn when `--log-level debug` or `--dump-on-failure` is set.

**HTTP_PROXY**, **HTTPS_PROXY**
: The proxy to use for http and https requests.

**NO_PROXY**
: Comma-separated hosts that bypass the proxy.

## LUA API REFERENCE
### Global Variables

**env**
: Table of environment variables for the current scenario with overrides from `-e` / `--env` flags.

**secrets**
: Table of secrets loaded via the `--secrets-file`, `-s`, `--secret` flags.

**req**
: Reference to the current HTTP request (Used in `$PRE`).

**res**
: Reference to the current HTTP response.

**sinq.responses**
: Table of all completed responses in the current scenario (1-indexed).

### Flow Control

**sinq.setNextRequest(index)**
: Execute request with the 1-indexed number in the scenario next (doesn't stop current request's lifecycle). Can be called from any hook, including `$PRE`. A request that runs more than once is reported once. Its status is the most severe across its runs (error, then failure, then aborted, then success, then skipped). It keeps the assertion failures of every run, its timings and attempt counts add up, and `--dump-on-failure` shows the first failing run.

**sinq.finishScenario()**
: Finish the scenario after the current request (doesn't stop current request's lifecycle).

### Time API

**sinq.time.ms**, **sinq.time.second**, **sinq.time.minute**, **sinq.time.hour**
: Time constants for retry logic. Use `math.floor` when dividing timestamps by these constants.

**sinq.time.now()**
: Returns current UNIX timestamp in milliseconds.

**sinq.time.fromString(str, format?)**
: Parses a time string into a UNIX timestamp (milliseconds).

**sinq.time.toString(ms, format?)**
: Formats a UNIX timestamp (milliseconds) into a time string.

**sinq.time.sleep(ms)**
: Blocks the current script for `ms` milliseconds (an unconditional delay before the request fires). Interrupted by `Ctrl+C` / the scenario `timeout`, counts against `script_timeout`, and keeps the worker busy like a `$RETRY` backoff. Non-numeric argument is an error; a negative or zero `ms` returns immediately. Prefer a `$RETRY` policy when the endpoint signals readiness.

### Encoding API

**sinq.encoding.base64Encode**, **sinq.encoding.base64Decode**, **sinq.encoding.base64UrlEncode**, **sinq.encoding.base64UrlDecode**
: Base64 encoding/decoding functions.

**sinq.encoding.hexEncode**, **sinq.encoding.hexDecode**
: Hexadecimal encoding/decoding functions.

**sinq.encoding.urlEncode(string)**, **sinq.encoding.urlDecode(string)**
: Percent-encode / decode for query strings and `application/x-www-form-urlencoded` bodies. Spaces map to `+`. `urlDecode` returns an error string as the second value for malformed input.

### Crypto API

**sinq.crypto.md5(string, encoding?)**, **sinq.crypto.sha1(string, encoding?)**, **sinq.crypto.sha256(string, encoding?)**, **sinq.crypto.sha512(string, encoding?)**, **sinq.crypto.hmac(source, algo?, key?, encoding?)**
: Hashing functions supporting hex, base64, base64url, and raw string encodings. Default is hex. If an unknown encoding or algorithm is provided, returns an error string as the second value. (Base64/hex/URL encoding helpers live under `sinq.encoding`, not `sinq.crypto`.)

### JWT API

**sinq.jwt.decode(token)**
: Decodes a JWT token without validating signature.

**sinq.jwt.verify(token, key, algo?)**
: Verifies the token using the key. Without `algo`, the algorithm named in the token's own header is used; pass `algo` to require a specific one.

**sinq.jwt.sign(claimsTable, key, method?)**
: Creates and signs a JWT token. Method defaults to HS256. A cyclic `claimsTable` returns `nil` and an error.

### JSON API

**sinq.json.parse(string)**
: Parses a JSON string into a Lua table.

**sinq.json.serialize(table, indent?)**
: Serializes a Lua table into a JSON string. Keys `1..n` with no gaps become an array, string keys an object. An empty table becomes `{}`, or `[]` if it is marked as an array. Mixed tables, gaps (`{ 1, nil, 3 }`), number keys that aren't whole or fall outside `1..n`, and cyclic tables return an error. Use **sinq.json.null** for holes in an array.

**sinq.json.array(table?)**
: Marks a table as a JSON array so it serializes as `[]` when empty, and returns it. Without an argument, returns a new empty array. Returns `nil` and an error if the table isn't array-shaped (empty, or keys `1..n` with no gaps). Arrays returned by **sinq.json.parse** are already marked. If the table already has a metatable, the mark is added to it.

**sinq.json.null**
: A constant used to explicitly serialize a `null` value in JSON tables.

**sinq.fake.uuid()**, **sinq.fake.uuidv4()**
: Returns a random UUIDv4 string. `uuidv4` is an alias for `uuid`.

**sinq.fake.email()**, **sinq.fake.ipv4()**, **sinq.fake.ipv6()**, **sinq.fake.url()**, **sinq.fake.userAgent()**, **sinq.fake.trace()**, **sinq.fake.username()**, **sinq.fake.password()**
: Fake generators for network and identity strings.

**sinq.fake.name()**, **sinq.fake.firstName()**, **sinq.fake.lastName()**, **sinq.fake.phone()**, **sinq.fake.address()**, **sinq.fake.company()**
: Fake generators for personal information.

**sinq.fake.word()**
: Fake generators for text.

**sinq.fake.int(min?, max?)**, **sinq.fake.float(min?, max?)**, **sinq.fake.shakespeare()**
: Fake generators for primitives (int, float, boolean).

**sinq.fake.timestamp(fromMs, toMs?)**
: Generate a random UNIX timestamp between `fromMs` and `toMs` (`toMs` defaults to current time if omitted).

**sinq.fake.oneOf(array)**
: Returns a random element from the provided array.

**sinq.fake.setSeed(int64)**
: Seeds the fake data generator for deterministic output.

### $PRE Scope API

**req.attach(filepath)**
: Replace request body with file contents.

**req.saveResponseTo(filepath)**
: Streams the upcoming response body directly to disk, bypassing the Lua memory buffer. Is meant for downloading large files. If used, `bodyRaw` and JSON methods will not be available in subsequent hooks. Expects path to be relative to the current file.

**req.cache(bool?)**, **req.cache(key)**
: Turns on/off client-side response caching. Without a key, the cache key is derived from the data sent over the wire, the cookies sent with it, attached filenames and the scenario directory; the parameter defaults to `true`. With a string, that string is the key as is, and every request using it shares one cached response (prefix keys yourself to keep requests apart). Other argument types raise an error. Each retry attempt is cached separately. The first request to fill a key decides what is stored: if it used `req.saveResponseTo`, later requests with that key get an empty body. Cached errors stay cached for the whole run.

**req.skip(bool?)**
: If `true` (default), marks the request to be skipped. The `$PRE` script will finish executing, but the HTTP request will not be fired and subsequent hooks are bypassed. The request is reported as skipped, without a test failure.

**req.multipart(entry, ...)**
: Builds a `multipart/form-data` body from one or more part tables, passed positionally or as one array table; repeated calls accumulate. Entry keys: `name` (required unless `contentDisposition` is set), `data` or `source` (mutually exclusive; `source` is a file path relative to the current `.sinq` file, absolute paths rejected), `filename` (string, or `true` for `basename(source)`), `contentType` (sniffed from the filename/source extension when omitted for file parts), `contentDisposition` (raw override), `headers` (per-part header table). Sets the request `Content-Type` only if one is not already set. Mutually exclusive with `req.attach` and a literal body.

**req.multipartBoundary(string)**
: Fixes the multipart boundary instead of using a random one. Validated (RFC 2046) at assembly; no-op without `req.multipart`.

### $RETRY Scope API

**sinq.retry.stop**
: Constant (-1) to break the retry loop.

**sinq.retry.when(condition, delay?)**
: Retry if condition is true.

**sinq.retry.whenExponential(condition, base?, constant?)**
: Retry with exponential backoff.

**sinq.retry.withJitter(condition, range?, delegate?, delegate_args...)**
: Retry with randomized jitter.

### $ASSERT Scope API

**sinq.assert.fail(reason)**
: Mark test as failed (does not halt execution).

**sinq.assert.code(expected, message?)**
: Fail if HTTP status code does not match.

**sinq.assert.equals(actual, expected, message?)**
: Fail if values are not equal. If `expected` is a table (including an array), this is a partial match: only the keys present in `expected` are checked, extra keys/elements in `actual` are ignored, and nested tables are compared the same way. The default failure message never includes the compared values, only types and the path of the mismatching field.

**sinq.assert.contains(str, substring, message?)**
: Fail if string lacks substring. The default failure message includes neither string.

**sinq.assert.isTrue(condition, message?)**
: Fail if condition is false or nil.

**sinq.assert.fileMatches(filepath)**
: Fail if response previously saved with `req.saveResponseTo` does not match the file at `filepath`.

### Response Methods And Fields

**res.bodyRaw**
: Raw response body string.

**res.json()**
: Unsafe convenience parser; throws a fatal error if body is not valid JSON.

**res.extractBodyJson()**
: Safely parses JSON; returns `(table, error)`.

**res.code**
: HTTP status code of the response.

**res.headers**
: Lua table representation of response headers.

**res.attempt**
: The current attempt, counting from 1.

**res.error**
: Set when the attempt got no response (refused, DNS, TLS, `req_timeout`, ...), otherwise nil. A table with `kind` (`timeout`, `dns`, `refused`, `reset`, `tls`, `redirects` or `other`) and `message`; `tostring(res.error)` gives `kind: message`. The response fields are nil for that attempt. `$RETRY` still runs; if the last attempt failed, the request is an error and `$ASSERT`/`$POST` don't run. Named kinds are stable within a major version; `other` may be split in a minor release.

**res.size**
: Count of bytes written to file if `req.saveResponseTo` was called in `$PRE` hook.

**res.oversized**
: Boolean flag set if the response body size was bigger than the configured size for the scenario.

## EXIT STATUS
**0**
: Success. All discovered scenarios executed successfully, all network calls completed, and no `$ASSERT` blocks triggered a failure.

**1**
: Failure. One or more scenarios failed an assertion, errored (including a request that still got no response after `$RETRY`) or were aborted, or the CLI received invalid arguments. Also returned when no scenario is selected to run: none were found, or the **--tag**/**--name** filters excluded all of them.

## AUTO-COMPLETION
To load completions dynamically in your current session:

- **Bash/Zsh:** `source <(sinq --completion)`
- **Fish:** `sinq --completion | source`
- **PowerShell:** `sinq --completion | Invoke-Expression`

To install completions permanently, save the output to your shell's completions directory:

- **Bash:** `sinq --completion > ~/.local/share/bash-completion/completions/sinq`
- **Zsh:** `sinq --completion > ~/.zfunc/_sinq` (and add `fpath+=~/.zfunc` before `compinit` in `.zshrc`)
- **Fish:** `sinq --completion > ~/.config/fish/completions/sinq.fish`
- **PowerShell:** Add `sinq --completion | Invoke-Expression` to your PowerShell profile.


## EXAMPLES
Run tests in the current directory with 20 concurrent workers:
    $ sinq --workers 20 .

Override a deeply nested environment key using dot-notation, and pass a JSON array as a value:
    $ sinq -e 'API.HOSTS=["localhost", "127.0.0.1"]' .

Run tests against specific directories with a secrets file and save JUnit output:
    $ sinq --secrets-file=prod.json --format=junit --out=results.xml ./auth ./billing

Parse and list scenarios without executing them:
    $ sinq --list ./tests

## LICENSE
Distributed under the terms of the GNU General Public License v3 (GPLv3).

## AUTHOR
Written by Veitangie.
