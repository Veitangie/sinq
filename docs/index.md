# Introduction to sinq

**sinq** is a concurrent integration and end-to-end HTTP testing tool that treats your filesystem as a workflow definition.

`sinq` is built around **stateful workflows**. You write a flow as an ordered set of files, `sinq` compiles each flow into a scenario, and it runs the scenarios in parallel.

![sinq running 15 scenarios: auth, an environment matrix, retries, and a failed Lua assertion with captured output](assets/demo.gif)

---

## Why sinq?

Most API testing tools treat a test as a collection of independent requests that you stitch together after the fact. `sinq` treats it as a workflow: an ordered run of requests that pass state from one to the next, the way a real session does. Log in, hold the token, create a record, poll until it settles, check the result.

That workflow is your directory layout. A folder is a flow and the files inside it are its steps, in natural-sorted order. Setup that several flows share, like authentication, goes in a parent folder and everything beneath it inherits it. Each leaf folder is one scenario with its own Lua state and its own cookie jar, so nothing bleeds between them. Since scenarios share nothing, `sinq` runs them all at once by default, limited only by the worker count and your network.

A `.sinq` file is a plain HTTP request with Lua wherever you need logic. Nothing sits between what you write and what goes on the wire. Four hooks cover the rest of the lifecycle: `$PRE` for setup and file I/O, `$RETRY` for polling, `$ASSERT` for checks, and `$POST` for pulling values out of a response so later requests can use them.

It ships as one static binary or a small container with nothing else to install, and it writes JUnit XML for CI.

![sinq performance: 30000 requests across 15000 scenarios in 5.08s](assets/performance.png)

---

## Next

* **[Getting Started](getting-started.md)**: install and run `sinq`.
* **[CLI Reference](cli.md)**: every flag, the scenario filters, and the environment variables.
* **[Scenarios & Configuration](scenario.md)**: the request lifecycle and `.scenario` files.
* **[Lua API Reference](lua-api.md)**: built-in functions, fake data, encoding, and crypto helpers.
* **[Versioning & Compatibility](versioning.md)**: what the SemVer promise does and does not cover.
