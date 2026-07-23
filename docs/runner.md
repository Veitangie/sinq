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

## Network & Session State

While the workers operate independently, they share underlying resources to optimize performance:

* **Connection Pooling:** All workers share a single, underlying `http.Transport`. This allows `sinq` to reuse keep-alive TCP connections
* **Cookie Isolation:** Despite sharing the TCP transport layer, every single scenario execution creates a brand new, isolated `http.CookieJar`. Cookies set by a server in Scenario A will be completely invisible to Scenario B.

## AST Bytecode Caching & Request Collapsing

When a worker encounters a Lua script block (like a `$PRE` or `$ASSERT` block), it does not execute the raw string. It parses and compiles the script into an Abstract Syntax Tree (AST) bytecode. 

To prevent 100 workers from simultaneously compiling the exact same `01_login.sinq` script, the Runner maintains a thread-safe, globally shared AST cache. The cache key is bound to the physical byte-offset of the script in the `.sinq` file.

Furthermore, `sinq` can cache the actual HTTP responses to avoid re-executing identical requests. This is **opt-in per request** by calling `req.cache(true)` in the request's `$PRE` block. When enabled, `sinq` behaves both as a concurrent singleflight coalescer and a global response cache across all workers. If multiple workers attempt the exact same request simultaneously, the first worker executes it while the others pause and receive the result instantly. The response is then cached for the duration specified by `--cache-timeout` (default 5s) up to a maximum response body size of `--max-cache-size`, meaning subsequent workers executing that scenario later will also skip the network call and receive the cached response. **Note that the request cache itself is unbounded and has no eviction policy. It will store all uniquely cached requests indefinitely until the test suite finishes.**

## Context Cancellation & Graceful Degradation

The runner relies on Go's `context` package to manage the lifecycle of the test suite. 

If a scenario exceeds its configured `timeout`, or if a user sends an interrupt signal (`SIGINT` / `Ctrl+C`) to the CLI, the context is immediately canceled. 
* Any in-flight HTTP requests are terminated.
* Any sleeping `$RETRY` loops are woken up and aborted.
* The worker marks the scenario as `Aborted` and skips all remaining requests in that chain.
