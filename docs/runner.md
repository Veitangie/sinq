# Execution Runner

The **Runner** is the engine that executes the scenarios discovered by the Treewalker. It is built for maximum speed and isolation.

## Concurrent Execution

In `sinq`, the absolute unit of concurrency is the **Scenario** (an entire workflow chain), not the individual HTTP Request.

When you run `sinq`, it spins up a pool of concurrent workers (default is 10, configurable via the `-w` or `--workers` flag). If you have 10 workers and 10 scenarios that each contain 5 requests, all 10 scenarios will execute simultaneously! 

However, the 5 requests *within* each scenario are strictly guaranteed to execute sequentially. This design choice ensures that complex, multi-step workflows (like logging in, extracting a token, and polling a background job) execute with perfect determinism, while your test suite as a whole finishes as fast as your network allows.

## Network & Session State

While the workers operate independently, they share underlying resources to optimize performance:

* **Connection Pooling:** All workers share a single, underlying TCP transport pool. This allows `sinq` to reuse keep-alive connections across different scenarios.
* **Cookie Isolation:** Despite sharing the TCP transport layer, every single scenario execution creates a brand new, isolated `http.CookieJar`. Cookies set by a server in Scenario A will be completely invisible to Scenario B.

---

## Concurrency Architecture

The `sinq` execution engine is built around a classic coordinator-worker pool pattern in Go.

### The Coordinator-Worker Model

The Treewalker emits a slice of scenario blueprints. The main Runner coordinator then feeds these blueprints into a buffered Go channel. As soon as a worker finishes a scenario, it pulls the next one from the channel queue until it is empty.

Each concurrent worker gets its own isolated Lua VM. Because the Treewalker branches the DAG at the directory level, if Leaf A and Leaf B both inherit `01_login.sinq`, they will each execute it independently in their own VMs.

### AST Bytecode Caching & Request Collapsing

When a worker encounters a Lua script block (like a `$PRE` or `$ASSERT` block), it does not execute the raw string. It parses and compiles the script into an Abstract Syntax Tree (AST) bytecode. 

To prevent 100 workers from simultaneously compiling the exact same `01_login.sinq` script, the Runner maintains a thread-safe, globally shared AST cache. The cache key is bound to the physical byte-offset of the script in the `.sinq` file.

Furthermore, `sinq` can cache the actual HTTP responses to avoid re-executing identical requests. This is **opt-in per request** by calling `req.cache(true)` in the request's `$PRE` block. When enabled, `sinq` behaves both as a concurrent singleflight coalescer and a global response cache across all workers. If multiple workers attempt the exact same request simultaneously, the first worker executes it while the others pause and receive the result instantly. The response is then cached for the duration specified by `--cache-timeout` (default 10s) up to a maximum response body size of `--max-cache-size`. **Note that the request cache itself is unbounded and has no eviction policy. It will store all uniquely cached requests indefinitely until the test suite finishes.**

### Context Cancellation & Graceful Degradation

The runner relies on Go's `context` package to manage the lifecycle of the test suite. 

If a scenario exceeds its configured `timeout`, or if a user sends an interrupt signal (`SIGINT` / `Ctrl+C`) to the CLI, the context is immediately canceled. 
* Any in-flight HTTP requests are terminated.
* Any sleeping `$RETRY` loops are woken up and aborted.
* The worker marks the scenario as `Aborted` and skips all remaining requests in that chain.
