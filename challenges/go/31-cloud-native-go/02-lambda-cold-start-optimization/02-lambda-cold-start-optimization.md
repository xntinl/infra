# 2. Lambda Cold Start Optimization

<!--
difficulty: advanced
concepts: [cold-start, init-phase, global-state, lazy-initialization, provisioned-concurrency]
tools: [go, aws-cli]
estimated_time: 40m
bloom_level: analyze
prerequisites: [lambda-handler-patterns, sync-once, context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Lambda Handler Patterns exercise
- Understanding of `sync.Once` and package-level initialization
- Familiarity with AWS Lambda execution model (init vs invoke phases)

## Learning Objectives

After completing this exercise, you will be able to:

- **Measure** cold start vs warm start latency in a Go Lambda function
- **Analyze** what contributes to cold start time (init phase, SDK clients, connections)
- **Apply** optimization techniques: global client reuse, lazy init, and minimal imports
- **Evaluate** the trade-off between init-time setup and per-invocation setup

## Why Cold Start Optimization Matters

A Lambda cold start occurs when AWS must create a new execution environment. In Go, the init phase runs package `init()` functions and the code before `lambda.Start()`. Anything you do there -- creating SDK clients, opening database connections, loading configuration -- adds to the cold start latency.

Go already has fast cold starts compared to Java or .NET, typically 50-200ms. But in latency-sensitive applications (API backends, real-time processing), every millisecond matters. Understanding what happens during init and how to structure your code to minimize that time is a key production skill.

## The Problem

Build a Lambda handler that connects to DynamoDB and fetches an item. Structure the code so that:

1. The DynamoDB client is created once during init, not per invocation
2. Measure and log the time spent in init vs invoke
3. Demonstrate the difference between cold and warm invocations
4. Use `sync.Once` for optional lazy initialization of expensive resources

## Requirements

1. **Global client initialization** -- create the AWS SDK DynamoDB client in a package-level variable, initialized before `lambda.Start()`
2. **Timing instrumentation** -- log timestamps at program start, after init, and at handler entry to measure cold start duration
3. **Lazy initialization pattern** -- implement a secondary resource (e.g., an HTTP client for an external API) using `sync.Once` so it is only created if the code path needs it
4. **Handler logic** -- accept a request with a `key` field, look up the key in DynamoDB (or simulate it), return the result
5. **Test with timing** -- write a test that calls the handler twice and demonstrates that the second call skips initialization

## Hints

<details>
<summary>Hint 1: Init-time client creation</summary>

```go
var dynamoClient *dynamodb.Client

func init() {
    cfg, err := config.LoadDefaultConfig(context.Background())
    if err != nil {
        log.Fatalf("unable to load SDK config: %v", err)
    }
    dynamoClient = dynamodb.NewFromConfig(cfg)
}
```

</details>

<details>
<summary>Hint 2: Measuring init time</summary>

```go
var programStart = time.Now()

func init() {
    // ... setup ...
    log.Printf("init completed in %v", time.Since(programStart))
}

func handleRequest(ctx context.Context, event MyEvent) (MyResponse, error) {
    invokeStart := time.Now()
    defer func() {
        log.Printf("invoke completed in %v", time.Since(invokeStart))
    }()
    // ...
}
```

</details>

<details>
<summary>Hint 3: sync.Once for lazy resources</summary>

```go
var (
    httpClient     *http.Client
    httpClientOnce sync.Once
)

func getHTTPClient() *http.Client {
    httpClientOnce.Do(func() {
        httpClient = &http.Client{Timeout: 5 * time.Second}
    })
    return httpClient
}
```

</details>

<details>
<summary>Hint 4: Simulating without AWS</summary>

Create an interface for the DynamoDB operations and use a mock in tests. The cold start timing pattern works the same way with or without real AWS calls.

</details>

## Verification

```bash
go test -v -race -count=2 ./...
```

Your output should show:
- First test invocation logs init time (cold start simulation)
- Second test invocation skips init (warm start)
- `sync.Once` resource is only initialized on first use
- No race conditions detected

## What's Next

Continue to [03 - SQS Message Handler](../03-sqs-message-handler/03-sqs-message-handler.md) to learn how to process SQS messages in a Lambda function.

## Summary

- Go Lambda cold starts include the time to run `init()` functions and code before `lambda.Start()`
- Create SDK clients and expensive resources once at package level, not per invocation
- Use `sync.Once` for resources that are only needed on certain code paths
- Instrument init and invoke phases with timestamps to measure cold start impact
- Go's cold starts are already fast (~50-200ms) but optimization still matters for latency-sensitive workloads

## Reference

- [AWS Lambda execution environment](https://docs.aws.amazon.com/lambda/latest/dg/lambda-runtime-environment.html)
- [Optimizing Lambda performance](https://docs.aws.amazon.com/lambda/latest/dg/best-practices.html)
- [sync.Once documentation](https://pkg.go.dev/sync#Once)
