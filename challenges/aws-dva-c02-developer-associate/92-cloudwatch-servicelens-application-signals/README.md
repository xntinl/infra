# 92. CloudWatch ServiceLens and Application Signals

<!--
difficulty: advanced
concepts: [servicelens, application-signals, service-map, slo-monitoring, log-correlation, trace-metrics-integration, service-level-objectives]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions, API Gateway, DynamoDB, with X-Ray tracing and CloudWatch metrics. ServiceLens and Application Signals are included in standard CloudWatch pricing. Total cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Completed exercise 10 (X-Ray instrumentation) recommended

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** how ServiceLens unifies X-Ray traces, CloudWatch metrics, and CloudWatch Logs into a single service-centric view
- **Design** a monitoring strategy that correlates traces with log entries and metric anomalies using ServiceLens
- **Implement** the infrastructure required for ServiceLens: X-Ray active tracing, structured logging, and CloudWatch metrics on all components
- **Create** Service Level Objectives (SLOs) using Application Signals to track availability and latency targets
- **Analyze** the ServiceLens service map to identify dependency bottlenecks and trace-to-log correlations

## Why ServiceLens and Application Signals

Modern applications generate three types of observability data: **metrics** (CloudWatch), **traces** (X-Ray), and **logs** (CloudWatch Logs). Without ServiceLens, these live in separate consoles. When a user reports high latency, you check CloudWatch metrics, then switch to X-Ray to find slow traces, then switch to CloudWatch Logs to read error messages. Each tool provides a fragment of the picture.

ServiceLens combines all three into a single view. The ServiceLens service map shows every service (Lambda, API Gateway, DynamoDB) as a node with live metrics overlaid. Click a node to see its latency percentiles, error rates, and recent traces. Click a trace to see correlated log entries from the same request. This three-way correlation -- metrics to traces to logs -- is the core value proposition.

Application Signals extends ServiceLens with **Service Level Objectives (SLOs)**. You define targets like "99.9% of requests complete under 500ms" or "API availability must exceed 99.95%". Application Signals continuously measures performance against these targets and shows remaining error budget. When the error budget burns too fast, alarms fire before the SLO is violated.

The DVA-C02 exam tests ServiceLens as the answer to "unified observability" questions. When asked how to correlate traces with logs, or how to view metrics and traces in a single console, ServiceLens is the answer. Application Signals SLOs appear in questions about SRE practices and proactive monitoring.

## The Challenge

Build a multi-service application with full observability -- X-Ray tracing, structured logging, and CloudWatch metrics -- then use ServiceLens to correlate data across all three pillars. Define an SLO for API latency.

### Requirements

| Requirement | Description |
|---|---|
| API Gateway | REST API with X-Ray tracing enabled |
| Order Lambda | Processes orders, writes to DynamoDB, X-Ray active tracing, structured JSON logs |
| DynamoDB | Orders table with on-demand capacity |
| X-Ray | Active tracing on API Gateway and Lambda, instrumented AWS SDK calls |
| Structured Logging | JSON logs with request_id, trace_id, and business context for log-trace correlation |
| CloudWatch Metrics | EMF-based custom metrics for order processing latency |
| SLO | Application Signals SLO for P99 latency < 2 seconds |

### Architecture

```
  ServiceLens Unified View
  ┌─────────────────────────────────────────────┐
  │                                             │
  │  Service Map (X-Ray)                        │
  │  ┌──────┐    ┌──────────┐    ┌──────────┐  │
  │  │API GW│───>│Order     │───>│DynamoDB  │  │
  │  │      │    │Lambda    │    │          │  │
  │  └──────┘    └──────────┘    └──────────┘  │
  │                                             │
  │  Metrics (CloudWatch)                       │
  │  - Latency P99: 245ms                      │
  │  - Error Rate: 0.1%                        │
  │  - Invocations: 150/min                    │
  │                                             │
  │  Logs (CloudWatch Logs)                     │
  │  - Correlated by X-Ray trace_id            │
  │  - Structured JSON with request context    │
  │                                             │
  │  SLO (Application Signals)                 │
  │  - P99 Latency < 2s: 99.95% (target 99.9%)│
  │  - Error Budget Remaining: 85%              │
  └─────────────────────────────────────────────┘
```

## Hints

<details>
<summary>Hint 1: Structured logging with trace correlation</summary>

For ServiceLens to correlate logs with traces, your log entries must include the X-Ray trace ID. In Go Lambda functions, the trace ID is available from the `_X_AMZN_TRACE_ID` environment variable or the Lambda context:

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/aws/aws-lambda-go/lambdacontext"
)

