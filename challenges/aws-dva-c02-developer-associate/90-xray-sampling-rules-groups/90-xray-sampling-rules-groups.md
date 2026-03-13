# 90. X-Ray Sampling Rules and Groups

<!--
difficulty: intermediate
concepts: [xray-sampling-rules, reservoir, fixed-rate, xray-groups, trace-filter-expressions, custom-sampling, sampling-priority]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function with X-Ray active tracing, custom X-Ray sampling rules, and trace groups. X-Ray has a free tier of 100,000 traces/month. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Configure** custom X-Ray sampling rules with reservoir size and fixed rate to control trace collection volume
2. **Differentiate** between the reservoir (guaranteed minimum traces per second) and fixed_rate (percentage of additional requests traced beyond the reservoir)
3. **Construct** X-Ray groups that filter traces by expressions to organize traces by error status, latency, or annotation values
4. **Analyze** how sampling rule priority determines which rule applies when multiple rules match a request
5. **Diagnose** a configuration where `reservoir=0` and `fixed_rate=0` results in zero traces being captured

## Why X-Ray Sampling Rules and Groups

By default, X-Ray traces the first request per second and 5% of additional requests. For a function receiving 1,000 requests per second, that means approximately 51 traces per second (1 from the reservoir + 50 from the 5% fixed rate). This default works for many workloads, but two scenarios require custom sampling rules.

First, you may need higher sampling rates for specific paths or error conditions. If your `/payments` endpoint processes 10 requests per second, the default 5% rate captures only 0.5 traces per second -- not enough to debug intermittent payment failures. A custom sampling rule with `reservoir=10` and `fixed_rate=1.0` traces every payment request.

Second, you may need lower sampling rates for high-volume, low-value endpoints. Health check endpoints receiving 10,000 requests per second generate enormous trace volumes at the default 5% rate. A custom rule with `reservoir=0` and `fixed_rate=0.001` reduces health check traces to 10 per second.

X-Ray groups complement sampling rules by organizing collected traces. A group with the filter expression `fault = true` collects all error traces into a single view. A group with `responsetime > 2` isolates slow requests. Groups can trigger CloudWatch alarms, enabling alerts on error trace rates without custom metrics.

The DVA-C02 exam tests sampling rule priority (lower number = higher priority), the reservoir vs fixed_rate distinction, and filter expression syntax.

## Building Blocks

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type Response struct {
	StatusCode int    `json:"statusCode"`
	Duration   string `json:"duration"`
	Path       string `json:"path"`
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	start := time.Now()
	path := event.Path
	if path == "" {
		path = "/default"
	}

	// Simulate variable latency based on path
	switch path {
	case "/payments":
		time.Sleep(time.Duration(200+rand.Intn(300)) * time.Millisecond)
	case "/health":
		time.Sleep(time.Duration(5+rand.Intn(10)) * time.Millisecond)
	default:
		time.Sleep(time.Duration(50+rand.Intn(150)) * time.Millisecond)
	}

	duration := time.Since(start)

	// Simulate errors on /payments (10% error rate)
	if path == "/payments" && rand.Float64() < 0.1 {
		body, _ := json.Marshal(map[string]string{
			"error":    "payment processing failed",
			"path":     path,
			"duration": duration.String(),
			"function": os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		})
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       string(body),
		}, nil
	}

	body, _ := json.Marshal(Response{
		StatusCode: 200,
		Duration:   duration.String(),
		Path:       path,
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Create the following files in your exercise directory:

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

provider "aws" {
  region = var.region
}
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "xray-sampling-demo"
}
```

### `build.tf`

```hcl
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
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "xray" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}
```

### `lambda.tf`

```hcl
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

  tracing_config {
    mode = "Active"
  }

  depends_on = [
    aws_iam_role_policy_attachment.basic,
    aws_iam_role_policy_attachment.xray,
    aws_cloudwatch_log_group.this,
  ]
}
```

### `monitoring.tf`

```hcl
# =======================================================
# TODO 1 -- Custom Sampling Rule for Payments (high rate)
# =======================================================
# Requirements:
#   - Create an aws_xray_sampling_rule named "payments-high-sampling"
#   - Set priority to 100 (lower number = higher priority)
#   - Set reservoir_size to 10 (guaranteed 10 traces/sec)
#   - Set fixed_rate to 1.0 (100% of remaining requests)
#   - Set url_path to "/payments"
#   - Set service_name to "*"
#   - Set service_type to "*"
#   - Set host to "*"
#   - Set http_method to "*"
#   - Set resource_arn to "*"
#   - Set version to 1
#
# This rule traces ALL payment requests because:
#   - reservoir=10 guarantees 10 traces/sec
#   - fixed_rate=1.0 traces 100% of any additional requests
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/xray_sampling_rule


