# CLI Reference

```text
sinq [flags] [paths...]
```

Each path is a directory to scan for `.sinq` / `.scenario` files, or a single `.sinq`
file to run on its own. If no path is given, `sinq` uses the current directory.
Multiple paths may be listed and are scanned independently.

## Flags

### Execution

| Flag | Value | Default | Notes |
| --- | --- | --- | --- |
| `-w, --workers` | int | `10` | Number of scenarios run concurrently. Must be positive. |
| `-c, --count` | uint | `1` | Run every scenario this many times. Multiplies with `env_matrix` variants, so a scenario with a 4-way matrix and `--count 3` runs 12 times. |
| `-i, --insecure` | | off | Skip TLS certificate verification. |
| `-u, --unrestricted` | | off | Load the Lua `os` and `io` libraries. This disables the script sandbox; only use it with files you trust. See [Libraries](lua-api.md#12-libraries). |
| `-l, --list` | | | Parse the paths and print the scenarios that would run, without executing them. |

### Selecting scenarios

| Flag | Value | Default | Notes |
| --- | --- | --- | --- |
| `-t, --tag` | string | | Run only scenarios carrying at least one of the given tags. Repeatable. |
| `--no-tag` | string | | Skip scenarios carrying any of the given tags. Repeatable. |
| `-n, --name` | regex | | Run only scenarios whose name matches at least one of the given [Go regular expressions](https://pkg.go.dev/regexp/syntax). Repeatable. |
| `--no-name` | regex | | Skip scenarios whose name matches any of the given regular expressions. Repeatable. |

How the four combine:

- Within one flag the values are **OR**-ed: `-t smoke -t auth` runs scenarios tagged `smoke` *or* `auth`.
- Across flag types the conditions are **AND**-ed: a scenario must satisfy the tag filter *and* the name filter to run.
- The `--no-*` filters win: if a scenario matches an exclude, it is skipped even if it also matches an include.

A scenario's name comes from its `.scenario` file (`"name"`), defaulting to the leaf directory path. Its tags are the union of the `"tags"` arrays in every `.scenario` file along its path.

### Output and reporting

| Flag | Value | Default | Notes |
| --- | --- | --- | --- |
| `-f, --format` | `std` \| `junit` | `std` | Report format. `std` is the human-readable console report; `junit` is JUnit XML (see [CI/CD Integration](ci-cd-integration.md)). |
| `-o, --out` | path | stdout | Write the report to a file instead of stdout. Missing parent directories are created. |
| `-S, --show` | `all` \| `no-skip` \| `failed` \| `none` | `no-skip` | Which scenario results to include in the report. `std` format only; `junit` output always includes every result. |
| `-V, --verbose` | | off | Add per-stage timings to each request in the report. `std` format only. |
| `-p, --print` | | off | Capture Lua `print` / `io.write` output and attach it to the scenario result. Without this, that output is discarded. |
| `-C, --color` | `always` \| `never` \| `auto` | `auto` | Terminal color. `auto` disables color when the output is not a TTY or when a CI/`NO_COLOR` environment variable is set. |
| `--no-spinner` | | off | Disable the live spinner / progress bar. |
| `--dump-on-failure` | | off | Print the full request and response on a failed assertion. May expose secrets; use with care in CI where logs are retained. |
| `-L, --log-level` | `debug` \| `info` \| `warn` \| `error` | `warn` | Diagnostic log verbosity, written to stderr. `debug` includes request/response bodies and may expose secrets. |

### Overrides and inputs

| Flag | Value | Default | Notes |
| --- | --- | --- | --- |
| `-e, --env` | `key=value` | | Override an environment value for every scenario. Repeatable. Keys use dot-notation for nesting (`API.TIMEOUT=500`) and values are parsed as JSON, so arrays and objects work (`HOSTS=["a","b"]`). |
| `-s, --secret` | `key=value` | | Same as `--env`, but for the `secrets` table. Repeatable. |
| `--secrets-file` | path | | Load `secrets` from a JSON file. Values from `--secret` override individual keys. |
| `--plugins` | path | | Directories to add to the Lua `require` search path. Join multiple with `:` (`;` on Windows), or pass the flag more than once. Takes precedence over `SINQ_LUA_PATH`. See [Libraries](lua-api.md#12-libraries). |

### Caching

These tune the opt-in per-request response cache (`req.cache(true)` or `req.cache(key)`); see [Execution Runner](runner.md#ast-bytecode-caching-request-collapsing).

| Flag | Value | Default | Notes |
| --- | --- | --- | --- |
| `--cache-timeout` | duration | `10s` | Timeout for the network call that fills the cache. That call is shared across scenarios via singleflight, so it can't use any single scenario's `req_timeout`. Accepts Go durations (`500ms`, `2m`). |
| `--max-cache-size` | size | `5MiB` | Largest response body that will be cached. Accepts `B`, `KiB`, `MiB`, `GiB` (and `KB`/`MB`/`GB`). |

### Information

| Flag | Notes |
| --- | --- |
| `-v, --version` | Print the version and exit. |
| `-h, --help` | Print the help text and exit. |
| `--completion` | Print a shell completion script for the detected shell (Bash, Zsh, Fish, or PowerShell on Windows) and exit. See [Getting Started](getting-started.md). |

## Environment variables

| Variable | Effect |
| --- | --- |
| `SINQ_LUA_PATH` | `PATH`-style list of directories added to the Lua `require` search path. Ignored if `--plugins` is given. |
| `NO_COLOR` / `SINQ_NO_COLOR` | Any non-empty value disables colored output and the spinner, regardless of `--color auto`. |
| `CI` | Any non-empty value has the same effect as `NO_COLOR`, and additionally makes `sinq` warn when `--log-level debug` or `--dump-on-failure` is set, since CI logs are usually retained. |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` | Standard Go proxy configuration for outgoing requests (lowercase names are also honored). |

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Every scenario ran and passed. |
| `1` | A scenario failed an assertion or errored, or the arguments were invalid. |

## Examples

```bash
# Run a suite with 20 workers and write a JUnit report
sinq -w 20 -f junit -o report.xml ./tests/e2e

# Only the scenarios tagged "smoke", skipping anything tagged "flaky"
sinq -t smoke --no-tag flaky ./tests

# Only scenarios whose name contains "checkout", run 5 times each
sinq -n 'checkout' --count 5 ./tests

# Override a nested env key and a secret, ignore self-signed certs
sinq -e 'API.BASE=https://staging.local' -s 'DB.PASS=hunter2' -i ./tests

# List what would run, without sending anything
sinq --list ./tests
```