func logStructured(ctx context.Context, level, message string, fields map[string]interface{}) {
    lc, _ := lambdacontext.FromContext(ctx)

    entry := map[string]interface{}{
        "level":      level,
        "message":    message,
        "request_id": lc.AwsRequestID,
        "trace_id":   os.Getenv("_X_AMZN_TRACE_ID"),
        "function":   os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
    }

    for k, v := range fields {
        entry[k] = v
    }

    data, _ := json.Marshal(entry)
    fmt.Println(string(data))
}
```

CloudWatch ServiceLens automatically detects the `trace_id` field and creates a link from the log entry to the corresponding X-Ray trace.

</details>

<details>
<summary>Hint 2: EMF metrics for ServiceLens integration</summary>

ServiceLens pulls metrics from CloudWatch. Publishing custom metrics via EMF (Embedded Metric Format) creates metrics that appear alongside the built-in Lambda metrics in the ServiceLens dashboard:

```go
func emitLatencyMetric(operation string, latencyMs float64) {
    emf := map[string]interface{}{
        "_aws": map[string]interface{}{
            "Timestamp": time.Now().UnixMilli(),
            "CloudWatchMetrics": []map[string]interface{}{
                {
                    "Namespace":  "OrderService",
                    "Dimensions": [][]string{{"Operation"}},
                    "Metrics": []map[string]string{
                        {"Name": "ProcessingLatency", "Unit": "Milliseconds"},
                    },
                },
            },
        },
        "Operation":         operation,
        "ProcessingLatency": latencyMs,
    }
    data, _ := json.Marshal(emf)
    fmt.Println(string(data))
}
```

</details>

<details>
<summary>Hint 3: Enabling ServiceLens prerequisites in Terraform</summary>

ServiceLens requires X-Ray active tracing on all components. There is no separate Terraform resource for "enabling ServiceLens" -- it activates automatically when X-Ray tracing data, CloudWatch metrics, and CloudWatch Logs are all present for the same services.

Key Terraform configurations:

```hcl
# API Gateway X-Ray tracing
resource "aws_api_gateway_stage" "prod" {
  # ...
  xray_tracing_enabled = true
}

# Lambda X-Ray tracing
resource "aws_lambda_function" "order" {
  # ...
  tracing_config {
    mode = "Active"
  }
}

# Lambda needs X-Ray write permission
resource "aws_iam_role_policy_attachment" "xray" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}
```

ServiceLens is accessed through the CloudWatch console under "ServiceLens" > "Service map". It automatically discovers services from X-Ray trace data.

</details>

<details>
<summary>Hint 4: Creating an SLO with Application Signals</summary>

Application Signals SLOs can be created via the AWS CLI. After enabling Application Signals on your account:

```bash
# Enable Application Signals (one-time setup)
aws application-signals start-discovery

# Create an SLO for API latency
aws application-signals create-service-level-objective \
  --name "order-api-latency" \
  --description "P99 latency for order API must be under 2 seconds" \
  --sli-config '{
    "SliMetricConfig": {
      "KeyAttributes": {
        "Type": "AWS::Lambda::Function",
        "Name": "order-service-demo"
      },
      "MetricType": "LATENCY",
      "Statistic": "p99",
      "PeriodSeconds": 300
    },
    "MetricThreshold": 2000,
    "ComparisonOperator": "LessThanOrEqualToThreshold"
  }' \
  --goal '{
    "AttainmentGoal": 99.9,
    "WarningThreshold": 99.95,
    "Interval": {
      "RollingInterval": {
        "DurationUnit": "DAY",
        "Duration": 30
      }
    }
  }'