# =======================================================
# TODO 2 -- Custom Sampling Rule for Health Checks (low rate)
# =======================================================
# Requirements:
#   - Create an aws_xray_sampling_rule named "health-check-low-sampling"
#   - Set priority to 200
#   - Set reservoir_size to 1 (just 1 trace/sec guaranteed)
#   - Set fixed_rate to 0.01 (1% of remaining requests)
#   - Set url_path to "/health"
#   - Set all other match fields to "*"
#   - Set version to 1
#
# This rule drastically reduces health check trace volume
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/xray_sampling_rule


# =======================================================
# TODO 3 -- X-Ray Group for Error Traces
# =======================================================
# Requirements:
#   - Create an aws_xray_group named "errors"
#   - Set filter_expression to 'fault = true'
#   - This groups all traces that contain a fault (5xx error)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/xray_group


# =======================================================
# TODO 4 -- X-Ray Group for Slow Requests
# =======================================================
# Requirements:
#   - Create an aws_xray_group named "slow-requests"
#   - Set filter_expression to 'responsetime > 2'
#   - This groups all traces with response time exceeding 2 seconds
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/xray_group
```

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.this.function_name
}
```

## Spot the Bug

A developer creates a custom sampling rule to trace API requests. After deploying, they invoke the Lambda function 1,000 times but find zero traces in X-Ray.

```hcl
resource "aws_xray_sampling_rule" "api_tracing" {
  rule_name      = "api-tracing"
  priority       = 1000
  reservoir_size = 0     # <-- No guaranteed traces
  fixed_rate     = 0     # <-- 0% of remaining requests
  url_path       = "*"
  service_name   = "*"
  service_type   = "*"
  host           = "*"
  http_method    = "*"
  resource_arn   = "*"
  version        = 1
}
```

<details>
<summary>Explain the bug</summary>

Both `reservoir_size` and `fixed_rate` are set to 0. The reservoir guarantees zero traces per second. The fixed rate of 0 (0%) means zero percent of remaining requests are traced. Combined: **no requests are ever sampled**.

The sampling algorithm works in two phases:
1. **Reservoir**: Each second, the first N requests (reservoir_size) are guaranteed to be traced
2. **Fixed rate**: After the reservoir is exhausted, the remaining requests are traced at the specified percentage

With `reservoir_size=0` and `fixed_rate=0`, both phases produce zero traces.

Additionally, this rule has `priority=1000`, which is lower priority than the default rule (priority 10000). However, the custom rule matches first because it has a lower priority number -- but since it samples nothing, it effectively suppresses all tracing for matching requests. The default rule never gets evaluated because the custom rule already matched.

**Fix -- set meaningful sampling values:**

```hcl
resource "aws_xray_sampling_rule" "api_tracing" {
  rule_name      = "api-tracing"
  priority       = 1000
  reservoir_size = 1      # At least 1 trace/sec guaranteed
  fixed_rate     = 0.05   # 5% of remaining requests
  url_path       = "*"
  service_name   = "*"
  service_type   = "*"
  host           = "*"
  http_method    = "*"
  resource_arn   = "*"
  version        = 1
}
```

Or remove the custom rule entirely and rely on the default sampling rule (1 trace/sec reservoir, 5% fixed rate).

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify sampling rules exist

```bash
aws xray get-sampling-rules \
  --query "SamplingRuleRecords[].SamplingRule.{Name:RuleName,Priority:Priority,Reservoir:ReservoirSize,Rate:FixedRate,Path:URLPath}" \
  --output table
```

Expected: your custom rules plus the Default rule.

### Step 3 -- Invoke the function and generate traces

```bash
FUNC=$(terraform output -raw function_name)

# Payment requests (should be fully traced)
for i in $(seq 1 10); do
  aws lambda invoke --function-name "$FUNC" \
    --payload '{"path": "/payments", "httpMethod": "POST"}' /dev/stdout 2>/dev/null
  echo ""
done

# Health checks (should be sparsely traced)
for i in $(seq 1 20); do
  aws lambda invoke --function-name "$FUNC" \
    --payload '{"path": "/health", "httpMethod": "GET"}' /dev/stdout 2>/dev/null
  echo ""
done
```

