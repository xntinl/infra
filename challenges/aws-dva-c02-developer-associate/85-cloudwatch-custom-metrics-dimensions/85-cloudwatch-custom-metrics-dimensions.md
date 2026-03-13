# 85. CloudWatch Custom Metrics and Dimensions

<!--
difficulty: basic
concepts: [cloudwatch-custom-metrics, metric-dimensions, putmetricdata-api, metric-namespaces, metric-resolution, metric-math, embedded-metric-format]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function that publishes custom CloudWatch metrics. CloudWatch custom metrics cost $0.30 per metric per month (first 10 metrics free). Lambda costs are negligible for testing. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Publish** custom CloudWatch metrics from a Go Lambda function using the PutMetricData API with namespaces, dimensions, and units
- **Explain** the difference between standard resolution (60 seconds) and high resolution (1 second) custom metrics
- **Construct** metric dimensions to slice data by Region, OrderType, or any custom attribute for granular monitoring
- **Query** custom metrics using the AWS CLI and CloudWatch console, including dimension filtering
- **Configure** the IAM permissions required for a Lambda function to publish CloudWatch metrics

## Why CloudWatch Custom Metrics

AWS services publish built-in metrics (Lambda invocations, DynamoDB consumed capacity, API Gateway latency), but your application has business metrics that AWS cannot measure: order counts, revenue, queue depth, error rates by customer segment. Custom metrics bridge this gap.

The PutMetricData API publishes metrics to a **namespace** with optional **dimensions** (key-value pairs). A metric with `{Region=us-east-1}` is a different time series from `{Region=eu-west-1}`.

The DVA-C02 exam tests: PutMetricData supports up to 1,000 data points per call and 30 dimensions per metric. Standard resolution stores at 1-minute granularity; high-resolution (`StorageResolution=1`) stores at 1-second but costs more. CloudWatch does not aggregate across dimensions automatically -- publish without dimensions for totals.

## Step 1 -- Create the Lambda Function

### `main.go`

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type OrderEvent struct {
	OrderID   string  `json:"order_id"`
	Region    string  `json:"region"`
	OrderType string  `json:"order_type"`
	Amount    float64 `json:"amount"`
}

