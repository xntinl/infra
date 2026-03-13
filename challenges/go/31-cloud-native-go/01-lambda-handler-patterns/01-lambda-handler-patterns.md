# 1. Lambda Handler Patterns

<!--
difficulty: advanced
concepts: [aws-lambda-go, handler-signature, events-package, context-propagation, structured-response]
tools: [go, aws-cli]
estimated_time: 35m
bloom_level: analyze
prerequisites: [http-programming, interfaces, context, json-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with `context.Context` and JSON encoding
- Basic understanding of AWS Lambda concepts (functions invoked by events)
- `github.com/aws/aws-lambda-go` module

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** AWS Lambda handlers in Go using the `lambda.Start` API
- **Analyze** the different handler signatures supported by `aws-lambda-go`
- **Use** the `events` package to handle API Gateway, SQS, and custom events
- **Design** handlers that propagate context and return structured responses

## Why Lambda Handler Patterns Matter

AWS Lambda functions in Go follow a specific contract. The `aws-lambda-go` library supports multiple handler signatures -- from the simplest `func()` to the full `func(context.Context, TIn) (TOut, error)`. Choosing the right signature determines how you receive event data, propagate deadlines, and return results.

Understanding these patterns is critical because a Lambda function that ignores context cannot honor timeout signals, and one that uses the wrong event type will fail to deserialize the incoming payload. Production Lambda functions almost always use the full signature with context, a typed input, and a structured output.

## The Problem

You will build a Go Lambda handler that processes API Gateway proxy requests. The handler must:

1. Accept an API Gateway V2 (HTTP API) event
2. Extract the HTTP method and path from the event
3. Route to different logic based on the path
4. Return a properly structured API Gateway proxy response
5. Propagate the Lambda context for deadline awareness

## Requirements

1. **Create a Lambda handler** using the full signature `func(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)`
2. **Route requests** -- `/health` returns 200 with `{"status":"ok"}`, `/hello` returns 200 with a greeting using a query parameter `name`, all other paths return 404
3. **Set headers** -- all responses must include `Content-Type: application/json`
4. **Use context** -- log the remaining time before deadline using `ctx.Deadline()`
5. **Write a test** that invokes the handler with a fabricated event and checks the response

## Hints

<details>
<summary>Hint 1: Project setup</summary>

```bash
mkdir -p ~/go-exercises/lambda-handler
cd ~/go-exercises/lambda-handler
go mod init lambda-handler
go get github.com/aws/aws-lambda-go
```

</details>

<details>
<summary>Hint 2: Handler signature</summary>

```go
func handleRequest(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
    // event.RawPath contains the path
    // event.QueryStringParameters is map[string]string
    // Return events.APIGatewayV2HTTPResponse{StatusCode: 200, Body: "..."}
}
```

</details>

<details>
<summary>Hint 3: Context deadline</summary>

```go
if deadline, ok := ctx.Deadline(); ok {
    remaining := time.Until(deadline)
    log.Printf("remaining time: %v", remaining)
}
```

</details>

<details>
<summary>Hint 4: Testing without AWS</summary>

You can call `handleRequest` directly in tests by fabricating an `events.APIGatewayV2HTTPRequest` struct. No AWS credentials needed.

</details>

## Verification

```bash
# Build the handler
go build -o bootstrap main.go

# Run tests
go test -v -race ./...
```

Your tests should confirm:
- `/health` returns status 200 with `{"status":"ok"}`
- `/hello?name=Lambda` returns status 200 with a greeting containing "Lambda"
- `/unknown` returns status 404
- All responses have `Content-Type: application/json` header

## What's Next

Continue to [02 - Lambda Cold Start Optimization](../02-lambda-cold-start-optimization/02-lambda-cold-start-optimization.md) to learn how to measure and reduce Lambda cold start times in Go.

## Summary

- `aws-lambda-go` supports multiple handler signatures; the full `func(ctx, TIn) (TOut, error)` is best for production
- The `events` package provides typed structs for API Gateway, SQS, S3, and other AWS event sources
- Context carries the Lambda deadline -- use it to detect when the function is about to be killed
- Lambda handlers are testable as plain Go functions without deploying to AWS
- The handler's main function calls `lambda.Start(handleRequest)` to register with the Lambda runtime

## Reference

- [aws-lambda-go documentation](https://pkg.go.dev/github.com/aws/aws-lambda-go)
- [AWS Lambda Go handler](https://docs.aws.amazon.com/lambda/latest/dg/golang-handler.html)
- [events package](https://pkg.go.dev/github.com/aws/aws-lambda-go/events)
