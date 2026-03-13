# 20. Lambda Provisioned Concurrency and Cold Starts

<!--
difficulty: intermediate
concepts: [provisioned-concurrency, cold-start, warm-start, lambda-alias, auto-scaling, application-auto-scaling, init-duration]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: design, implement
prerequisites: [exercise-18]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function with provisioned concurrency. Provisioned concurrency incurs charges even when the function is idle (~$0.03/hr for 5 provisioned instances at 256 MB). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| Exercise 18 completed | Understanding of Lambda versions and aliases |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a Lambda deployment with provisioned concurrency on an alias to eliminate cold starts for latency-sensitive workloads
2. **Implement** provisioned concurrency configuration with auto-scaling based on provisioned concurrency utilization
3. **Differentiate** between cold start (INIT phase + INVOKE phase) and warm start (INVOKE phase only) by examining CloudWatch `Init Duration` metrics
4. **Configure** Application Auto Scaling to dynamically adjust provisioned concurrency between a minimum and maximum based on utilization
5. **Justify** when to use provisioned concurrency versus accepting cold starts based on cost-latency tradeoffs

## Why This Matters

Lambda cold starts occur when the service must create a new execution environment for your function. This involves downloading your code, starting the runtime, and executing your initialization code (the `init()` function in Go). For Go functions with the `provided.al2023` runtime, cold starts typically add 100-300 ms. For functions inside a VPC, cold starts historically added 5-10 seconds (though ENI improvements have reduced this to ~1 second since 2019).

Provisioned concurrency pre-creates a pool of initialized execution environments that are always ready to handle requests. There is no INIT phase -- the function starts in the INVOKE phase immediately. This is critical for API-backed Lambda functions where P99 latency matters, or for functions behind Application Load Balancers that have health check timeouts.

The DVA-C02 exam tests several provisioned concurrency details: it must be configured on an alias or version (not `$LATEST`), it counts against your account's concurrency limit, and it incurs charges even when idle. The exam also tests the difference between `reserved concurrency` (a hard cap on total concurrent executions, free) and `provisioned concurrency` (pre-warmed environments, paid).

## Building Blocks

Create the Lambda function code in a file called `main.go`:

### `main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

var initTime time.Time

func init() {
	initTime = time.Now()
	// Simulate a slow initialization (loading config, warming caches)
	time.Sleep(500 * time.Millisecond)
	fmt.Println("INIT phase completed at", initTime.Format(time.RFC3339))
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	invokeTime := time.Now()
	timeSinceInit := invokeTime.Sub(initTime)

	isColdStart := timeSinceInit < 2*time.Second

	return map[string]interface{}{
		"function_name":        os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"memory_size":          os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"),
		"init_time":            initTime.Format(time.RFC3339Nano),
		"invoke_time":          invokeTime.Format(time.RFC3339Nano),
		"time_since_init_ms":   timeSinceInit.Milliseconds(),
		"likely_cold_start":    isColdStart,
		"message":              fmt.Sprintf("Init was %dms ago", timeSinceInit.Milliseconds()),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Create the following Terraform files. Your job is to fill in each `# TODO` block.

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}

provider "aws" { region = var.region }
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "provisioned-concurrency-demo"
}
```

### `build.tf`

```hcl
# -------------------------------------------------------
# Build and package
# -------------------------------------------------------
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/main.go")
  }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = path.module
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `iam.tf`

