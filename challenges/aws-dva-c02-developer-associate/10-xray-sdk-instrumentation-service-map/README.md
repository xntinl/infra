# 10. X-Ray SDK Instrumentation and Service Map Analysis

<!--
difficulty: advanced
concepts: [xray-active-tracing, xray-sdk, custom-subsegments, annotations, metadata, service-map, trace-filter-expressions]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates API Gateway, two Lambda functions, a DynamoDB table, and an SQS queue -- all with X-Ray active tracing enabled. Cost is approximately $0.03/hr. X-Ray has a perpetual free tier of 100,000 traces recorded per month. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Completed exercises 1-9 (recommended but not required)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** X-Ray service maps to identify latency bottlenecks and uninstrumented downstream calls in a multi-service architecture
- **Design** a tracing strategy that uses annotations for indexed queries and metadata for non-indexed debug context
- **Implement** custom subsegments in Lambda function code to capture granular timing for business logic operations
- **Configure** active tracing on API Gateway, Lambda, and downstream AWS SDK calls using the X-Ray SDK for Go
- **Analyze** traces using filter expressions to isolate failed or slow requests by custom annotation values

## Why This Matters

When a user reports that "the API is slow," you need more than CloudWatch metrics to pinpoint the cause. A single API Gateway request might flow through a Lambda function, query DynamoDB, publish to SQS, and trigger a downstream consumer. Without distributed tracing, you are guessing which hop introduced the latency. X-Ray solves this by propagating a trace ID across services, recording the duration of each hop as a segment or subsegment, and rendering the entire call chain as a service map.

The DVA-C02 exam tests three areas heavily. First, the difference between annotations (indexed, queryable with filter expressions, limited to 50 per trace) and metadata (not indexed, unlimited, for debug context). Second, how to instrument AWS SDK calls so that DynamoDB and SQS calls appear as subsegments rather than opaque gaps. Third, filter expression syntax for querying traces by annotation values, HTTP status codes, or response times. Understanding these concepts separates candidates who have used X-Ray in practice from those who have only read about it.

## The Challenge

Build a multi-service order processing flow and instrument it end-to-end with X-Ray. Your goal is to see every downstream call as a distinct subsegment in the trace, query traces by custom annotations, and identify a deliberate instrumentation gap.

### Requirements

| Requirement | Description |
|---|---|
| API Gateway | REST API with X-Ray active tracing enabled, single POST /orders endpoint |
| Order Lambda | Receives the API request, writes to DynamoDB, publishes to SQS, adds custom annotations and subsegments |
| DynamoDB Table | Stores orders, must appear as an instrumented subsegment in the trace |
| SQS Queue | Receives order messages for async processing |
| Consumer Lambda | Triggered by SQS, processes the message, writes a status update to DynamoDB |
| X-Ray Annotations | `order_status` (success/failed) and `customer_tier` (standard/premium) on the Order Lambda |
| X-Ray Metadata | Full order payload stored as metadata (not indexed) |
| Filter Expressions | Query traces where `annotation.order_status = "failed"` |

### Architecture

```
                        +---------------------------------------------+
                        |              X-Ray Service Map              |
                        |                                             |
  Client --> [API GW] --> [Order Lambda] --+--> [DynamoDB: orders]   |
                        |                   |                         |
                        |                   +--> [SQS: order-queue]  |
                        |                            |                |
                        |                   [Consumer Lambda] <------+
                        |                            |
                        |                   [DynamoDB: orders] (status update)
                        +---------------------------------------------+

  Trace flow:
  ---------------------------------------------------------------------
  Segment: API GW         |xxxx|
  Segment: Order Lambda   |  |xxxxxxxxxxxxxxxx|
    Subsegment: DynamoDB  |  |  |xxxxx|       |
    Subsegment: SQS       |  |         |xxxx| |
  Segment: Consumer Lambda|              (async, separate trace)
  ---------------------------------------------------------------------
```

## Hints

