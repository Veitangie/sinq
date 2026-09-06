# Scenarios & Configuration

A **scenario** is a sequence of HTTP requests run in order, defined across one or more `.sinq` files.

## Writing Requests

A `.sinq` file is a standard HTTP request. You can define multiple requests in a single file by separating them with the `###` delimiter. The text after `###` on the same line names the request that *follows* it. This name shows up in `--list` output and in the standard/JUnit reports.

```http
### Login Request
POST /api/auth
{"user": "admin"}

### Fetch Data
GET /api/data
```

A leading `###` (before any request content) works the same way and names the file's *first* request, even if the file only contains one:

```http
### Healthcheck
GET /health
```

To make your requests dynamic, you can use Lua scripts. `sinq` provides two ways to run scripts:

1. **Inline Interpolation (`${...}`)**: Used to dynamically insert values into headers, URLs, or JSON payloads (e.g., `${env.BASE_URL}`).
2. **Lifecycle Hooks (`$PRE`, `$ASSERT`, etc.)**: Used to control the flow of the request, such as failing a test if a status code is wrong, or polling until a background job completes.

## Configuration

You can configure timeouts, environment variables, and limits using JSON files ending in `.scenario`. The filename itself is arbitrary, only the extension matters (e.g. `config.scenario`, `00_base.scenario`).

When you place a `.scenario` file in a directory, those settings apply to all tests in that directory and its subdirectories.

Available settings:

```json
{
  "name": "My API Test",
  "description": "Tests the core user flow",
  "env": {
    "BASE_URL": "https://api.local"
  },
  "req_timeout": "5s",
  "script_timeout": "5s",
  "timeout": "10m",
  "fail_fast": true,
  "max_retries": 10,
  "max_redirects": 5,
  "max_body": "1MiB",
  "env_matrix": [],
  "tags": []
}
```

* **`name`**: Scenario name, shown in reports and used by the `-n` / `--no-name` filters. Defaults to the leaf directory path. If the same `.scenario` file is inherited by several leaf directories, they all take this name.
* **`description`**: Free-text description of the scenario.
* **`env`**: Variables defined here become accessible in your `.sinq` files as `${env.VARIABLE_NAME}` or in Lua scripts via the `env` table.
* **`req_timeout`**: Maximum time to wait for a single HTTP network request.
* **`script_timeout`**: Maximum time a single Lua script block is allowed to run.
* **`timeout`**: Maximum time allowed for the entire scenario to complete.
* **`fail_fast`**: If true, the scenario aborts immediately upon the first assertion failure.
* **`max_retries`**: The maximum amount of times any request in the scenario can be retried upon retry script returning a valid non-negative number.
* **`max_redirects`**: The maximum amount of redirects the client will follow before returning the redirect as the actual response.
* **`max_body`**: The maximum response body size stored in memory. Responses exceeding this are safely truncated.
* **`env_matrix`**: Data sets for running the scenario once per combination (parameterized / matrix testing). Aggregated along the path. See [Environment Matrix](env-matrix.md).
* **`tags`**: Labels attached to every scenario that inherits this file. Collected into one list along the path and matched by the `-t` / `--tag` and `--no-tag` filters.

---

## The Request Lifecycle State Machine

Each request goes through these stages in order:

1. **`$PRE` Execution:** Executes first. This is where you configure dynamic variables or file I/O (`req.attach`, `req.multipart`, `req.saveResponseTo`, `req.cache`, `req.skip`). *Current HTTP request body and headers are not yet accessible.*
2. **Materialization:** The engine scans the raw HTTP text and evaluates inline scripts (e.g., `${env.HOST}`). The output is injected directly into the byte stream.
3. **HTTP Parsing:** The materialized byte stream is parsed into a standard Go `http.Request`.
4. **Execution (Send):** The HTTP request is sent over the network.
5. **`$RETRY` Loop:** Executes immediately after receiving the response. Must return a number: milliseconds to sleep before retrying (jumps back to Step 4). A negative number breaks the loop.
6. **`$ASSERT` Execution:** Evaluates the final response to pass or fail the test.
7. **`$POST` Execution:** Used to extract state. *Skipped if `$ASSERT` failed and `fail_fast` is true.*

## Configuration Aggregation (Deep Merging)

When a leaf directory inherits a `config.scenario` file from a parent, configurations are **deep merged**.

If a parent sets `"req_timeout": "5s"` and a child sets `"env": {"NEW": "true"}`, the resulting scenario will have both settings. If both define the same key, the child's value overwrites the parent's value. Unmentioned default values (like `fail_fast`) are preserved throughout the merge chain.

## AST Caching & Request Collapsing

`sinq` compiles every Lua block to bytecode (AST) and caches it in memory. The cache key is tied to the physical byte-offset of the script in the file. 

If multiple workers process an identical request at the same time, `sinq` can collapse them with a `singleflight` mechanism. This is strictly **opt-in per request** by calling `req.cache(true)` in the `$PRE` block. When enabled, the first worker performs the network call, while all other waiting workers receive the cached result instantly when the first finishes.
