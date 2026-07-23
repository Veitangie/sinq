# sinq 1 "June 2026" "sinq" "Sinq Manual"

## NAME

sinq - The Spanish Inquisition

## SYNOPSIS

**sinq** [flags] [directories...]

## DESCRIPTION

**sinq** is a concurrent HTTP functional and integration testing tool. It treats your filesystem as a workflow definition, executing sequences of requests to walk through different workflow scenarios. Every leaf directory becomes an isolated execution scenario with its own state, configuration, and request chain. The environment matrix mechanism allows for combinatorial/matrix/parametrized testing, reusing the same workflow for different datasets.

## CONCEPTS
### Scenarios and Treewalker
The basic unit of execution for `sinq` is a scenario. `sinq` uses a directory-traversal engine called the Treewalker to treat your physical filesystem as a Directed Acyclic Graph (DAG). It recursively descends into subdirectories, inheriting and appending sorted `.scenario` and `.sinq` files from parent directories. Once it reaches a leaf directory (a directory containing at least one `.sinq` or `.scenario` file but no subdirectories), it compiles the path into an executable scenario.

### Concurrency
In `sinq`, the absolute unit of concurrency is the Scenario, not the Request. Requests within each scenario are strictly guaranteed to execute sequentially, while multiple scenarios execute simultaneously in a worker pool.

## FILE FORMAT

A `.sinq` file is a standard HTTP request with optional embedded Lua scripts:
```
GET ${env.baseUrl}/ping HTTP/1.1
My-Header: ${ math.random(5, 10) }

{
  "myCustomBodyField": "${ return 'hello world' }"
}

$ASSERT{
  sinq.assert.code(200)
}
```
Every `$NAME?{ ... }` block is treated as Lua script. Scripts with non-reserved names are executed before sending the request. Their return values get substituted into the final request.

Lifecycle scripts have reserved names, and are executed at different times during the request lifecycle:

* `$PRE`: Executes immediately when a worker picks up the request, before it is materialized.

* `$RETRY`: Executes immediately after receiving the HTTP response. It must return a Lua number representing milliseconds to wait before retrying, or a negative number to stop.

* `$ASSERT`: Executes after the retry loop completes. Used to fail tests.

* `$POST`: Executes after assertions. Used to extract data from the response and save it to the global sandbox.

## CONFIGURATION

`sinq` uses JSON-formatted `.scenario` files along the scenario path to manage environments, timeouts, and other configurations. When a leaf directory inherits a `.scenario` file from a parent, the configurations are deep merged (the only exclusion being the `env_matrix` and `tag` lists, which all get combined into flat lists), with the deeper (child) configuration taking precedence. Unmentioned default values are preserved. 

Available keys include `name`, `description`, `env`, `req_timeout`, `script_timeout`, `timeout`, `fail_fast`, `max_retries`, `max_redirects`, `max_body`, `env_matrix` and `tags`. The `env` object is parsed into a global Lua table and can be accessed directly in any Lua script. `env_matrix` is accumulated from all the `.scenario` files in the scenario. Every entry should be a json object, every key being a label for a particular dataset, and the value for the key being another object. Then, treating every entry as a matrix or axis, `sinq` creates a Cartesian product of all of them, and the scenario is run multiple times for every unique resulting combination, the combinations being deep merged with the `env` data.

## OPTIONS

**-v**, **--version**
: Print the current sinq version and exit.

**-h**, **--help**
: Print help message and exit.

**-w**, **--workers** *int*
: Number of concurrent workers (default 10).

**-i**, **--insecure**
: Disable SSL/TLS certificate verification.

**-s**, **--secret** *string*
: Key=value pair overrides for scenario secrets

**-e**, **--env** *string*
: Key=value pair overrides for all scenario environments

**-o**, **--out** *path*
: Path to write the output file (prints to stdout if omitted).

**-L**, **--log-level** *string*
: Log level to use: debug, info, warn or error (default "warn")

**-f**, **--format** *string*
: Output format: std or junit (default "std").

**-V**, **--verbose**
: Enable verbose reporting (reports each stage duration and timestamps)

**-c**, **--color** *string*
: Terminal colors: always, never, auto (default "auto").

**-S**, **--show** *string*
: Which results to show in the output: all, no-skip, failures (default "no-skip")

**-l**, **--list**
: Parse and list scenarios at specified directories.

**-t**, **--tag** *string*
: Execute only scenarios that have the tag

