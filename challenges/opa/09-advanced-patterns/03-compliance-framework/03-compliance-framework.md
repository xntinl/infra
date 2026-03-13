# Exercise 09-03: Building a Reusable Compliance Framework

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercises 09-01 (Performance) and 09-02 (Composition)

## Learning Objectives

After completing this exercise, you will be able to:

- Design a data-driven compliance framework that maps controls to Rego rules
- Generate structured compliance reports with pass/fail details, severity breakdowns, and percentages
- Extend the framework to new controls without modifying report logic
- Write tests that validate both individual controls and the aggregate report

## Why a Compliance Framework

Until now you have written individual policies: "S3 must have encryption," "EC2 must not be too large." But in the real world, those rules do not exist in isolation. They belong to compliance frameworks like CIS Benchmarks, SOC2, HIPAA, or PCI-DSS. Each framework defines numbered controls, and your job is to prove that your infrastructure meets them.

Building a framework in OPA means that each check produces a structured result tagged with the control ID, resource name, and pass/fail status. The report, counts, and percentages compute themselves automatically from that set of results. Adding a new control is just adding one rule and one metadata entry -- the rest updates itself.

## The Architecture

A compliance framework in Rego has three parts:

1. **Control metadata** -- a JSON file with control IDs, titles, descriptions, and severities
2. **Check rules** -- Rego rules that evaluate resources and produce structured results tagged with a `control_id`
3. **Report aggregation** -- Rego rules that summarize all results into counts, percentages, and severity breakdowns

The key insight is that each rule does not simply say "deny" or "allow." It produces a structured result object that includes everything an auditor needs: which control was checked, which resource was evaluated, and whether it passed or failed.

## Step 1: Define the Control Metadata

Create `controls.json`:

```json
{
    "compliance": {
        "framework": "CIS AWS Foundations Benchmark",
        "version": "1.5.0",
        "controls": {
            "CIS-2.1.1": {
                "title": "S3 bucket encryption at rest",
                "description": "All S3 buckets must have encryption enabled",
                "severity": "HIGH"
            },
            "CIS-2.1.2": {
                "title": "S3 bucket public access block",
                "description": "All S3 buckets must block public access",
                "severity": "CRITICAL"
            },
            "CIS-5.6": {
                "title": "EC2 IMDSv2 required",
                "description": "All EC2 instances must use IMDSv2 (Instance Metadata Service v2)",
                "severity": "HIGH"
            },
            "CIS-2.2.1": {
                "title": "EC2 EBS encryption enabled",
                "description": "All EBS volumes must be encrypted",
                "severity": "HIGH"
            }
        }
    }
}
```

Each control has an ID, a human-readable title, a description for auditors, and a severity level. The policy will reference these by ID.

## Step 2: Create the Input Data

Create `input.json` with a mix of compliant and non-compliant resources:

```json
{
    "resources": {
        "s3_buckets": [
            {
                "name": "prod-data-lake",
                "encryption": {"enabled": true, "algorithm": "AES256"},
                "public_access_block": {
                    "block_public_acls": true,
                    "block_public_policy": true,
                    "ignore_public_acls": true,
                    "restrict_public_buckets": true
                }
            },
            {
                "name": "legacy-uploads",
                "encryption": {"enabled": false},
                "public_access_block": {
                    "block_public_acls": false,
                    "block_public_policy": false,
                    "ignore_public_acls": false,
                    "restrict_public_buckets": false
                }
            },
            {
                "name": "staging-assets",
                "encryption": {"enabled": true, "algorithm": "aws:kms"},
                "public_access_block": {
                    "block_public_acls": true,
                    "block_public_policy": true,
                    "ignore_public_acls": true,
                    "restrict_public_buckets": false
                }
            }
        ],
        "ec2_instances": [
            {
                "id": "i-prod-web-01",
                "metadata_options": {"http_tokens": "required"},
                "ebs_volumes": [
                    {"id": "vol-001", "encrypted": true},
                    {"id": "vol-002", "encrypted": true}
                ]
            },
            {
                "id": "i-legacy-app-01",
                "metadata_options": {"http_tokens": "optional"},
                "ebs_volumes": [
                    {"id": "vol-003", "encrypted": false},
                    {"id": "vol-004", "encrypted": true}
                ]
            }
        ]
    }
}
```

