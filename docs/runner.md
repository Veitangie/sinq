# Execution Runner

The **Runner** executes the scenarios the Treewalker discovered.

## Concurrent Execution

The unit of concurrency is the **scenario**, a whole workflow chain, not the individual request.

`sinq` runs a pool of workers (10 by default, set with `-w` / `--workers`). With 10 workers and 10 scenarios of 5 requests each, all 10 scenarios run at once.

The 5 requests inside a scenario always run in order. A login-then-extract-token-then-poll chain behaves predictably, while unrelated scenarios still overlap.

## Network & Session State

Workers run independently but share two things:

* **Connection pool.** All workers use one TCP transport pool, so keep-alive connections are reused across scenarios.
* **Cookie isolation.** Each scenario run still gets its own `http.CookieJar`. A cookie set for Scenario A is not visible to Scenario B.

---

## Concurrency Architecture

The engine is a coordinator-worker pool.

### The Coordinator-Worker Model

The Treewalker emits a slice of scenario blueprints. The coordinator feeds them into a buffered channel, and each worker pulls the next blueprint when it finishes the current one, until the channel is empty.

Every worker has its own Lua VM. If Leaf A and Leaf B both inherit `01_login.sinq`, each runs its own copy in its own VM.

### AST Bytecode Caching & Request Collapsing

A worker does not run a Lua block as raw text. It compiles the block to bytecode (an AST) first.

So that 100 workers do not all compile the same `01_login.sinq` script at once, the Runner keeps one shared, thread-safe AST cache, keyed by the script's byte offset in the file.

`sinq` can also cache the HTTP responses themselves, opt-in per request via `req.cache(true)` in `$PRE`. With it enabled, the cache doubles as a singleflight coalescer: if several workers fire the identical request at the same time, the first one runs it and the rest wait and take its result. The response is kept for `--cache-timeout` (default 10s), up to `--max-cache-size` of body. The cache has no eviction: every distinct cached request stays until the run ends.

### Context Cancellation

The Runner drives the suite lifecycle with Go's `context`.

When a scenario exceeds its `timeout`, or you send `SIGINT` / `Ctrl+C`, the context is canceled:

* in-flight requests are dropped,
* sleeping `$RETRY` loops wake and stop,
* the worker marks the scenario `Aborted` and skips the rest of its requests.