```

Note: Application Signals SLO creation is typically done through the console or CLI, as Terraform support may be limited. The Terraform approach focuses on ensuring the underlying observability data is properly configured.

</details>

## Spot the Bug

A developer enables X-Ray tracing on their Lambda function and checks ServiceLens. The service map shows the Lambda node but there is no connection to DynamoDB -- the DynamoDB node does not appear even though the Lambda writes to DynamoDB on every invocation.

```go
func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())
    // AWS SDK is NOT instrumented with X-Ray
    dynamoClient = dynamodb.NewFromConfig(cfg)
}
```

```hcl
resource "aws_lambda_function" "order" {
  # ...
  tracing_config {
    mode = "Active"
  }
}
```

<details>
<summary>Explain the bug</summary>

The Lambda function has X-Ray active tracing enabled, which creates a segment for each invocation. However, the **AWS SDK is not instrumented** with the X-Ray SDK. Without instrumentation, DynamoDB calls are not recorded as subsegments in the X-Ray trace. ServiceLens builds its service map from X-Ray trace data -- if DynamoDB calls are not captured as subsegments, ServiceLens cannot draw the connection between the Lambda function and DynamoDB.

**Fix -- instrument the AWS SDK before creating clients:**

```go
import "github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())
    awsv2.AWSV2Instrumentor(&cfg.APIOptions)  // Instrument the SDK
    dynamoClient = dynamodb.NewFromConfig(cfg)
}
```

After instrumentation:
- Every DynamoDB call creates an X-Ray subsegment
- The ServiceLens service map shows a connection from Lambda to DynamoDB
- Clicking the DynamoDB node in ServiceLens shows DynamoDB-specific metrics (latency, throttling)
- Traces show the DynamoDB call duration instead of an unexplained time gap

This is the same instrumentation fix from exercise 10, but in the ServiceLens context it has a visible impact on the service map topology.

</details>

## Verify What You Learned

After deploying and generating traffic, run these verification commands:

```bash
# Verify Lambda has active tracing
aws lambda get-function-configuration \
  --function-name $(terraform output -raw function_name) \
  --query "TracingConfig.Mode" --output text
```

Expected: `Active`

```bash
# Verify API Gateway has X-Ray tracing
aws apigateway get-stage \
  --rest-api-id $(terraform output -raw api_id) \
  --stage-name prod \
  --query "tracingEnabled" --output text
```

Expected: `True`

```bash
# Check the X-Ray service graph (ServiceLens data source)
aws xray get-service-graph \
  --start-time $(date -u -v-30M +%s) \
  --end-time $(date -u +%s) \
  --query "Services[*].{Name:Name,Type:Type,Edges:length(Edges)}" \
  --output table
```

Expected: nodes for API Gateway, Lambda, and DynamoDB with edges connecting them.

```bash
# Verify structured logs contain trace IDs
aws logs filter-log-events \
  --log-group-name "/aws/lambda/$(terraform output -raw function_name)" \
  --filter-pattern '{ $.trace_id = "*" }' \
  --limit 3 \
  --query "events[].message" --output text
