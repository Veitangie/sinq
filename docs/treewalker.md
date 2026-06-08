# Treewalker: DAG Engine & Configuration Aggregation

`sinq` uses a directory-traversal engine called the Treewalker. Instead of relying on monolithic definitions, the Treewalker treats your physical filesystem as a Directed Acyclic Graph (DAG) to build test workflows.

## Core Algorithm

1. **Discovery:** Starting at the target root, the engine finds all `.scenario` and `.sinq` files.
2. **Sorting:** Files within the same directory are sorted in **natural alphanumeric order**. This means `2_request.sinq` will correctly execute before `10_finalize.sinq`.
3. **Descent & Inheritance:** The engine recursively descends into subdirectories. Child directories *inherit and append* the sorted `.scenario` and `.sinq` files from their parents.
4. **Leaf Compilation:** Once the engine reaches a directory containing **at least one** `.sinq` file but **no subdirectories**, it compiles the accumulated path into an executable **Scenario**.

*Note: Sibling leaf directories are completely isolated. `sinq` will spin up separate workers to execute them concurrently.*

## Configuration Aggregation (Deep Merging)

`config.scenario` files control the environment variables, timeouts, and behavioral limits of a scenario. 

When a leaf directory inherits a `config.scenario` file from a parent, the configurations are **deep merged**, with the deeper (child) configuration taking precedence.

**Parent `config.scenario`:**
```json
{
  "timeout": "5s",
  "env": {
    "BASE_URL": "[https://api.local](https://api.local)",
    "FEATURE_FLAG": "true"
  }
}
```

**Child (Leaf) `config.scenario`:**
```json
{
  "timeout": "15s",
  "env": {
    "FEATURE_FLAG": "false",
    "NEW_VAR": "hello"
  }
}
```

**Final Aggregated Configuration for the Leaf:**
```json
{
  "timeout": "15s",
  "fail_fast": true, 
  "env": {
    "BASE_URL": "[https://api.local](https://api.local)",
    "FEATURE_FLAG": "false",
    "NEW_VAR": "hello"
  }
}
```
*(Notice how the unmentioned defaults, like `fail_fast`, are preserved, `BASE_URL` is inherited, and `FEATURE_FLAG` is overwritten).*

## Scenario Ordering

 Scenarios parsed by the Treewalker tend to seem deterministically parsed, but **Treewalker doesn't guarantee deterministic scenario ordering**.
