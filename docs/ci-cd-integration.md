# CI/CD Integration & Reporting

`sinq` is designed to run natively in Continuous Integration environments (GitHub Actions, GitLab CI, Jenkins) with minimal dependency overhead.

## Exit Codes

`sinq` communicates test status to the CI runner via standard UNIX exit codes:
* **`0` (Success):** All discovered scenarios executed successfully, all network calls completed, and no `$ASSERT` blocks triggered a failure.
* **`1` (Failure):** One or more scenarios failed an assertion, encountered a network timeout, crashed, or the CLI received invalid arguments.

## JUnit XML Reporting

While standard console output works for debugging and local test runs, most CI platforms rely on JUnit XML files to generate test metrics, track regressions, and visually highlight failing steps.

To generate a JUnit report, use the `-f junit` flag and output it to a file:
```bash
sinq -f junit -o report.xml ./tests/integration
```

### How `sinq` maps to JUnit Elements

* **`<testsuite>`:** Maps directly to a single `sinq` Scenario (a leaf directory).
* **`<testcase>`:** Maps directly to a single `.sinq` file request within that scenario.
* **`<failure>`:** Triggered exclusively by Lua `$ASSERT` block failures (e.g., `sinq.assert.fail("Data missing")`). This indicates the system is up, but the business logic is wrong.
* **`<error>`:** Triggered by Go-level runtime panics, context timeouts, dial connection refusals, or malformed HTTP parsing errors. This indicates the system or the test architecture is fundamentally broken.

### Example Output
```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="1" errors="0" time="1.503">
  <testsuite name="auth/trade" tests="2" failures="0" errors="0" time="0.800" timestamp="2026-06-08T10:00:00Z">
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
