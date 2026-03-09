# 47. Mock Provider for Unit Testing

## Prerequisites

- Terraform >= 1.7 installed
- No cloud credentials required for this exercise
- Completed exercise 46 (Integration Test with Apply)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `mock_provider` in `.tftest.hcl` files to replace a real cloud provider with a simulated one
- Run tests completely offline without cloud credentials or API calls
- Validate module logic (naming, counts, relationships) using synthetic attribute values
- Distinguish when to use unit tests (mock), plan-mode tests, and integration tests

## Why Mock Providers

Integration tests give you the highest confidence because they exercise real APIs, but they are slow, require credentials, and cost money. Plan-mode tests are faster but still need a configured provider to initialize. Mock providers eliminate both constraints: `mock_provider "aws" {}` tells Terraform to simulate the AWS provider entirely, generating synthetic values for computed attributes like ARNs and IDs without making any network calls.

This makes mock-provider tests the fastest tier of the testing pyramid. They run in seconds, work in any CI environment regardless of cloud access, and are ideal for validating the internal logic of your modules -- how resources are named, how many are created, how inputs flow through to outputs. Combined with plan-mode tests for logic verification and integration tests for API-level confidence, mock providers complete a three-tier testing strategy that balances speed with thoroughness.

## Step 1 -- Create an IAM Role Module

Create the directory `modules/iam-role/` with one file:

```hcl
# modules/iam-role/main.tf

variable "role_name"   { type = string }
variable "service"     { type = string }
variable "policy_arns" { type = list(string) }

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = [var.service]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = var.role_name
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "this" {
  for_each   = toset(var.policy_arns)
  role       = aws_iam_role.this.name
  policy_arn = each.value
}

output "role_name"        { value = aws_iam_role.this.name }
output "role_arn"          { value = aws_iam_role.this.arn }
output "attachment_count" { value = length(aws_iam_role_policy_attachment.this) }
```

## Step 2 -- Write Unit Tests with mock_provider

```hcl
# modules/iam-role/unit.tftest.hcl

mock_provider "aws" {}

run "creates_role_with_correct_name" {
  command = plan

  variables {
    role_name   = "test-lambda-role"
    service     = "lambda.amazonaws.com"
    policy_arns = ["arn:aws:iam::aws:policy/AWSLambdaBasicExecutionRole"]
  }

  assert {
    condition     = aws_iam_role.this.name == "test-lambda-role"
    error_message = "Role name should match input"
  }
}

run "attaches_multiple_policies" {
  command = plan

  variables {
    role_name   = "test-multi-policy"
    service     = "ecs-tasks.amazonaws.com"
    policy_arns = [
      "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
      "arn:aws:iam::aws:policy/AmazonDynamoDBReadOnlyAccess",
      "arn:aws:iam::aws:policy/CloudWatchLogsFullAccess",
    ]
  }

  assert {
    condition     = output.attachment_count == 3
    error_message = "Should attach 3 policies"
  }

  assert {
    condition     = output.role_name == "test-multi-policy"
    error_message = "Role name output should match"
  }
}

run "empty_policy_list" {
  command = plan

  variables {
    role_name   = "test-no-policies"
    service     = "lambda.amazonaws.com"
    policy_arns = []
  }

  assert {
    condition     = output.attachment_count == 0
    error_message = "Should have zero attachments with empty policy list"
  }
}
```

The `mock_provider "aws" {}` line at the top replaces the real AWS provider for all `run` blocks in this file. No `provider` block, no credentials, no API calls.

## Step 3 -- Run the Tests

```bash
cd modules/iam-role
terraform init
terraform test
```

### Scenario A -- All tests pass (expected)

```
modules/iam-role/unit.tftest.hcl... in progress
  run "creates_role_with_correct_name"... pass
  run "attaches_multiple_policies"... pass
  run "empty_policy_list"... pass
modules/iam-role/unit.tftest.hcl... tearing down
modules/iam-role/unit.tftest.hcl... pass

Success! 3 passed, 0 failed.
```

Notice: no AWS credentials were needed, no resources were created, and the tests ran in seconds.

### Scenario B -- Verifying synthetic values

The mock provider generates synthetic values for computed attributes. For example, `aws_iam_role.this.arn` receives a synthetic ARN string rather than a real one. You can observe this by adding a verbose assertion:

```hcl
assert {
  condition     = length(output.role_arn) > 0
  error_message = "ARN should be non-empty (synthetic value from mock)"
}
```