<details>
<summary>Hint 1: Enabling active tracing on API Gateway and Lambda</summary>

API Gateway and Lambda both have X-Ray tracing settings, but they are configured differently in Terraform.

For API Gateway REST API, you need the `aws_api_gateway_stage` resource with `xray_tracing_enabled = true`. This tells API Gateway to start a trace segment for every incoming request.

For Lambda, set `tracing_config { mode = "Active" }` on the `aws_lambda_function` resource. In Active mode, Lambda creates a segment for the invocation. In PassThrough mode, Lambda only propagates an existing trace header but does not create its own segment if none exists.

```hcl
resource "aws_lambda_function" "order" {
  # ... other config ...

  tracing_config {
    mode = "Active"
  }
}
```

The Lambda execution role also needs the `AWSXRayDaemonWriteAccess` managed policy to send trace data.

</details>

<details>
<summary>Hint 2: Instrumenting AWS SDK calls with the X-Ray SDK for Go</summary>

Simply enabling active tracing on Lambda does not automatically capture DynamoDB or SQS calls as subsegments. You must instrument the AWS SDK clients using the X-Ray SDK for Go. Without instrumentation, downstream calls appear as time gaps in the trace.

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/sqs"
    "github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"
)

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())

    // Instrument the AWS SDK config so all clients created from it
    // automatically generate X-Ray subsegments for every API call
    awsv2.AWSV2Instrumentor(&cfg.APIOptions)

    // Now every DynamoDB/SQS call is automatically wrapped in an X-Ray subsegment
    dynamoClient = dynamodb.NewFromConfig(cfg)
    sqsClient = sqs.NewFromConfig(cfg)
}
```

The `awsv2.AWSV2Instrumentor` call must happen before you create AWS SDK clients. This is the Go equivalent of Python's `patch_all()`.

</details>

<details>
<summary>Hint 3: Adding custom annotations and metadata</summary>

Annotations are indexed key-value pairs you can search with filter expressions. Metadata is unindexed and can hold complex objects. Both are added using the `xray` package.

```go
import "github.com/aws/aws-xray-sdk-go/xray"

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
    // Create a custom subsegment
    ctx, seg := xray.BeginSubsegment(ctx, "process_order")
    defer seg.Close(nil)

    order := parseOrder(event.Body)

    // Annotations: indexed, queryable, max 50 per trace
    // Keys must be alphanumeric (no spaces or special chars)
    xray.AddAnnotation(ctx, "order_status", "success")
    xray.AddAnnotation(ctx, "customer_tier", order.Tier)

    // Metadata: not indexed, can hold any serializable object
    // Organized by namespace (default namespace is "default")
    xray.AddMetadata(ctx, "order_payload", order)

    // ... business logic ...
}
```

Annotation keys must be alphanumeric with underscores. Values can be strings, numbers, or booleans. Metadata values can be any serializable object.

</details>

<details>
<summary>Hint 4: Querying traces with filter expressions</summary>

X-Ray filter expressions let you search traces by annotations, HTTP status, duration, and more. Use the AWS CLI to query:

```bash
# Find all traces where the order failed (using custom annotation)
aws xray get-trace-summaries \
  --start-time $(date -u -v-1H +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'annotation.order_status = "failed"'

# Find traces slower than 2 seconds
aws xray get-trace-summaries \
  --start-time $(date -u -v-1H +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'responsetime > 2'

# Combine filters: failed orders from premium customers
aws xray get-trace-summaries \
  --start-time $(date -u -v-1H +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'annotation.order_status = "failed" AND annotation.customer_tier = "premium"'

# Find traces with a specific HTTP status from API Gateway
aws xray get-trace-summaries \
  --start-time $(date -u -v-1H +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'http.status = 502'
```

Note: `responsetime` is in seconds (not milliseconds). Annotation values must be quoted strings. Filter expressions are case-sensitive.

</details>

<details>
<summary>Hint 5: SQS to Lambda consumer tracing</summary>

SQS-triggered Lambda functions receive the trace header from the SQS message attributes, but the trace is not automatically linked to the producer's trace. This creates a separate trace for the consumer. To correlate them, use the `AWSTraceHeader` message system attribute.

When publishing to SQS from the Order Lambda, the X-Ray SDK (if the AWS SDK is instrumented) automatically injects the `AWSTraceHeader` into the message. On the consumer side, Lambda extracts this header and creates a new segment linked to the original trace.

In Terraform, ensure the SQS event source mapping is configured:

```hcl
resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.consumer.arn
  batch_size       = 1
}
```

The consumer Lambda also needs `tracing_config { mode = "Active" }` and the X-Ray write policy.

</details>

## Spot the Bug

The following Lambda function has X-Ray active tracing enabled in Terraform, and the developer expects to see DynamoDB calls as subsegments in the X-Ray trace. Instead, the DynamoDB call appears as a 200ms gap in the trace timeline with no subsegment detail.

```go
package main

import (
    "context"
    "encoding/json"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-xray-sdk-go/xray"
)

var client *dynamodb.Client

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())
    // BUG: AWS SDK config is NOT instrumented with X-Ray
    client = dynamodb.NewFromConfig(cfg)
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
    ctx, seg := xray.BeginSubsegment(ctx, "write_order")
    defer seg.Close(nil)

    var order map[string]interface{}
    json.Unmarshal([]byte(event.Body), &order)

    // This DynamoDB call will NOT appear as an X-Ray subsegment
    // because the AWS SDK was not instrumented
    client.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String("orders"),
        // ...
    })

    return events.APIGatewayProxyResponse{
        StatusCode: 200,
        Body:       `{"message": "Order created"}`,
    }, nil
}

