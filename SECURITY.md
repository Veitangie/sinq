# Security Policy

Security is a top priority for `sinq`. Because this tool handles sensitive CI/CD environment variables, executes embedded Lua environments, and parses file systems, vulnerabilities are treated with the highest urgency.

## Supported Versions

Currently, only the latest major release is actively supported with security updates.

| Version | Supported          |
| ------- | ------------------ |
| v1.x.x  | :white_check_mark: |
| < v1.0  | :x:                |

## Operational Notes

These are intended behaviors, not vulnerabilities, but they change the trust boundary and are easy to miss:

* **`--unrestricted` / `-u` disables the Lua sandbox.** It loads the full `os` and `io` libraries, giving scenario scripts shell execution (`os.execute`), read/write access to any path the process can reach (`io.open`), and the ability to terminate `sinq` mid-run (`os.exit`). Only pass it for scenario files you fully control and trust.
* **Verbose and debug output can contain secrets.** `--verbose`, `--log-level debug`, and `--dump-on-failure` include request and response data that may embed tokens or credentials. Avoid them in CI environments where logs are retained, or scrub the output before publishing it.
* **Assertion failure messages leave out the compared values by default.** The default failure text of `sinq.assert.equals` and `sinq.assert.contains` names only types and, for tables, the path of the mismatching field, so a failure doesn't put secrets into normal reports or JUnit XML. A custom `message` is printed as written, and so is anything passed to `sinq.assert.fail`, so don't build those from sensitive values.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.** Publicly disclosing a vulnerability puts users' CI/CD pipelines at immediate risk before a patch can be developed. 

If you believe you have found a security vulnerability in `sinq`, please report it privately:

1. **Via GitHub Private Vulnerability Reporting (Preferred):** Go to the **Security** tab in this repository, click **Advisories**, and then click **Report a vulnerability**. This opens a private, secure channel directly to the maintainer.
2. **Via Email:** You can also send a direct email to `security@veitangie.dev`.

### What to include in your report:
* A description of the vulnerability and its potential impact.
* Steps to reproduce the issue (including any specific configurations or Lua scripts used).
* The OS, CPU architecture and `sinq` version where the vulnerability was identified.

### Response Timeline
You should expect an initial acknowledgment of your report within 48 hours. If the vulnerability is confirmed, I will work with you to patch it, coordinate a release, and publish a formal GitHub Security Advisory.

Thank you for helping keep the open-source community secure!
