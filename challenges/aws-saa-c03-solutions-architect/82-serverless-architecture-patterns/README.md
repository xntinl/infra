# 82. Serverless Architecture Patterns

<!--
difficulty: advanced
concepts: [serverless-patterns, synchronous-pattern, asynchronous-pattern, streaming-pattern, orchestration-pattern, cold-starts, concurrency-limits, reserved-concurrency, provisioned-concurrency, cost-modeling, lambda-destinations, saga-pattern, fan-out-fan-in]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate, create
prerequisites: [77-lambda-event-sources-patterns, 78-api-gateway-rest-http-websocket, 79-step-functions-workflow-orchestration, 81-eventbridge-event-driven-architecture]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** All services used are within free tier for this exercise (Lambda, API Gateway HTTP, SQS, DynamoDB on-demand, Step Functions, EventBridge). Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercises 77-81 | Understanding of Lambda, API GW, Step Functions, EventBridge |
| Go 1.21+ installed | `go version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Evaluate** which serverless pattern (synchronous, asynchronous, streaming, orchestration) best fits a given use case.
2. **Design** complete serverless architectures that address cold starts, concurrency limits, and cost constraints.
3. **Analyze** the cost implications of each pattern at different scale points.
4. **Create** a multi-pattern architecture that combines synchronous request-response with asynchronous background processing.
5. **Assess** failure modes and design appropriate error handling, retry strategies, and dead-letter queues for each pattern.

---

## Why This Matters

Serverless architecture pattern selection is the capstone SAA-C03 topic for the compute domain. The exam does not ask you to build serverless applications, but it presents scenarios with specific requirements -- latency, throughput, cost, reliability -- and asks which pattern to use. The wrong pattern creates real problems: using synchronous patterns for batch processing wastes money on idle connections; using asynchronous patterns for user-facing requests creates poor UX with no immediate response.

The four fundamental patterns appear repeatedly:

**Synchronous (request-response):** API Gateway -> Lambda -> DynamoDB. The client sends a request and waits for the response. Use for user-facing APIs where immediate response is required. Limitation: Lambda cold starts add latency (100ms-3s depending on runtime and VPC), and the maximum timeout is 29 seconds for API Gateway integration.

**Asynchronous (fire-and-forget):** S3 -> Lambda -> SQS/DynamoDB. The producer sends an event and does not wait. Lambda processes it in the background. Use for file processing, image thumbnailing, ETL ingestion. Limitation: no immediate response to the producer; requires a separate mechanism (WebSocket, polling) for status updates.

**Streaming (continuous processing):** Kinesis/DynamoDB Streams -> Lambda. Lambda continuously polls for new records. Use for real-time analytics, change data capture, log aggregation. Limitation: ordering is per-shard, and a failed batch blocks the shard.

**Orchestration (workflow):** Step Functions coordinating multiple Lambda functions. Use for multi-step business processes, saga patterns, human approval workflows. Limitation: Standard workflow state transitions are more expensive than direct Lambda invocations.

The exam tests one critical anti-pattern: using the synchronous pattern for batch processing. If a client uploads a 10 GB file and you process it synchronously in a Lambda behind API Gateway, the request will timeout at 29 seconds. The correct pattern is: the client uploads to S3, S3 triggers Lambda asynchronously, Lambda processes the file and writes results to DynamoDB, the client polls for completion or receives a WebSocket notification.

---

## The Challenge

You are designing the backend for an e-commerce platform. The platform has four distinct workloads, each requiring a different serverless pattern. Design and implement all four:

### Requirements

1. **Product catalog API** (Synchronous): REST API for browsing products. Requirements: sub-200ms p99 latency, 1000 req/sec peak.

2. **Order processing** (Asynchronous): Process new orders after submission. Requirements: reliable processing with no order loss, eventual consistency acceptable.

3. **Inventory updates** (Streaming): Track inventory changes in real-time from DynamoDB Streams. Requirements: ordered processing per product, exactly-once semantics.

4. **Order fulfillment** (Orchestration): Multi-step workflow: validate -> charge -> reserve inventory -> ship -> notify. Requirements: exactly-once execution, rollback on failure (saga pattern).

### Architecture

```
Pattern 1 - Synchronous:
  Client -> HTTP API -> Lambda -> DynamoDB
                                    |
Pattern 3 - Streaming:              |
  DynamoDB Streams -> Lambda -> CloudWatch Metrics

Pattern 2 - Asynchronous:
  Client -> HTTP API -> SQS -> Lambda -> DynamoDB
         (returns 202)

