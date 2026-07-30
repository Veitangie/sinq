# Scenarios & Configuration

In `sinq`, a **Scenario** is simply a sequence of HTTP requests that are executed in order. You define these requests in `.sinq` files.

## Writing Requests

A `.sinq` file is essentially a standard HTTP request. You can define multiple requests in a single file by separating them with the `###` delimiter.

```http
### Login Request
POST /api/auth
{"user": "admin"}

### Fetch Data
GET /api/data
```

To make your requests dynamic, you can use Lua scripts. `sinq` provides two ways to run scripts:

1. **Inline Interpolation (`${...}`)**: Used to dynamically insert values into headers, URLs, or JSON payloads (e.g., `${env.BASE_URL}`).
2. **Lifecycle Hooks (`$PRE`, `$ASSERT`, etc.)**: Used to control the flow of the request, such as failing a test if a status code is wrong, or polling until a background job completes.

## Configuration

You can configure timeouts, environment variables, and limits using `.scenario` JSON files. 

When you place a `config.scenario` file in a directory, those settings apply to all tests in that directory and its subdirectories.

Here are the available settings you can configure:

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
  "max_body": "1MiB"
}
```

* **`env`**: Variables defined here become accessible in your `.sinq` files as `${env.VARIABLE_NAME}` or in Lua scripts via the `env` table.
* **`req_timeout`**: Maximum time to wait for a single HTTP network request.
* **`script_timeout`**: Maximum time a single Lua script block is allowed to run.
* **`timeout`**: Maximum time allowed for the entire scenario to complete.
* **`fail_fast`**: If true, the scenario aborts immediately upon the first assertion failure.
* **`max_retries`**: The maximum amount of times any request in the scenario can be retried upon retry script returning a valid non-negative number.
* **`max_redirects`**: The maximum amount of redirects the client will follow before returning the redirect as the actual response.
* **`max_body`**: The maximum response body size stored in memory. Responses exceeding this are safely truncated.

---

## The Request Lifecycle State Machine

When a worker executes a `.sinq` request, it strictly enforces the following state machine:

1. **`$PRE` Execution:** Executes first. This is where you configure dynamic variables or file I/O. *Current HTTP request body and headers are not yet accessible.*
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

To maintain high performance, `sinq` compiles all Lua scripts into bytecode (AST) and caches them in memory. The cache key is tied to the physical byte-offset of the script in the file. 

Furthermore, if multiple workers attempt to process identical requests simultaneously, `sinq` can use a `singleflight` mechanism to collapse the execution. This is strictly **opt-in per request** by calling `req.cache(true)` in the `$PRE` block. When enabled, the first worker performs the network call, while all other waiting workers receive the cached result instantly when the first finishes.
