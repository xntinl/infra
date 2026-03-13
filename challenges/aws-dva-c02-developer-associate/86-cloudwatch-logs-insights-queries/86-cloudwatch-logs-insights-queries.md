# 86. CloudWatch Logs Insights Query Language

<!--
difficulty: basic
concepts: [logs-insights, query-language, fields-command, filter-command, stats-command, sort-command, parse-command, structured-logging, json-logs]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [85-cloudwatch-custom-metrics-dimensions]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function that generates structured JSON logs. CloudWatch Logs Insights charges $0.005 per GB of data scanned. For testing volumes this is negligible. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Construct** CloudWatch Logs Insights queries using `fields`, `filter`, `stats`, `sort`, and `parse` commands
- **Analyze** structured JSON log data from Lambda functions to extract error rates, latency percentiles, and top-N patterns
- **Differentiate** between `filter` (row-level filtering) and `stats` (aggregation) commands and combine them for complex analysis
- **Apply** the `parse` command to extract fields from unstructured log lines using glob and regex patterns
- **Explain** how Logs Insights automatically discovers fields in JSON log lines versus requiring `parse` for unstructured text

## Why CloudWatch Logs Insights

Lambda functions produce CloudWatch Logs, but scrolling through log streams is not practical at scale. CloudWatch Logs Insights provides a purpose-built query language for searching, filtering, and aggregating log data across multiple log groups.

The query language has five core commands: **fields** (select), **filter** (where), **stats** (aggregate), **sort** (order), **parse** (extract from text). Logs Insights automatically extracts fields from JSON logs. Unstructured logs require `parse`.

DVA-C02 exam examples: `filter @duration > 3000` (slow invocations), `stats pct(@duration, 95)` (P95 latency), `stats count(*) by error_type` (error breakdown).

## Step 1 -- Create the Lambda Function with Structured Logging

### `main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

type OrderEvent struct {
	OrderID   string  `json:"order_id"`
	Customer  string  `json:"customer"`
	Region    string  `json:"region"`
	Amount    float64 `json:"amount"`
	ItemCount int     `json:"item_count"`
}

type LogEntry struct {
	Level string `json:"level"` // INFO, ERROR
	Message string `json:"message"`
	OrderID string `json:"order_id,omitempty"` // Remaining fields omitempty
	Customer string `json:"customer,omitempty"`
	Region string `json:"region,omitempty"`
	Amount float64 `json:"amount,omitempty"`
	DurationMs float64 `json:"duration_ms,omitempty"`
	ErrorType string `json:"error_type,omitempty"`
	Step string `json:"step,omitempty"`
}

func logJSON(entry LogEntry) {
	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}

func handler(ctx context.Context, event OrderEvent) (map[string]interface{}, error) {
	startTime := time.Now()

	logJSON(LogEntry{Level: "INFO", Message: "Processing order", OrderID: event.OrderID,
		Customer: event.Customer, Region: event.Region, Amount: event.Amount, Step: "start"})

	time.Sleep(time.Duration(10+rand.Intn(50)) * time.Millisecond)
	errorRoll := rand.Float64()
	if errorRoll < 0.15 {
		logJSON(LogEntry{Level: "ERROR", Message: "Payment failed", OrderID: event.OrderID, ErrorType: "PaymentError", Step: "payment"})
		return map[string]interface{}{"status": "failed", "error": "PaymentError"}, nil
	}
	if errorRoll < 0.25 {
		logJSON(LogEntry{Level: "ERROR", Message: "Inventory check failed", OrderID: event.OrderID, ErrorType: "InventoryError", Step: "inventory"})
		return map[string]interface{}{"status": "failed", "error": "InventoryError"}, nil
	}

	time.Sleep(time.Duration(50+rand.Intn(200)) * time.Millisecond)

	elapsed := float64(time.Since(startTime).Milliseconds())

	logJSON(LogEntry{Level: "INFO", Message: "Order processed successfully", OrderID: event.OrderID,
		Customer: event.Customer, Region: event.Region, Amount: event.Amount, DurationMs: elapsed, Step: "complete"})

	fmt.Printf("METRIC order_id=%s region=%s duration_ms=%.0f status=success\n",
		event.OrderID, event.Region, elapsed)

	return map[string]interface{}{"status": "success", "order_id": event.OrderID, "duration_ms": elapsed}, nil
}