Pattern 4 - Orchestration:
  EventBridge -> Step Functions -> [Lambda, Lambda, Lambda]
                                        |
                                   (on failure) -> Compensating actions
```

---

## Hints

<details>
<summary>Hint 1: Synchronous Pattern -- Cold Start Mitigation</summary>

For the product catalog API requiring sub-200ms p99 latency, cold starts are the main concern. Strategies:

1. **Provisioned concurrency** (eliminate cold starts entirely):
   ```hcl
   resource "aws_lambda_provisioned_concurrency_config" "catalog" {
     function_name                  = aws_lambda_function.catalog.function_name
     qualifier                      = aws_lambda_alias.live.name
     provisioned_concurrent_executions = 5
   }
   ```
   Cost: you pay for provisioned instances even when idle. Use for latency-critical, predictable traffic.

2. **SnapStart** (Java/JVM only): caches initialized snapshots to reduce cold start from ~3s to ~200ms.

3. **Keep functions warm** (not recommended): scheduled invocation keeps instances warm but does not guarantee coverage during traffic spikes.

4. **Minimize package size**: smaller deployment packages initialize faster. Go binaries on provided.al2023 have the fastest cold starts (~100ms).

</details>

<details>
<summary>Hint 2: Asynchronous Pattern -- SQS as Buffer</summary>

For order processing, SQS provides the durability and decoupling layer:

```
Client -> API GW -> Lambda (accept) -> SQS -> Lambda (process) -> DynamoDB
       <- 202 Accepted                  ^
                                        |
                                    DLQ (failed orders)
```

The "accept" Lambda validates the order and enqueues it to SQS, returning 202 immediately. The "process" Lambda reads from SQS via event source mapping. This pattern provides:

- **Durability**: SQS retains messages for up to 14 days
- **Rate limiting**: Lambda concurrency controls processing rate
- **Error isolation**: failed orders go to DLQ, not lost
- **Backpressure**: SQS absorbs traffic spikes without throttling the client

</details>

<details>
<summary>Hint 3: Streaming Pattern -- DynamoDB Streams + Lambda</summary>

DynamoDB Streams captures every change to the table. Lambda processes changes in order per partition key:

```hcl
resource "aws_lambda_event_source_mapping" "streams" {
  event_source_arn  = aws_dynamodb_table.this.stream_arn
  function_name     = aws_lambda_function.stream_processor.arn
  starting_position = "LATEST"
  batch_size        = 100

  bisect_batch_on_function_error = true
  maximum_retry_attempts         = 3

  destination_config {
    on_failure {
      destination_arn = aws_sqs_queue.stream_dlq.arn
    }
  }
}
```

Key settings:
- `bisect_batch_on_function_error`: splits a failed batch in half and retries each half separately, isolating the poisonous record
- `maximum_retry_attempts`: limits retries to prevent infinite blocking of the shard
- `destination_config.on_failure`: sends failed records to DLQ after retry exhaustion

</details>

<details>
<summary>Hint 4: Orchestration Pattern -- Saga with Compensation</summary>

The saga pattern handles failures in multi-step workflows by executing compensating actions:

```json
{
  "ProcessPayment": {
    "Type": "Task",
    "Resource": "arn:aws:lambda:...:charge-payment",
    "Catch": [{
      "ErrorEquals": ["States.ALL"],
      "Next": "RefundPayment"
    }],
    "Next": "ReserveInventory"
  },
  "ReserveInventory": {
    "Type": "Task",
    "Resource": "arn:aws:lambda:...:reserve-inventory",
    "Catch": [{
      "ErrorEquals": ["States.ALL"],
      "Next": "ReleaseInventoryAndRefund"
    }],
    "Next": "ShipOrder"
  }
}
```

Each step's Catch block triggers the compensating actions for all previously completed steps. This is the saga pattern -- not full ACID transactions, but eventual consistency with guaranteed compensation.

</details>

<details>
<summary>Hint 5: Cost Modeling</summary>

Cost comparison at 1 million requests/month:

| Pattern | Component | Cost |
|---|---|---|
| **Synchronous** | HTTP API: 1M x $1/M = $1.00 | |
| | Lambda: 1M x 200ms x 128MB = 25,000 GB-s = $0.42 | |
| | DynamoDB reads: 1M x $0.25/M = $0.25 | |
| | **Total: ~$1.67/month** | |
| **Async (SQS)** | HTTP API: 1M x $1/M = $1.00 | |
| | SQS: 2M messages (send+receive) = free tier | |
| | Lambda (2x): $0.84 | |
| | DynamoDB writes: 1M x $1.25/M = $1.25 | |
| | **Total: ~$3.09/month** | |

Asynchronous costs more per request but provides reliability guarantees. The reliability premium is worth it for orders (losing a $100 order costs more than the $0.000002 SQS message).

</details>

---

## Spot the Bug

The following architecture uses the synchronous pattern for batch image processing. Identify the fundamental design flaw.

```
User uploads 500 images via web form
    |
    v
