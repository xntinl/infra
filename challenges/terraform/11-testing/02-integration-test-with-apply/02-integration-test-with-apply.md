# 46. Integration Test with Apply

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 45 (Plan-Mode Test with .tftest.hcl)

## Learning Objectives

After completing this exercise, you will be able to:

- Write integration tests using `command = apply` that create real infrastructure
- Validate actual resource state with assertions after apply
- Rely on Terraform's automatic cleanup (destroy) to remove test resources, even when assertions fail
- Use `contains()` to verify list membership in assertions

## Why Integration Tests

Plan-mode tests validate your Terraform logic, but they cannot catch problems that only surface when resources interact with real cloud APIs: permission errors, service quotas, naming conflicts, provider bugs, and eventual-consistency delays. Integration tests fill this gap by actually creating the resources, asserting against the real state, and then destroying everything.

The key feature of `terraform test` with `command = apply` is **automatic cleanup**. Whether the test passes or fails, Terraform destroys all resources created during the test run. This prevents orphaned resources from accumulating in your sandbox account and running up costs. The tradeoff is speed -- integration tests take longer than plan-mode tests because they make real API calls. Use both levels: plan-mode tests for fast logic validation and integration tests for end-to-end confidence.

## Step 1 -- Create an SSM Config Module

Create the directory `modules/ssm-config/` with one file:

```hcl
# modules/ssm-config/main.tf

variable "prefix" { type = string }
variable "config" { type = map(string) }

resource "aws_ssm_parameter" "this" {
  for_each = var.config
  name     = "/${var.prefix}/${each.key}"
  type     = "String"
  value    = each.value
  tags     = { ManagedBy = "terraform" }
}

output "parameter_count" { value = length(aws_ssm_parameter.this) }
output "parameter_names" { value = [for k, v in aws_ssm_parameter.this : v.name] }
output "parameter_arns"  { value = { for k, v in aws_ssm_parameter.this : k => v.arn } }
```

## Step 2 -- Write the Integration Test

```hcl
# modules/ssm-config/integration.tftest.hcl

run "create_parameters" {
  command = apply

  variables {
    prefix = "test-kata"
    config = {
      "db-host" = "localhost"
      "db-port" = "5432"
      "api-key" = "test-key"
    }
  }

  assert {
    condition     = output.parameter_count == 3
    error_message = "Should create exactly 3 parameters"
  }

  assert {
    condition     = contains(output.parameter_names, "/test-kata/db-host")
    error_message = "Should include db-host parameter"
  }

  assert {
    condition     = length(output.parameter_arns) == 3
    error_message = "Should output 3 ARNs"
  }
}
```

Notice `command = apply` instead of `command = plan`. Terraform will actually create three SSM parameters in your AWS account during this test.

## Step 3 -- Run the Test

```bash
cd modules/ssm-config
terraform init
terraform test
```

### Scenario A -- All assertions pass (expected)

```
modules/ssm-config/integration.tftest.hcl... in progress
  run "create_parameters"... pass
modules/ssm-config/integration.tftest.hcl... tearing down
modules/ssm-config/integration.tftest.hcl... pass

Success! 1 passed, 0 failed.
```

After the assertions are evaluated, Terraform automatically destroys the three SSM parameters. No manual cleanup needed.

### Scenario B -- Assertion failure with automatic cleanup

Change one assertion to expect 4 parameters instead of 3:

```hcl
assert {
  condition     = output.parameter_count == 4
  error_message = "Should create exactly 4 parameters"
}
```

```bash
terraform test
```

```
modules/ssm-config/integration.tftest.hcl... in progress
  run "create_parameters"... fail
    Error: Test assertion failed

    Should create exactly 4 parameters

modules/ssm-config/integration.tftest.hcl... tearing down
modules/ssm-config/integration.tftest.hcl... fail

Failure! 0 passed, 1 failed.
```

Even though the assertion failed, Terraform still ran the teardown phase and destroyed the real resources. This is a critical safety feature.

### Scenario C -- Verifying resources are gone after test

After the test completes, check that the parameters no longer exist:

```bash
aws ssm get-parameter --name "/test-kata/db-host" --region us-east-1
```

Expected: `An error occurred (ParameterNotFound)` -- confirming automatic cleanup worked.

## Common Mistakes

### Forgetting that integration tests require real credentials

Unlike plan-mode tests (and mock-provider tests in exercise 47), integration tests make real API calls. Your AWS credentials must have sufficient permissions to create and destroy the resources being tested. Running these tests without credentials produces authentication errors, not test failures.

### Testing in a production account

Integration tests create and destroy real resources. Always run them in a sandbox or dedicated testing account. Accidentally running against production could create naming conflicts or interact with live systems.

## Verify What You Learned

```bash
cd modules/ssm-config && terraform init && terraform test
```

Expected: `Success! 1 passed, 0 failed.`

```bash
cd modules/ssm-config && terraform test -verbose
```

Expected: verbose output showing the apply phase, assertions, and teardown phase.

```bash
aws ssm get-parameter --name "/test-kata/db-host" --region us-east-1 2>&1
```

Expected (after test completes): `ParameterNotFound` error, confirming cleanup.

```bash
cd modules/ssm-config && terraform test 2>&1 | grep "tearing down"
```

Expected: `modules/ssm-config/integration.tftest.hcl... tearing down`

## What's Next

In exercise 47 you will use `mock_provider` to write unit tests that run without any cloud credentials at all, completing the testing pyramid: unit tests (mock), logic tests (plan-mode), and integration tests (apply).

## Reference

- [Tests](https://developer.hashicorp.com/terraform/language/tests)

## Additional Resources

- [Write Terraform Tests](https://developer.hashicorp.com/terraform/tutorials/configuration-language/test) -- official tutorial covering both plan-mode and apply-mode tests with automatic cleanup
- [Terraform CI/CD and Testing on AWS](https://aws.amazon.com/blogs/devops/terraform-ci-cd-and-testing-on-aws-with-the-new-terraform-test-framework/) -- AWS DevOps blog on the native test framework with real integration examples
- [Terraform Testing: A Complete Guide](https://spacelift.io/blog/terraform-test) -- comprehensive guide explaining integration tests with command = apply and API validation