func main() {
	rand.Seed(time.Now().UnixNano())
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
  default     = "logs-insights-demo"
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
resource "aws_cloudwatch_query_definition" "error_rate" {
  name            = "${var.project_name}/error-rate"
  log_group_names = [aws_cloudwatch_log_group.this.name]
  query_string    = "filter level = \"ERROR\" | stats count(*) as error_count by error_type | sort error_count desc"
}
```

### `outputs.tf`

```hcl
output "function_name" {
  description = "Lambda function name"
  value       = aws_lambda_function.this.function_name
}

output "log_group" {
  description = "CloudWatch log group name"
  value       = aws_cloudwatch_log_group.this.name
}
```

## Step 3 -- Build, Apply, and Generate Log Data

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

Generate log data by invoking the function 20 times:

```bash
FUNCTION_NAME=$(terraform output -raw function_name)
for i in $(seq 1 20); do
  REGION=$(echo "us-east-1 eu-west-1 ap-southeast-1" | tr ' ' '\n' | shuf -n1)
  aws lambda invoke --function-name "$FUNCTION_NAME" \
    --payload "{\"order_id\":\"ord-$(printf '%03d' $i)\",\"customer\":\"cust-$((RANDOM%5+1))\",\"region\":\"$REGION\",\"amount\":$((RANDOM%200+10))}" \
    /dev/null 2>/dev/null &
done && wait
```

## Step 4 -- Run Logs Insights Queries

Wait 30 seconds for logs to be indexed, then run queries:

### Query 1: Find all errors

```bash
LOG_GROUP=$(terraform output -raw log_group)

QUERY_ID=$(aws logs start-query \
  --log-group-name "$LOG_GROUP" \
  --start-time $(date -u -v-15M '+%s') \
  --end-time $(date -u '+%s') \
  --query-string 'fields @timestamp, level, message, order_id, error_type | filter level = "ERROR" | sort @timestamp desc | limit 20' \
  --query "queryId" --output text)

sleep 5
aws logs get-query-results --query-id "$QUERY_ID" \
  --query "results[*].[field, value]" --output table
```

### Query 2: Error count by type

```bash
QUERY_ID=$(aws logs start-query \
  --log-group-name "$LOG_GROUP" \
  --start-time $(date -u -v-15M '+%s') \
  --end-time $(date -u '+%s') \
  --query-string 'filter level = "ERROR" | stats count(*) as error_count by error_type | sort error_count desc' \
  --query "queryId" --output text)

sleep 5
aws logs get-query-results --query-id "$QUERY_ID" \
  --query "results" --output json | jq .
```

### Query 3: Latency percentiles by region

```bash
QUERY_ID=$(aws logs start-query --log-group-name "$LOG_GROUP" \
  --start-time $(date -u -v-15M '+%s') --end-time $(date -u '+%s') \
  --query-string 'filter step = "complete" | stats avg(duration_ms) as avg_ms, pct(duration_ms, 95) as p95, count(*) by region | sort avg_ms desc' \
  --query "queryId" --output text)
sleep 5 && aws logs get-query-results --query-id "$QUERY_ID" --query "results" --output json | jq .
```

### Query 4: Parse unstructured log lines

```bash
QUERY_ID=$(aws logs start-query --log-group-name "$LOG_GROUP" \
  --start-time $(date -u -v-15M '+%s') --end-time $(date -u '+%s') \
  --query-string 'filter @message like /METRIC/ | parse @message "METRIC order_id=* region=* duration_ms=* status=*" as oid, region, dur, st | sort dur desc' \
  --query "queryId" --output text)
sleep 5 && aws logs get-query-results --query-id "$QUERY_ID" --query "results" --output json | jq .
```


## Common Mistakes

### 1. Using filter on fields that only exist in some log lines

If you filter on `error_type` but not all log lines have this field, non-matching lines are excluded. To count errors vs successes, filter on `level` which exists in all JSON log lines.

### 2. Forgetting that Logs Insights auto-discovers JSON fields

JSON fields are automatically available for filtering and aggregation. You do not need `parse` for JSON logs -- only for unstructured text like `METRIC order_id=123 duration_ms=50`.

### 3. Not waiting for log indexing

New logs may take 30-60 seconds to be indexed. If your query returns no results immediately after generating logs, wait and retry.


## Verify What You Learned

```bash
# Verify log group has data
aws logs describe-log-groups --log-group-name-prefix $(terraform output -raw log_group) \
  --query "logGroups[0].storedBytes" --output text
```

Expected: a non-zero value indicating logs are stored.

```bash
# Verify saved query definition exists
aws logs describe-query-definitions \
  --query-definition-name-prefix "logs-insights-demo" \
  --query "queryDefinitions[*].name" --output json
```

Expected: the saved error-rate query.

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

You explored CloudWatch Logs Insights for analyzing structured and unstructured Lambda logs. Next, you will build on this with **CloudWatch alarms and composite alarms** for automated incident detection.

## Summary

- Five core commands: **fields** (select), **filter** (where), **stats** (aggregate), **sort** (order), **parse** (extract from text)
- **JSON log lines** are automatically parsed -- fields become queryable without configuration
- **Unstructured log lines** require `parse` with glob (`*`) or regex patterns to extract fields
- Built-in fields: **@timestamp**, **@message**, **@logStream**, **@duration** (Lambda), **@billedDuration**, **@memorySize**, **@maxMemoryUsed**
- **stats** supports `count()`, `sum()`, `avg()`, `min()`, `max()`, `pct(field, percentile)` -- use `by` for grouping
- Queries scan data within the time range -- **$0.005 per GB scanned**, so narrow time ranges reduce cost
- Logs Insights can query **multiple log groups** simultaneously for cross-service tracing

## Reference

- [CloudWatch Logs Insights Query Syntax](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax.html)
- [Logs Insights Sample Queries](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax-examples.html)
- [Terraform aws_cloudwatch_query_definition](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_query_definition)
- [CloudWatch Logs Pricing](https://aws.amazon.com/cloudwatch/pricing/)

## Additional Resources

- [Logs Insights Supported Functions](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax-operations-functions.html) -- complete list of aggregation and string functions
- [Lambda Structured Logging](https://docs.aws.amazon.com/lambda/latest/dg/monitoring-cloudwatchlogs.html) -- best practices for JSON logging in Lambda
