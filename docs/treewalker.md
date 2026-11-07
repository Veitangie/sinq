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

## How Scenarios Are Built

1. **Sorting:** files within a directory are sorted in natural order, so `2_request.sinq` runs before `10_finalize.sinq`.
2. **Inheritance:** each subdirectory appends its own sorted files to the ones inherited from its parents.
3. **Emission:** a directory with no subdirectories and at least one `.sinq` or `.scenario` file becomes one scenario, built from everything accumulated along its path.

Sibling scenarios share nothing and are handed to the worker pool to run concurrently.

A single `.sinq` file passed as a path runs as its own scenario with the default configuration. It doesn't pick up `.scenario` files, not even ones in its own directory.

### Scenario Ordering

**The order scenarios run in is unspecified.** Files *within* one scenario always run in natural-sorted order.