```hcl
# -------------------------------------------------------
# IAM
# -------------------------------------------------------
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda function
# -------------------------------------------------------
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30
  publish          = true

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# -- Alias for provisioned concurrency --
resource "aws_lambda_alias" "live" {
  name             = "live"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version
}

# =======================================================
# TODO 1 -- Provisioned Concurrency Configuration
# =======================================================
# Requirements:
#   - Create an aws_lambda_provisioned_concurrency_config
#   - Set function_name to the Lambda function name
#   - Set qualifier to the "live" alias name
#   - Set provisioned_concurrent_executions = 5
#   - This pre-warms 5 execution environments on the "live" alias
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_provisioned_concurrency_config
# Note: Provisioned concurrency must target an alias or version, NOT $LATEST


# =======================================================
# TODO 2 -- Application Auto Scaling Target
# =======================================================
# Requirements:
#   - Create an aws_appautoscaling_target for the Lambda alias
#   - Set service_namespace = "lambda"
#   - Set resource_id to "function:<function_name>:<alias_name>"
#   - Set scalable_dimension = "lambda:function:ProvisionedConcurrency"
#   - Set min_capacity = 2
#   - Set max_capacity = 10
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_target


# =======================================================
# TODO 3 -- Auto Scaling Policy (Target Tracking)
# =======================================================
# Requirements:
#   - Create an aws_appautoscaling_policy with
#     policy_type = "TargetTrackingScaling"
#   - Use a target_tracking_scaling_policy_configuration block
#   - Set target_value = 0.7 (scale when 70% of provisioned
#     concurrency is in use)
#   - Use a predefined_metric_specification with
#     predefined_metric_type = "LambdaProvisionedConcurrencyUtilization"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy
```

### `outputs.tf`

```hcl
output "function_name"    { value = aws_lambda_function.this.function_name }
output "alias_name"       { value = aws_lambda_alias.live.name }
output "alias_arn"        { value = aws_lambda_alias.live.arn }
output "function_version" { value = aws_lambda_function.this.version }
```

## Spot the Bug

A developer configures provisioned concurrency on `$LATEST` instead of an alias:

```hcl
resource "aws_lambda_provisioned_concurrency_config" "this" {
  function_name                  = aws_lambda_function.this.function_name
  qualifier                      = "$LATEST"
  provisioned_concurrent_executions = 5
}
```

<details>
<summary>Explain the bug</summary>

Provisioned concurrency cannot be configured on `$LATEST`. It requires a published version number or an alias name. `$LATEST` is mutable -- code and configuration can change at any time -- so Lambda cannot safely pre-initialize environments from it.

The `terraform apply` will fail with `InvalidParameterValueException: Provisioned Concurrency is not supported on $LATEST`.

The fix -- use an alias or version:

```hcl
resource "aws_lambda_provisioned_concurrency_config" "this" {
  function_name                  = aws_lambda_function.this.function_name
  qualifier                      = aws_lambda_alias.live.name  # "live" alias
  provisioned_concurrent_executions = 5
}
```

Also ensure the Lambda function has `publish = true` so that numbered versions exist for the alias to reference.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify provisioned concurrency is active

```bash
aws lambda get-provisioned-concurrency-config \
  --function-name provisioned-concurrency-demo \
  --qualifier live \
  --query "{Status:Status,Requested:RequestedProvisionedConcurrentExecutions,Available:AvailableProvisionedConcurrentExecutions}" \
  --output json
```

Expected: `Status: "READY"`, `Requested: 5`, `Available: 5`

### Step 3 -- Invoke the function and check for cold start

```bash
aws lambda invoke --function-name provisioned-concurrency-demo --qualifier live \
  /dev/stdout 2>/dev/null | jq .
```

Expected: `likely_cold_start: false` and `time_since_init_ms` showing a large value (the environment was pre-warmed).

### Step 4 -- Compare with an unprovisioned invocation

Invoke `$LATEST` (no provisioned concurrency) to see a cold start:

```bash
aws lambda invoke --function-name provisioned-concurrency-demo \
  /dev/stdout 2>/dev/null | jq .
```

Expected: `likely_cold_start: true` and `time_since_init_ms` showing a small value (INIT just happened).

### Step 5 -- Verify auto-scaling configuration

```bash
aws application-autoscaling describe-scalable-targets \
  --service-namespace lambda \
  --query "ScalableTargets[?ResourceId=='function:provisioned-concurrency-demo:live']" \
  --output json
```

Expected: JSON showing `MinCapacity: 2`, `MaxCapacity: 10`.

### Step 6 -- Check auto-scaling policy

```bash
aws application-autoscaling describe-scaling-policies \
  --service-namespace lambda \
  --query "ScalingPolicies[?ResourceId=='function:provisioned-concurrency-demo:live'].{Name:PolicyName,Type:PolicyType,Target:TargetTrackingScalingPolicyConfiguration.TargetValue}" \
  --output table
```

