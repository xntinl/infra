# 14. Event-Driven Order Processing System

<!--
difficulty: insane
concepts: [event-driven-architecture, api-gateway, lambda, dynamodb, sqs, sns, eventbridge, xray, dlq, structured-logging]
tools: [terraform, aws-cli]
estimated_time: 120m
bloom_level: create
prerequisites: [dva-01 through dva-12]
aws_cost: ~$0.05/hr
-->

## Prerequisites

| Prerequisite | Why |
|---|---|
| Exercise 1: Lambda Environment, Layers, Configuration | Lambda configuration, environment variables, layers |
| Exercise 2: API Gateway REST vs HTTP, Validation | REST API integration, request validation |
| Exercise 3: DynamoDB Developer SDK Operations | Table design, conditional writes, SDK operations |
| Exercise 5: Lambda Error Handling, Retry, DLQ | DLQ configuration, retry behavior, error handling patterns |
| Exercise 7: Parameter Store and AppConfig | Runtime configuration, feature flags |
| Exercise 8: SQS Lambda Concurrency Throttling | SQS event source mappings, concurrency, throttling |
| Exercise 10: X-Ray SDK Instrumentation | Active tracing, annotations, subsegments, service maps |
| Exercise 12: DynamoDB Streams Lambda Trigger Patterns | Stream processing, filter criteria, idempotent writes |

## The Scenario

Your company is migrating from a monolithic order processing system to a serverless event-driven architecture. The current system handles 50,000 orders per day with peaks during flash sales that can reach 10x normal volume. The existing system suffers from cascading failures: when the payment processor slows down, the entire application becomes unresponsive because order intake, validation, payment, and notification all run in the same process.

Your architecture must decompose the monolith into independent stages connected by asynchronous messaging. An API Gateway endpoint receives order submissions from the frontend. A validation Lambda checks inventory, writes the order to DynamoDB, and publishes an event. An SQS queue buffers payment processing requests to absorb traffic spikes. A payment Lambda processes payments at a controlled rate using reserved concurrency. When payment succeeds or fails, an EventBridge event triggers notification routing: SNS for customer emails, and a separate Lambda for analytics and order history updates. Every asynchronous boundary must have a dead-letter queue to capture failed messages.

The CTO requires full observability from day one. Every Lambda must have X-Ray active tracing with custom annotations for order ID and customer tier. All functions must emit structured JSON logs with a correlation ID that propagates across the entire order lifecycle. The team must be able to query X-Ray for all failed orders from premium customers and trace any single order from API submission through payment completion. This is not optional -- the last outage took 4 hours to diagnose because there was no tracing, and the postmortem mandated observability as a deployment prerequisite.

## Constraints

1. All infrastructure must be defined in Terraform. No manual Console configuration.
2. Every SQS queue must have a dead-letter queue with `maxReceiveCount = 3`.
3. Every Lambda must have X-Ray active tracing enabled (`mode = "Active"`).
4. Every Lambda must emit structured JSON logs (not `print()` statements) with a `correlation_id` field that originates at the API Gateway request and propagates through all downstream services.
5. The payment processing Lambda must use reserved concurrency (limit of 5) to protect the downstream payment API from overload.
6. DynamoDB writes must be idempotent using `ConditionExpression` with `attribute_not_exists` to prevent duplicate orders.
7. EventBridge rules must use event patterns that match on `detail-type` and `source` fields, not catch-all patterns.
8. X-Ray annotations must include `order_id`, `customer_tier`, and `order_status` on every Lambda invocation to support filter expression queries.
9. The API Gateway must use a request model to validate the order payload structure before reaching the Lambda function.
10. All Lambda functions must use a shared Lambda Layer for common utilities (structured logging, correlation ID propagation, X-Ray helpers).

## Success Criteria

- A POST to the API Gateway `/orders` endpoint with a valid order payload returns a 200 response with the order ID within 2 seconds
- A POST with an invalid payload (missing required fields) returns a 400 response from API Gateway before reaching the Lambda function
- An order progresses through statuses: `received` -> `payment_pending` -> `paid` (or `payment_failed`) -> `notified`
- Each status transition is visible as a separate DynamoDB Streams event in the event log table
- The X-Ray service map shows all services: API Gateway, validation Lambda, DynamoDB, SQS, payment Lambda, EventBridge, notification Lambda
- Querying X-Ray with `annotation.order_status = "payment_failed"` returns traces for failed payments only
- Sending 20 orders in rapid succession does not cause the payment Lambda to exceed 5 concurrent executions (SQS absorbs the burst)
- Messages that fail processing 3 times appear in the corresponding dead-letter queue, not lost
- CloudWatch Logs Insights query `filter correlation_id = "abc-123"` returns log entries from all Lambdas that processed that order
- `terraform plan` shows no changes after deployment (no drift)

## Verification Commands

```bash
API_URL=$(terraform output -raw api_url)

# Valid order (expect 200 with order_id)
curl -s -X POST "$API_URL/orders" -H "Content-Type: application/json" \
  -d '{"customer_id":"cust-001","tier":"premium","items":[{"sku":"WIDGET-A","qty":2}],"total":49.99}' | jq .

# Invalid order (expect 400 from API GW validation)
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$API_URL/orders" \
  -H "Content-Type: application/json" -d '{"invalid":"payload"}'

# DLQ should be empty
aws sqs get-queue-attributes --queue-url $(terraform output -raw payment_dlq_url) \
  --attribute-names ApproximateNumberOfMessages --output text

# X-Ray service map shows all services
aws xray get-service-graph --start-time $(date -u -v-10M +%s) --end-time $(date -u +%s) \
  --query "Services[*].Name" --output table

# X-Ray filter by annotation
aws xray get-trace-summaries --start-time $(date -u -v-1H +%s) --end-time $(date -u +%s) \
  --filter-expression 'annotation.customer_tier = "premium"' --query "TraceSummaries | length(@)"

# Reserved concurrency on payment Lambda
aws lambda get-function --function-name $(terraform output -raw payment_function) \
  --query "Concurrency" --output json

terraform plan
```

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```
