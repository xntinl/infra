# 87. CloudWatch Alarms and Composite Alarms

<!--
difficulty: intermediate
concepts: [cloudwatch-metric-alarm, composite-alarm, threshold, period, evaluation-periods, datapoints-to-alarm, alarm-states, alarm-actions]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions, CloudWatch metric alarms, and a composite alarm. CloudWatch alarms cost $0.10/alarm/month. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Configure** a CloudWatch metric alarm with threshold, period, evaluation periods, and datapoints-to-alarm parameters
2. **Differentiate** between evaluation periods and datapoints-to-alarm and explain how they control alarm sensitivity
3. **Construct** a composite alarm that combines multiple metric alarms using AND/OR boolean logic
4. **Analyze** how alarm evaluation settings impact false positive rates versus detection latency
5. **Diagnose** alarm flapping caused by overly sensitive evaluation configurations on transient errors

## Why CloudWatch Alarms and Composite Alarms

Individual CloudWatch alarms monitor a single metric against a threshold. When your Lambda error rate spikes above 5%, an alarm fires. When P99 latency exceeds 2 seconds, another alarm fires. But individual alarms create noise -- a single transient error triggers the error alarm even though the system is healthy. A brief latency spike during a cold start fires the latency alarm when no real degradation exists.

Composite alarms solve the noise problem by combining multiple alarms with boolean logic. Instead of paging on-call for every individual alarm, you create a composite alarm that fires only when `ErrorRateAlarm AND LatencyAlarm` are both in ALARM state simultaneously. This means the system is genuinely degraded -- errors are happening AND responses are slow -- not just experiencing a transient blip.

The DVA-C02 exam tests three areas. First, the relationship between `period` (the time window for each data point), `evaluation_periods` (how many consecutive periods to evaluate), and `datapoints_to_alarm` (how many of those periods must breach the threshold). Setting `evaluation_periods=3` with `datapoints_to_alarm=2` means "2 out of the last 3 periods must breach" -- this is called an M-of-N alarm and is the recommended pattern for reducing false positives. Second, the exam tests composite alarm syntax using `ALARM("alarm-name")` expressions. Third, understanding alarm states: OK, ALARM, and INSUFFICIENT_DATA (when no data exists for the metric).

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

	"github.com/aws/aws-lambda-go/lambda"
)

type Request struct {
	SimulateError   bool `json:"simulate_error"`
	SimulateLatency bool `json:"simulate_latency"`
}

type Response struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Duration   string `json:"duration"`
}

