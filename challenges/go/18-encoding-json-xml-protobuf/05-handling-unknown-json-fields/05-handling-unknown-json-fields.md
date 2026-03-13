# Exercise 05: Handling Unknown JSON Fields

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 18 - Encoding

## Overview

Real-world JSON is messy. APIs evolve, clients send unexpected fields, and you need strategies for dealing with the unknown. Go gives you several tools: disallowing unknown fields, capturing them in a map, using `json.RawMessage` to defer parsing, and two-pass decoding to handle polymorphic data.

## Prerequisites

- Exercises 01-02 (JSON basics, struct tags)
- Maps and interfaces

## Key Concepts

### DisallowUnknownFields

By default, `json.Unmarshal` silently ignores fields not in your struct. To reject them:

```go
dec := json.NewDecoder(strings.NewReader(data))
dec.DisallowUnknownFields()
err := dec.Decode(&target)
// returns error if JSON has keys not matching struct fields
```

### Catch-All with a Map

Unmarshal into `map[string]interface{}` (or `map[string]json.RawMessage`) to keep every field:

```go
var raw map[string]json.RawMessage
json.Unmarshal(data, &raw)
```

### json.RawMessage

`json.RawMessage` is a `[]byte` that is not parsed during unmarshal. It lets you defer decoding until you know the type:

```go
type Event struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

You first decode the envelope, inspect `Type`, then decode `Payload` into the correct struct.

### Two-Pass Decoding Pattern

```go
var event Event
json.Unmarshal(data, &event)

switch event.Type {
case "click":
    var click ClickPayload
    json.Unmarshal(event.Payload, &click)
case "purchase":
    var purchase PurchasePayload
    json.Unmarshal(event.Payload, &purchase)
}
```

## Task

Build an event processor that handles polymorphic JSON:

1. Define an `Envelope` struct with `Type` (string), `Timestamp` (string), and `Payload` (`json.RawMessage`).

2. Define payload types:
   - `UserCreated` -- `UserID` string, `Email` string
   - `OrderPlaced` -- `OrderID` string, `Amount` float64, `Items` []string
   - `SystemAlert` -- `Level` string, `Message` string

3. Write a `processEvent` function that:
   - Unmarshals the envelope
   - Switches on `Type` to decode `Payload` into the correct struct
   - Returns a formatted string describing the event
   - Returns an error for unknown event types

4. Process this array of events (use `json.Decoder` with token-by-token reading or unmarshal the full array):

```json
[
  {"type":"user.created","timestamp":"2026-03-13T10:00:00Z","payload":{"user_id":"u1","email":"a@b.com"}},
  {"type":"order.placed","timestamp":"2026-03-13T10:05:00Z","payload":{"order_id":"o1","amount":59.99,"items":["widget","gadget"]}},
  {"type":"system.alert","timestamp":"2026-03-13T10:10:00Z","payload":{"level":"warning","message":"High memory usage"}},
  {"type":"unknown.event","timestamp":"2026-03-13T10:15:00Z","payload":{"foo":"bar"}}
]
```

5. Use `DisallowUnknownFields` on at least one of the payload types to demonstrate strict parsing. Add an event with an extra field in its payload and show the error.

## Hints

- The `Payload` field stays as raw bytes until you explicitly unmarshal it -- this is the whole point of `json.RawMessage`.
- Use `json:"user_id"` tags on the payload structs to match the snake_case JSON keys.
- For `DisallowUnknownFields`, you need `json.NewDecoder` + `bytes.NewReader` since it is a decoder option, not available on `json.Unmarshal`.
- Handle the unknown event type gracefully: print an error message but continue processing.

## Verification

Expected output (format may vary):

```
[user.created] 2026-03-13T10:00:00Z - New user u1 (a@b.com)
[order.placed] 2026-03-13T10:05:00Z - Order o1: $59.99 (2 items)
[system.alert] 2026-03-13T10:10:00Z - WARNING: High memory usage
[unknown.event] 2026-03-13T10:15:00Z - ERROR: unknown event type "unknown.event"
```

## Key Takeaways

- `json.RawMessage` defers parsing so you can inspect a discriminator field first
- Two-pass decoding is the standard pattern for polymorphic JSON in Go
- `DisallowUnknownFields` adds strict validation at the decoder level
- Combining a typed envelope with raw payload gives you both structure and flexibility
- Always handle unknown types gracefully in production code
