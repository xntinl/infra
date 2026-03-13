# 49. Compliance Audit Automation

<!--
difficulty: insane
concepts: [compliance-scanning, security-benchmarks, secret-detection, license-auditing, vulnerability-scanning, remediation-planning]
tools: [just, bash, grep, jq, openssl, trivy, semgrep]
estimated_time: 3h-4h
bloom_level: evaluate
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Familiarity with infrastructure security concepts: CIS benchmarks, OWASP, least
  privilege
- `jq` installed for JSON report processing
- Optional: `trivy` for container vulnerability scanning, `semgrep` for static
  analysis (system should work without them, falling back to built-in grep-based checks)

## Learning Objectives

After completing this challenge, you will be able to:

- **Evaluate** infrastructure configurations, container images, and codebases against
  security benchmarks and compliance standards using automated scanning
- **Design** a prioritized remediation workflow that transforms audit findings into
  actionable, tracked improvements with evidence and guidance

## The Challenge

Build a comprehensive compliance audit system as a justfile. This system scans multiple
dimensions of a project's security posture: infrastructure configuration files
(Terraform, Kubernetes manifests, Docker Compose), Dockerfiles, application source code
(for exposed secrets and insecure patterns), dependencies (for known vulnerabilities and
license violations), and generates structured reports with evidence, severity ratings,
and remediation guidance.

The infrastructure configuration scanner is the first pillar. Parse Terraform files and
Kubernetes manifests for common misconfigurations: S3 buckets without encryption enabled,
security groups with `0.0.0.0/0` ingress on sensitive ports (22, 3389, 3306, 5432),
containers running as root, missing resource limits in Kubernetes pod specs, databases
with public accessibility enabled, CloudWatch logging disabled, IAM policies with
`Action: "*"`. Each check should be a discrete, named rule with a severity level
(critical, high, medium, low) and a reference to the relevant CIS benchmark or security
standard.

Secret detection is the second pillar. Scan all files in the repository for patterns
that indicate exposed secrets: AWS access keys (`AKIA...`), private keys
(`-----BEGIN RSA PRIVATE KEY-----`), high-entropy strings in configuration files,
hardcoded passwords in source code (`password = "..."`, `secret_key: "..."`), tokens in
URLs, and `.env` files committed to Git. This must handle multiple file formats (YAML,
JSON, HCL, Python, JavaScript, Go, Rust) and minimize false positives for clearly
non-secret patterns like test fixtures, documentation examples, and placeholder values
like `"changeme"` or `"xxx"`.

Dockerfile best-practice validation is the third pillar. Check for: running as root user
(no `USER` instruction), using `latest` tag for base images, missing `HEALTHCHECK`
instruction, excessive layer count (more than 15 layers), `COPY . .` without a
`.dockerignore` file, `apt-get install` without `--no-install-recommends`, missing
`apt-get clean` or `rm -rf /var/lib/apt/lists/*` after install, and secrets passed as
`ARG` or `ENV` in build stages.

Dependency auditing covers both vulnerabilities and licenses. For each supported package
manager (Cargo.lock, package-lock.json, go.sum, requirements.txt, Pipfile.lock), parse
the lock file, check dependencies against known CVE databases (or use `trivy` if
available), and check dependency licenses against a configurable allow-list (MIT,
Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC are typically allowed; GPL, AGPL, SSPL may
require legal review). Flag any dependency with a known critical CVE or a disallowed
license.

The report must be structured, timestamped, and actionable. Generate both a JSON report
(for machine processing, CI integration, and tracking over time) and a human-readable
Markdown report. Each finding must include: rule ID, severity, file path, line number
(where applicable), description of the issue, evidence (the problematic line or
configuration), remediation guidance, and a reference URL. The Markdown report should
include an executive summary with pass/fail counts by severity and category.

## Requirements

1. Implement at least 10 infrastructure configuration checks for Terraform and/or
   Kubernetes manifests: unencrypted storage, overly permissive network rules, missing
   logging, publicly accessible databases, root containers, missing resource limits,
   wildcard IAM actions, missing tags/labels, insecure TLS versions, and disabled audit
   logging

2. Implement secret detection scanning with patterns for: AWS access keys
   (`AKIA[0-9A-Z]{16}`), GCP service account key files, private keys (RSA, EC, DSA
   headers), high-entropy base64 strings in config contexts, hardcoded passwords
   (`password\s*[=:]\s*["']`), API tokens, and connection strings with embedded
   credentials

3. Implement Dockerfile best-practice checks: root user, latest tags, missing
   HEALTHCHECK, COPY without .dockerignore, secrets in build args, missing package
   manager cleanup, excessive layer count, and use of deprecated instructions

4. Implement dependency vulnerability checking: parse at least two lock file formats
   (e.g., Cargo.lock and package-lock.json) and check against a local vulnerability
   database file or external tool like `trivy`