func handler(ctx context.Context, event json.RawMessage) (Response, error) {
	var req Request
	json.Unmarshal(event, &req)

	start := time.Now()

	if req.SimulateLatency {
		delay := time.Duration(2000+rand.Intn(1000)) * time.Millisecond
		time.Sleep(delay)
	} else {
		delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(delay)
	}

	duration := time.Since(start)

	if req.SimulateError {
		return Response{}, fmt.Errorf("simulated error after %s", duration)
	}

	return Response{
		StatusCode: 200,
		Message:    fmt.Sprintf("Success from %s", os.Getenv("AWS_LAMBDA_FUNCTION_NAME")),
		Duration:   duration.String(),
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
  default     = "cw-alarms-demo"
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

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `monitoring.tf`

```hcl
# =======================================================
# TODO 1 -- Error Rate Metric Alarm
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_metric_alarm named "cw-alarms-demo-error-rate"
#   - Monitor the Lambda "Errors" metric (namespace "AWS/Lambda")
#   - Set comparison_operator to "GreaterThanThreshold"
#   - Set threshold to 0 (fire when any errors occur)
#   - Set period to 60 (1-minute data points)
#   - Set evaluation_periods to 3
#   - Set datapoints_to_alarm to 2 (M-of-N: 2 out of 3 periods)
#   - Set statistic to "Sum"
#   - Add dimensions: FunctionName = lambda function name
#   - Set treat_missing_data to "notBreaching"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm


# =======================================================
# TODO 2 -- Latency Metric Alarm
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_metric_alarm named "cw-alarms-demo-latency"
#   - Monitor the Lambda "Duration" metric (namespace "AWS/Lambda")
#   - Set comparison_operator to "GreaterThanThreshold"
#   - Set threshold to 2000 (2 seconds in milliseconds)
#   - Set period to 60 (1-minute data points)
#   - Set evaluation_periods to 3
#   - Set datapoints_to_alarm to 2
#   - Set statistic to "p99" using extended_statistic instead of statistic
#   - Add dimensions: FunctionName = lambda function name
#   - Set treat_missing_data to "notBreaching"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm
# Hint: Use extended_statistic = "p99" instead of statistic for percentiles


# =======================================================
# TODO 3 -- Composite Alarm (AND logic)
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_composite_alarm named "cw-alarms-demo-composite"
#   - Use alarm_rule to combine the error rate and latency alarms
#     with AND logic: both must be in ALARM state
#   - Syntax: ALARM("alarm-name-1") AND ALARM("alarm-name-2")
#   - This fires only when the system has BOTH errors AND high latency
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_composite_alarm
# Hint: The alarm_rule uses alarm names (not ARNs) in ALARM() expressions
```

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.this.function_name
}

output "error_alarm_name" {
  value = aws_cloudwatch_metric_alarm.error_rate.alarm_name
}

output "latency_alarm_name" {
  value = aws_cloudwatch_metric_alarm.latency.alarm_name
}

output "composite_alarm_name" {
  value = aws_cloudwatch_composite_alarm.this.alarm_name
}
```

## Spot the Bug

A developer configures a CloudWatch alarm to monitor Lambda errors. They set `evaluation_periods=1` with `period=60`. The alarm flaps between OK and ALARM every minute during normal operation because transient errors (cold start timeouts, occasional network blips) trigger the alarm, then it recovers, then another transient error triggers it again.

```hcl
resource "aws_cloudwatch_metric_alarm" "error_rate" {
  alarm_name          = "lambda-error-alarm"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1          # <-- Only 1 period evaluated
  period              = 60         # <-- 60-second window
  threshold           = 0
  statistic           = "Sum"
  namespace           = "AWS/Lambda"
  metric_name         = "Errors"

  dimensions = {
    FunctionName = aws_lambda_function.this.function_name
  }

  # datapoints_to_alarm defaults to evaluation_periods (1)
  # So a SINGLE 60-second window with any error triggers ALARM
}
```

<details>
<summary>Explain the bug</summary>

With `evaluation_periods=1` and `period=60`, the alarm evaluates a single 60-second window. Any error in that window triggers the alarm. When the next minute has no errors, the alarm returns to OK. When the next transient error occurs, it goes back to ALARM. This constant state change is called **flapping** and generates excessive notifications.

The root cause is that the alarm is too sensitive for a metric that naturally has occasional transient errors. A single cold start timeout or a momentary network issue should not page the on-call engineer.

**Fix -- use M-of-N evaluation to require sustained errors:**

```hcl
resource "aws_cloudwatch_metric_alarm" "error_rate" {
  alarm_name          = "lambda-error-alarm"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 5          # Evaluate the last 5 periods
  datapoints_to_alarm = 3          # 3 of 5 must breach (M-of-N)
  period              = 60         # 60-second windows
  threshold           = 0
  statistic           = "Sum"
  namespace           = "AWS/Lambda"
  metric_name         = "Errors"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.this.function_name
  }
}
```

With `evaluation_periods=5` and `datapoints_to_alarm=3`, the alarm requires errors in 3 out of the last 5 minutes. A single transient error in one minute does not trigger the alarm. Only a sustained pattern of errors (3+ minutes with errors in a 5-minute window) causes the alarm to fire. This eliminates flapping while still detecting genuine degradation.

Additionally, `treat_missing_data = "notBreaching"` prevents the alarm from entering INSUFFICIENT_DATA when the function has no invocations during a period.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Invoke the function normally (no errors)

```bash
FUNC=$(terraform output -raw function_name)

for i in $(seq 1 5); do
  aws lambda invoke --function-name "$FUNC" --payload '{}' /dev/stdout 2>/dev/null
  echo ""
done
```

### Step 3 -- Verify alarms are in OK state

```bash
aws cloudwatch describe-alarms \
  --alarm-names "cw-alarms-demo-error-rate" "cw-alarms-demo-latency" \
  --query "MetricAlarms[].{Name:AlarmName,State:StateValue}" \
  --output table
