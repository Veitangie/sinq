# Treewalker & Configuration

The **Treewalker** is the part of `sinq` responsible for discovering your tests and figuring out how to run them. Instead of forcing you to write one massive, monolithic test file, the Treewalker lets you organize your API requests into separate files and folders.

## How it Discovers Tests

When you point `sinq` at a directory, the Treewalker scans it for `.sinq` and `.scenario` files. 

* **Parent folders** hold shared setup steps (like a `01_login.sinq` script). 
* **Child folders** (leaf directories) represent a specific scenario (like `create_user` or `delete_user`).

When `sinq` runs a child folder, it automatically "inherits" all the test scripts from its parent folders. This means you can write your login logic once in a parent folder, and every child scenario will automatically execute that login step before running its own specific requests!

## Sharing Configuration

You don't just share test scripts; you can also share configuration variables across your scenarios.

You can place a `config.scenario` JSON file in any directory to set environment variables or execution limits. When a child scenario runs, it inherits configurations from its parent directories.

If a child folder defines the same variable as a parent, the child's value overwrites the parent's. This is called **deep merging**.

**Parent `config.scenario`:**
```json
{
  "req_timeout": "5s",
  "env": {
    "BASE_URL": "https://api.local",
    "FEATURE_FLAG": "true"
  }
}
```

**Child (Leaf) `config.scenario`:**
```json
{
  "req_timeout": "15s",
  "env": {
    "FEATURE_FLAG": "false",
    "NEW_VAR": "hello"
  }
}
```

**Final Aggregated Configuration for the Leaf Scenario:**
```json
{
  "req_timeout": "15s",
  "fail_fast": true, 
  "env": {
    "BASE_URL": "https://api.local",
    "FEATURE_FLAG": "false",
    "NEW_VAR": "hello"
  }
}
```
*(Notice how the unmentioned defaults, like `fail_fast`, are preserved, `BASE_URL` is inherited, and `FEATURE_FLAG` is overwritten).*

---

## DAG Engine & Blueprints

Under the hood, the Treewalker treats your physical filesystem as a Directed Acyclic Graph (DAG) to build test workflows.

### Core Algorithm

1. **Discovery:** Starting at the target root, the engine finds all `.scenario` and `.sinq` files.
2. **Sorting:** Files within the same directory are sorted in **natural alphanumeric order**. This means `2_request.sinq` will correctly execute before `10_finalize.sinq`.
3. **Descent & Inheritance:** The engine recursively descends into subdirectories. Child directories *inherit and append* the sorted `.scenario` and `.sinq` files from their parents.
4. **Blueprint Emission:** Once the engine reaches a directory containing **at least one** `.sinq` or `.scenario` file but **no subdirectories**, it compiles the accumulated path into a "scenario blueprint".
5. The Treewalker then emits a slice of these generated scenario blueprints to the Runner.

*Note: Sibling leaf directories are completely isolated. `sinq` will spin up separate workers to execute these emitted blueprints concurrently.*

### Scenario Ordering

Scenarios parsed by the Treewalker tend to seem deterministically parsed, but **Treewalker doesn't guarantee deterministic scenario ordering**. The blueprints are handed off to the execution pool without strict global execution order guarantees.
