# Exercise 09-02: Composing Policies From Multiple Packages

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 09-01 (Performance)

## Learning Objectives

After completing this exercise, you will be able to:

- Organize policies into a multi-package repository with shared libraries
- Import and reuse helper functions across domain-specific policies
- Run all tests in a policy tree with a single command using bundle mode

## Why Composition Matters

When you have 3 policies, everything fits in one file. When you have 30, you need structure. Without it, you end up duplicating validation logic, tests break when you rename a function, and onboarding new team members takes longer than it should.

The Rego package and import system lets you organize policies the same way you would organize any real codebase: shared libraries for reusable logic, domain-specific packages for actual policy decisions, and colocated tests next to their code.

## How Packages and Imports Work

Every Rego file belongs to a package. A package is simply a path in the `data.` tree -- `package policies.s3` means the rules in that file live at `data.policies.s3`. The statement `import data.lib.helpers` brings in rules from another package so you can call them.

A common directory layout for a policy monorepo looks like this:

```
policies/
  lib/
    helpers.rego         # shared utility functions
    helpers_test.rego    # tests for the helpers
  aws/
    s3.rego              # S3 policies
    s3_test.rego         # S3 tests
    ec2.rego             # EC2 policies
    ec2_test.rego        # EC2 tests
  data.json              # shared data (e.g., allowed region lists)
```

The key principles:

1. **`lib/`** contains pure utility functions -- they do not make allow/deny decisions, they just compute values
2. **`aws/`** (or whatever your domain is) contains the real policies that import from `lib`
3. Tests live next to their code with a `_test.rego` suffix
4. `opa test -b policies/` runs everything in one shot

When you use the `-b` flag (bundle mode), OPA loads the entire directory and resolves all imports automatically. This is cleaner and more efficient than passing multiple `-d` flags.

## Step 1: Create Shared Helper Functions

Create `policies/lib/helpers.rego`:

```rego
package lib.helpers

import rego.v1

# Check if a resource has all required tags
has_required_tags(resource, required_tags) if {
    every tag in required_tags {
        resource.tags[tag]
    }
}

# Check if a value exists in a list
is_in_list(value, list) if {
    value in list
}

# Check that a string is not empty or null
not_empty(value) if {
    value != ""
    value != null
}

# Return the set of indices where a collection matches a field value
matching_indices(collection, field, expected_value) := {i |
    some i, elem in collection
    elem[field] == expected_value
}
```

Create `policies/lib/helpers_test.rego`:

```rego
package lib.helpers_test

import rego.v1
import data.lib.helpers

test_has_required_tags_present if {
    resource := {"tags": {"Environment": "prod", "Team": "platform", "Project": "api"}}
    helpers.has_required_tags(resource, ["Environment", "Team"])
}

test_has_required_tags_missing if {
    resource := {"tags": {"Environment": "prod"}}
    not helpers.has_required_tags(resource, ["Environment", "Team"])
}

test_is_in_list_found if {
    helpers.is_in_list("us-east-1", ["us-east-1", "eu-west-1"])
}

test_is_in_list_not_found if {
    not helpers.is_in_list("ap-south-1", ["us-east-1", "eu-west-1"])
}

test_not_empty_with_value if {
    helpers.not_empty("something")
}

test_not_empty_with_empty_string if {
    not helpers.not_empty("")
}

test_matching_indices_counts_correctly if {
    collection := [{"status": "ok"}, {"status": "error"}, {"status": "ok"}]
    result := helpers.matching_indices(collection, "status", "ok")
    count(result) == 2
}
```

Run the helper tests in isolation to make sure they pass before building on top of them:

```bash
opa test -v -b policies/lib/
```

Expected output:

```
policies/lib/helpers_test.rego:
data.lib.helpers_test.test_has_required_tags_present: PASS (Xns)
data.lib.helpers_test.test_has_required_tags_missing: PASS (Xns)
data.lib.helpers_test.test_is_in_list_found: PASS (Xns)
data.lib.helpers_test.test_is_in_list_not_found: PASS (Xns)
data.lib.helpers_test.test_not_empty_with_value: PASS (Xns)
data.lib.helpers_test.test_not_empty_with_empty_string: PASS (Xns)
data.lib.helpers_test.test_matching_indices_counts_correctly: PASS (Xns)
--------------------------------------------------------------------------------
PASS: 7/7
```

## Step 2: Create S3 Policies That Import Helpers

Create `policies/aws/s3.rego`:

```rego
package policies.aws.s3

import rego.v1
import data.lib.helpers

required_tags := ["Environment", "Team"]
allowed_regions := ["us-east-1", "us-west-2", "eu-west-1"]

# Generate a violation for each bucket missing required tags
violations contains msg if {
    some bucket in input.buckets
    not helpers.has_required_tags(bucket, required_tags)
    msg := sprintf("S3 bucket '%s' is missing required tags: %v", [bucket.name, required_tags])
}

# Generate a violation for each bucket without encryption
violations contains msg if {
    some bucket in input.buckets
    not bucket.encryption.enabled
    msg := sprintf("S3 bucket '%s' does not have encryption enabled", [bucket.name])
}

# Generate a violation for each bucket in a disallowed region
violations contains msg if {
    some bucket in input.buckets
    not helpers.is_in_list(bucket.region, allowed_regions)
    msg := sprintf("S3 bucket '%s' is in region '%s', allowed regions: %v", [bucket.name, bucket.region, allowed_regions])
}

# A resource is compliant only when it has zero violations
compliant if {
    count(violations) == 0
}
```

Create `policies/aws/s3_test.rego`:

```rego
package policies.aws.s3_test

import rego.v1
import data.policies.aws.s3

test_compliant_bucket if {
    s3.compliant with input as {"buckets": [{
        "name": "my-bucket",
        "region": "us-east-1",
        "encryption": {"enabled": true},
        "tags": {"Environment": "prod", "Team": "platform"}
    }]}
}

test_bucket_missing_tags if {
    violations := s3.violations with input as {"buckets": [{
        "name": "bucket-no-tags",
        "region": "us-east-1",
        "encryption": {"enabled": true},
        "tags": {}
    }]}
    count(violations) > 0
}

test_bucket_without_encryption if {
    violations := s3.violations with input as {"buckets": [{
        "name": "bucket-insecure",
        "region": "us-east-1",
        "encryption": {"enabled": false},
        "tags": {"Environment": "prod", "Team": "platform"}
    }]}
    count(violations) == 1
}

test_bucket_in_disallowed_region if {
    violations := s3.violations with input as {"buckets": [{
        "name": "bucket-asia",
        "region": "ap-southeast-1",
        "encryption": {"enabled": true},
        "tags": {"Environment": "prod", "Team": "platform"}
    }]}
    count(violations) == 1
}
```

## Step 3: Create EC2 Policies That Import the Same Helpers

Create `policies/aws/ec2.rego`:

```rego
package policies.aws.ec2

import rego.v1
import data.lib.helpers

required_tags := ["Environment", "Team"]
allowed_types := ["t3.micro", "t3.small", "t3.medium", "t3.large"]

violations contains msg if {
    some instance in input.instances
    not helpers.has_required_tags(instance, required_tags)
    msg := sprintf("EC2 instance '%s' is missing required tags: %v", [instance.id, required_tags])
}

violations contains msg if {
    some instance in input.instances
    not helpers.is_in_list(instance.instance_type, allowed_types)
    msg := sprintf("EC2 instance '%s' uses type '%s', allowed types: %v", [instance.id, instance.instance_type, allowed_types])
}

violations contains msg if {
    some instance in input.instances
    not instance.monitoring_enabled
    msg := sprintf("EC2 instance '%s' does not have detailed monitoring enabled", [instance.id])
}

compliant if {
    count(violations) == 0
}
```

Create `policies/aws/ec2_test.rego`:

```rego
package policies.aws.ec2_test

import rego.v1
import data.policies.aws.ec2

test_compliant_instance if {
    ec2.compliant with input as {"instances": [{
        "id": "i-123",
        "instance_type": "t3.small",
        "monitoring_enabled": true,
        "tags": {"Environment": "prod", "Team": "platform"}
    }]}
}

test_disallowed_instance_type if {
    violations := ec2.violations with input as {"instances": [{
        "id": "i-456",
        "instance_type": "m5.4xlarge",
        "monitoring_enabled": true,
        "tags": {"Environment": "prod", "Team": "platform"}
    }]}
    count(violations) == 1
}

test_missing_monitoring if {
    violations := ec2.violations with input as {"instances": [{
        "id": "i-789",
        "instance_type": "t3.micro",
        "monitoring_enabled": false,
        "tags": {"Environment": "prod", "Team": "platform"}
    }]}
    count(violations) == 1
}

test_multiple_violations if {
    violations := ec2.violations with input as {"instances": [{
        "id": "i-000",
        "instance_type": "c5.9xlarge",
        "monitoring_enabled": false,
        "tags": {}
    }]}
    count(violations) == 3
}
```

## Step 4: Add Shared Data

Create `policies/data.json`:

```json
{
    "config": {
        "company": "mycompany",
        "default_region": "us-east-1"
    }
}
```

## Step 5: Run All Tests at Once

This is where composition pays off. A single command tests the entire policy tree:

```bash
opa test -v -b policies/
```

Expected output:

```
policies/aws/ec2_test.rego:
data.policies.aws.ec2_test.test_compliant_instance: PASS (Xns)
data.policies.aws.ec2_test.test_disallowed_instance_type: PASS (Xns)
data.policies.aws.ec2_test.test_missing_monitoring: PASS (Xns)
data.policies.aws.ec2_test.test_multiple_violations: PASS (Xns)
policies/aws/s3_test.rego:
data.policies.aws.s3_test.test_compliant_bucket: PASS (Xns)
data.policies.aws.s3_test.test_bucket_missing_tags: PASS (Xns)
data.policies.aws.s3_test.test_bucket_without_encryption: PASS (Xns)
data.policies.aws.s3_test.test_bucket_in_disallowed_region: PASS (Xns)
policies/lib/helpers_test.rego:
data.lib.helpers_test.test_has_required_tags_present: PASS (Xns)
data.lib.helpers_test.test_has_required_tags_missing: PASS (Xns)
data.lib.helpers_test.test_is_in_list_found: PASS (Xns)
data.lib.helpers_test.test_is_in_list_not_found: PASS (Xns)
data.lib.helpers_test.test_not_empty_with_value: PASS (Xns)
data.lib.helpers_test.test_not_empty_with_empty_string: PASS (Xns)
data.lib.helpers_test.test_matching_indices_counts_correctly: PASS (Xns)
--------------------------------------------------------------------------------
PASS: 15/15
```

15 tests across three packages, all passing with one command.

## Step 6: Evaluate a Specific Policy With the Full Bundle

Create `input-s3-mixed.json`:

```json
{
  "buckets": [
    {"name": "bucket-ok", "region": "us-east-1", "encryption": {"enabled": true}, "tags": {"Environment": "prod", "Team": "platform"}},
    {"name": "bucket-bad", "region": "ap-south-1", "encryption": {"enabled": false}, "tags": {}}
  ]
}
```

```bash
opa eval -b policies/ -i input-s3-mixed.json "data.policies.aws.s3.violations" --format pretty
```

The output will list three violations for `bucket-bad`: missing tags, no encryption, and disallowed region. The compliant `bucket-ok` produces no violations.

The `-b` flag loads the entire directory as a bundle, automatically resolving the import from `policies.aws.s3` to `lib.helpers`. This is cleaner than chaining multiple `-d` flags.

## A Common Mistake: Package Name Does Not Match Directory Path

If your file is at `policies/aws/s3.rego` but the package declaration says `package s3` instead of `package policies.aws.s3`, OPA will load the file but your imports will not resolve. The package name must reflect where the file sits in the bundle directory tree. When you get "undefined" errors on imports, this is the first thing to check.

## Verify What You Learned

**Command 1** -- Run all tests and confirm all 15 pass:

```bash
opa test -b policies/ 2>&1 | tail -1
```

Expected output:

```
PASS: 15/15
```

**Command 2** -- Evaluate the EC2 policy with an instance that violates everything:

Create `input-ec2-bad.json`:

```json
{"instances": [{"id": "i-bad", "instance_type": "c5.9xlarge", "monitoring_enabled": false, "tags": {}}]}
```

```bash
opa eval -b policies/ -i input-ec2-bad.json "count(data.policies.aws.ec2.violations)" --format pretty
```

Expected output:

```
3
```

**Command 3** -- Verify that a compliant instance produces no violations:

Create `input-ec2-ok.json`:

```json
{"instances": [{"id": "i-ok", "instance_type": "t3.small", "monitoring_enabled": true, "tags": {"Environment": "dev", "Team": "backend"}}]}
```

```bash
opa eval -b policies/ -i input-ec2-ok.json "data.policies.aws.ec2.compliant" --format pretty
```

Expected output:

```
true
```

You now know how to organize a policy codebase with shared libraries, domain-specific packages, colocated tests, and bundle-mode evaluation. In the next exercise, you will use these composition techniques to build a full compliance framework.

## What's Next

Exercise 09-03 builds a reusable compliance framework -- the most complex exercise in this tutorial. You will map CIS Benchmark controls to Rego rules and generate structured compliance reports with pass/fail details, severity breakdowns, and compliance percentages.

## Reference

- [OPA packages and imports](https://www.openpolicyagent.org/docs/latest/policy-language/#packages)
- [opa test -- bundle mode](https://www.openpolicyagent.org/docs/latest/cli/#opa-test)
- [Bundle file format](https://www.openpolicyagent.org/docs/latest/management-bundles/#bundle-file-format)

## Additional Resources

- [Styra Academy -- OPA fundamentals](https://academy.styra.com/)
- [OPA best practices for policy structure](https://www.openpolicyagent.org/docs/latest/policy-language/#best-practices)
- [Conftest -- policy testing for configuration files](https://www.conftest.dev/)
