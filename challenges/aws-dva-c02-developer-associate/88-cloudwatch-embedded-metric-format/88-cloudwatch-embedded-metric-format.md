# 88. CloudWatch Embedded Metric Format (EMF)

<!--
difficulty: intermediate
concepts: [embedded-metric-format, custom-metrics, structured-logging, cloudwatch-metrics, dimensions, namespaces, metric-extraction]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function that emits custom CloudWatch metrics via EMF. CloudWatch custom metrics cost $0.30/metric/month for the first 10,000. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Construct** EMF-formatted JSON log output from a Lambda function that CloudWatch automatically extracts as custom metrics
2. **Explain** the required structure of an EMF log entry including the `_aws` key, namespace, dimensions, and metric definitions
3. **Differentiate** between EMF-based metric publishing and the CloudWatch PutMetricData API in terms of cost, latency, and IAM requirements
4. **Diagnose** why missing or malformed `_aws` metadata causes CloudWatch to treat EMF output as plain log text with no metric extraction
5. **Apply** EMF dimensions to create high-cardinality metrics without exceeding CloudWatch dimension limits

## Why Embedded Metric Format

Publishing custom CloudWatch metrics traditionally requires calling the `PutMetricData` API. Each API call adds latency to your Lambda function (50-100ms round trip), requires `cloudwatch:PutMetricData` IAM permissions, and costs $0.01 per 1,000 API requests on top of metric storage costs. At scale -- thousands of invocations per minute -- these API calls become a bottleneck and a cost driver.

Embedded Metric Format (EMF) eliminates all three problems. Your Lambda function writes a specially structured JSON log line to stdout. CloudWatch Logs automatically recognizes the EMF structure (identified by the `_aws` key), extracts the embedded metric values, and publishes them as CloudWatch metrics. No API calls. No additional IAM permissions beyond log writing. No latency overhead. The metric appears in CloudWatch Metrics just as if you had called `PutMetricData`.

The DVA-C02 exam tests EMF in two ways. First, recognizing that EMF is the most cost-effective and lowest-latency method for publishing custom metrics from Lambda. Second, understanding the JSON structure requirements: the `_aws` key with `Timestamp` and `CloudWatchMetrics` array containing namespace, dimensions, and metric definitions. If the `_aws` key is missing or malformed, CloudWatch treats the log entry as plain text and no metric is extracted -- a silent failure that is difficult to debug.

## Building Blocks

### `lambda/main.go`

Your job is to fill in the `// TODO` sections to emit properly formatted EMF logs:

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
	Operation string `json:"operation"`
	UserTier  string `json:"user_tier"`
}

type Response struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
}

func handler(ctx context.Context, event json.RawMessage) (Response, error) {
	var req Request
	json.Unmarshal(event, &req)

	if req.Operation == "" {
		req.Operation = "get_item"
	}
	if req.UserTier == "" {
		req.UserTier = "standard"
	}

	start := time.Now()

	// Simulate work with variable latency
	latency := 50 + rand.Intn(200)
	if req.UserTier == "premium" {
		latency = 20 + rand.Intn(50)
	}
	time.Sleep(time.Duration(latency) * time.Millisecond)

	duration := float64(time.Since(start).Milliseconds())
	success := rand.Float64() > 0.1

	// TODO 1 -- Emit an EMF log entry
	// Build a map with the following structure:
	//
	// {
	//   "_aws": {
	//     "Timestamp": <unix_epoch_milliseconds>,
	//     "CloudWatchMetrics": [
	//       {
	//         "Namespace": "EMFDemo",
	//         "Dimensions": [["Operation", "UserTier"]],
	//         "Metrics": [
	//           {"Name": "ProcessingLatency", "Unit": "Milliseconds"},
	//           {"Name": "RequestCount", "Unit": "Count"},
	//           {"Name": "ErrorCount", "Unit": "Count"}
	//         ]
	//       }
	//     ]
	//   },
	//   "Operation": req.Operation,
	//   "UserTier": req.UserTier,
	//   "ProcessingLatency": duration,
	//   "RequestCount": 1,
	//   "ErrorCount": <0 if success, 1 if failure>,
	//   "FunctionName": os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
	//   "message": "Processed request"
	// }
	//
	// Marshal the map to JSON and print it to stdout with fmt.Println
	// CloudWatch Logs will recognize the _aws key and extract the metrics
	//
	// IMPORTANT: The dimension values (Operation, UserTier) must appear
	// as top-level keys in the same JSON object. They serve double duty:
	// dimension definitions in _aws.CloudWatchMetrics AND actual values
	// as top-level fields.

	errorCount := 0
	if !success {
		errorCount = 1
	}

	_ = duration
	_ = errorCount
	// Replace the line below with your EMF implementation
	fmt.Println(`{"message": "placeholder - implement EMF here"}`)

	if !success {
		return Response{StatusCode: 500, Message: "Simulated error"}, nil
	}

	return Response{StatusCode: 200, Message: "Success"}, nil
}

