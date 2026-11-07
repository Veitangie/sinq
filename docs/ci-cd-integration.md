# CI/CD Integration & Reporting

`sinq` runs in CI (GitHub Actions, GitLab CI, Jenkins) with nothing to install beyond the binary.

## Exit Codes

`sinq` communicates test status to the CI runner via standard UNIX exit codes:

* **`0` (Success):** All discovered scenarios executed successfully, all network calls completed, and no `$ASSERT` blocks triggered a failure.
* **`1` (Failure):** One or more scenarios failed an assertion, errored (including a request that still got no response after `$RETRY`) or were aborted, or the CLI received invalid arguments. A run where no scenario is selected (none found, or the `--tag`/`--name` filters excluded all of them) also fails, so a mistyped filter doesn't pass silently.

## JUnit XML Reporting

The console output is for local runs. Most CI platforms read JUnit XML to track results over time and surface failing steps.

To generate a JUnit report, use the `-f junit` flag and output it to a file:
```bash
sinq -f junit -o report.xml ./tests/integration
```

### How `sinq` maps to JUnit Elements

* **`<testsuite>`:** Maps to a single run of a `sinq` Scenario (a leaf directory). Every run is its own suite with its own name, because CI tools tell suites apart by name: an [environment matrix](env-matrix.md) variant appends its labels (`checkout_admin_card`), and with `--count` above 1 each run also gets its run number, counting from 1 (`checkout_admin_card#2`).
* **`<properties>`:** Holds the scenario's tags as `<property name="tags" value="smoke, api"></property>` and a matrix variant's labels as `<property name="labels" value="admin, card"></property>`, each joined with `, `. It comes before the `<testcase>` elements, as the JUnit schema expects. Each property is left out when there's nothing to put in it, and the whole element is left out for a scenario with neither.
* **`<testcase>`:** Maps directly to a single `.sinq` file request within that scenario. Its `classname` is the suite name, so the same request in different runs stays distinct.
* **`<failure>`:** Triggered exclusively by Lua `$ASSERT` block failures (e.g., `sinq.assert.fail("Data missing")`). This indicates the system is up, but the business logic is wrong.
* **`<error>`:** Triggered by Go-level runtime panics, the scenario `timeout`, a request that still got no response after `$RETRY` (refused connection, `req_timeout`, TLS or DNS failure), or malformed HTTP parsing errors. This indicates the system or the test architecture is fundamentally broken. A request interrupted by cancelling the run (`Ctrl+C` / `SIGINT`) is also reported as an `<error>`, with the message `Request was interrupted`.
* **`<skipped/>`:** Marks a request that never ran. Either it called `req.skip()`, or it was never reached: the scenario stopped earlier (after an error, an assertion failure with `fail_fast`, or a cancelled run), `sinq.setNextRequest` jumped past it, or the whole scenario was filtered out by `--tag`/`--name`.

### Example Output
```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="4" failures="1" errors="0" time="1.503">
  <testsuite name="auth/trade" tests="2" failures="0" errors="0" time="0.800" timestamp="2026-06-08T10:00:00Z">
    <properties>
      <property name="tags" value="smoke, trade"></property>
    </properties>
    <testcase name="01_login.sinq" classname="auth/trade" time="0.200"></testcase>
    <testcase name="02_buy_item.sinq" classname="auth/trade" time="0.600"></testcase>
  </testsuite>
  <testsuite name="auth/combat" tests="2" failures="1" errors="0" time="0.703" timestamp="2026-06-08T10:00:00Z">
    <testcase name="01_login.sinq" classname="auth/combat" time="0.200"></testcase>
    <testcase name="02_enter_dungeon.sinq" classname="auth/combat" time="0.503">
      <failure message="Failed to enter dungeon, HTTP 500" type="AssertionFailure"></failure>
    </testcase>
  </testsuite>
</testsuites>
```

The second run of a matrix variant, with `--count 2`:
```xml
  <testsuite name="checkout_admin_card#2" tests="1" failures="0" errors="0" time="0.150" timestamp="2026-06-08T10:00:00Z">
    <properties>
      <property name="labels" value="admin, card"></property>
    </properties>
    <testcase name="01_checkout.sinq" classname="checkout_admin_card#2" time="0.150"></testcase>
  </testsuite>
```
