# Architecture Overview

`sinq` works in three phases: it discovers the tests on disk, runs them across a worker pool, and reports the results.

## 1. Treewalker

The Treewalker walks the directories you point `sinq` at and turns them into a list of scenarios. Test flows are folders rather than one large file, and a child folder inherits the setup files and configuration of its parents.

[Treewalker & configuration aggregation](treewalker.md)

## 2. Execution Runner

The Runner takes the scenarios from the Treewalker and runs them on a pool of workers. Every scenario run gets a fresh Lua state and its own HTTP cookie jar, so one scenario cannot see another's variables or session even though they run at the same time.

[Runner & concurrency](runner.md)

## 3. Reporters

The Runner emits events as each request progresses, and an aggregator turns them into one result per scenario run as it finishes. Reporters render those results to the console or write a JUnit XML file for CI.

[CI/CD integration](ci-cd-integration.md)
