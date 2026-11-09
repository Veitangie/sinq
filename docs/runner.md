# Execution Runner

The **Runner** executes the scenarios the Treewalker discovered.

## Concurrent Execution

The unit of concurrency is the **scenario**, not the individual stage. `sinq` runs a fixed pool of workers (10 by default, set with `-w` / `--workers`), and each worker runs one scenario at a time. Stages inside a scenario always run in order; unrelated scenarios overlap.

The Treewalker produces a list of scenario runs. The coordinator feeds them into a channel, and each worker pulls the next one when it finishes the current one.

## Isolation and Shared State

Every scenario run starts with a fresh Lua state and its own `http.CookieJar`. If Leaf A and Leaf B both inherit `01_login.sinq`, each runs its own copy, and a cookie set in A is never visible to B.

Workers share:

* **The connection pool.** Keep-alive connections are reused across scenarios.
* **The compiled script cache.** Each Lua block is compiled once and reused, keyed by a hash of its file, name, byte offset and contents.
* **The response cache**, when a request opts in (below).

## Response Caching & Request Collapsing

A request opts into caching with `req.cache(true)` in `$PRE`, or `req.cache(key)` to share one response between all requests that use the same key. If several workers fire the same cached request at once, the first one sends it and the rest wait for its result (singleflight).

That shared call belongs to no single scenario, so it is bounded by `--cache-timeout` (default 10s) instead of `req_timeout`, and reads at most `--max-cache-size` (default 5 MiB) of body. Nothing is evicted: every cached response stays until the run ends.

## Cancellation

* **Scenario `timeout`:** in-flight requests are dropped, sleeping `$RETRY` loops wake, and the scenario ends with an **Error** (deadline exceeded).
* **`SIGINT` / `Ctrl+C`** (or `SIGTERM`): the same, but running scenarios are marked **Aborted**. Scenarios that hadn't started yet are not started and don't appear in the report. The run exits with `130`, unless something had already failed or errored (see [Exit codes](cli.md#exit-codes)).