**-n**, **--name** *string*
: Execute only scenarios which names match the regular expression

**-u**, **--unrestricted**
: Load lua "os" and "io" modules for scripts

**--secrets-file** *path*
: Path to JSON-formatted secrets file

**--skip-tag** *string*
: Do not execute scenarios that have the tag

**--skip-name** *string*
: Do not execute scenarios which names match the regular expression

**--plugins** *string*
: Paths to lua plugin directory entries, joined with ':' on Linux and MacOS, ';' on Windows

**--max-cache-size** *string*
: Global maximum response size for cached requests, default 5MB

**--cache-timeout** *string*
: Global timeout for the cached requests, default 10s


**--dump-on-failure**
: Print full request and response data on failed assertion

## ENVIRONMENTAL VARIABLES

`sinq` reads the following environment variables:

**SINQ_LUA_PATH**
: Defines a list of directories that `sinq` will look for plugins in, joined with ':' on Linux and MacOS, ';' on Windows. Is ignored if **--plugins** flag is used

**HTTP_PROXY**
: Defines the proxy to use for http requests

**HTTPS_PROXY**
: Defines the proxy to use for https requests

**NO_PROXY**
: If set, no proxy is used

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
: Execute request with the 1-indexed number in the scenario next (doesn't stop current request's lifecycle).

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

### Crypto API

**sinq.crypto.base64Encode**, **sinq.crypto.base64Decode**, **sinq.crypto.base64UrlEncode**, **sinq.crypto.base64UrlDecode**
: Base64 encoding/decoding functions.

**sinq.crypto.hexEncode**, **sinq.crypto.hexDecode**
: Hexadecimal encoding/decoding functions.

**sinq.crypto.md5(string, encoding?)**, **sinq.crypto.sha1(string, encoding?)**, **sinq.crypto.sha256(string, encoding?)**, **sinq.crypto.sha512(string, encoding?)**, **sinq.crypto.hmac(source, algo?, key?, encoding?)**
: Hashing functions supporting hex, base64, base64url, and raw string encodings. Default is hex. If an unknown encoding or algorithm is provided, returns an error string as the second value.

### JWT API

**sinq.jwt.decode(token)**
: Decodes a JWT token without validating signature.

**sinq.jwt.verify(token, key, algo?)**
: Verifies the token using the key and optional algorithm. Algorithm defaults to hs256.

**sinq.jwt.sign(claimsTable, key, method?)**
: Creates and signs a JWT token. Method defaults to hs256. Passing a cyclic `claimsTable` is UB.

### JSON API

**sinq.json.parse(string)**
: Parses a JSON string into a Lua table.

**sinq.json.serialize(table, indent?)**
: Serializes a Lua table into a JSON string. Passing a cyclic table returns an error.

**sinq.json.null**
: A constant used to explicitly serialize a `null` value in JSON tables.

**sinq.fake.uuid()**
: Returns a random UUIDv4 string.

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

**req.cache(bool?)**
: Turns on/off client-side request caching. The cache is based on the data sent over the wire and any attached filenames. Parameter defaults to `true` if omitted.

**req.skip(bool?)**
: If `true` (default), marks the request to be skipped. The `$PRE` script will finish executing, but the HTTP request will not be fired and subsequent hooks are bypassed. The request is marked as `Aborted` in the reporter without throwing a test failure.

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
: Fail if values are not strictly equal.

**sinq.assert.contains(str, substring, message?)**
: Fail if string lacks substring.

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
: Count of times the request was retried before.

**res.size**
: Count of bytes written to file if `req.saveResponseTo` was called in `$PRE` hook.

**res.oversized**
: Boolean flag set if the response body size was bigger than the configured size for the scenario.

## EXIT STATUS
**0**
: Success. All discovered scenarios executed successfully, all network calls completed, and no `$ASSERT` blocks triggered a failure.

**1**
: Failure. One or more scenarios failed an assertion, encountered a network timeout, crashed, or the CLI received invalid arguments.

## EXAMPLES
Run tests in the current directory with 20 concurrent workers:
    $ sinq --workers 20 .

Run tests against specific directories with a secrets file and save JUnit output:
    $ sinq --secrets-file=prod.json --format=junit --out=results.xml ./auth ./billing

Parse and list scenarios without executing them:
    $ sinq --list ./tests

## LICENSE
Distributed under the terms of the GNU General Public License v3 (GPLv3).

## AUTHOR
Written by Veitangie.