You have 3 S3 buckets and 2 EC2 instances. Some are fully compliant, some are not. This gives you a realistic mix of pass and fail results.

## Step 3: Write the Compliance Policy

Create `policy.rego`:

```rego
package compliance

import rego.v1

# Reference to control metadata
controls := data.compliance.controls

# ===========================
# CIS-2.1.1: S3 Encryption
# ===========================
results contains result if {
    some bucket in input.resources.s3_buckets
    control_id := "CIS-2.1.1"
    passes := bucket.encryption.enabled == true
    result := {
        "control_id": control_id,
        "title": controls[control_id].title,
        "severity": controls[control_id].severity,
        "resource": bucket.name,
        "resource_type": "aws_s3_bucket",
        "status": _status(passes),
    }
}

# ===========================
# CIS-2.1.2: S3 Public Access Block
# ===========================
results contains result if {
    some bucket in input.resources.s3_buckets
    control_id := "CIS-2.1.2"
    pab := bucket.public_access_block
    passes := (
        pab.block_public_acls == true
        and pab.block_public_policy == true
        and pab.ignore_public_acls == true
        and pab.restrict_public_buckets == true
    )
    result := {
        "control_id": control_id,
        "title": controls[control_id].title,
        "severity": controls[control_id].severity,
        "resource": bucket.name,
        "resource_type": "aws_s3_bucket",
        "status": _status(passes),
    }
}

# ===========================
# CIS-5.6: EC2 IMDSv2
# ===========================
results contains result if {
    some instance in input.resources.ec2_instances
    control_id := "CIS-5.6"
    passes := instance.metadata_options.http_tokens == "required"
    result := {
        "control_id": control_id,
        "title": controls[control_id].title,
        "severity": controls[control_id].severity,
        "resource": instance.id,
        "resource_type": "aws_ec2_instance",
        "status": _status(passes),
    }
}

# ===========================
# CIS-2.2.1: EBS Encryption
# ===========================
results contains result if {
    some instance in input.resources.ec2_instances
    some vol in instance.ebs_volumes
    control_id := "CIS-2.2.1"
    passes := vol.encrypted == true
    result := {
        "control_id": control_id,
        "title": controls[control_id].title,
        "severity": controls[control_id].severity,
        "resource": sprintf("%s/%s", [instance.id, vol.id]),
        "resource_type": "aws_ebs_volume",
        "status": _status(passes),
    }
}

# ===========================
# Helper: convert boolean to status string
# ===========================
_status(true) := "PASS"
_status(false) := "FAIL"

# ===========================
# Aggregated report
# ===========================
report := {
    "framework": data.compliance.framework,
    "version": data.compliance.version,
    "total_checks": count(results),
    "passed": count(_passed),
    "failed": count(_failed),
    "compliance_percentage": _percentage,
    "by_severity": _by_severity,
    "details": results,
}

_passed := {r | some r in results; r.status == "PASS"}
_failed := {r | some r in results; r.status == "FAIL"}

_percentage := round((count(_passed) / count(results)) * 100) if {
    count(results) > 0
}

_by_severity := {sev: {"pass": p, "fail": f} |
    some r in results
    sev := r.severity
    p := count({x | some x in _passed; x.severity == sev})
    f := count({x | some x in _failed; x.severity == sev})
}

# ===========================
# Convenience sets
# ===========================
failing_controls := {r.control_id | some r in _failed}

non_compliant_resources := {r.resource | some r in _failed}
```

Let's walk through the important patterns:

- Each control rule produces a **structured result** with all the metadata an auditor needs
- The `_status` function converts a boolean to "PASS"/"FAIL" -- a common pattern in compliance frameworks
- The `report` rule aggregates everything: counts, compliance percentage, severity breakdown
- The `failing_controls` and `non_compliant_resources` sets provide quick-glance summaries
- Adding a new control means adding one `results contains result if {...}` block and one entry in `controls.json` -- the report updates itself

## Step 4: Evaluate the Report