```

Expected: JSON log entries containing `trace_id` fields.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a fully observable application with ServiceLens correlating traces, metrics, and logs. In the next exercise, you will explore **Step Functions Standard vs Express** workflows, understanding the pricing, execution guarantees, and state machine definitions.

## Summary

- **ServiceLens** unifies X-Ray traces, CloudWatch metrics, and CloudWatch Logs into a single service-centric dashboard
- ServiceLens activates automatically when X-Ray tracing, metrics, and logs are all configured -- no separate enablement step
- The service map is built from **X-Ray trace data** -- uninstrumented SDK calls create gaps in the map
- **Log-trace correlation** requires structured logs containing the X-Ray trace ID (`_X_AMZN_TRACE_ID` environment variable)
- **Application Signals** extends ServiceLens with SLO monitoring: define targets for latency and availability, track error budget consumption
- To see DynamoDB in the service map, the AWS SDK **must be instrumented** with `awsv2.AWSV2Instrumentor` -- active tracing alone is not sufficient
- EMF metrics published from Lambda appear alongside built-in metrics in the ServiceLens dashboard
- ServiceLens is accessed via CloudWatch console > ServiceLens > Service map

## Reference

- [CloudWatch ServiceLens](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/ServiceLens.html)
- [Application Signals](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch-Application-Monitoring-Sections.html)
- [X-Ray Service Map](https://docs.aws.amazon.com/xray/latest/devguide/xray-console-servicemap.html)
- [Terraform aws_lambda_function tracing_config](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#tracing_config)

## Additional Resources

- [ServiceLens Console Walkthrough](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/ServiceLens_Troubleshooting.html) -- using the ServiceLens console to debug issues
- [Application Signals SLOs](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch-Application-Monitoring-Sections.html) -- creating and managing service level objectives
- [Log Insights Integration](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/AnalyzingLogData.html) -- querying correlated logs from ServiceLens
- [X-Ray SDK for Go](https://docs.aws.amazon.com/xray/latest/devguide/xray-sdk-go.html) -- instrumenting Go applications for complete trace data

<details>
<summary>Full Solution</summary>

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"
	"github.com/aws/aws-xray-sdk-go/xray"
	"github.com/google/uuid"
)

var (
	dynamoClient *dynamodb.Client
	tableName    string
)

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	awsv2.AWSV2Instrumentor(&cfg.APIOptions)
	dynamoClient = dynamodb.NewFromConfig(cfg)
}

func logStructured(ctx context.Context, level, message string, fields map[string]interface{}) {
	lc, _ := lambdacontext.FromContext(ctx)
	entry := map[string]interface{}{
		"level":      level,
		"message":    message,
		"request_id": lc.AwsRequestID,
		"trace_id":   os.Getenv("_X_AMZN_TRACE_ID"),
		"function":   os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range fields {
		entry[k] = v
	}
	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}

func emitLatencyMetric(operation string, latencyMs float64) {
	emf := map[string]interface{}{
		"_aws": map[string]interface{}{
			"Timestamp": time.Now().UnixMilli(),
			"CloudWatchMetrics": []map[string]interface{}{
				{
					"Namespace":  "OrderService",
					"Dimensions": [][]string{{"Operation"}},
					"Metrics": []map[string]string{
						{"Name": "ProcessingLatency", "Unit": "Milliseconds"},
						{"Name": "RequestCount", "Unit": "Count"},
					},
				},
			},
		},
		"Operation":         operation,
		"ProcessingLatency": latencyMs,
		"RequestCount":      1,
	}
	data, _ := json.Marshal(emf)
	fmt.Println(string(data))
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	start := time.Now()
	orderID := uuid.New().String()

	logStructured(ctx, "info", "Processing order", map[string]interface{}{
		"order_id": orderID,
		"method":   event.HTTPMethod,
		"path":     event.Path,
	})

	ctx, seg := xray.BeginSubsegment(ctx, "create_order")
	_ = xray.AddAnnotation(ctx, "order_id", orderID)

	_, err := dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]dynamodbtypes.AttributeValue{
			"order_id":  &dynamodbtypes.AttributeValueMemberS{Value: orderID},
			"status":    &dynamodbtypes.AttributeValueMemberS{Value: "created"},
			"timestamp": &dynamodbtypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
		},
	})
	seg.Close(err)

	duration := float64(time.Since(start).Milliseconds())
	emitLatencyMetric("create_order", duration)

	if err != nil {
		logStructured(ctx, "error", "Failed to create order", map[string]interface{}{
			"order_id": orderID,
			"error":    err.Error(),
			"duration": duration,
		})
		body, _ := json.Marshal(map[string]string{"error": err.Error()})
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: string(body)}, nil
	}

	logStructured(ctx, "info", "Order created successfully", map[string]interface{}{
		"order_id": orderID,
		"duration": duration,
	})

	body, _ := json.Marshal(map[string]string{
		"order_id": orderID,
		"status":   "created",
	})
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: string(body)}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module servicelens-demo

go 1.21

require (
    github.com/aws/aws-lambda-go v1.47.0
    github.com/aws/aws-sdk-go-v2 v1.24.0
    github.com/aws/aws-sdk-go-v2/config v1.26.0
    github.com/aws/aws-sdk-go-v2/service/dynamodb v1.26.0
    github.com/aws/aws-xray-sdk-go v1.8.3
    github.com/google/uuid v1.6.0
)
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 5.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
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
  default     = "servicelens-demo"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"
  attribute {
    name = "order_id"
    type = "S"
  }
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/lambda"
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/bootstrap"
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

data "aws_iam_policy_document" "dynamodb_access" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem"]
    resources = [aws_dynamodb_table.orders.arn]
  }
}

resource "aws_iam_role_policy" "dynamodb" {
  name   = "dynamodb-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.dynamodb_access.json
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

  tracing_config { mode = "Active" }

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.orders.name
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.basic,
    aws_iam_role_policy_attachment.xray,
    aws_cloudwatch_log_group.this,
  ]
}

resource "aws_lambda_permission" "apigw" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

resource "aws_api_gateway_resource" "orders" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "orders"
}

resource "aws_api_gateway_method" "post" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.orders.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "lambda" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.orders.id
  http_method             = aws_api_gateway_method.post.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  depends_on  = [aws_api_gateway_integration.lambda]
  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "prod" {
  rest_api_id          = aws_api_gateway_rest_api.this.id
  deployment_id        = aws_api_gateway_deployment.this.id
  stage_name           = "prod"
  xray_tracing_enabled = true
}
```

### `outputs.tf`

```hcl
output "api_url" {
  value = "${aws_api_gateway_stage.prod.invoke_url}/orders"
}

output "api_id" {
  value = aws_api_gateway_rest_api.this.id
}

output "function_name" {
  value = aws_lambda_function.this.function_name
}

output "table_name" {
  value = aws_dynamodb_table.orders.name
}
```

</details>