API Gateway (REST API, 29s timeout)
    |
    v
Lambda function (15 min timeout)
    - Downloads all 500 images from S3
    - Resizes each image
    - Uploads thumbnails to S3
    - Returns list of thumbnail URLs
    |
    v
User receives response with all 500 thumbnail URLs
```

<details>
<summary>Explain the bug</summary>

**The synchronous pattern is fundamentally wrong for batch processing.** API Gateway has a maximum integration timeout of 29 seconds. Processing 500 images takes much longer than 29 seconds. The API Gateway timeout will expire, returning a 504 Gateway Timeout to the user, while the Lambda function continues executing (wasting resources since nobody will receive the response).

Even if you increase Lambda's timeout to 15 minutes, the API Gateway 29-second limit is a hard constraint. The Lambda finishes processing but the connection to the client is already dead.

**Fix: Use the asynchronous pattern:**

```
User uploads 500 images via web form
    |
    v
API Gateway -> Lambda (accept)
    - Creates a job record in DynamoDB (status: "processing")
    - Sends messages to SQS (one per image, or batches)
    - Returns 202 Accepted with job ID immediately
    |
    v
SQS -> Lambda (process)
    - Processes images one at a time (or in small batches)
    - Updates DynamoDB with each completed thumbnail
    - When all done, updates job status to "complete"

User polls GET /jobs/{jobId} for status
    OR
User receives WebSocket notification when complete
```

This pattern:
1. Returns immediately (202 Accepted) -- no timeout issues
2. Processes images individually -- if one fails, others succeed
3. Scales horizontally -- multiple Lambda instances process concurrently
4. Provides progress visibility -- each completed image updates the job record
5. Handles failures gracefully -- failed images go to DLQ for retry

**Key principle:** If the processing time exceeds 29 seconds, you cannot use the synchronous API Gateway -> Lambda pattern. Switch to asynchronous.

</details>

---

## Verify What You Learned

After implementing all four patterns, verify each one:

```bash
# 1. Synchronous: Query the product catalog
CATALOG_URL=$(terraform output -raw catalog_api_url)
curl -s "${CATALOG_URL}/products" | jq .
```

Expected: List of products from DynamoDB with sub-second response.

```bash
# 2. Asynchronous: Submit an order
ORDER_URL=$(terraform output -raw order_api_url)
curl -s -X POST "${ORDER_URL}/orders" \
  -H "Content-Type: application/json" \
  -d '{"product_id":"P-001","quantity":2,"amount":49.99}' | jq .
```

Expected: 202 response with order ID and status "processing."

```bash
# 3. Streaming: Write to DynamoDB and verify stream processing
aws dynamodb put-item \
  --table-name saa-ex82-products \
  --item '{"id":{"S":"P-TEST"},"name":{"S":"Stream Test"},"price":{"N":"9.99"},"stock":{"N":"100"}}'
sleep 5
aws logs tail "/aws/lambda/saa-ex82-stream-processor" --since 2m --format short
```

Expected: Log showing DynamoDB Stream event processed (INSERT event for P-TEST).

```bash
# 4. Orchestration: Start a fulfillment workflow
SFN_ARN=$(terraform output -raw fulfillment_state_machine_arn)
aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id":"ORD-001","amount":49.99,"product_id":"P-001","quantity":2}'
```

Expected: Execution ARN returned. Check status after a few seconds to verify SUCCEEDED.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Full Solution

<details>
<summary>Complete multi-pattern architecture</summary>

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
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
  default     = "saa-ex82"
}
```

### `iam.tf`