func main() {
	lambda.Start(handler)
}
```

### Terraform Skeleton

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
  default     = "emf-demo"
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

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.this.function_name
}
```

## Spot the Bug

A developer writes a Lambda function that emits what they believe is an EMF log entry. They check CloudWatch Logs and see the JSON output. But no custom metric appears in CloudWatch Metrics under the "MyApp" namespace, even after waiting 10 minutes.

```go
func emitMetric(operation string, latency float64) {
    entry := map[string]interface{}{
        "Namespace":  "MyApp",
        "Operation":  operation,
        "Latency":    latency,
        "Count":      1,
        "Dimensions": [][]string{{"Operation"}},
        "Metrics": []map[string]string{
            {"Name": "Latency", "Unit": "Milliseconds"},
            {"Name": "Count", "Unit": "Count"},
        },
        "message": "request processed",
    }

    data, _ := json.Marshal(entry)
    fmt.Println(string(data))
}
```

<details>
<summary>Explain the bug</summary>

The EMF JSON is missing the **`_aws` key**. CloudWatch Logs identifies EMF entries by looking for a top-level `_aws` key containing a `CloudWatchMetrics` array. Without `_aws`, CloudWatch treats the log line as a plain structured log -- it is searchable via CloudWatch Logs Insights, but no metric is extracted.

The developer put `Namespace`, `Dimensions`, and `Metrics` at the top level of the JSON. These must be nested inside `_aws.CloudWatchMetrics[0]`.

**Fix -- wrap metric definitions in the `_aws` key:**

```go
func emitMetric(operation string, latency float64) {
    entry := map[string]interface{}{
        "_aws": map[string]interface{}{
            "Timestamp": time.Now().UnixMilli(),
            "CloudWatchMetrics": []map[string]interface{}{
                {
                    "Namespace":  "MyApp",
                    "Dimensions": [][]string{{"Operation"}},
                    "Metrics": []map[string]string{
                        {"Name": "Latency", "Unit": "Milliseconds"},
                        {"Name": "Count", "Unit": "Count"},
                    },
                },
            },
        },
        "Operation": operation,
        "Latency":   latency,
        "Count":     1,
        "message":   "request processed",
    }

    data, _ := json.Marshal(entry)
    fmt.Println(string(data))
}
```

Key structural requirements:
- `_aws` must be a top-level key
- `_aws.CloudWatchMetrics` must be an array of metric definition objects
- Each definition has `Namespace`, `Dimensions`, and `Metrics`
- Dimension and metric values must appear as top-level keys in the same JSON object
- `Timestamp` is optional but recommended (defaults to log ingestion time)

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Invoke the function multiple times

```bash
FUNC=$(terraform output -raw function_name)

for i in $(seq 1 20); do
  aws lambda invoke --function-name "$FUNC" \
    --payload '{"operation": "get_item", "user_tier": "standard"}' /dev/stdout 2>/dev/null
  echo ""
done

for i in $(seq 1 10); do
  aws lambda invoke --function-name "$FUNC" \
    --payload '{"operation": "put_item", "user_tier": "premium"}' /dev/stdout 2>/dev/null
  echo ""
done
```

### Step 3 -- Verify logs contain EMF structure

