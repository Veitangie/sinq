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
* If a script does not explicitly return a value, `sinq` will attempt to execute it normally, and if that yields no result, it falls back to recompiling it with a `return` statement. For complex multiline interpolation, it is always faster to write `return my_value` explicitly.
* **If both attempts fail, the error of the first (unmodified) run is shown**

### AST Caching
To maintain high performance, `sinq` compiles all Lua scripts into bytecode (AST) and caches them in memory. The cache key is tied to the physical byte-offset of the script in the file. This means you can have 1,000 workers executing the same scenario concurrently, and the Lua engine will only compile the bytecode once.

### Escape Sequences
If you need to send a literal `$PRE{` or `${` string in a JSON payload without `sinq` attempting to execute it as Lua, use the backslash escape character `\`.

```text
POST /comments
Content-Type: text/plain

User said: \$PRE{ this is not a script }
```