This passes because the mock provider populates the ARN with a non-empty synthetic string.

### Scenario C -- Testing without AWS_PROFILE or AWS_ACCESS_KEY_ID

Unset all AWS environment variables and run the test:

```bash
unset AWS_PROFILE AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
cd modules/iam-role
terraform test
```

The test still passes. This confirms that `mock_provider` truly eliminates the credential requirement.

## Common Mistakes

### Asserting exact mock values for computed attributes

Synthetic values generated by `mock_provider` are not predictable strings. Do not write assertions like `output.role_arn == "arn:aws:iam::123456789012:role/test"`. Instead, validate properties you control (like the role name) or use structural checks (like `length() > 0`).

### Mixing mock_provider with command = apply

`mock_provider` is designed for plan-mode tests. While it technically works with `command = apply`, the "applied" state contains only synthetic values and does not exercise real APIs. If you need real API validation, use integration tests without mocks (exercise 46).

## Verify What You Learned

```bash
cd modules/iam-role && terraform init && terraform test
```

Expected: `Success! 3 passed, 0 failed.`

```bash
cd modules/iam-role && terraform test -verbose
```

Expected: detailed output showing each run block with planned values and assertion results.

```bash
cd modules/iam-role && terraform test 2>&1 | grep -c "pass"
```

Expected: `4` (three individual passes plus one file-level pass).

```bash
cd modules/iam-role && terraform test 2>&1 | grep "fail"
```

Expected: no output (no failures).

## Section 11 Summary -- Testing

Across three exercises you built a complete testing strategy for Terraform modules:

- **Exercise 45** -- Plan-mode tests with `.tftest.hcl` validate logic (naming, tags, transformations) without creating infrastructure.
- **Exercise 46** -- Integration tests with `command = apply` create real resources, assert against actual state, and automatically clean up.
- **Exercise 47** -- Mock providers with `mock_provider` enable credential-free unit tests that run in seconds anywhere.

These three tiers form a testing pyramid: many fast unit tests at the base, logic tests in the middle, and fewer but higher-confidence integration tests at the top.

## Course Summary

You completed 47 exercises across 11 sections:

1. **Variables and Types** -- complex types, optional attributes, validation, sensitive values
2. **Expressions and Functions** -- network math, flattening, templates, maps, regex, formatting, fallbacks
3. **Dynamic Blocks** -- security groups, IAM policies, tags from data
4. **for_each and count** -- lists, maps, conditionals, YAML, set conversion
5. **State Operations** -- import, move, remove, declarative import, raw state
6. **Lifecycle Rules** -- replacement order, prevent destroy, ignore drift, triggered replacement
7. **Data Sources** -- AMIs, AZs, account identity, remote state, external scripts
8. **Outputs and Locals** -- structured outputs, derived values, chained transforms
9. **Provider Configuration** -- multi-region, cross-account, provider passthrough
10. **Modules** -- minimal modules, for_each, depends_on, version pinning
11. **Testing** -- plan-mode tests, integration tests, mock providers

## What's Next

Congratulations -- you have completed the full Terraform exercise course. From here, consider these directions for continued learning:

- **Terraform Cloud / HCP Terraform** -- remote state, plan approvals, policy-as-code with Sentinel or OPA
- **CI/CD pipelines** -- integrate `terraform plan` and `terraform test` into GitHub Actions, GitLab CI, or your preferred automation platform
- **Provider development** -- build custom providers using the Terraform Plugin Framework
- **Advanced patterns** -- workspaces for environment isolation, Terragrunt for DRY configurations, and module composition at scale
- **Certification** -- the HashiCorp Terraform Associate certification validates the skills covered in this course

## Reference

- [Test Mocking](https://developer.hashicorp.com/terraform/language/tests/mocking)

## Additional Resources

- [Mastering the Terraform Test Block](https://ckdbtech.com/mastering-terraform-test-block/) -- comprehensive walkthrough covering unit testing with mock_provider and comparison with integration tests
- [Write Terraform Tests](https://developer.hashicorp.com/terraform/tutorials/configuration-language/test) -- official tutorial including mock provider examples
- [Terraform Testing: A Complete Guide](https://spacelift.io/blog/terraform-test) -- practical guide on all testing strategies: unit with mocks, plan-mode, and integration with apply
- [Tests Reference](https://developer.hashicorp.com/terraform/language/tests) -- complete reference for .tftest.hcl files, mock_provider, and synthetic computed attributes
