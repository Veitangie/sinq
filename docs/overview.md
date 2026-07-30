# Architecture Overview

`sinq` is designed to be highly concurrent, allowing complex, multi-step API tests to run as fast as your network allows without stepping on each other's toes.

To achieve this, `sinq` splits its workload across three primary components:

## 1. Treewalker
The Treewalker is the discovery engine. It scans your physical filesystem and builds a list of "Scenarios" to execute. Instead of one massive YAML file, `sinq` uses folders to organize test flows, allowing child folders to inherit setup files and configurations from their parent folders.

[Read more about the Treewalker & Configuration Aggregation](treewalker.md)

## 2. Execution Runner
Once the Treewalker discovers all the scenarios, it hands them off to the Runner. The Runner manages a pool of independent workers. Each worker gets its own isolated Lua environment and HTTP cookie jar, ensuring that Scenario A never accidentally shares state or sessions with Scenario B, even while executing concurrently.

[Read more about the Runner & Concurrency](runner.md)

## 3. Reporters
As the Runner executes scenarios, it streams the results to Reporters. Reporters format the output for human readability in the terminal, or generate structured artifacts like JUnit XML for your CI/CD pipelines.

[Read more about CI/CD Integration](ci-cd-integration.md)