5. Implement dependency license auditing: extract license information from lock files
   or package metadata, check against a configurable allow-list file, and flag
   disallowed or unknown licenses with the specific package and license identifier

6. Each finding must be classified by severity (critical, high, medium, low, info) with
   a unique rule ID (e.g., `TF-001`, `SECRET-003`, `DOCKER-007`, `DEP-CVE-002`,
   `DEP-LIC-001`)

7. Generate a JSON report (`audit-report.json`) with structured findings: rule_id,
   severity, category, file, line, description, evidence (the actual offending text),
   remediation, reference_url, and scan timestamp

8. Generate a Markdown report (`audit-report.md`) with executive summary (total findings
   by severity, pass rate by category), detailed findings grouped by category, and a
   prioritized remediation section

9. Implement `audit-diff` recipe that compares the current audit report against a
   previous baseline JSON and shows only new findings — essential for CI to catch
   regressions without being overwhelmed by known issues

10. Implement `remediate` recipe that generates a prioritized remediation plan: critical
    findings first, grouped by file for efficient fixing, with estimated effort (quick
    fix, moderate, significant) per finding

11. Support configurable exceptions: an `.audit-ignore` file listing rule IDs or file
    path patterns to suppress (with required justification comments), so known false
    positives or accepted risks do not pollute reports — report suppressed count
    separately

12. Implement `audit-summary` recipe that provides a one-screen terminal overview:
    finding counts by category and severity in a matrix, top 5 most critical findings
    with file paths, and trend compared to previous audit (more/fewer/same findings)

## Hints

- For Terraform scanning, `grep -rn` with patterns like `encrypted\s*=\s*false` or
  `cidr_blocks.*0\.0\.0\.0/0` catches many common misconfigurations; for deeper
  analysis, parse HCL resource blocks with `awk` using `BEGIN`/`END` patterns

- High-entropy string detection: compute Shannon entropy of candidate strings —
  anything above 4.5 bits per character in a configuration value context is suspicious;
  implement with `awk` character frequency counting over the string

- For secret pattern matching, be precise: `AKIA[0-9A-Z]{16}` matches AWS access keys
  specifically; `[A-Za-z0-9+/]{40,}={0,2}` matches base64 but needs context filtering
  (is it in a config file? next to a key-like variable name?) to reduce false positives

- The `.audit-ignore` file can use a simple format:
  `RULE-ID:filepath_glob:justification text` — parse it before reporting and filter
  matching findings from the main report into a suppressed section

- For dependency license extraction from Rust:
  `cargo metadata --format-version 1 | jq '.packages[].license'` gives license info;
  for Node: read `node_modules/*/package.json` license fields

## Success Criteria

1. `just audit` runs all checks (infrastructure, secrets, Dockerfile, dependencies) and
   produces both `audit-report.json` and `audit-report.md` with consistent findings

2. The Markdown report contains an executive summary with finding counts by severity
   and category, and a clear overall pass/fail determination

3. Each finding in both reports includes rule ID, severity, file path, line number,
   evidence, remediation guidance, and reference URL

4. Secret detection correctly identifies planted test AWS keys, hardcoded passwords, and
   private key files without flagging documentation examples or test fixture strings

5. Dockerfile checks correctly flag images using `latest` tags, containers running as
   root (no USER instruction), and missing HEALTHCHECK instruction

6. `just audit-diff baseline=previous-report.json` shows only new findings not present
   in the baseline, correctly handling the case where some old findings are fixed

7. Entries in `.audit-ignore` with matching rule IDs and file patterns are excluded from
   the main report, with a "suppressed findings" count shown in the summary

8. `just remediate` produces a prioritized action plan sorted by severity, grouped by
   file, with specific remediation steps for each finding

## Research Resources

- [CIS Benchmarks](https://www.cisecurity.org/cis-benchmarks)
  -- security configuration standards for cloud platforms and containers

- [OWASP Cheat Sheet: Secrets Management](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
  -- patterns for identifying and managing exposed secrets

- [Dockerfile Best Practices](https://docs.docker.com/build/building/best-practices/)
  -- official Docker guidance that your checks should enforce

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- multi-line shell blocks for complex scanning and reporting logic

- [SPDX License List](https://spdx.org/licenses/)
  -- standardized license identifiers for dependency license auditing

- [Shannon Entropy - Wikipedia](https://en.wikipedia.org/wiki/Entropy_(information_theory))
  -- mathematical basis for high-entropy secret string detection

## What's Next

Proceed to exercise 50, the final challenge, where you will build a GitOps
reconciliation engine that watches a repository and reconciles desired state with actual
infrastructure.

## Summary

- **Multi-dimensional auditing** -- scanning infrastructure configs, secrets, containers, and dependencies from a single command
- **Structured reporting** -- generating machine-readable and human-readable reports with evidence and remediation guidance
- **Remediation planning** -- transforming audit findings into prioritized, actionable improvement plans with effort estimates
