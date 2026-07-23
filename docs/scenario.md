# The Scenario Lifecycle & Parser Rules

Every leaf directory in `sinq` represents an isolated, concurrently executed **Scenario**. A scenario is a sequence of HTTP requests defined in `.sinq` files. 

Understanding how `sinq` parses these files and manages the lifecycle of a request is crucial for building complex workflows.

## The Request Lifecycle State Machine

When a worker picks up a scenario, it executes the requests sequentially. For a single `.sinq` file, the engine strictly enforces the following state machine:

1. **`$PRE` Script Execution:** The `$PRE` block executes first. This is where you configure dynamic variables or check global state inherited from the previous file. *Note: The current HTTP request body and headers are not yet accessible in this phase.*
2. **Materialization (Interpolation):** The engine scans the raw HTTP text and evaluates all general and unnamed scripts (e.g., `${env.HOST}`). The output of these scripts is injected directly into the raw text byte stream.
3. **HTTP Parsing:** The fully materialized byte stream is parsed into a standard Go `http.Request`.
4. **Execution (Send):** The HTTP request is sent over the network. The response is captured and parsed into `sinq.responses`.
5. **`$RETRY` Loop:** If a `$RETRY` block exists, it executes. If it returns a number greater than 0, the worker sleeps for that many milliseconds and then jumps back to Step 4. If it returns 0 or less, the loop breaks.
6. **`$ASSERT` Execution:** The `$ASSERT` block evaluates the final response. If you call `sinq.assert.fail("reason")`, the test fails.
7. **`$POST` Execution:** The `$POST` block executes. *If the `$ASSERT` block failed the test and the scenario's `fail_fast` configuration is `true`, this step is skipped.*

## The Custom Parser & Scripts

A `.sinq` file is parsed using a custom lexer that separates raw HTTP text from Lua scripts. 

### Unnamed Scripts & Interpolation
The syntax `${env.BASE_URL}` is syntactic sugar. When the parser encounters a `$` followed immediately by `{`, it creates an **Unnamed Script**. 

Under the hood, `sinq` takes the contents of that script, prepends the `return` keyword, and executes it in the Lua VM. 
* `${ env.BASE_URL }` effectively becomes `$Unnamed_1{ return env.BASE_URL }`.
* If a script does not explicitly return a value, `sinq` will attempt to execute it normally, and if that yields a compilation error, it falls back to recompiling it with a `return` statement. For complex multiline interpolation, it is always faster to write `return my_value` explicitly.
* **If both attempts fail, the error of the first (unmodified) run is shown.**

### AST Caching & Request Collapsing
To maintain high performance, `sinq` compiles all Lua scripts into bytecode (AST) and caches them in memory. The cache key is tied to the physical byte-offset of the script in the file. This means you can have 1,000 workers executing the same scenario concurrently, and the Lua engine will only compile the bytecode once. Furthermore, if multiple workers attempt to process identical requests simultaneously, `sinq` can use a `singleflight` mechanism to collapse the execution. This is strictly **opt-in per request** by calling `req.cache(true)` in the `$PRE` block. When enabled, the first worker performs the processing, while all other waiting workers receive the cached result instantly when the first finishes.

### Escape Sequences
If you need to send a literal `$PRE{` or `${` string in a JSON payload without `sinq` attempting to execute it as Lua, use the backslash escape character `\`.

```text
POST /comments
Content-Type: text/plain

User said: \$PRE{ this is not a script }
```

## Scenario Configuration

`sinq` uses JSON-formatted `.scenario` files along the scenario path to manage environments, timeouts, and other configurations.

Default configuration that can be overridden in `.scenario` files:
```json
{
  "name": "path/to/dir/of/last/file",
  "description": "",
  "env": { },
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

* **`name`**: The name of the scenario. If this particular `.scenario` file is used in several scenarios - they will all have the same name.
* **`description`**: Description of the scenario.
* **`env`**: Object that will be parsed into `sinq.env` Lua table, which will then be accessible from all Lua scripts.
* **`req_timeout`**: Timeout for any single request in the scenario.
* **`script_timeout`**: Timeout for any single script run in the scenario.
* **`timeout`**: Total timeout for the whole scenario.
* **`fail_fast`**: When true, scenarios will not be run if any of them fails to compile for some reason, and the scenarios stop at the first failed assertion.
* **`max_retries`**: The maximum amount of times any request in the scenario can be retried upon retry script returning a valid non-negative number.
* **`max_redirects`**: The maximum amount of redirects the client will follow before returning the redirect as the actual response.
* **`max_body`**: Maximum size of response body that will be stored in memory during scenario execution. If a response exceeds this limit, it is safely truncated and the response's `oversized` flag is set to `true`.
* **`env_matrix`**: Data sets for the environment matrix mechanism - `sinq`'s take on matrix/combinatorial/parametrized testing. For more information and examples please check out the [documentation](env-matrix.md).
* **`tags`**: Tags or labels assigned to scenarios containing this `.scenario` file. They get collected into one list for the resulting scenario.

Everything defined in the `env` object can be accessed directly in your HTTP paths and headers using `${env.variableName}`, or inside your Lua scripts via the global `env` table.
