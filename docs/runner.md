# Runner Architecture & Concurrency Model

The `sinq` execution engine is built around a classic coordinator-worker pool pattern. Understanding how the runner distributes work and manages memory will help you optimize your test suites and debug complex state issues.

## The Coordinator-Worker Model

When `sinq` launches, the Treewalker builds a complete list of all executable scenarios (leaf directories). The main Runner then spins up a fixed pool of concurrent workers (configured via the `-w` or `--workers` flag, which defaults to 10). 

The coordinator feeds these scenarios into a buffered Go channel. As soon as a worker finishes a scenario, it pulls the next one from the queue until the channel is empty. 

## The Unit of Concurrency

In `sinq`, the absolute unit of concurrency is the **Scenario**, not the Request. 

If you have 10 workers and 10 scenarios containing 5 requests each, all 10 scenarios will execute simultaneously. However, the 5 requests *within* each scenario are strictly guaranteed to execute **sequentially**. 

This design choice was made to ensure that complex, multi-step workflows (like logging in, extracting a token, and polling a background job) execute with determinism, while the test suite as a whole finishes as fast as the network allows.

## Worker Isolation

Each concurrent worker gets its own Lua VM. Because the Treewalker branches the DAG at the directory level, if Leaf A and Leaf B both inherit `01_login.sinq`, they will each execute it independently.

To achieve this, `sinq` uses a soft-reset mechanism. Instead of destroying and rebuilding the Lua `LState` for every scenario, the worker reuses the VM but provides a semi-sandboxed environment to the scripts that is reset across scenario boundaries. However, this semi-sandboxed environment still allows user code to persistently mutate global libraries. These mutations (for example of `table.insert`) **will** persist for all scenarios run on the worker. Any mutations of `sinq`, `env` and `secrets` tables are guaranteed to persist within the scenario but not carry over to any other scenario.

*(If you suspect a core Lua library was mutated and leaked across scenarios, you can force a hard-reset of the VM on every request using the `--safe` flag).*

## Network & Session State

While the workers operate independently, they share underlying resources to optimize performance:

* **Connection Pooling:** All workers share a single, underlying `http.Transport`. This allows `sinq` to reuse keep-alive TCP connections
* **Cookie Isolation:** Despite sharing the TCP transport layer, every single scenario execution creates a brand new, isolated `http.CookieJar`. Cookies set by a server in Scenario A will be completely invisible to Scenario B.

## AST Bytecode Caching & Request Collapsing

When a worker encounters a Lua script block (like a `$PRE` or `$ASSERT` block), it does not execute the raw string. It parses and compiles the script into an Abstract Syntax Tree (AST) bytecode. 

To prevent 100 workers from simultaneously compiling the exact same `01_login.sinq` script, the Runner maintains a thread-safe, globally shared AST cache. The cache key is bound to the physical byte-offset of the script in the `.sinq` file.

Furthermore, if multiple concurrent workers attempt to process the exact same request file simultaneously, `sinq` can use a `singleflight` mechanism. This is **opt-in per request** by calling `req.cache(true)` in the request's `$PRE` block. When enabled, the first worker performs the parsing and execution, while all other waiting workers pause and receive the cached result instantly when the first worker finishes, preventing the thundering herd problem.

## Context Cancellation & Graceful Degradation

The runner relies on Go's `context` package to manage the lifecycle of the test suite. 

If a scenario exceeds its configured `timeout`, or if a user sends an interrupt signal (`SIGINT` / `Ctrl+C`) to the CLI, the context is immediately canceled. 
* Any in-flight HTTP requests are terminated.
* Any sleeping `$RETRY` loops are woken up and aborted.
* The worker marks the scenario as `Aborted` and skips all remaining requests in that chain.
