# Versioning & Compatibility

From `v1.0.0` onward, `sinq` follows [Semantic Versioning](https://semver.org/):

- **Patch** (`v1.0.x`): bug fixes only, with no intended change to behavior or output.
- **Minor** (`v1.x.0`): new, backward-compatible functionality. New flags, new Lua APIs, or new config keys with safe defaults.
- **Major** (`vX.0.0`): any breaking change.

The compatibility promise covers:

- CLI flag names, their semantics and defaults, and process exit codes.
- The `.sinq` file format and the `.scenario` configuration schema.
- The Lua API surface exposed to scripts (globals, the `sinq.*` namespace, and the lifecycle scopes).
- The structure of the JUnit XML report.

It does **not** cover, and any of the following may change in any release:

- The exact text, layout, and colors of the human-readable `std` reporter.
- Log message wording and formatting.
- Anything under `internal/` (this module exposes no importable Go API).
- Performance characteristics.