func main() {
    lambda.Start(handler)
}
```

<details>
<summary>Explain the bug</summary>

The `aws-xray-sdk-go` is imported and `xray.BeginSubsegment` is used for custom subsegments, but the AWS SDK config is never instrumented with `awsv2.AWSV2Instrumentor`. Without instrumentation, the X-Ray SDK has no hook into the AWS SDK client to record DynamoDB calls as subsegments. The `PutItem` call executes successfully, but X-Ray sees it only as a time gap.

The fix is to instrument the AWS SDK config **before** creating clients:

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"
)

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())

    // Instrument the AWS SDK -- must be called before creating clients
    awsv2.AWSV2Instrumentor(&cfg.APIOptions)

    client = dynamodb.NewFromConfig(cfg)
}
```

This is a common exam question: the X-Ray SDK must instrument the AWS SDK to auto-generate subsegments. Simply enabling active tracing on the Lambda function is not sufficient -- that only creates the Lambda segment, not subsegments for downstream calls.

</details>

## Verify What You Learned

After deploying and invoking the API several times, run these verification commands:

```bash
# Verify API Gateway has X-Ray tracing enabled
aws apigateway get-stage --rest-api-id $(terraform output -raw api_id) --stage-name prod \
  --query "tracingEnabled" --output text
```

Expected: `True`

```bash
# Verify Lambda has active tracing
aws lambda get-function-configuration --function-name $(terraform output -raw order_function_name) \
  --query "TracingConfig.Mode" --output text
```

Expected: `Active`

```bash
# Retrieve the service map (shows all nodes and edges)
aws xray get-service-graph \
  --start-time $(date -u -v-10M +%s) \
  --end-time $(date -u +%s) \
  --query "Services[*].Name" --output table
```

Expected: Names including your API Gateway, Lambda functions, DynamoDB, and SQS.

```bash
# Query traces by custom annotation
aws xray get-trace-summaries \
  --start-time $(date -u -v-1H +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'annotation.order_status = "success"' \
  --query "TraceSummaries | length(@)"
```

Expected: A number greater than 0 (matches the number of successful invocations).

