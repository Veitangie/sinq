# Versioning & Compatibility

From `v1.0.0` onward, `sinq` follows [Semantic Versioning](https://semver.org/):

- **Patch** (`v1.0.x`): bug fixes only, with no intended change to behavior or output.
- **Minor** (`v1.x.0`): new, backward-compatible functionality. New flags, new Lua APIs, new config keys with safe defaults, or new attributes and elements in the JUnit report.
- **Major** (`vX.0.0`): any breaking change.

The compatibility promise covers:

- CLI flag names, their semantics and defaults, and process exit codes.
- The `.sinq` file format and the `.scenario` configuration schema.
- The Lua API surface exposed to scripts (globals, the `sinq.*` namespace, and the lifecycle scopes).
- The [`res.error.kind`](lua-api.md#send-failures-reserror) values. A named kind (`timeout`, `dns`, `refused`, `reset`, `tls`, `redirects`) keeps its meaning and is only removed or narrowed in a major release. The one exception is `other`: a minor release may split errors off it into a new named kind, so a check against `other` can change in a minor release.
- The structure of the JUnit XML report. Existing elements and attributes keep their names and meaning, and are only removed or changed in a major release. A minor release may add new attributes, elements or `<property>` entries, so a tool reading the report should ignore what it doesn't know, as JUnit consumers generally do.

It does **not** cover, and any of the following may change in any release:

- The exact text, layout, and colors of the human-readable `std` reporter.
- Log message wording and formatting.
- The wording of error messages, including `res.error.message` and `tostring(res.error)`.
- Anything under `internal/` (this module exposes no importable Go API).
- `res.bodyJson`, the internal cache behind `res.json()` and `res.extractBodyJson()`. Use those functions instead.
- Performance characteristics.

Changes in each release are listed in the [changelog](https://github.com/Veitangie/sinq/blob/main/CHANGELOG.md).
