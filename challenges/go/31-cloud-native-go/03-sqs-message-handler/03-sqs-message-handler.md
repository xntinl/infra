# 3. SQS Message Handler

<!--
difficulty: advanced
concepts: [sqs-event, batch-processing, visibility-timeout, partial-failure, dead-letter-queue]
tools: [go, aws-cli]
estimated_time: 35m
bloom_level: analyze
prerequisites: [lambda-handler-patterns, error-handling, json-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Lambda Handler Patterns exercise
- Understanding of message queues (producer/consumer pattern)
- Familiarity with JSON unmarshalling and error handling

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a Lambda handler that processes SQS event batches
- **Analyze** partial batch failure reporting using `SQSBatchResponse`
- **Design** handlers that correctly handle visibility timeout implications
- **Test** SQS event processing with fabricated event payloads

## Why SQS Message Handling Matters

SQS-triggered Lambda functions receive batches of messages. If your handler returns an error, Lambda treats the entire batch as failed and all messages become visible again for reprocessing. This is wasteful when only one message in a batch of 10 failed.

AWS provides partial batch failure reporting via `ReportBatchItemFailures`. Your handler returns a list of failed message IDs, and Lambda only retries those specific messages. Getting this right is essential for building reliable, efficient event-driven systems.

## The Problem

Build a Lambda handler that processes SQS messages containing order events. Each message body is JSON with an order ID and amount. The handler must:

1. Process each message in the batch independently
2. Report partial failures so only failed messages are retried
3. Handle malformed messages gracefully
4. Log processing results per message

## Requirements

1. **Handler signature** -- `func(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error)`
2. **Message processing** -- parse each `SQSMessage.Body` as JSON into an `Order` struct with `ID string` and `Amount float64`
3. **Validation** -- reject orders with `Amount <= 0` by adding their `MessageId` to the batch item failures
4. **Partial failure** -- return `events.SQSEventResponse` with `BatchItemFailures` containing only the failed message IDs
5. **Test coverage** -- test with a batch containing valid, invalid, and malformed messages

## Hints

<details>
<summary>Hint 1: Order struct and parsing</summary>

```go
type Order struct {
    ID     string  `json:"id"`
    Amount float64 `json:"amount"`
}

func parseOrder(body string) (Order, error) {
    var o Order
    err := json.Unmarshal([]byte(body), &o)
    return o, err
}
```

</details>

<details>
<summary>Hint 2: Partial batch failure response</summary>

```go
func handleRequest(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
    var failures []events.SQSBatchItemFailure

    for _, record := range event.Records {
        if err := processMessage(record); err != nil {
            failures = append(failures, events.SQSBatchItemFailure{
                ItemIdentifier: record.MessageId,
            })
        }
    }

    return events.SQSEventResponse{BatchItemFailures: failures}, nil
}
```

</details>

<details>
<summary>Hint 3: Enable partial batch in AWS</summary>

For partial batch failures to work, you must configure the Lambda event source mapping with `FunctionResponseTypes: ["ReportBatchItemFailures"]`. Without this, Lambda ignores the response and retries the whole batch on any error.

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- A batch of 3 valid orders returns zero failures
- A batch with 1 invalid order (amount <= 0) returns exactly 1 failure with the correct message ID
- A batch with a malformed JSON body returns that message as a failure
- The handler never returns a top-level error (partial failures are reported via the response)

## What's Next

Continue to [04 - EventBridge Event Routing](../04-eventbridge-event-routing/04-eventbridge-event-routing.md) to learn how to handle EventBridge events in Lambda.

## Summary

- SQS-triggered Lambda functions receive batches of messages in `events.SQSEvent`
- Returning a top-level error retries the entire batch; use `SQSEventResponse.BatchItemFailures` for partial failures
- Always process each message independently and collect failures
- Malformed messages should be reported as failures (and eventually sent to a DLQ), not cause the handler to crash
- Test SQS handlers by fabricating `events.SQSEvent` structs with synthetic message data

## Reference

- [events.SQSEvent](https://pkg.go.dev/github.com/aws/aws-lambda-go/events#SQSEvent)
- [Reporting batch item failures](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html#services-sqs-batchfailurereporting)
- [SQS Lambda integration](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html)
