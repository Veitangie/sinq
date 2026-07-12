# sinq 1 "June 2026" "sinq" "Sinq Manual"

## NAME
sinq - The Spanish Inquisition

## SYNOPSIS
**sinq** [flags] [directories...]

## DESCRIPTION
**sinq** is a concurrent HTTP functional and integration testing tool. It treats your filesystem as a workflow definition, executing sequences of requests to walk through different workflow scenarios. It natively parses environment matrices to allow for combinatorial/matrix/parametrized testing, executes requests concurrently, and evaluates embedded Lua scripts for state management. Every leaf directory becomes an isolated execution scenario with its own state, configuration, and request chain.

## CONCEPTS
**Scenarios and Treewalker**
The basic unit of execution for `sinq` is a scenario. `sinq` uses a directory-traversal engine called the Treewalker to treat your physical filesystem as a Directed Acyclic Graph (DAG). It recursively descends into subdirectories, inheriting and appending sorted `.scenario` and `.sinq` files from parent directories. Once it reaches a leaf directory (a directory containing at least one `.sinq` file but no subdirectories), it compiles the path into an executable scenario.

**Concurrency**
In `sinq`, the absolute unit of concurrency is the Scenario, not the Request. Requests within each scenario are strictly guaranteed to execute sequentially, while multiple scenarios execute simultaneously in a worker pool.

**File Format & Scripts**
A `.sinq` file is a standard HTTP request with optional embedded Lua scripts. Lifecycle scripts strictly control the execution flow and state of the request:

* `$PRE`: Executes immediately when a worker picks up the request, before it is materialized.

* `$RETRY`: Executes immediately after receiving the HTTP response. It must return a Lua number representing milliseconds to wait before retrying, or a negative number to stop.

* `$ASSERT`: Executes after the retry loop completes. Used to fail tests.

* `$POST`: Executes after assertions. Used to extract data from the response and save it to the global sandbox.

**Configuration & Inheritance**
`sinq` uses JSON-formatted `.scenario` files along the scenario path to manage environments, timeouts, and other configurations. When a leaf directory inherits a `.scenario` file from a parent, the configurations are deep merged (the only exclusion being the `env_matrix` lists, which all get combined into one big list), with the deeper (child) configuration taking precedence. Unmentioned default values are preserved, while explicitly declared keys override their parent counterparts. 

Available keys include `name`, `description`, `env`, `req_timeout`, `script_timeout`, `timeout`, `fail_fast`, `max_retries`, `max_redirects`, `max_body_size`, and `env_matrix`. The `env` object is parsed into a global Lua table and can be accessed directly in any lua script.

## OPTIONS
**-w**, **--workers** *int*
: Number of concurrent workers (default 10).

**-s**, **--safe**
: Instantiate a new Lua VM per request instead of resetting state.

**-i**, **--insecure**
: Disable SSL/TLS certificate verification.

**-S**, **--secrets** *path*
: Path to the secrets JSON file.

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

**-l**, **--list**
: Parse and list scenarios at specified directories.

**-h**, **--help**
: Print this help message and exit.

**-v**, **--version**
: Print the current sinq version and exit.

## LUA API REFERENCE
### Global Variables

**env**
: Table of environment variables for the current scenario.

**secrets**
: Table of secrets loaded via the `--secrets` flag.

**req**
: Reference to the current HTTP request (Used in `$PRE`).

**res**
: Reference to the current HTTP response.

**sinq.responses**
: Table of all completed responses in the current scenario (1-indexed).

### Flow Control & Time

**sinq.setNextRequest(index)**
: Execute request with the 1-indexed number in the scenario next (doesn't stop current request's lifecycle).

**sinq.finishScenario()**
: Finish the scenario after the current request (doesn't stop current request's lifecycle).

**sinq.ms**, **sinq.second**, **sinq.minute**, **sinq.hour**
: Time constants for retry logic.

### $PRE Scope API

**req.attach(filepath)**
: Replace request body with file contents.

**req.saveResponseTo(filepath)**
: Stream upcoming response directly to disk.

### $RETRY Scope API

**sinq.retry.stop**
: Constant (-1) to break the retry loop.

**sinq.retry.when(condition, delay)**
: Retry if condition is true.

**sinq.retry.whenExponential(condition, base, constant)**
: Retry using exponential backoff.

**sinq.retry.withJitter(condition, range, delegate, args...)**
: Add randomized jitter to a retry delegate.

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
    $ sinq --secrets=prod.json --format=junit --out=results.xml ./auth ./billing

Parse and list scenarios without executing them:
    $ sinq --list ./tests

## LICENSE
Distributed under the terms of the GNU General Public License v3 (GPLv3).

## AUTHOR
Written by Veitangie.