### Step 4 -- Check X-Ray groups

```bash
aws xray get-groups \
  --query "Groups[].{Name:GroupName,Filter:FilterExpression}" \
  --output table
```

Expected: `errors` and `slow-requests` groups with their filter expressions.

### Step 5 -- Query traces in error group

```bash
aws xray get-trace-summaries \
  --start-time $(date -u -v-30M +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'fault = true' \
  --query "TraceSummaries | length(@)"
```

Expected: a number (may be 0 if no errors were simulated during testing).

### Step 6 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>monitoring.tf -- TODO 1 -- Custom Sampling Rule for Payments</summary>

```hcl
resource "aws_xray_sampling_rule" "payments" {
  rule_name      = "payments-high-sampling"
  priority       = 100
  reservoir_size = 10
  fixed_rate     = 1.0
  url_path       = "/payments"
  service_name   = "*"
  service_type   = "*"
  host           = "*"
  http_method    = "*"
  resource_arn   = "*"
  version        = 1
}
```

</details>

<details>
<summary>monitoring.tf -- TODO 2 -- Custom Sampling Rule for Health Checks</summary>

```hcl
resource "aws_xray_sampling_rule" "health_check" {
  rule_name      = "health-check-low-sampling"
  priority       = 200
  reservoir_size = 1
  fixed_rate     = 0.01
  url_path       = "/health"
  service_name   = "*"
  service_type   = "*"
  host           = "*"
  http_method    = "*"
  resource_arn   = "*"
  version        = 1
}
```

</details>

<details>
<summary>monitoring.tf -- TODO 3 -- X-Ray Group for Error Traces</summary>

```hcl
resource "aws_xray_group" "errors" {
  group_name        = "errors"
  filter_expression = "fault = true"
}
```

</details>

<details>
<summary>monitoring.tf -- TODO 4 -- X-Ray Group for Slow Requests</summary>

```hcl
resource "aws_xray_group" "slow_requests" {
  group_name        = "slow-requests"
  filter_expression = "responsetime > 2"
}
```

</details>

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured custom sampling rules to control trace volume and organized traces into groups. In the next exercise, you will create **CloudWatch Synthetics canaries** that proactively test your API endpoints on a schedule and alert you before users notice outages.

## Summary

- **Sampling rules** control which requests X-Ray traces: `reservoir_size` guarantees N traces/sec, `fixed_rate` sets the percentage of additional requests
- **Priority** determines rule evaluation order: lower number = higher priority; first matching rule wins
- The **default rule** (priority 10000) traces 1 request/sec reservoir + 5% fixed rate
- Setting both `reservoir_size=0` and `fixed_rate=0` suppresses all tracing for matching requests
- **X-Ray groups** organize traces by filter expressions like `fault = true` or `responsetime > 2`
- Groups can generate CloudWatch metrics and trigger alarms on error rates or latency thresholds
- Filter expressions support: `fault`, `error`, `responsetime`, `http.status`, `annotation.key`, and boolean operators
- Sampling rules match on `service_name`, `service_type`, `url_path`, `host`, `http_method`, and `resource_arn`

## Reference

- [X-Ray Sampling Rules](https://docs.aws.amazon.com/xray/latest/devguide/xray-console-sampling.html)
- [X-Ray Groups](https://docs.aws.amazon.com/xray/latest/devguide/xray-console-groups.html)
- [Terraform aws_xray_sampling_rule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/xray_sampling_rule)
- [Terraform aws_xray_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/xray_group)

## Additional Resources

- [X-Ray Sampling Algorithm](https://docs.aws.amazon.com/xray/latest/devguide/xray-sdk-go-configuration.html#xray-sdk-go-configuration-sampling) -- detailed explanation of reservoir borrowing and rate limiting
- [Filter Expression Syntax](https://docs.aws.amazon.com/xray/latest/devguide/xray-console-filters.html) -- complete reference for trace filter expressions
- [X-Ray Pricing](https://aws.amazon.com/xray/pricing/) -- $5.00 per million traces recorded, $0.50 per million traces retrieved
- [X-Ray Limits](https://docs.aws.amazon.com/general/latest/gr/xray.html) -- maximum 25 sampling rules, 25 groups per account