```

Expected: Both alarms in `OK` or `INSUFFICIENT_DATA` state.

### Step 4 -- Generate errors and check alarm transition

```bash
for i in $(seq 1 20); do
  aws lambda invoke --function-name "$FUNC" \
    --payload '{"simulate_error": true}' /dev/stdout 2>/dev/null
  sleep 3
done
```

Wait 3-5 minutes for alarm evaluation, then check:

```bash
aws cloudwatch describe-alarms \
  --alarm-names "cw-alarms-demo-error-rate" \
  --query "MetricAlarms[0].StateValue" --output text
```

Expected: `ALARM`

### Step 5 -- Verify composite alarm state

```bash
aws cloudwatch describe-alarms \
  --alarm-names "cw-alarms-demo-composite" \
  --alarm-types "CompositeAlarm" \
  --query "CompositeAlarms[0].StateValue" --output text
```

Expected: `OK` (because the latency alarm is not in ALARM state -- only errors, not high latency).

### Step 6 -- Verify no infrastructure drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>monitoring.tf -- TODO 1 -- Error Rate Metric Alarm</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "error_rate" {
  alarm_name          = "cw-alarms-demo-error-rate"
  alarm_description   = "Lambda error rate alarm - fires when errors occur in 2 of 3 periods"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 60
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.this.function_name
  }
}
```

</details>

<details>
<summary>monitoring.tf -- TODO 2 -- Latency Metric Alarm</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "latency" {
  alarm_name          = "cw-alarms-demo-latency"
  alarm_description   = "Lambda P99 latency alarm - fires when P99 exceeds 2 seconds"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 60
  extended_statistic  = "p99"
  threshold           = 2000
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.this.function_name
  }
}
```

</details>

<details>
<summary>monitoring.tf -- TODO 3 -- Composite Alarm</summary>

```hcl
resource "aws_cloudwatch_composite_alarm" "this" {
  alarm_name = "cw-alarms-demo-composite"
  alarm_description = "Fires when BOTH error rate AND latency alarms are in ALARM state"

  alarm_rule = "ALARM(\"cw-alarms-demo-error-rate\") AND ALARM(\"cw-alarms-demo-latency\")"

  depends_on = [
    aws_cloudwatch_metric_alarm.error_rate,
    aws_cloudwatch_metric_alarm.latency,
  ]
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

You configured metric alarms with M-of-N evaluation and combined them into a composite alarm. In the next exercise, you will use **CloudWatch Embedded Metric Format (EMF)** to publish custom metrics directly from Lambda log output -- zero API calls, zero cost beyond standard CloudWatch pricing.

## Summary

- **Metric alarms** evaluate a single CloudWatch metric against a threshold over time
- **Period** defines the length of each evaluation window (e.g., 60 seconds)
- **Evaluation periods** is how many consecutive periods are evaluated; **datapoints to alarm** is how many must breach the threshold (M-of-N pattern)
- M-of-N alarms (e.g., 2 of 3 periods) reduce false positives from transient errors while maintaining detection sensitivity
- **Composite alarms** combine multiple metric alarms using `ALARM("name")` expressions with AND/OR/NOT boolean logic
- `treat_missing_data = "notBreaching"` prevents alarms from entering INSUFFICIENT_DATA when no invocations occur
- Use `extended_statistic = "p99"` for percentile-based alarms instead of the `statistic` parameter
- Alarm states: **OK** (within threshold), **ALARM** (threshold breached), **INSUFFICIENT_DATA** (not enough data to evaluate)

## Reference

- [CloudWatch Metric Alarms](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/AlarmThatSendsEmail.html)
- [Composite Alarms](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Create_Composite_Alarm.html)
- [Terraform aws_cloudwatch_metric_alarm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm)
- [Terraform aws_cloudwatch_composite_alarm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_composite_alarm)

## Additional Resources

- [Evaluating an Alarm](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/AlarmThatSendsEmail.html#alarm-evaluation) -- how CloudWatch evaluates M-of-N datapoints
- [Using Percentile Statistics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_concepts.html#Percentiles) -- extended statistics and percentile alarms
- [Alarm Actions](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/AlarmThatSendsEmail.html#alarms-and-actions) -- SNS, Auto Scaling, EC2, and Lambda actions
- [CloudWatch Alarm Pricing](https://aws.amazon.com/cloudwatch/pricing/) -- $0.10/alarm/month for standard resolution, $0.30 for high resolution
