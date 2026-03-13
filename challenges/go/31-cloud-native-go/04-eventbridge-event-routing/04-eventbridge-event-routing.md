# 4. EventBridge Event Routing

<!--
difficulty: advanced
concepts: [eventbridge, event-pattern, detail-type, source, cloud-events]
tools: [go, aws-cli]
estimated_time: 35m
bloom_level: analyze
prerequisites: [lambda-handler-patterns, json-encoding, interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Lambda Handler Patterns exercise
- Understanding of JSON encoding and interfaces
- Familiarity with event-driven architecture concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a Lambda handler that receives EventBridge events
- **Analyze** event structure including `source`, `detail-type`, and `detail` fields
- **Design** a routing pattern that dispatches events to different handlers based on their type
- **Test** EventBridge event processing with crafted event payloads

## Why EventBridge Event Routing Matters

EventBridge is the backbone of event-driven architectures on AWS. Unlike SQS where you process raw messages, EventBridge events have a structured envelope with metadata (`source`, `detail-type`, `time`, `region`) and a `detail` field containing the actual payload.

A single Lambda function often receives multiple event types from EventBridge. You need a clean routing pattern that deserializes the `detail` field into the correct struct based on the `detail-type`. Without this, your handler becomes a tangled mess of type assertions and conditional logic.

## The Problem

Build a Lambda handler that receives EventBridge events from an e-commerce system. The function handles three event types:

1. `order.created` -- contains order ID, customer, and items
2. `order.shipped` -- contains order ID and tracking number
3. `order.cancelled` -- contains order ID and reason

Route each event to a dedicated processor function based on the `DetailType` field.

## Requirements

1. **Handler signature** -- `func(ctx context.Context, event events.CloudWatchEvent) error`
2. **Event routing** -- use `event.DetailType` to dispatch to `handleOrderCreated`, `handleOrderShipped`, or `handleOrderCancelled`
3. **Type-safe detail parsing** -- unmarshal `event.Detail` (which is `json.RawMessage`) into the correct struct for each event type
4. **Unknown events** -- log and skip unknown `DetailType` values without returning an error
5. **Source validation** -- only process events from source `ecommerce.orders`; ignore all others
6. **Tests** -- verify routing, parsing, source filtering, and unknown event handling

## Hints

<details>
<summary>Hint 1: Event type structs</summary>

```go
type OrderCreated struct {
    OrderID  string   `json:"order_id"`
    Customer string   `json:"customer"`
    Items    []string `json:"items"`
}

type OrderShipped struct {
    OrderID        string `json:"order_id"`
    TrackingNumber string `json:"tracking_number"`
}

type OrderCancelled struct {
    OrderID string `json:"order_id"`
    Reason  string `json:"reason"`
}
```

</details>

<details>
<summary>Hint 2: Routing pattern</summary>

```go
func handleRequest(ctx context.Context, event events.CloudWatchEvent) error {
    if event.Source != "ecommerce.orders" {
        log.Printf("ignoring event from source: %s", event.Source)
        return nil
    }

    switch event.DetailType {
    case "order.created":
        var detail OrderCreated
        if err := json.Unmarshal(event.Detail, &detail); err != nil {
            return fmt.Errorf("unmarshal order.created: %w", err)
        }
        return handleOrderCreated(ctx, detail)
    // ... other cases
    }
}
```

</details>

<details>
<summary>Hint 3: Fabricating test events</summary>

```go
detail, _ := json.Marshal(OrderCreated{
    OrderID:  "ORD-001",
    Customer: "alice",
    Items:    []string{"widget", "gadget"},
})

event := events.CloudWatchEvent{
    Source:     "ecommerce.orders",
    DetailType: "order.created",
    Detail:     detail,
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- `order.created` events are routed to the correct handler and detail is parsed
- `order.shipped` events extract the tracking number
- `order.cancelled` events extract the reason
- Events from a different source are silently ignored
- Unknown `DetailType` values are logged but do not cause errors

## What's Next

Continue to [05 - S3 Event Processing](../05-s3-event-processing/05-s3-event-processing.md) to learn how to process S3 bucket notification events.

## Summary

- EventBridge events have a structured envelope (`Source`, `DetailType`, `Detail`) that enables type-safe routing
- Use `event.DetailType` as the dispatch key and `json.Unmarshal(event.Detail, &target)` for type-safe parsing
- Validate `event.Source` to ensure you only process events from expected producers
- Handle unknown event types gracefully -- log them but do not return errors
- The `events.CloudWatchEvent` type in `aws-lambda-go` represents EventBridge events (historical naming)

## Reference

- [events.CloudWatchEvent](https://pkg.go.dev/github.com/aws/aws-lambda-go/events#CloudWatchEvent)
- [EventBridge event structure](https://docs.aws.amazon.com/eventbridge/latest/userguide/aws-events.html)
- [Lambda with EventBridge](https://docs.aws.amazon.com/lambda/latest/dg/services-cloudwatchevents.html)