```bash
# Verify no infrastructure drift
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

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

You instrumented a multi-service flow with X-Ray to trace requests across API Gateway, Lambda, DynamoDB, and SQS. In the next exercise, you will work directly with **CloudFormation intrinsic functions, rollback behavior, and drift detection** -- core DVA-C02 exam topics that test your understanding of AWS's native infrastructure-as-code service.

## Summary

- **Active tracing** on Lambda creates a segment for each invocation; on API Gateway, it adds a segment for each API request
- The X-Ray SDK for Go must **instrument the AWS SDK** (`awsv2.AWSV2Instrumentor`) before creating clients -- otherwise downstream calls appear as gaps, not subsegments
- **Annotations** are indexed key-value pairs (max 50 per trace) queryable with filter expressions; **metadata** is unindexed and holds debug context
- **Filter expressions** use syntax like `annotation.key = "value"` and `responsetime > N` to query traces
- SQS-to-Lambda traces are linked via the `AWSTraceHeader` message system attribute but appear as **separate trace segments**
- The Lambda execution role needs **AWSXRayDaemonWriteAccess** to send trace data -- without it, traces silently fail to appear
- Custom subsegments (`xray.BeginSubsegment`/`seg.Close`) add granular timing within a Lambda invocation for business logic operations

## Reference

- [AWS X-Ray Developer Guide](https://docs.aws.amazon.com/xray/latest/devguide/aws-xray.html)
- [X-Ray SDK for Go](https://docs.aws.amazon.com/xray/latest/devguide/xray-sdk-go.html)
- [X-Ray Filter Expressions](https://docs.aws.amazon.com/xray/latest/devguide/xray-console-filters.html)
- [Terraform aws_lambda_function tracing_config](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#tracing_config)
- [Terraform aws_api_gateway_stage xray_tracing_enabled](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_stage#xray_tracing_enabled)

## Additional Resources

- [X-Ray Annotations vs Metadata](https://docs.aws.amazon.com/xray/latest/devguide/xray-sdk-go-segment.html) -- when to use each and their limits
- [X-Ray Daemon](https://docs.aws.amazon.com/xray/latest/devguide/xray-daemon.html) -- how the daemon buffers and sends trace data (Lambda includes it automatically)
- [X-Ray Sampling Rules](https://docs.aws.amazon.com/xray/latest/devguide/xray-console-sampling.html) -- controlling trace volume and cost with reservoir and rate settings
- [Tracing SQS Messages with X-Ray](https://docs.aws.amazon.com/xray/latest/devguide/xray-services-sqs.html) -- how AWSTraceHeader propagates across async boundaries

<details>
<summary>Full Solution</summary>

### File Structure

```
10-xray-sdk-instrumentation-service-map/
├── main.tf
├── order_lambda/
│   ├── main.go
│   └── go.mod
└── consumer_lambda/
    ├── main.go
    └── go.mod
```

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
  description = "Project name for resource naming"
  type        = string
  default     = "xray-demo"
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

### `events.tf`

```hcl
resource "aws_sqs_queue" "orders" {
  name                       = "${var.project_name}-order-queue"
  visibility_timeout_seconds = 60
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

# -- Order Lambda Role --
resource "aws_iam_role" "order_lambda" {
  name               = "${var.project_name}-order-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "order_basic" {
  role       = aws_iam_role.order_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "order_xray" {
  role       = aws_iam_role.order_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

data "aws_iam_policy_document" "order_lambda_policy" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:UpdateItem"]
    resources = [aws_dynamodb_table.orders.arn]
  }
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.orders.arn]
  }
}

resource "aws_iam_role_policy" "order_lambda" {
  name   = "order-lambda-policy"
  role   = aws_iam_role.order_lambda.id
  policy = data.aws_iam_policy_document.order_lambda_policy.json
}