```hcl
resource "aws_iam_role" "lambda" {
  name = "${var.project_name}-lambda-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "lambda_dynamodb" {
  name = "${var.project_name}-dynamodb"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Scan",
                  "dynamodb:Query", "dynamodb:UpdateItem",
                  "dynamodb:DescribeStream", "dynamodb:GetRecords",
                  "dynamodb:GetShardIterator", "dynamodb:ListStreams"]
      Resource = ["${aws_dynamodb_table.products.arn}",
                  "${aws_dynamodb_table.products.arn}/stream/*",
                  "${aws_dynamodb_table.orders.arn}"]
    }]
  })
}

resource "aws_iam_role_policy" "lambda_sqs" {
  name = "${var.project_name}-sqs"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["sqs:SendMessage", "sqs:ReceiveMessage",
                  "sqs:DeleteMessage", "sqs:GetQueueAttributes"]
      Resource = [aws_sqs_queue.orders.arn, aws_sqs_queue.orders_dlq.arn]
    }]
  })
}

resource "aws_iam_role" "sfn" {
  name = "${var.project_name}-sfn-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "states.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "sfn_lambda" {
  name = "${var.project_name}-sfn-lambda"
  role = aws_iam_role.sfn.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.handler.arn
    }]
  })
}
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "handler" {
  function_name    = "${var.project_name}-handler"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = "function.zip"
  source_code_hash = filebase64sha256("function.zip")
  timeout          = 30
  memory_size      = 128
}

resource "aws_lambda_function" "stream_processor" {
  function_name    = "${var.project_name}-stream-processor"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = "function.zip"
  source_code_hash = filebase64sha256("function.zip")
  timeout          = 60
  memory_size      = 128
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "products" {
  name             = "${var.project_name}-products"
  billing_mode     = "PAY_PER_REQUEST"
  hash_key         = "id"
  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  attribute {
    name = "id"
    type = "S"
  }
}

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

### `api.tf`

```hcl
# Pattern 1: Synchronous (Product Catalog)
resource "aws_apigatewayv2_api" "catalog" {
  name          = "${var.project_name}-catalog"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "catalog" {
  api_id                 = aws_apigatewayv2_api.catalog.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.handler.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "catalog" {
  api_id    = aws_apigatewayv2_api.catalog.id
  route_key = "GET /products"
  target    = "integrations/${aws_apigatewayv2_integration.catalog.id}"
}

resource "aws_apigatewayv2_stage" "catalog" {
  api_id      = aws_apigatewayv2_api.catalog.id
  name        = "$default"
  auto_deploy = true
}

resource "aws_lambda_permission" "catalog" {
  statement_id  = "AllowCatalogAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.handler.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.catalog.execution_arn}/*/*"
}

# Pattern 2: Asynchronous (Order Processing)
resource "aws_apigatewayv2_api" "orders" {
  name          = "${var.project_name}-orders"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "orders" {
  api_id                 = aws_apigatewayv2_api.orders.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.handler.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "orders" {
  api_id    = aws_apigatewayv2_api.orders.id
  route_key = "POST /orders"
  target    = "integrations/${aws_apigatewayv2_integration.orders.id}"
}

resource "aws_apigatewayv2_stage" "orders" {
  api_id      = aws_apigatewayv2_api.orders.id
  name        = "$default"
  auto_deploy = true
}

resource "aws_lambda_permission" "orders" {
  statement_id  = "AllowOrdersAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.handler.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.orders.execution_arn}/*/*"
}
```

### `events.tf`

```hcl
# Pattern 2: Asynchronous -- SQS queues and event source mapping
resource "aws_sqs_queue" "orders_dlq" {
  name = "${var.project_name}-orders-dlq"
}

resource "aws_sqs_queue" "orders" {
  name                       = "${var.project_name}-orders"
  visibility_timeout_seconds = 360
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.orders_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_lambda_event_source_mapping" "orders" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.handler.arn
  batch_size       = 10
  enabled          = true
}

# Pattern 3: Streaming -- DynamoDB Streams event source mapping
resource "aws_lambda_event_source_mapping" "streams" {
  event_source_arn  = aws_dynamodb_table.products.stream_arn
  function_name     = aws_lambda_function.stream_processor.arn
  starting_position = "LATEST"
  batch_size        = 100

  bisect_batch_on_function_error = true
  maximum_retry_attempts         = 3

  destination_config {
    on_failure {
      destination_arn = aws_sqs_queue.orders_dlq.arn
    }
  }
}
```

### `workflow.tf`

```hcl
# Pattern 4: Orchestration -- Step Functions saga
resource "aws_sfn_state_machine" "fulfillment" {
  name     = "${var.project_name}-fulfillment"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"

  definition = jsonencode({
    Comment = "Order fulfillment saga"
    StartAt = "ValidateOrder"
    States = {
      ValidateOrder = {
        Type     = "Task"
        Resource = aws_lambda_function.handler.arn
        Next     = "ProcessPayment"
        Retry = [{
          ErrorEquals     = ["Lambda.ServiceException"]
          IntervalSeconds = 2
          MaxAttempts     = 2
          BackoffRate     = 2.0
        }]
        Catch = [{
          ErrorEquals = ["States.ALL"]
          Next        = "OrderFailed"
          ResultPath  = "$.error"
        }]
      }
      ProcessPayment = {
        Type     = "Task"
        Resource = aws_lambda_function.handler.arn
        Next     = "FulfillOrder"
        Catch = [{
          ErrorEquals = ["States.ALL"]
          Next        = "OrderFailed"
          ResultPath  = "$.error"
        }]
      }
      FulfillOrder = {
        Type = "Parallel"
        Branches = [
          {
            StartAt = "ReserveInventory"
            States = {
              ReserveInventory = {
                Type     = "Task"
                Resource = aws_lambda_function.handler.arn
                End      = true
              }
            }
          },
          {
            StartAt = "NotifyCustomer"
            States = {
              NotifyCustomer = {
                Type     = "Task"
                Resource = aws_lambda_function.handler.arn
                End      = true
              }
            }
          }
        ]
        Next = "OrderComplete"
        Catch = [{
          ErrorEquals = ["States.ALL"]
          Next        = "CompensatePayment"
          ResultPath  = "$.error"
        }]
      }
      CompensatePayment = {
        Type     = "Task"
        Resource = aws_lambda_function.handler.arn
        Next     = "OrderFailed"
      }
      OrderComplete = { Type = "Succeed" }
      OrderFailed   = { Type = "Fail", Error = "OrderFailed", Cause = "Order processing failed" }
    }
  })

  tags = { Name = "${var.project_name}-fulfillment" }
}
```

### `outputs.tf`

```hcl
output "catalog_api_url" {
  value = aws_apigatewayv2_api.catalog.api_endpoint
}

output "order_api_url" {
  value = aws_apigatewayv2_api.orders.api_endpoint
}

output "fulfillment_state_machine_arn" {
  value = aws_sfn_state_machine.fulfillment.arn
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws lambda list-functions --query 'Functions[?starts_with(FunctionName,`saa-ex82`)].FunctionName'
aws stepfunctions list-state-machines --query 'stateMachines[?starts_with(name,`saa-ex82`)].name'
```

Expected: Empty lists.

---

## What's Next

Exercise 83 begins the **Storage** section with **EFS vs FSx decision framework**. You will compare Elastic File System (NFS for Linux), FSx for Windows (SMB), FSx for Lustre (HPC), and FSx for NetApp ONTAP (multi-protocol) -- understanding when each file system type is the right choice based on protocol, OS, performance, and cost requirements.

---

## Summary

- **Synchronous pattern** (API GW -> Lambda -> DB): use for user-facing APIs requiring immediate responses; constrained by API Gateway's 29-second timeout
- **Asynchronous pattern** (API GW -> SQS -> Lambda): use for background processing; returns 202 immediately, processes reliably via SQS
- **Streaming pattern** (DynamoDB Streams -> Lambda): use for real-time change data capture; ordered per partition key, blocked by failed batches
- **Orchestration pattern** (Step Functions): use for multi-step workflows; saga pattern with compensating actions for failure handling
- **API Gateway 29-second timeout** is the critical constraint -- anything longer must use asynchronous pattern
- **Cold starts** affect synchronous latency: Go/provided.al2023 (~100ms), Python (~200ms), Java (~3s without SnapStart)
- **Provisioned concurrency** eliminates cold starts but costs money for idle instances
- **SQS visibility timeout** >= 6x Lambda timeout to prevent duplicate processing
- **bisect_batch_on_function_error** for DynamoDB Streams isolates poisonous records without blocking the entire shard
- **Saga pattern** in Step Functions: each step's Catch triggers compensating actions for previously completed steps
- **Cost modeling**: synchronous is cheapest per request; asynchronous adds SQS cost but provides durability; orchestration adds per-transition cost but provides visibility

## Reference

- [Lambda Best Practices](https://docs.aws.amazon.com/lambda/latest/dg/best-practices.html)
- [Serverless Application Lens - Well-Architected](https://docs.aws.amazon.com/wellarchitected/latest/serverless-applications-lens/welcome.html)
- [Step Functions Saga Pattern](https://docs.aws.amazon.com/prescriptive-guidance/latest/patterns/implement-the-serverless-saga-pattern-by-using-aws-step-functions.html)
- [Lambda Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/configuration-concurrency.html)

## Additional Resources

- [Provisioned Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/provisioned-concurrency.html) -- pre-warming Lambda instances for latency-sensitive workloads
- [Lambda Power Tuning](https://github.com/alexcasalboni/aws-lambda-power-tuning) -- find the optimal memory/cost configuration for your functions
- [EventBridge Pipes](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes.html) -- point-to-point integrations without Lambda glue code
- [Serverless Patterns Collection](https://serverlessland.com/patterns) -- community-maintained catalog of serverless architecture patterns