func handler(ctx context.Context, event OrderEvent) (map[string]interface{}, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	cwClient := cloudwatch.NewFromConfig(cfg)

	processingMs := float64(50 + rand.Intn(200))
	now := time.Now()

	_, err = cwClient.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: strPtr("MyApp/Orders"),
		MetricData: []types.MetricDatum{
			{
				MetricName: strPtr("OrderCount"),
				Timestamp:  &now,
				Value:      float64Ptr(1),
				Unit:       types.StandardUnitCount,
				Dimensions: []types.Dimension{
					{Name: strPtr("Region"), Value: strPtr(event.Region)},
					{Name: strPtr("OrderType"), Value: strPtr(event.OrderType)},
				},
			},
			{
				MetricName: strPtr("OrderAmount"),
				Timestamp:  &now,
				Value:      &event.Amount,
				Unit:       types.StandardUnitNone,
				Dimensions: []types.Dimension{
					{Name: strPtr("Region"), Value: strPtr(event.Region)},
				},
			},
			{
				MetricName:        strPtr("ProcessingTime"),
				Timestamp:         &now,
				Value:             &processingMs,
				Unit:              types.StandardUnitMilliseconds,
				StorageResolution: int32Ptr(1), // High resolution
				Dimensions: []types.Dimension{
					{Name: strPtr("Region"), Value: strPtr(event.Region)},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("put metric data: %w", err)
	}

	fmt.Printf("Published metrics: OrderCount=1 Amount=%.2f Region=%s\n", event.Amount, event.Region)

	return map[string]interface{}{
		"order_id": event.OrderID, "status": "processed", "processing_ms": processingMs,
	}, nil
}

func strPtr(s string) *string       { return &s }
func float64Ptr(f float64) *float64 { return &f }
func int32Ptr(i int32) *int32       { return &i }

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Configuration

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
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
  default     = "cw-custom-metrics"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
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
data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# CloudWatch PutMetricData permission
data "aws_iam_policy_document" "cloudwatch_metrics" {
  statement {
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["MyApp/Orders"]
    }
  }
}

resource "aws_iam_role_policy" "cloudwatch_metrics" {
  name   = "cloudwatch-put-metrics"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
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
  timeout          = 15

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_metric_alarm" "high_order_amount" {
  alarm_name          = "${var.project_name}-high-order-amount"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "OrderAmount"
  namespace           = "MyApp/Orders"
  period              = 60
  statistic           = "Sum"
  threshold           = 500
  alarm_description   = "Alarm when total order amount exceeds $500 in 1 minute"

  dimensions = {
    Region = "us-east-1"
  }
}
```

### `outputs.tf`

```hcl
output "function_name" {
  description = "Lambda function name"
  value       = aws_lambda_function.this.function_name
}

output "alarm_name" {
  description = "CloudWatch alarm name"
  value       = aws_cloudwatch_metric_alarm.high_order_amount.alarm_name
}
```

## Step 3 -- Build and Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

## Step 4 -- Invoke the Lambda and Publish Metrics

```bash
FUNCTION_NAME=$(terraform output -raw function_name)

aws lambda invoke --function-name "$FUNCTION_NAME" \
  --payload '{"order_id":"ord-001","region":"us-east-1","order_type":"standard","amount":49.99}' \
  /dev/stdout 2>/dev/null | jq .

aws lambda invoke --function-name "$FUNCTION_NAME" \
  --payload '{"order_id":"ord-002","region":"us-east-1","order_type":"express","amount":129.50}' \
  /dev/stdout 2>/dev/null | jq .

aws lambda invoke --function-name "$FUNCTION_NAME" \
  --payload '{"order_id":"ord-003","region":"eu-west-1","order_type":"standard","amount":75.00}' \
  /dev/stdout 2>/dev/null | jq .
```

## Step 5 -- Query Custom Metrics

Wait 1-2 minutes for metrics to appear, then query:

```bash
aws cloudwatch list-metrics --namespace "MyApp/Orders" \
  --query "Metrics[*].{Name:MetricName,Dimensions:Dimensions[*].Name}" --output table

aws cloudwatch get-metric-statistics \
  --namespace "MyApp/Orders" --metric-name "OrderAmount" \
  --dimensions Name=Region,Value=us-east-1 \
  --start-time "$(date -u -v-10M '+%Y-%m-%dT%H:%M:%SZ')" \
  --end-time "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  --period 60 --statistics Sum --output json | jq '.Datapoints[0].Sum'
```

## Common Mistakes

### 1. Missing cloudwatch:PutMetricData permission

Without `cloudwatch:PutMetricData` in the IAM role, PutMetricData fails with AccessDenied. The Lambda may still return success if the error is unhandled, but no metrics appear.

### 2. Expecting automatic aggregation across dimensions

Publishing `OrderCount` with `{Region=us-east-1}` and `{Region=eu-west-1}` creates two separate time series. To get a total, publish an additional data point without the Region dimension.

### 3. Using wrong metric resolution

Standard resolution stores at 1-minute granularity. High resolution (`StorageResolution=1`) stores at 1-second but costs more.

## Verify What You Learned

```bash
# Verify metrics exist in the namespace
aws cloudwatch list-metrics --namespace "MyApp/Orders" \
  --query "length(Metrics)" --output text
```

Expected: at least 3 (OrderCount, OrderAmount, ProcessingTime).

```bash
aws cloudwatch describe-alarms --alarm-names $(terraform output -raw alarm_name) \
  --query "MetricAlarms[0].{Name:AlarmName,Metric:MetricName,Namespace:Namespace}" --output table
```

Expected: alarm targeting `OrderAmount` in `MyApp/Orders` namespace.

```bash
terraform plan
```

Expected: `No changes.`

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list  # Expected: empty
```

## What's Next

You published custom CloudWatch metrics with dimensions from a Lambda function. Next, you will explore **CloudWatch Logs Insights** -- querying structured JSON logs to calculate error rates and latency percentiles.

## Summary

- **Custom metrics** are published via PutMetricData to a namespace (e.g., `MyApp/Orders`) with optional dimensions
- **Dimensions** are key-value pairs creating separate time series -- CloudWatch does **not aggregate** across dimensions automatically
- **Standard resolution** stores at 1-minute granularity; **high resolution** (`StorageResolution=1`) at 1-second
- PutMetricData supports up to **1,000 data points** per call and **30 dimensions** per metric
- Lambda requires `cloudwatch:PutMetricData` IAM permission -- use a namespace condition to restrict access
- Metrics appear within **1-2 minutes**; custom metric pricing: $0.30/metric/month (first 10 free)

## Reference

- [CloudWatch Custom Metrics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/publishingMetrics.html)
- [PutMetricData API](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_PutMetricData.html)
- [Terraform aws_cloudwatch_metric_alarm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm)

## Additional Resources

- [CloudWatch Embedded Metric Format](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format.html) -- publish metrics via structured log lines
- [CloudWatch Metric Math](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/using-metric-math.html) -- compute derived metrics using expressions