# -- Consumer Lambda Role --
resource "aws_iam_role" "consumer_lambda" {
  name               = "${var.project_name}-consumer-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "consumer_basic" {
  role       = aws_iam_role.consumer_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "consumer_xray" {
  role       = aws_iam_role.consumer_lambda.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

data "aws_iam_policy_document" "consumer_lambda_policy" {
  statement {
    actions   = ["dynamodb:UpdateItem"]
    resources = [aws_dynamodb_table.orders.arn]
  }
  statement {
    actions   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"]
    resources = [aws_sqs_queue.orders.arn]
  }
}

resource "aws_iam_role_policy" "consumer_lambda" {
  name   = "consumer-lambda-policy"
  role   = aws_iam_role.consumer_lambda.id
  policy = data.aws_iam_policy_document.consumer_lambda_policy.json
}
```

### `lambda.tf`

```hcl
# -- Order Lambda --
# NOTE: Build the Go binary before applying:
#   cd order_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && zip ../build/order_lambda.zip bootstrap && cd ..
data "archive_file" "order_lambda" {
  type        = "zip"
  source_file = "${path.module}/order_lambda/bootstrap"
  output_path = "${path.module}/build/order_lambda.zip"
}

resource "aws_lambda_function" "order" {
  function_name    = "${var.project_name}-order"
  filename         = data.archive_file.order_lambda.output_path
  source_code_hash = data.archive_file.order_lambda.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  role             = aws_iam_role.order_lambda.arn
  timeout          = 30
  memory_size      = 256

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.orders.name
      QUEUE_URL  = aws_sqs_queue.orders.url
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.order_basic,
    aws_iam_role_policy_attachment.order_xray,
    aws_cloudwatch_log_group.order_lambda
  ]
}

# -- Consumer Lambda --
# NOTE: Build the Go binary before applying:
#   cd consumer_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && zip ../build/consumer_lambda.zip bootstrap && cd ..
data "archive_file" "consumer_lambda" {
  type        = "zip"
  source_file = "${path.module}/consumer_lambda/bootstrap"
  output_path = "${path.module}/build/consumer_lambda.zip"
}

resource "aws_lambda_function" "consumer" {
  function_name    = "${var.project_name}-consumer"
  filename         = data.archive_file.consumer_lambda.output_path
  source_code_hash = data.archive_file.consumer_lambda.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  role             = aws_iam_role.consumer_lambda.arn
  timeout          = 30
  memory_size      = 256

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.orders.name
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.consumer_basic,
    aws_iam_role_policy_attachment.consumer_xray,
    aws_cloudwatch_log_group.consumer_lambda
  ]
}

resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.consumer.arn
  batch_size       = 1
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.order.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "order_lambda" {
  name              = "/aws/lambda/${var.project_name}-order"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "consumer_lambda" {
  name              = "/aws/lambda/${var.project_name}-consumer"
  retention_in_days = 1
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name        = "${var.project_name}-api"
  description = "X-Ray instrumented order API"
}

resource "aws_api_gateway_resource" "orders" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "orders"
}

resource "aws_api_gateway_method" "post_orders" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.orders.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "order_lambda" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.orders.id
  http_method             = aws_api_gateway_method.post_orders.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.order.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  depends_on = [aws_api_gateway_integration.order_lambda]

  lifecycle {
    create_before_destroy = true
  }
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

output "order_function_name" {
  value = aws_lambda_function.order.function_name
}

output "consumer_function_name" {
  value = aws_lambda_function.consumer.function_name
}

output "table_name" {
  value = aws_dynamodb_table.orders.name
}

output "queue_url" {
  value = aws_sqs_queue.orders.url
}
```

### `order_lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"
	"github.com/aws/aws-xray-sdk-go/xray"
	"github.com/google/uuid"
)

var (
	dynamoClient *dynamodb.Client
	sqsClient    *sqs.Client
	tableName    string
	queueURL     string
)

func init() {
	tableName = os.Getenv("TABLE_NAME")
	queueURL = os.Getenv("QUEUE_URL")

	cfg, _ := config.LoadDefaultConfig(context.TODO())

	// Instrument the AWS SDK -- must be called before creating clients
	awsv2.AWSV2Instrumentor(&cfg.APIOptions)

	dynamoClient = dynamodb.NewFromConfig(cfg)
	sqsClient = sqs.NewFromConfig(cfg)
}

