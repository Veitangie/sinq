[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![codecov](https://codecov.io/github/Veitangie/sinq/graph/badge.svg?token=MVHIV761LR)](https://codecov.io/github/Veitangie/sinq)
![Pipeline Status](https://github.com/Veitangie/sinq/actions/workflows/ci.yml/badge.svg)
![Release Version](https://img.shields.io/github/v/release/Veitangie/sinq?include_prereleases&logo=github)

Have you ever felt the pain of wiring up a bunch of independent requests into one stateful flow from start to finish? I have, and while manageable, one thing always bugged me. Why do we treat end-to-end API tests as a bunch of independent requests that we have to somehow bundle together, when they actually are much closer to stateful workflows? That's why this tool was born. It is designed to do one job - execute sequences of requests to walk through different workflow scenarios. Instead of maintaining large request collections or YAML-heavy test definitions, I wanted to describe real user workflows directly in files and let `sinq` compile them into executable scenarios. And make it natively parallel, why not? So I present to you:

# sinq 

`sinq` is a concurrent integration and end-to-end http testing tool that treats your filesystem as a workflow definition.

Write requests as near-raw HTTP, add Lua where logic is needed, and organize files into directory trees. Every leaf directory becomes an isolated execution scenario with its own state, configuration, and request chain.

## Why sinq?

`sinq` is:
* **Workflow-Oriented:**
    - Build authentication, creation, processing and verification flows as file trees.
    - Shared setup lives in parent directories.
    - Leaf directories become executable scenarios.
* **Simple:** A `.sinq` file is just **raw HTTP + Lua**. There are no abstractions to fight, what you write is what gets sent over the wires. If you can write a cURL command and a basic script, you can write a `sinq` test.
* **Natively parallel:** Scenarios don't share global state, which allows to run all of them in parallel, bounded only by network (or configuration).
* **Fully scriptable:** Pass JWTs, correlation IDs, and dynamic payloads between chained requests, manage execution flow, run scripts on lifecycle hooks and more.
* **Lightweight & built for CI/CD:** Distributed both as a lightweight binary and a container, requires minimal setup to run. Native support for JUnit XML reporting.

### Show Don't Tell: A Simple Healthcheck

Here is what a simple one-off request looks like in `sinq`.

**`healthcheck.sinq`**
```text
GET ${env.BASE_URL}/health

$ASSERT{ sinq.assert.code(200, "Healthcheck failed") }
```

### Show Don't Tell 2: A Stateful Poller

Here is what a complete authentication, execution, and polling chain looks like in `sinq`. 

**`01_login.sinq`**
```text
POST ${env.BASE_URL}/login
Accept: application/json

$ASSERT{ sinq.assert.code(200, "Login failed") }

$POST{
    -- Parse the body as json
    local data = res.json()
    -- Save only what matters to the global environment
    AUTH_TOKEN = data.token
}
```

**`02_trigger_and_poll.sinq`**
```text
POST ${env.BASE_URL}/jobs
Authorization: Bearer ${AUTH_TOKEN}

{ "action": "export" }

$RETRY{ return sinq.retry.when(res.json().status == "pending", 50 * sinq.ms) }

$ASSERT{ sinq.assert.isTrue(res.json().status == "complete", "Job never completed") }
```

---

## Installation & Usage

### Via Go (Requires Go 1.25+)
```bash
go install github.com/Veitangie/sinq/cmd/sinq@latest
```
> NOTE: To use sinq after installing it via go make sure your go bin directory is in your `$PATH`

### Docker (Alpine Minimal)
```bash
docker pull ghcr.io/veitangie/sinq:latest
docker run -v $(pwd):/tests ghcr.io/veitangie/sinq /tests
```

### From Install Script
```bash
curl https://raw.githubusercontent.com/Veitangie/sinq/refs/heads/main/install.sh | bash
```
> NOTE: This script is targeting **stable releases only**, to install a release candidate or a pre-release you need to pass target version explicitly

### From Source
```bash
git clone git@github.com:Veitangie/sinq.git
cd sinq
go build ./cmd/sinq/
```

### As [GitHub Action](https://github.com/Veitangie/sinq-action)
```yaml
steps:
  - name: Checkout code
    uses: actions/checkout@v6

  - name: Run Sinq
    uses: Veitangie/sinq-action@v1
    with:
      args: '-w 10 -S path/to/secrets.json tests/e2e'
```

---

## File & Directory Structure

### Example File Structure And Resulting Scenarios

The basic unit of execution for `sinq` is a scenario. They are built from the filesystem roots passed to the tool.

Because `sinq` relies on directory hierarchy to build scenarios, you can define shared setup steps (like authentication) at the root, and branch off into specific test cases in subdirectories. Every leaf directory becomes one executable scenario. Parent files are inherited by all descendant scenarios. Scenario configurations are **aggregated** along the whole path, with the deeper nested ones taking precedence.

#### Example 1: Deep Chain
If you don't branch, `sinq` just keeps appending files until it hits the bottom.
```text
flow/
├── 01_init.sinq
└── stage_one/
    ├── 00_process.sinq
    └── stage_two/
        └── 00_finalize.sinq
```
Because `stage_two` is the only directory with no subfolders, this resolves into **exactly one scenario**:

* **Execution Order:** `01_init.sinq` ➔ `00_process.sinq` ➔ `00_finalize.sinq`
*Notice how the files in the subdirectories always follow the files from parent directories despite natural order globally being different. **Natural ordering only applies within the same directory***

#### Example 2: Branching
```text
tests/
├── 00_base.scenario       (Sets "timeout": "5s", "env": {"host": "api.local"})
├── 01_auth.sinq           (Logs in, saves AUTH_TOKEN to globals)
├── users/
│   ├── 02_create.sinq     (Uses AUTH_TOKEN)
│   └── 03_delete.sinq     (Uses AUTH_TOKEN)
└── payments/
    ├── payments.scenario  (Overrides "timeout": "15s" only for the payments/ scenario)
    ├── 02_process.sinq    (Uses AUTH_TOKEN)
    └── 03_refund.sinq     (Uses AUTH_TOKEN)
```

Running `sinq ./tests` identifies **two leaf directories** (`users/` and `payments/`), resulting in **two distinct scenarios** that will run concurrently:

1.  **Scenario A (The `users` leaf):**
    * **Config:** `00_base.scenario`
    * **Execution Order:** `01_auth.sinq` ➔ `02_create.sinq` ➔ `03_delete.sinq`
2.  **Scenario B (The `payments` leaf):**
    * **Config:** Aggregation of `00_base.scenario` + `payments.scenario` (Timeout is now 15s)
    * **Execution Order:** `01_auth.sinq` ➔ `02_process.sinq` ➔ `03_refund.sinq`

*Notice how `01_auth.sinq` is executed independently at the start of both scenarios. They do not share the same Lua VM instance; they just inherit the same structure.*

More detailed explanation of the algorithm can be found in the [Treewalker documentation](docs/treewalker.md)

> Currently, leaf directories are expected to contain at least one `.sinq` or `.scenario` file.
> Directories that contain neither `.sinq`/`.scenario` files nor subdirectories are not considered valid scenario definitions.

---

## The `.sinq` Format

A `.sinq` file is a standard HTTP request with optional embedded Lua scripts. 

There are two categories of scripts within a `.sinq` file:
1. **General/Inline Scripts:** `$MY_VAR`, `$` (unnamed). These are evaluated to dynamically generate the outgoing HTTP request. The return value is injected directly into the request payload or headers. If a general script fails, `sinq` attempts to automatically prepend `return ` and retry, enabling simple string interpolations like `${env.HOST}`.
2. **Lifecycle Scripts:** `$PRE`, `$RETRY`, `$ASSERT`, `$POST`. These strictly control the execution flow and state of the request. To prevent side-effect leaks and ensure thread safety, **specific APIs are only available during specific lifecycle stages**.

### Lifecycle Scopes

* **`$PRE` (Setup & File I/O):** Executes immediately when a worker picks up the request, before it is materialized. This is the **only** scope where you can modify the filesystem interactions for the request. Current request body payload is inaccessible here.
    * `req.attach("path/file.txt")` — Replaces the request body with the contents of a file. (Fails if a body is already defined in the request).
    * `res.saveTo("path/download.bin")` — Streams the incoming response body directly to disk, bypassing the Lua memory buffer.

* **`$RETRY` (Retry Policies):** Executes immediately after receiving the HTTP response. **This is the only lifecycle script that must return a value.** It must return a Lua number representing milliseconds to wait before retrying, or a negative number to stop retrying.
    * Scope-Exclusive API: `sinq.retry.when()`, `sinq.retry.whenExponential()`, `sinq.retry.withJitter()`.

* **`$ASSERT` (Validation):** Executes after the retry loop completes. Used to fail tests.
    * Scope-Exclusive API: `sinq.assert.code()`, `sinq.assert.equals()`, `sinq.assert.contains()`, `sinq.assert.isTrue()`, `sinq.assert.fail()`.

* **`$POST` (State Extraction):** Executes after assertions. Used to extract data from the response and save it to the global sandbox for subsequent requests. **It will not execute if `$ASSERT` calls `sinq.assert.fail` and the scenario setting `fail_fast` is true.**

---

## Lua API

`sinq` exposes the following API and global variables to Lua scripts:

### Globals & Control Flow
* `env` — Table of environment variables configured for the scenario.
* `secrets` — Table of secrets passed via the `-S` argument.
* `sinq.setNextRequest(index)` — Change execution flow to the n-th request (1-indexed). Allows loops or skipping requests.
* `res` — Shorthand for current request's response table
* `req` — Shorthand for current request table

### Time Constants
* `sinq.ms` (1)
* `sinq.second` (1000)
* `sinq.minute` (60000)
* `sinq.hour` (3600000)

### Responses
All completed responses are stored in the global `sinq.responses` table. Lua is 1-indexed, so the response to the first request is `sinq.responses[1]`.

Each response table contains:
* `attempt` *(number)* — The current retry iteration.
* `code` *(number)* — HTTP status code.
* `headers` *(table)* — Response headers (multiple headers with the same key are stored as nested array tables).
* `oversized` *(boolean | nil)* — `true` if the payload exceeded `MaxBodySize` and was clamped.
* `size` *(number | nil)* — Total bytes written to disk (only present if `res.saveTo()` was used in `$PRE`).
* `bodyRaw` *(string | nil)* — Raw response body bytes (only present if `res.saveTo()` wasn't used in `$PRE`).
* `bodyJson` *(table | nil)* — The cached JSON table (initially `nil`).

**JSON Parsing Methods (Only Present If `res.saveTo()` Wasn't Used In $PRE):**
* `extractBodyJson()` — Safely parses `bodyRaw`, stores it in `bodyJson`, and returns `(table, error)`.
* `json()` — Unsafe convenience parser. Returns the table directly, but calls `error()` and fails the scenario if the body is not valid JSON.

*Note: The response table is populated during execution; a response index is only non-nil after the corresponding request has been sent.*

---

## Configuration & Environment

`sinq` uses JSON-formatted `.scenario` files along the scenario path to manage environments, timeouts, and other configurations.

Default configuration that can be overridden in `.scenario` files:
```json
{
  "name": "path/to/first/.scenario/file",
  "description": "",
  "env": { },
  "timeout": "5s",
  "fail_fast": true,
  "max_retries": 10,
  "max_redirects": 5,
  "max_body_size": "1MB"
}
```

* **`max_body_size`**: Protects the Lua VM from massive payloads. If a response exceeds this limit, it is safely truncated and the response's `oversized` flag is set to `true`.
* **`timeout`**: The global timeout for the entire scenario execution.

Everything defined in the `env` object can be accessed directly in your HTTP paths and headers using `${env.variableName}`, or inside your Lua scripts via the global `env` table.

---

## Secrets & Security

Secrets are loaded from a JSON file provided through `--secrets`.

To reduce the risk of accidental exposure, some error messages intentionally omit sensitive values. Verbose mode (`--verbose`) may include additional debugging information and **should be used carefully in CI environments where logs are retained**.

---

## Lua Sandboxing & Performance

By default, `sinq` prioritizes performance by reusing and resetting Lua Virtual Machines (`LState`) between scenarios instead of destroying and recreating them from scratch. While intentional global variables (like `AUTH_TOKEN`) are passed safely through scenario request chains and discarded at the end of every scenario, mutating core Lua libraries can theoretically pollute the VM worker for the next scenario that picks it up.

**The `--safe` (`-s`) Flag:**
If you suspect state leakage across concurrent scenarios is causing flaky tests, use the `-s` flag. This forces `sinq` to instantiate a brand new, pristine Lua VM for every single request. It guarantees total isolation but incurs a performance and memory allocation penalty.

**How to avoid needing `--safe`:**
1. **Scope your variables:** Use `local myVar = ...` for temporary data. Only assign to global variables (or `_G`) when you explicitly need to pass data to the next `.sinq` file in the scenario chain.
2. **Never mutate standard libraries:** Do not overwrite core Lua functions (e.g., `table.insert = ...`).

---

## Usage

Point `sinq` at a directory containing your `.sinq` files. `sinq` will automatically sort them in natural order, bundle them into scenarios and execute them concurrently.

```bash
# Run a standard test suite
sinq ./tests/integration

# Run tests and output a JUnit report for your CI pipeline
sinq -f junit -o report.xml ./tests/integration

# Ignore self-signed TLS certificates and enable verbose debug logging
sinq -iV ./tests/local
```

### Options

```text
  -w, --workers int    Number of concurrent workers (default 10)
  -s, --safe           Instantiate a new Lua VM per request instead of resetting state
  -i, --insecure       Disable SSL/TLS certificate verification
  -S, --secrets path   Path to the secrets JSON file
  -o, --out path       Path to write the output file (prints to stdout if omitted)
  -f, --format string  Output format: std or junit (default "std")
  -V, --verbose        Enable verbose logging
  -c, --color string   Terminal colors: always, never, auto (default "auto")
  -l, --list           Parse and list scenarios at specified directories
  -h, --help           Print this help message and exit
  -v, --version        Print the current sinq version and exit
```

---

## When To Choose Sinq

Choose `sinq` when:
- You need stateful integration workflows with minimal setup.
- You prefer files over GUI collections.
- You don't need the whole JS runtime to make http requests.
- You run tests primarily in CI or CLI interfaces.

Choose another tool when:
- You need a graphical API explorer.
- You need browser automation.
- You need fully custom test frameworks written in a general-purpose language.

---

## Useful Links

- [Documentation](docs/)
- [Tree-sitter Grammar](https://github.com/Veitangie/tree-sitter-sinq) - syntax highlighting for tree-sitter compatible editors
- [VSCode Extension](https://marketplace.visualstudio.com/items?itemName=Veitangie.sinq) - syntax highlighting for VSCode

---

## Acknowledgments & Credits
This project includes natural sorting logic adapted from [facette/natsort](https://github.com/facette/natsort), which is distributed under the 3-Clause BSD License.
Copyright (c) 2015, Vincent Batoufflet and Marc Falzon. All rights reserved.

Big thanks to [yuin](https://github.com/yuin) for his [gopher-lua](https://github.com/yuin/gopher-lua) project.

---

## License
Copyright (C) 2026 Veitangie.
Distributed under the terms of the [GNU General Public License v3 (GPLv3)](LICENSE.md).
