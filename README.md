[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![codecov](https://codecov.io/github/Veitangie/sinq/graph/badge.svg?token=MVHIV761LR)](https://codecov.io/github/Veitangie/sinq)
![Pipeline Status](https://github.com/Veitangie/sinq/actions/workflows/ci.yml/badge.svg)
![Release Version](https://img.shields.io/github/v/release/Veitangie/sinq?include_prereleases&logo=github)
# sinq 

`sinq` is a concurrent integration testing tool that treats your filesystem as a workflow definition.

Write requests as near-raw HTTP, add Lua where logic is needed, and organize files into directory trees. Every leaf directory becomes an isolated execution scenario with its own state, configuration, and request chain.

Instead of maintaining large Postman collections or YAML-heavy test definitions, you describe real user workflows directly in files and let `sinq` compile them into executable scenarios.

## Why sinq?

`sinq` is:
* **Workflow-Oriented:**
    - Build authentication, creation, processing and verification flows as file trees.
    - Shared setup lives in parent directories.
    - Leaf directories become executable scenarios.
* **Simple:** A `.sinq` file is just **raw HTTP + Lua**. There are no abstractions to fight, what you write is what gets sent over the wires. If you can write a cURL command and a basic script, you can write a `sinq` test.
* **Fully scriptable:** Pass JWTs, correlation IDs, and dynamic payloads between chained requests, manage execution flow, run scripts on lifecycle hooks and more.
* **Lightweight & built for CI/CD:** Distributed both as a lightweight binary and a container, requires minimal setup to run. Native support for JUnit XML reporting.

### Show Don't Tell: A Simple Healthcheck

Here is what a simple one-off request looks like in `sinq`.

**`healthcheck.sinq`**
```text
GET ${env.BASE_URL}/health

$ASSERT{
    if sinq.responses[1].code ~= 200 then
        sinq.test.fail("Healthcheck failed")
    end
}
```

### Show Don't Tell 2: A Stateful Poller

Here is what a complete authentication, execution, and polling chain looks like in `sinq`. 

**`01_login.sinq`**
```text
POST ${env.BASE_URL}/login
Accept: application/json

$POST{
    -- Lua arrays indices start at 1
    if sinq.responses[1].code ~= 200 then 
        sinq.test.fail("Login failed") 
    end
    -- Save the token into the global sandbox for the next file
    AUTH_TOKEN = sinq.responses[1].body.token
}
```

**`02_trigger_and_poll.sinq`**
```text
$PRE{
    if not AUTH_TOKEN then error("Token did not carry over") end
}
POST ${env.BASE_URL}/jobs
Authorization: Bearer ${ AUTH_TOKEN }

{ "action": "export" }

$RETRY{
    -- Poll until the background job is complete
    local status = sinq.responses[2].body.status
    if status == "pending" then
        return 50 -- sleep 50ms and retry
    end
    return -1 -- Stop retrying, proceed to Assert
}

$ASSERT{
    if sinq.responses[2].body.status ~= "complete" then
        sinq.test.fail("Job never completed")
    end
}
```


---

## Installation

### From Source (Requires Go 1.25+)
```bash
go install github.com/Veitangie/sinq/cmd/sinq@latest
```

### Docker (Alpine Minimal)
```bash
docker pull ghcr.io/veitangie/sinq:latest
docker run -v $(pwd):/tests ghcr.io/veitangie/sinq /tests
```

---

## The `.sinq` Format

A `.sinq` file is just a standard HTTP request with optional embedded Lua scripts. There are two types of Lua scripts within a `.sinq` file:
* General scripts: `$MY_CUSTOM_SCRIPT_NAME`, `$` (unnamed scripts) - these ones are evaluated after the pre-request phase, their return value getting injected into the outgoing request instead of the script body. The name, being the part that immediately follows the `$` sign can be any valid non-reserved string, but can't contain new lines, the name ends on script body opening with `{`. These scripts have to return something, a script that returns nothing will fail the request. If any of the scripts requiring a return value fail, `sinq` automatically retries them after prepending `return`. This allows for `${env.HOST}` interpolation, but for more complex multiline scripts it's better to do explicit returns.
* Life cycle scripts: `$PRE`, `$POST`, `$ASSERT`, `$RETRY` - those are the reserved script names, case sensitive. There can be only one instance of each defined for every request. Most of these scripts don't have to return anything, their return values will be ignored. They are used for arbitrary code execution during different life cycle stages of a request. 
  * `PRE` is executed immediately on a worker picking up the request, strictly after previous request's `POST` script. General scripts are executed immediately after it. **This is the only life cycle script in which current request body is not accessible**.
  * `RETRY` is executed immediately after receiving the response to the script. **This is the only life cycle script that has to return a value** - the value must be a Lua number representing the amount of milliseconds after which to retry this request. A 0 or negative value mean no more retries.
  * `ASSERT` is executed after `RETRY` returns a value <= 0. This is the stage to utilize Lua API for failing tests: `sinq.test.fail("Reason")`.
  * `POST` is executed after `ASSERT`. **It will not execute if `ASSERT` called `sinq.test.fail("Reason")` and scenario setting `fail_fast` is set to true**.

## File & Directory Structure

### Example File Structure And Resulting Scenarios

Because `sinq` relies on directory depth to build scenarios, you can define shared setup steps (like authentication) at the root, and branch off into specific test cases in subdirectories. Every leaf directory becomes one executable scenario. Parent files are inherited by all descendant scenarios. Scenario configurations are **aggregated** along the whole path, with the deeper nested ones taking precedence.

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

> Currently, leaf directories are expected to contain at least one `.sinq` file.
> Directories that contain neither `.sinq` files nor subdirectories are not considered valid scenario definitions.
---

## Lua API

`sinq` currently provides the following API accessible from Lua scripts:

```lua
env -- A global Lua table populated with environment configured for the scenario
secrets -- A global Lua table populated with secrets passed with --secrets or -S argument
sinq -- A global table providing utilities to manage scenario and request lifecycle
sinq.test.fail("Reason") -- Fail current scenario with the message passed as the argument, the message must be present
sinq.setNextRequest(1) -- After current request continue the scenario from n-th request, 1-indexed, the argument must be a valid integer in the range of total requests
sinq.responses[1] -- A table representing responses to completed requests. 1-indexed
-- Every response contains: 
-- code - integer representation of status code,
-- headers - table with the response header values,
-- rawBody - a string with raw response body bytes,
-- optional: body - a table with response body parsed as json and converted into Lua table
-- Response index is always the same as the number of request that produced it, therefore these entries represent
-- the last response to this request
-- The table is populated during execution, so a response can only be non-nil after the request has been sent.

-- All of these tables can be mutated from the user code, but sinq guarantees that these changes don't persist across 
-- scenario boundaries, and that no changes apart from populating sinq.responses are made by the runtime implicitly

```

---

## Configuration & Environment

`sinq` uses JSON-formatted `config.scenario` files along scenario path to manage environments, timeouts, and other scenario specific configuration.

Default configuration that can be overridden in `.scenario` files:
```json
{
  "name": "path/to/first/.scenario/file",
  "description": "",
  "env": { },
  "timeout": "5s",
  "fail_fast": true,
  "max_retries": 10,
  "max_redirects": 5
}
```
Everything defined in the env object can be accessed directly in your HTTP paths and headers using `${env.variableName}`, as long as it is a valid Lua table key, or inside your lua scripts via the global `env` table.

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

## Roadmap to `v1.0.0`

`sinq` is currently in a quiet `v0.0.x` pre-release phase for hardening and API stabilization. 

**Upcoming Features:**
- [x] Filetree parser and Scenario compiler
- [x] Concurrent execution engine
- [x] GitHub Actions CI & Docker pipeline
- [ ] Tree-sitter grammar
- [ ] Official VSCode Extension for `.sinq` syntax highlighting
- [ ] More extensive Lua API: native JWT support, native XML body support, convenient assertions, convenient retries
- [ ] Complete documentation

---

## Acknowledgments & Credits
This project includes natural sorting logic adapted from [facette/natsort](https://github.com/facette/natsort), which is distributed under the 3-Clause BSD License.
Copyright (c) 2015, Vincent Batoufflet and Marc Falzon. All rights reserved.

Big thanks to [yuin](https://github.com/yuin) for his [gopher-lua](https://github.com/yuin/gopher-lua) project.

## License
Copyright (C) 2026 Veitangie.
Distributed under the terms of the [GNU General Public License v3 (GPLv3)](LICENSE.md).