type OrderRequest struct {
	Tier  string   `json:"tier"`
	Items []string `json:"items"`
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var body OrderRequest
	if err := json.Unmarshal([]byte(event.Body), &body); err != nil {
		body = OrderRequest{Tier: "standard"}
	}

	orderID := uuid.New().String()
	customerTier := body.Tier
	if customerTier == "" {
		customerTier = "standard"
	}

	// Custom subsegment for business logic
	ctx, seg := xray.BeginSubsegment(ctx, "process_order")

	_ = xray.AddAnnotation(ctx, "order_status", "success")
	_ = xray.AddAnnotation(ctx, "customer_tier", customerTier)
	_ = xray.AddMetadata(ctx, "order_payload", body)

	// Write to DynamoDB (automatically traced via awsv2 instrumentation)
	itemsJSON, _ := json.Marshal(body.Items)
	_, err := dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]dynamodbtypes.AttributeValue{
			"order_id":      &dynamodbtypes.AttributeValueMemberS{Value: orderID},
			"customer_tier": &dynamodbtypes.AttributeValueMemberS{Value: customerTier},
			"status":        &dynamodbtypes.AttributeValueMemberS{Value: "received"},
			"items":         &dynamodbtypes.AttributeValueMemberS{Value: string(itemsJSON)},
		},
	})
	if err != nil {
		_ = xray.AddAnnotation(ctx, "order_status", "failed")
		seg.Close(err)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: string(resp)}, nil
	}

	// Publish to SQS (automatically traced via awsv2 instrumentation)
	msgBody, _ := json.Marshal(map[string]string{"order_id": orderID})
	_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(msgBody)),
	})
	if err != nil {
		_ = xray.AddAnnotation(ctx, "order_status", "failed")
		seg.Close(err)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: string(resp)}, nil
	}

	seg.Close(nil)

	resp, _ := json.Marshal(map[string]string{
		"message":  "Order created",
		"order_id": orderID,
	})
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(resp),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `consumer_lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"
	"github.com/aws/aws-xray-sdk-go/xray"
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

func handler(ctx context.Context, event events.SQSEvent) error {
	for _, record := range event.Records {
		var message struct {
			OrderID string `json:"order_id"`
		}
		if err := json.Unmarshal([]byte(record.Body), &message); err != nil {
			return fmt.Errorf("failed to unmarshal message: %w", err)
		}

		ctx, seg := xray.BeginSubsegment(ctx, "update_order_status")
		_ = xray.AddAnnotation(ctx, "order_id", message.OrderID)

		_, err := dynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(tableName),
			Key: map[string]dynamodbtypes.AttributeValue{
				"order_id": &dynamodbtypes.AttributeValueMemberS{Value: message.OrderID},
			},
			UpdateExpression:          aws.String("SET #s = :status"),
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]dynamodbtypes.AttributeValue{":status": &dynamodbtypes.AttributeValueMemberS{Value: "processed"}},
		})

		seg.Close(err)
		if err != nil {
			return fmt.Errorf("failed to update order %s: %w", message.OrderID, err)
		}
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

### Testing

```bash
# Build the Go binaries
cd order_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
cd consumer_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..

# Deploy
terraform init && terraform apply -auto-approve

# Send test orders
API_URL=$(terraform output -raw api_url)

curl -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{"tier": "premium", "items": ["widget-a", "widget-b"]}'

curl -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{"tier": "standard", "items": ["gadget-x"]}'

# Wait a minute for traces to propagate, then query
sleep 60

# Check the service map
aws xray get-service-graph \
  --start-time $(date -u -v-10M +%s) \
  --end-time $(date -u +%s) \
  --query "Services[*].{Name:Name,Type:Type}" --output table

# Query by annotation
aws xray get-trace-summaries \
  --start-time $(date -u -v-1H +%s) \
  --end-time $(date -u +%s) \
  --filter-expression 'annotation.customer_tier = "premium"'
```

</details>