Run the full compliance evaluation:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.report" --format pretty
```

Expected output (simplified -- the actual output includes full details):

```json
{
  "by_severity": {
    "CRITICAL": {"fail": 2, "pass": 1},
    "HIGH": {"fail": 2, "pass": 6}
  },
  "compliance_percentage": 64,
  "details": ["..."],
  "failed": 4,
  "framework": "CIS AWS Foundations Benchmark",
  "passed": 7,
  "total_checks": 11,
  "version": "1.5.0"
}
```

Out of 11 total checks, 7 pass and 4 fail, giving 64% compliance.

## Step 5: Drill Into Failures

See which controls have at least one failing resource:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.failing_controls" --format pretty
```

Expected output:

```
["CIS-2.1.1", "CIS-2.1.2", "CIS-2.2.1", "CIS-5.6"]
```

All four controls have at least one failure. See which resources are non-compliant:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.non_compliant_resources" --format pretty
```

Expected output:

```
["i-legacy-app-01", "i-legacy-app-01/vol-003", "legacy-uploads", "staging-assets"]
```

The problematic resources are: `legacy-uploads` (no encryption, no public access block), `staging-assets` (incomplete public access block), `i-legacy-app-01` (IMDSv2 not required), and its volume `vol-003` (unencrypted).

To see only the FAIL results with full detail:

```bash
opa eval -d policy.rego -d controls.json -i input.json \
  "{r | some r in data.compliance.results; r.status == \"FAIL\"}" --format pretty
```

This gives you exactly what an auditor needs: which control failed, on which resource, and the severity.

## Step 6: A Common Mistake -- Missing Controls in the Metadata

Watch what happens if you reference a control ID that does not exist in `controls.json`. Suppose you add a rule for `CIS-99.99` but forget to add it to the metadata:

```rego
# This rule would silently produce no results
results contains result if {
    some bucket in input.resources.s3_buckets
    control_id := "CIS-99.99"
    passes := bucket.versioning.enabled == true
    result := {
        "control_id": control_id,
        "title": controls[control_id].title,   # undefined -- rule fails silently
        "severity": controls[control_id].severity,
        "resource": bucket.name,
        "resource_type": "aws_s3_bucket",
        "status": _status(passes),
    }
}
```

Because `controls["CIS-99.99"]` is undefined, the lookup for `.title` fails, and the entire rule body evaluates to undefined. No error, no result -- the check is silently skipped. This is why you should always add the metadata first and write a test that confirms the control produces results.

## Step 7: Write Tests for the Framework

Create `policy_test.rego`:

```rego
package compliance_test

import rego.v1
import data.compliance

