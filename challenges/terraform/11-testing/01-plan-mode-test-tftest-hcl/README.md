# 45. Plan-Mode Test with .tftest.hcl

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 44 (Pin Module Versions from Git)

## Learning Objectives

After completing this exercise, you will be able to:

- Write native Terraform tests using `.tftest.hcl` files
- Define multiple `run` blocks with `command = plan` to validate logic without creating real infrastructure
- Use `assert` blocks with conditions and error messages to verify outputs
- Inject different variable sets per run block to test multiple scenarios

## Why Plan-Mode Testing

Before Terraform introduced native testing, teams relied on external tools like Terratest (Go) or kitchen-terraform (Ruby) to validate their configurations. These tools added language dependencies, complex setup, and slow feedback loops.

Native `.tftest.hcl` files run with a simple `terraform test` command and require nothing beyond Terraform itself. When you set `command = plan`, Terraform calculates the plan and evaluates your assertions against the planned values -- without creating any real infrastructure. This means tests run in seconds, cost nothing, and can execute in CI environments without cloud credentials (when combined with mock providers, which you will learn in exercise 47).

Plan-mode tests are ideal for validating naming conventions, tag logic, computed locals, and output transformations -- anything that depends on your Terraform logic rather than cloud API behavior.

## Step 1 -- Create a Naming Module

Create the directory `modules/naming/` with one file:

```hcl
# modules/naming/main.tf

variable "project"     { type = string }
variable "environment" { type = string }
variable "component"   { type = string }

locals {
  name = "${var.project}-${var.environment}-${var.component}"
  tags = {
    Name        = local.name
    Project     = var.project
    Environment = var.environment
  }
}

output "name" { value = local.name }
output "tags" { value = local.tags }
```

This module produces a standardized name and a set of tags from three inputs. No cloud resources are involved -- pure logic.

## Step 2 -- Write Plan-Mode Tests

Create the test file in the same directory:

```hcl
# modules/naming/naming.tftest.hcl

run "basic_naming" {
  command = plan

  variables {
    project     = "myapp"
    environment = "dev"
    component   = "api"
  }

  assert {
    condition     = output.name == "myapp-dev-api"
    error_message = "Name should follow project-environment-component pattern"
  }

  assert {
    condition     = output.tags["Project"] == "myapp"
    error_message = "Tags should include project"
  }

  assert {
    condition     = output.tags["Environment"] == "dev"
    error_message = "Tags should include environment"
  }
}

run "production_naming" {
  command = plan

  variables {
    project     = "platform"
    environment = "prod"
    component   = "worker"
  }

  assert {
    condition     = output.name == "platform-prod-worker"
    error_message = "Production naming should work correctly"
  }
}
```

Each `run` block is an independent test case with its own variables and assertions.

## Step 3 -- Execute the Tests

```bash
cd modules/naming
terraform init
terraform test
```

### Scenario A -- All tests pass (expected)

```
modules/naming/naming.tftest.hcl... in progress
  run "basic_naming"... pass
  run "production_naming"... pass
modules/naming/naming.tftest.hcl... tearing down
modules/naming/naming.tftest.hcl... pass

Success! 2 passed, 0 failed.
```

### Scenario B -- A failing assertion

Change one assertion to expect the wrong value:

```hcl
assert {
  condition     = output.name == "wrong-value"
  error_message = "Name should follow project-environment-component pattern"
}
```

```bash
terraform test
```

```
modules/naming/naming.tftest.hcl... in progress
  run "basic_naming"... fail
    Error: Test assertion failed

    Name should follow project-environment-component pattern

modules/naming/naming.tftest.hcl... tearing down
modules/naming/naming.tftest.hcl... fail

Failure! 0 passed, 1 failed.
```

The `error_message` tells you exactly which assertion failed and why.

### Scenario C -- Adding a third test case

You can add as many `run` blocks as needed. For example, test an edge case with an empty component:

```hcl
run "empty_component" {
  command = plan

  variables {
    project     = "myapp"
    environment = "dev"
    component   = ""
  }

  assert {
    condition     = output.name == "myapp-dev-"
    error_message = "Empty component should still produce a valid name"
  }
}
```

This highlights a potential problem with the naming logic that you might want to address with input validation.

## Common Mistakes

### Placing test files outside the module directory

By default, `terraform test` looks for `.tftest.hcl` files in the current directory and a `tests/` subdirectory. If you place the test file outside the module directory, you need to configure the `module` block inside the `run` block to point to the correct path. The simplest approach is to keep test files alongside the module code.

### Using command = apply when plan suffices

Plan-mode tests are faster, cheaper, and do not require cloud credentials. Reserve `command = apply` for integration tests that need to verify real API behavior (covered in exercise 46).

## Verify What You Learned

```bash
cd modules/naming && terraform init && terraform test
```

Expected: `Success! 2 passed, 0 failed.`

```bash
cd modules/naming && terraform test -verbose
```

Expected: detailed output showing each run block, the planned values, and assertion results.

```bash
cd modules/naming && terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
cd modules/naming && terraform test 2>&1 | grep -c "pass"
```

Expected: `3` (two individual passes plus one file-level pass).

## What's Next

In exercise 46 you will write integration tests that use `command = apply` to create real infrastructure, validate it with assertions, and then automatically destroy everything when the test finishes.

## Reference

- [Tests](https://developer.hashicorp.com/terraform/language/tests)

## Additional Resources

- [Write Terraform Tests](https://developer.hashicorp.com/terraform/tutorials/configuration-language/test) -- official step-by-step tutorial for writing native tests with .tftest.hcl
- [Terraform Testing: A Complete Guide](https://spacelift.io/blog/terraform-test) -- practical guide covering plan-mode tests, variables in run blocks, and testing patterns
- [Mastering the Terraform Test Block](https://ckdbtech.com/mastering-terraform-test-block/) -- detailed walkthrough with examples of plan-mode tests for logic validation
