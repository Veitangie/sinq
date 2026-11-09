# CI/CD Integration & Reporting

`sinq` runs in CI (GitHub Actions, GitLab CI, Jenkins) with nothing to install beyond the binary.

## Exit Codes

`sinq` communicates test status to the CI runner via its exit code. When several apply, the most severe wins: `3`, then `2`, then `1`, then `130`.

* **`0` (Success):** Every selected scenario ran and passed.
* **`1` (Failure):** At least one `$ASSERT` failed, and nothing worse happened. The system under test is up, but its behaviour is wrong.
* **`2` (Error):** At least one stage errored: a request still got no response after `$RETRY` (refused connection, `req_timeout`, TLS or DNS failure), a scenario hit its `timeout`, or a script raised an error. These are the stages the JUnit report marks as `<error>`, and they usually mean the environment or the tests themselves are broken.
* **`3` (Setup error):** The run couldn't be set up: invalid arguments, a path that doesn't exist or fails to parse, a secrets file that can't be read, a `--out` file that can't be written, or no scenario selected (none found, or the `--tag`/`--name` filters excluded all of them, so a mistyped filter doesn't pass silently).
* **`130` (Cancelled):** The run was interrupted (`Ctrl+C` / `SIGINT`, or `SIGTERM`) before anything failed or errored. A cancelled run that already had a failing assertion exits `1`.

Every non-zero code fails a CI job. To keep a job from failing on assertion failures alone, while errors and setup mistakes still fail it, accept `1` explicitly. On GitLab, the job then shows as a warning:

```yaml
integration-tests:
  script:
    - sinq -f junit -o sinq.xml tests/integration
  allow_failure:
    exit_codes: [1]
```

In a plain shell step: `sinq -f junit -o sinq.xml tests/integration || [ $? -eq 1 ]`.

## JUnit XML Reporting

The console output is for local runs. Most CI platforms read JUnit XML to track results over time and surface failing steps.

To generate a JUnit report, use the `-f junit` flag and output it to a file:
```bash
sinq -f junit -o report.xml ./tests/integration
```

### How `sinq` maps to JUnit Elements

* **`<testsuite>`:** Maps to a single run of a `sinq` Scenario (a leaf directory). Every run is its own suite with its own name, because CI tools tell suites apart by name: an [environment matrix](env-matrix.md) variant appends its labels (`checkout_admin_card`), and with `--count` above 1 each run also gets its run number, counting from 1 (`checkout_admin_card#2`).
* **`<properties>`:** Holds the scenario's tags as `<property name="tags" value="smoke, api"></property>` and a matrix variant's labels as `<property name="labels" value="admin, card"></property>`, each joined with `, `. It comes before the `<testcase>` elements, as the JUnit schema expects. Each property is left out when there's nothing to put in it, and the whole element is left out for a scenario with neither.
* **`<testcase>`:** Maps to a single stage within that scenario. Its `name` is the stage name, or the file path for an unnamed stage; `file` is always the path of the file the stage lives in, relative to the working directory sinq runs from. GitLab shows it in its Filename column and links it from the repository root, so run sinq from the root (`sinq tests/integration`, not `cd tests && sinq integration`) for the links to work. Its `classname` is the suite name, so the same stage in different runs stays distinct.
* **`<failure>`:** Triggered exclusively by Lua `$ASSERT` block failures (e.g., `sinq.assert.fail("Data missing")`). This indicates the system is up, but the business logic is wrong.
* **`<error>`:** Triggered by Go-level runtime panics, the scenario `timeout`, a request that still got no response after `$RETRY` (refused connection, `req_timeout`, TLS or DNS failure), or malformed HTTP parsing errors. This indicates the system or the test architecture is fundamentally broken. A stage interrupted by cancelling the run (`Ctrl+C` / `SIGINT`) is also reported as an `<error>`, with the message `Stage was interrupted`.
* **`<skipped/>`:** Marks a stage that never ran. Either it called `req.skip()`, or it was never reached: the scenario stopped earlier (after an error, an assertion failure with `fail_fast`, or a cancelled run), `sinq.setNextStage` jumped past it, or the whole scenario was filtered out by `--tag`/`--name`.

### Example Output
```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="4" failures="1" errors="0" time="1.503">
  <testsuite name="auth/trade" tests="2" failures="0" errors="0" time="0.800" timestamp="2026-06-08T10:00:00Z">
    <properties>
      <property name="tags" value="smoke, trade"></property>
    </properties>
    <testcase name="01_login.sinq" file="tests/integration/auth/01_login.sinq" classname="auth/trade" time="0.200"></testcase>
    <testcase name="02_buy_item.sinq" file="tests/integration/auth/trade/02_buy_item.sinq" classname="auth/trade" time="0.600"></testcase>
  </testsuite>
  <testsuite name="auth/combat" tests="2" failures="1" errors="0" time="0.703" timestamp="2026-06-08T10:00:00Z">
    <testcase name="01_login.sinq" file="tests/integration/auth/01_login.sinq" classname="auth/combat" time="0.200"></testcase>
    <testcase name="02_enter_dungeon.sinq" file="tests/integration/auth/combat/02_enter_dungeon.sinq" classname="auth/combat" time="0.503">
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
    <testcase name="01_checkout.sinq" file="tests/integration/checkout/01_checkout.sinq" classname="checkout_admin_card#2" time="0.150"></testcase>
  </testsuite>
```

## Showing Results on Your CI Platform

Each platform reads the same report; what differs is where it has to be written and what the platform does with it. In every case, run `sinq` from the repository root, so the `file` paths match the repository. `sinq` exits with `1` when a test fails, which fails the job, so make sure the report is still collected after a failed run.

=== "GitLab CI"

    GitLab reads the report natively. Declare it as a `junit` report artifact:

    ```yaml
    integration-tests:
      image:
        name: ghcr.io/veitangie/sinq:v1
        entrypoint: [""]   # the image's entrypoint is sinq itself; GitLab needs a shell
      script:
        - sinq -f junit -o sinq.xml tests/integration
      artifacts:
        when: always       # keep the report when sinq fails the job
        reports:
          junit: sinq.xml
    ```

    The results appear in the pipeline's **Tests** tab: one suite per scenario run, with each failure's message. The **Filename** column links to the stage's file at the pipeline's commit.

=== "GitHub Actions"

    GitHub has no built-in JUnit view, so a reporter action turns the report into a check run. [`mikepenz/action-junit-report`](https://github.com/mikepenz/action-junit-report) works well with `sinq`'s output:

    ```yaml
    permissions:
      contents: read
      checks: write        # the reporter creates a check run

    jobs:
      integration-tests:
        runs-on: ubuntu-latest
        steps:
          - uses: actions/checkout@v7
          - name: Install sinq
            run: curl -sL https://raw.githubusercontent.com/Veitangie/sinq/refs/heads/main/install.sh | bash
          - name: Run sinq
            run: sinq -f junit -o sinq.xml tests/integration
          - name: Report
            if: ${{ !cancelled() }}   # also after sinq fails the job
            uses: mikepenz/action-junit-report@v6
            with:
              report_paths: sinq.xml
              detailed_summary: true
    ```

    The check run lists the totals and each failure, and every failure becomes an annotation on the stage's file. `sinq` doesn't write line numbers, so the annotation points at the file's first line. [`dorny/test-reporter`](https://github.com/dorny/test-reporter) (`reporter: java-junit`) also reads the report, if you prefer its per-suite summary.

=== "Bitbucket Pipelines"

    Bitbucket picks the report up on its own, as long as it's written into one of the folders it scans, such as `test-results/`:

    ```yaml
    pipelines:
      default:
        - step:
            name: Integration tests
            image: alpine:3.20
            script:
              - apk add --no-cache bash curl
              - curl -sL https://raw.githubusercontent.com/Veitangie/sinq/refs/heads/main/install.sh | bash
              - sinq -f junit -o test-results/sinq.xml tests/integration
    ```

    Failed tests appear in the step's **Tests** tab with their messages, also when `sinq` failed the step. Bitbucket doesn't link to files, and it may show the same failing stage from several `--count` runs as a single failure.
