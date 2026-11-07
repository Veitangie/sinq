# Alternatives & Comparisons

How `sinq` compares to the API testing tools people bring up most, starting with the ones closest to it in approach.

| Tool | Tests written as | Scripting | Parallel by default | Built for |
| --- | --- | --- | --- | --- |
| **sinq** | Plain-text `.sinq` (raw HTTP) + directory tree | Embedded Lua | Yes | Stateful HTTP workflows in CI |
| Hurl | Plain-text `.hurl` (raw HTTP) | Predicates and filters | Yes, per file (`--test`) | Plain-text HTTP tests from the CLI |
| Venom | YAML | Go templates and `govaluate` | Yes | Multi-protocol integration tests in CI |
| Postman / Insomnia | GUI collections (JSON) | JavaScript | No | Interactive API exploration |
| Bruno | Plain-text collections (`.bru`) | JavaScript (Node) | No | Git-friendly Postman-style client |
| REST Client / httpyac | `.http` / `.rest` files | Limited, or JS in httpyac | No | Sending requests from your editor |
| k6 | JavaScript | JavaScript | Yes (virtual users) | Load and performance testing |

## Sinq vs. Hurl

[Hurl](https://hurl.dev) is probably the closest tool to `sinq`. Both keep requests and assertions in plain-text files that stay close to raw HTTP.

The differences:

- **Scripting.** Hurl provides a fixed set of predicates and filters. For things outside that set (pulling one claim out of a JWT, generating a payload on the fly, retrying based on what came back in the body) you reach the limit of what they can express. `sinq` runs a full Lua VM, so those cases are just code.
- **Unit of a flow.** Both run tests in parallel: Hurl runs `.hurl` files in parallel in `--test` mode, and `sinq` runs scenarios in parallel. A Hurl flow is one file, while a `sinq` scenario is a directory path whose files run in order.
- **Reuse.** A `sinq` scenario inherits requests and config from the directories above it, so shared auth and setup live in one file at the root. With Hurl, that setup is usually repeated per file or passed along as state you manage yourself.

If your assertions stay simple, Hurl is the lighter choice: no scripting language to learn and a longer track record.

## Sinq vs. Venom

[Venom](https://github.com/ovh/venom) is OVHcloud's test runner. Like `sinq` it ships as a single Go binary, runs headless in CI, and writes JUnit XML. Suites are YAML: a suite holds test cases, a test case holds steps, and each step runs an "executor" with assertions attached.

- **Format.** Venom suites are YAML, with logic written as Go templates and `govaluate` expressions inside strings. `sinq` files are near-raw HTTP with Lua for the dynamic parts.
- **Structure.** Venom describes the flow inside the YAML. `sinq` uses the directory layout, and child folders inherit setup and config from their parents.
- **Concurrency.** `sinq` runs scenarios in parallel by default.

Venom also covers far more than HTTP, with executors for gRPC, SQL, SSH, Kafka, Redis, AMQP, IMAP, and shell commands. `sinq` is HTTP-only, so if a suite also needs to talk to a database or a queue, Venom is the better fit.

## Sinq vs. Postman / Insomnia

[Postman](https://www.postman.com) and [Insomnia](https://insomnia.rest) are GUI-first tools for exploring and testing APIs interactively.

- **Workflow.** Chaining requests is done by wiring them together in the UI or writing JavaScript in the app's script panel. In `sinq` the chain is the order of the files, and everything lives in your editor.
- **Concurrency.** `sinq` runs independent scenarios in parallel.
- **Version control.** `sinq` tests are plain `.sinq` files next to your code, with no exported JSON collections to sync or JSON diffs to review, and no paid workspace for team access.

Their strength is the interface: a visual request explorer, built-in mock servers, and shared team workspaces. `sinq` is a CLI and does not offer those.

## Sinq vs. Bruno

[Bruno](https://www.usebruno.com) is a Postman-style client that stores collections as plain text, which makes it much friendlier under git.

- **Model.** Bruno is organized as collections of requests. `sinq` is organized as scenarios and directory trees, which suits end-to-end flows without one large shared environment.
- **Scripting.** Bruno scripts in JavaScript on Node. `sinq` has a small Lua VM built into the binary, with nothing to install.

Bruno also keeps a desktop GUI for building and browsing requests, so if you want plain-text storage together with a visual client, that is what it offers.

## Sinq vs. REST Client / httpyac

The VS Code [REST Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client) and [httpyac](https://httpyac.github.io) are made for sending requests straight from your editor as you work.

They fit the edit-and-send loop during development. `sinq` is made to run headless in CI, with retry policies, JUnit XML, environment matrices for parameterized runs, tag and name filters, and parallel execution. If you mainly want to hit an endpoint from your editor, they do that well.

## Sinq vs. k6

[k6](https://k6.io) is a load and performance testing tool from Grafana. Tests are JavaScript, executed by a Go engine that can drive many virtual users at once.

- **Goal.** k6 measures how a system behaves under load: throughput, latency percentiles, error rate at a given number of virtual users. `sinq` checks whether a workflow returns the correct result, and runs scenarios concurrently to finish the suite sooner rather than to generate load.
- **Assertions.** k6 uses lightweight checks and thresholds suited to load runs. `sinq` is built around per-request assertion hooks for functional correctness.
- **Reporting.** k6 produces load metrics and time series. `sinq` produces per-scenario pass/fail and JUnit XML for CI gating.

For measuring how fast or how reliable a service is under pressure, use k6. For confirming that a multi-step flow does the right thing, use `sinq`. The two work well side by side.

## Summary

Reach for `sinq` when you want a small, fast, file-based CLI for writing stateful HTTP workflows, keeping them DRY, and running them in CI.