```bash
aws logs filter-log-events \
  --log-group-name "/aws/lambda/emf-demo" \
  --filter-pattern '{ $._aws.CloudWatchMetrics IS NOT NULL }' \
  --limit 3 \
  --query "events[].message" --output text
```

Expected: JSON log lines containing the `_aws` key with `CloudWatchMetrics` array.

### Step 4 -- Verify custom metrics appear in CloudWatch

Wait 2-3 minutes after invocations, then:

```bash
aws cloudwatch list-metrics \
  --namespace "EMFDemo" \
  --query "Metrics[].{Name:MetricName,Dims:Dimensions[*].Value}" \
  --output table
```

Expected: `ProcessingLatency`, `RequestCount`, and `ErrorCount` metrics with dimension values.

### Step 5 -- Query metric data

```bash
aws cloudwatch get-metric-statistics \
  --namespace "EMFDemo" \
  --metric-name "RequestCount" \
  --dimensions Name=Operation,Value=get_item Name=UserTier,Value=standard \
  --start-time $(date -u -v-30M +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Sum \
  --query "Datapoints[0].Sum"
```

Expected: a number matching your invocation count.

### Step 6 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>lambda/main.go -- TODO 1 -- EMF Log Entry Implementation</summary>

Replace the placeholder `fmt.Println` in `main.go` with:

```go
	emfEntry := map[string]interface{}{
		"_aws": map[string]interface{}{
			"Timestamp": time.Now().UnixMilli(),
			"CloudWatchMetrics": []map[string]interface{}{
				{
					"Namespace":  "EMFDemo",
					"Dimensions": [][]string{{"Operation", "UserTier"}},
					"Metrics": []map[string]string{
						{"Name": "ProcessingLatency", "Unit": "Milliseconds"},
						{"Name": "RequestCount", "Unit": "Count"},
						{"Name": "ErrorCount", "Unit": "Count"},
					},
				},
			},
		},
		"Operation":         req.Operation,
		"UserTier":          req.UserTier,
		"ProcessingLatency": duration,
		"RequestCount":      1,
		"ErrorCount":        errorCount,
		"FunctionName":      os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"message":           "Processed request",
	}

	emfJSON, _ := json.Marshal(emfEntry)
	fmt.Println(string(emfJSON))
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

Note: Custom metrics created via EMF persist in CloudWatch for 15 months (high-resolution) to 63 months (standard resolution) after the last data point. There is no additional cost for stored metric data after creation.

## What's Next

You published custom metrics using EMF with zero API calls. In the next exercise, you will enable **CloudWatch Contributor Insights for DynamoDB** to identify the most accessed and throttled partition keys in your table.

## Summary

- **EMF** is a structured JSON format that CloudWatch Logs automatically extracts as custom metrics -- no `PutMetricData` API calls required
- The `_aws` key is mandatory: it contains `CloudWatchMetrics` (array of namespace/dimensions/metrics definitions) and an optional `Timestamp`
- Dimension values must appear as **top-level keys** in the same JSON object alongside the `_aws` key
- EMF requires only `logs:PutLogEvents` permission (included in `AWSLambdaBasicExecutionRole`) -- no `cloudwatch:PutMetricData` needed
- Missing or malformed `_aws` key is a **silent failure**: the log is written but no metric is extracted
- EMF supports up to 30 dimensions and 100 metrics per log entry
- Metric values must be numeric (int or float); dimension values must be strings

## Reference

- [CloudWatch Embedded Metric Format Specification](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html)
- [Using EMF with Lambda](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Generation_Lambda.html)
- [Terraform aws_lambda_function](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function)

## Additional Resources

- [EMF Client Libraries](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Libraries.html) -- official SDKs for Node.js, Python, Java, and .NET (Go uses manual JSON construction)
- [CloudWatch Metrics Pricing](https://aws.amazon.com/cloudwatch/pricing/) -- $0.30/metric/month for first 10,000 custom metrics
- [CloudWatch Logs Insights with EMF](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/AnalyzingLogData.html) -- querying EMF logs alongside extracted metrics
- [High-Cardinality Dimensions](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_concepts.html#Dimension) -- how unique dimension combinations create separate metric time series