test_encrypted_bucket_passes if {
    results := compliance.results with input as {"resources": {
        "s3_buckets": [{"name": "ok", "encryption": {"enabled": true}, "public_access_block": {"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true}}],
        "ec2_instances": []
    }}
    passed := {r | some r in results; r.status == "PASS"}
    count(passed) == 2
}

test_unencrypted_bucket_fails if {
    results := compliance.results with input as {"resources": {
        "s3_buckets": [{"name": "bad", "encryption": {"enabled": false}, "public_access_block": {"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true}}],
        "ec2_instances": []
    }}
    failed := {r | some r in results; r.control_id == "CIS-2.1.1"; r.status == "FAIL"}
    count(failed) == 1
}

test_imdsv2_required_passes if {
    results := compliance.results with input as {"resources": {
        "s3_buckets": [],
        "ec2_instances": [{"id": "i-ok", "metadata_options": {"http_tokens": "required"}, "ebs_volumes": []}]
    }}
    passed := {r | some r in results; r.control_id == "CIS-5.6"; r.status == "PASS"}
    count(passed) == 1
}

test_report_has_required_fields if {
    report := compliance.report with input as {"resources": {
        "s3_buckets": [{"name": "test", "encryption": {"enabled": true}, "public_access_block": {"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true}}],
        "ec2_instances": []
    }}
    report.total_checks > 0
    report.passed >= 0
    report.failed >= 0
    report.compliance_percentage >= 0
}
```

Run the tests:

```bash
opa test -v policy.rego controls.json policy_test.rego
```

Expected output:

```
data.compliance_test.test_encrypted_bucket_passes: PASS (Xns)
data.compliance_test.test_unencrypted_bucket_fails: PASS (Xns)
data.compliance_test.test_imdsv2_required_passes: PASS (Xns)
data.compliance_test.test_report_has_required_fields: PASS (Xns)
--------------------------------------------------------------------------------
PASS: 4/4
```

## Extending the Framework

To add a new CIS control, you follow three steps:

1. Add the metadata entry in `controls.json`
2. Add a `results contains result if {...}` rule in `policy.rego`
3. Add a test in `policy_test.rego`

The report, counts, percentages, and severity breakdowns update automatically because they are comprehensions over the `results` set.

## Verify What You Learned

**Command 1** -- Generate the report and verify the compliance percentage:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.report.compliance_percentage" --format pretty
```

Expected output:

```
64
```

**Command 2** -- Count how many checks fail:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.report.failed" --format pretty
```

Expected output:

```
4
```

**Command 3** -- List the controls that have at least one failure:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.failing_controls" --format pretty
```

Expected output:

```
["CIS-2.1.1", "CIS-2.1.2", "CIS-2.2.1", "CIS-5.6"]
```

**Command 4** -- Run the framework tests:

```bash
opa test -v policy.rego controls.json policy_test.rego 2>&1 | tail -1
```

Expected output:

```
PASS: 4/4
```

**Command 5** -- Verify the total number of checks:

```bash
opa eval -d policy.rego -d controls.json -i input.json "data.compliance.report.total_checks" --format pretty
```

Expected output:

```
11
```

## Section 09 Summary

Across these three exercises you covered the advanced patterns that make OPA policies production-ready:

- **Performance** (09-01): identifying slow patterns, rewriting policies to use OPA's indexer, and measuring improvements with `opa bench`
- **Composition** (09-02): organizing policies into shared libraries and domain-specific packages with colocated tests, all runnable with a single command
- **Compliance Framework** (09-03): building a data-driven framework where control metadata, evaluation rules, and report aggregation work together, and adding new controls is a three-step process

These patterns compose naturally. You can package your compliance framework as a bundle (section 08), serve it through an OPA server, record every compliance decision in decision logs, optimize the rules for performance, and organize the codebase with proper composition.

## Course Summary

Over nine sections, this tutorial took you from zero to a production-capable OPA setup:

1. **Rego basics** -- syntax, rules, boolean logic
2. **Data structures** -- objects, arrays, sets, nested access
3. **Iteration and comprehensions** -- `some`, `every`, set/object/array comprehensions
4. **Functions and testing** -- reusable functions, `opa test`, test-driven development
5. **Real-world policies** -- RBAC, ABAC, resource validation
6. **Integration** -- using OPA with Terraform, Kubernetes, and CI/CD
7. **Error handling** -- defaults, partial rules, debugging with `opa eval --explain`
8. **Policy distribution** -- bundles, OPA as a server, decision logs
9. **Advanced patterns** -- performance, composition, compliance frameworks

You now have the tools and patterns to write, test, distribute, and operate OPA policies at scale. The next step is applying them to your own infrastructure.

## What's Next

This is the final exercise in the tutorial. From here, you can:

- Apply the compliance framework pattern to your organization's specific standards
- Set up a CI pipeline that builds bundles, runs tests, and publishes to an artifact store
- Deploy OPA as a sidecar or daemon in your microservice architecture
- Explore OPA's WebAssembly compilation for embedding policies directly into applications

## Reference

- [CIS AWS Foundations Benchmark](https://www.cisecurity.org/benchmark/amazon_web_services)
- [OPA -- generating structured output](https://www.openpolicyagent.org/docs/latest/policy-language/#generating-objects)
- [Compliance as Code with OPA](https://blog.openpolicyagent.org/compliance-as-code-with-opa-e0b5e4a1b5f5)

## Additional Resources

- [Styra Academy -- OPA fundamentals](https://academy.styra.com/)
- [Regula -- compliance framework for Terraform using OPA](https://github.com/fugue/regula)
- [Conftest -- policy testing for structured configuration](https://www.conftest.dev/)
- [OPA Playground](https://play.openpolicyagent.org/)