Expected: a target tracking policy with target value `0.7`.

## Solutions

<details>
<summary>TODO 1 -- Provisioned Concurrency Configuration (lambda.tf)</summary>

```hcl
resource "aws_lambda_provisioned_concurrency_config" "this" {
  function_name                  = aws_lambda_function.this.function_name
  qualifier                      = aws_lambda_alias.live.name
  provisioned_concurrent_executions = 5
}
```

</details>

<details>
<summary>TODO 2 -- Application Auto Scaling Target (lambda.tf)</summary>

```hcl
resource "aws_appautoscaling_target" "lambda" {
  service_namespace  = "lambda"
  resource_id        = "function:${aws_lambda_function.this.function_name}:${aws_lambda_alias.live.name}"
  scalable_dimension = "lambda:function:ProvisionedConcurrency"
  min_capacity       = 2
  max_capacity       = 10

  depends_on = [aws_lambda_provisioned_concurrency_config.this]
}
```

</details>

<details>
<summary>TODO 3 -- Auto Scaling Policy (Target Tracking) (lambda.tf)</summary>

```hcl
resource "aws_appautoscaling_policy" "lambda" {
  name               = "provisioned-concurrency-autoscaling"
  service_namespace  = aws_appautoscaling_target.lambda.service_namespace
  resource_id        = aws_appautoscaling_target.lambda.resource_id
  scalable_dimension = aws_appautoscaling_target.lambda.scalable_dimension
  policy_type        = "TargetTrackingScaling"

  target_tracking_scaling_policy_configuration {
    target_value = 0.7

    predefined_metric_specification {
      predefined_metric_type = "LambdaProvisionedConcurrencyUtilization"
    }
  }
}
```

</details>

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify everything is removed:

```bash
aws lambda get-provisioned-concurrency-config \
  --function-name provisioned-concurrency-demo \
  --qualifier live 2>&1 | head -1
```

Expected: `ResourceNotFoundException` or empty output.

## What's Next

In **Exercise 21 -- Lambda VPC Networking and NAT Access**, you will deploy a Lambda function inside a VPC private subnet, configure NAT Gateway for internet access, and learn why Lambda functions in a VPC never receive public IP addresses.

## Summary

You configured provisioned concurrency with auto-scaling to eliminate cold starts:

- **Provisioned concurrency** pre-warms execution environments so functions start in the INVOKE phase (no INIT)
- Must be configured on an **alias or version**, not `$LATEST`
- **Costs money even when idle** -- unlike reserved concurrency which is free
- **Reserved concurrency** is a hard cap on total concurrent executions; **provisioned concurrency** is a pre-warmed pool
- **Application Auto Scaling** dynamically adjusts provisioned concurrency based on utilization metrics
- The `LambdaProvisionedConcurrencyUtilization` metric tracks what percentage of provisioned environments are in use
- Cold starts show an `Init Duration` in CloudWatch Logs; warm starts from provisioned concurrency do not

Key exam concept: provisioned concurrency and reserved concurrency serve different purposes. Reserved concurrency limits total executions (free, acts as a cap). Provisioned concurrency pre-warms environments (paid, eliminates cold starts). You can use both together.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_lambda_provisioned_concurrency_config` | Pre-warms execution environments on alias/version |
| `aws_appautoscaling_target` | Registers Lambda provisioned concurrency as scalable resource |
| `aws_appautoscaling_policy` | Target tracking policy for utilization-based scaling |
| `LambdaProvisionedConcurrencyUtilization` | CloudWatch metric for auto-scaling trigger |

## Additional Resources

- [Lambda Provisioned Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/provisioned-concurrency.html)
- [Managing Provisioned Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/provisioned-concurrency.html#managing-provisioned-concurrency)
- [Application Auto Scaling for Lambda](https://docs.aws.amazon.com/autoscaling/application/userguide/services-that-can-integrate-lambda.html)
- [Lambda Cold Start Optimization](https://docs.aws.amazon.com/lambda/latest/operatorguide/execution-environments.html)
- [Reserved vs Provisioned Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/lambda-concurrency.html)
