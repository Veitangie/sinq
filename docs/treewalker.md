# Treewalker & Configuration

The **Treewalker** discovers your tests and works out how to run them. API requests live in separate files and folders instead of one large file.

## How it Discovers Tests

When you point `sinq` at a directory, the Treewalker scans it for `.sinq` and `.scenario` files.

* **Parent folders** hold shared setup steps, such as a `01_login.sinq` script.
* **Leaf folders** (no subdirectories) are one scenario each, such as `create_user` or `delete_user`.

A leaf folder inherits every script from its parent folders. Write the login once in a parent folder and every leaf scenario runs it before its own requests.

## Sharing Configuration

Configuration is inherited the same way as scripts.

Put a JSON file ending in `.scenario` (for example `config.scenario`) in any directory to set environment variables or execution limits. Only the extension matters, not the filename. A leaf scenario inherits the `.scenario` files from every directory above it.

When a child and a parent set the same key, the child's value wins. This per-key merge is called **deep merging**.

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

**The Treewalker does not guarantee the order scenarios run in.** Blueprints go to the worker pool with no global ordering. Files *within* one scenario always run in natural-sorted order; only the order of scenarios relative to each other is unspecified.
