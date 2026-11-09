# Scenarios & Configuration

A **scenario** is a sequence of **stages** run in order, defined across one or more `.sinq` files, and each stage runs as a sequence of **steps**:

* **Scenario:** one leaf directory, with everything it inherits from the directories above it. Scenarios run in parallel.
* **Stage:** usually one HTTP request with its lifecycle hooks, but it can also be [hooks alone](#script-only-stages). Stages are separated by `###` or by file boundaries, and run in order.
* **Step:** one part of a stage's [lifecycle](#the-stage-lifecycle), such as `$PRE`, sending the request, or `$ASSERT`.

## Writing Stages

A `.sinq` file is a standard HTTP request. You can define multiple stages in a single file by separating them with the `###` delimiter. The text after `###` on the same line names the stage that *follows* it. This name shows up in `--list` output and in the standard/JUnit reports.

```http
### Login Request
POST ${env.BASE_URL}/api/auth
Content-Type: application/json

{"user": "admin"}

### Fetch Data
GET ${env.BASE_URL}/api/data
```

A leading `###` (before any other content) works the same way and names the file's *first* stage, even if the file only contains one:

```http
### Healthcheck
GET ${env.BASE_URL}/health
```

Outside Lua blocks, a backslash escapes the character after it, whatever that is, and the backslash itself isn't sent: `\$` is a literal `$`, `\#` a literal `#`, and `\\` a literal `\`. This applies to request bodies too, so a JSON escape needs its backslash doubled: write `"line1\\nline2"` to send `"line1\nline2"`, and `"say \\"hi\\""` to send `"say \"hi\""`. Lua blocks are left as written.

To make your requests dynamic, you can use Lua scripts. `sinq` provides two ways to run scripts:

1. **Inline Interpolation (`${...}`)**: Used to dynamically insert values into headers, URLs, or JSON payloads (e.g., `${env.BASE_URL}`).
2. **Lifecycle Hooks (`$PRE`, `$ASSERT`, etc.)**: Used to control the flow of the stage, such as failing a test if a status code is wrong, or polling until a background job completes.

### Script-Only Stages

A stage with lifecycle hooks but no request runs only its hooks. Use one for work that sends nothing over HTTP: preparing data, checking state gathered by earlier stages, or branching the flow.

```text
### Prepare the order
$PRE{
    ORDER_ITEMS = sinq.json.array({ "health_potion", "steel_sword" })
}

### Place the order
POST ${env.BASE_URL}/orders
Content-Type: application/json

{ "items": ${ ORDER_ITEMS } }

$ASSERT{ sinq.assert.code(201) }
```

* Only `$PRE`, `$ASSERT` and `$POST` are allowed. `$RETRY` needs a request to retry, so it is an error in a script-only stage.
* Reading or writing `res` raises `res is not available in a stage without a request`, and the stage has no entry in `sinq.responses`. Code shared with request stages can use `rawget(res, "error")` or `sinq.responses[n]` to check without raising.
* `req.skip()` skips the stage. The other `req` functions raise an error (`invalid call for non-request stage`), since there is nothing to send.
* The stage is reported like any other and is a test case in JUnit reports, but doesn't count toward "requests sent".
* An inline script such as `${ ... }` on its own is still a request whose request line comes from the script. Only lifecycle hooks make a stage script-only.

!!! note
    A stage that loses its request line, for example by deleting it by mistake, quietly becomes a script-only stage. Any hook that reads `res` then fails with the error above, and `--list` marks the stage `(Lua only)`.

## Configuration

You can configure timeouts, environment variables, and limits using JSON files ending in `.scenario`. The filename itself is arbitrary, only the extension matters (e.g. `config.scenario`, `00_base.scenario`).

When you place a `.scenario` file in a directory, those settings apply to all tests in that directory and its subdirectories.

Available settings, shown with their defaults:

```json
{
  "name": "My API Test",
  "description": "Tests the core user flow",
  "env": {
    "BASE_URL": "https://api.local"
  },
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

* **`name`**: Scenario name, shown in reports and used by the `-n` / `--no-name` filters. Defaults to the leaf directory's path relative to the folder you run `sinq` on. A scenario whose files sit directly in that folder takes the folder's name, and a single `.sinq` file run on its own takes the file's name. If the same `.scenario` file is inherited by several leaf directories, they all take this name.
* **`description`**: Free-text description of the scenario.
* **`env`**: Variables defined here become accessible in your `.sinq` files as `${env.VARIABLE_NAME}` or in Lua scripts via the `env` table.
* **`req_timeout`**: Maximum time to wait for a single HTTP network request.
* **`script_timeout`**: Maximum time a single Lua script block is allowed to run.
* **`timeout`**: Maximum time allowed for the entire scenario to complete.
* **`fail_fast`**: If true, the scenario stops at the first stage with a failed assertion; that stage's `$POST` and the stages after it don't run.
* **`max_retries`**: Maximum number of retries per request when `$RETRY` keeps returning a non-negative delay.
* **`max_redirects`**: Maximum number of redirects to follow. One more redirect fails the attempt with [`res.error.kind`](lua-api.md#send-failures-reserror) `redirects`.
* **`max_body`**: The maximum response body size stored in memory. Responses exceeding this are safely truncated.
* **`env_matrix`**: Data sets for running the scenario once per combination (parameterized / matrix testing). Aggregated along the path. See [Environment Matrix](env-matrix.md).
* **`tags`**: Labels attached to every scenario that inherits this file. Collected into one list along the path and matched by the `-t` / `--tag` and `--no-tag` filters. In JUnit reports they're written as a `tags` property of the scenario's `<testsuite>`.

---

## The Stage Lifecycle

Each stage goes through these steps in order. A script-only stage skips steps 2 to 5.

1. **`$PRE` Execution:** Executes first. This is where you configure dynamic variables or file I/O (`req.attach`, `req.multipart`, `req.saveResponseTo`, `req.cache`, `req.skip`). *Current HTTP request body and headers are not yet accessible.*
2. **Materialization:** The engine scans the raw HTTP text and evaluates inline scripts (e.g., `${env.HOST}`). The output is injected directly into the byte stream: strings as is, numbers and booleans as text, tables as compact JSON (see [Lua API](lua-api.md#returned-values)).
3. **HTTP Parsing:** The materialized byte stream is parsed into a standard Go `http.Request`.
4. **Execution (Send):** The HTTP request is sent over the network.
5. **`$RETRY` Loop:** Executes immediately after every attempt, including one that got no response (`res.error` is set then). Must return a number: milliseconds to sleep before retrying (jumps back to Step 4). A negative number breaks the loop. If the last attempt got no response, the stage ends as an error here.
6. **`$ASSERT` Execution:** Evaluates the final response to pass or fail the test.
7. **`$POST` Execution:** Used to extract state. *Skipped if `$ASSERT` failed and `fail_fast` is true.*

## Configuration Inheritance

`.scenario` files along a scenario's path are deep merged, with the child winning on conflicting keys; settings nobody mentions keep their defaults. See [Treewalker](treewalker.md) for the exact rules.
