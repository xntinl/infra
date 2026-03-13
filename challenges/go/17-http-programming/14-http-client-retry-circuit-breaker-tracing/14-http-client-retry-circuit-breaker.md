# 14. HTTP Client: Retry, Circuit Breaker, and Tracing

<!--
difficulty: insane
concepts: [http-client, retry, exponential-backoff, jitter, circuit-breaker, request-tracing, resilience, observability]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [http-client, http-client-timeouts, context, error-handling, middleware-chains]
-->

## The Challenge

Build a production-grade HTTP client wrapper that adds retry logic with exponential backoff and jitter, a circuit breaker that stops calling failing services, and request tracing that tracks the full lifecycle of each request. The client must be composable -- each concern (retry, circuit breaker, tracing) is implemented as a `http.RoundTripper` decorator that wraps the underlying transport.

The difficulty is in the interaction between these layers. The circuit breaker must not count retries as separate failures. The retry logic must respect the circuit breaker's open state and not retry when the circuit is open. Tracing must capture the full picture: how many attempts were made, whether the circuit breaker tripped, and the final outcome. Getting the layering order right is critical.

## Requirements

### Retry with Backoff

1. Implement a `RetryTransport` that wraps an `http.RoundTripper`
2. Retry on 5xx status codes and network errors (not on 4xx)
3. Configurable maximum retry count (default: 3)
4. Exponential backoff: `base * 2^attempt` with configurable base duration (default: 100ms)
5. Add random jitter (up to 50% of the backoff) to prevent thundering herd
6. Respect `Retry-After` header if present (both seconds and HTTP-date formats)
7. Do not retry non-idempotent methods (POST) unless explicitly configured
8. Respect context cancellation -- stop retrying if the context is done

### Circuit Breaker

9. Implement a `CircuitBreakerTransport` that wraps an `http.RoundTripper`
10. Three states: Closed (normal), Open (rejecting), Half-Open (testing)
11. Transition to Open after N consecutive failures (configurable, default: 5)
12. In Open state, reject requests immediately with a descriptive error
13. After a configurable timeout (default: 30s), transition to Half-Open
14. In Half-Open, allow one probe request: if it succeeds, close the circuit; if it fails, reopen
15. Track per-host circuit breakers (different hosts have independent circuits)

### Request Tracing

16. Implement a `TracingTransport` that wraps an `http.RoundTripper`
17. Generate a unique trace ID for each top-level request (propagated through retries)
18. Record: request method, URL, attempt number, response status, latency per attempt, final outcome
19. Set the `X-Request-ID` header on outgoing requests
20. Provide a `TraceLog` that can be queried for recent request traces

### Composability

21. All three transports implement `http.RoundTripper` and compose by wrapping
22. The correct layering order is: `Tracing -> Retry -> CircuitBreaker -> http.DefaultTransport`
23. Provide a `NewResilientClient` constructor that assembles the full stack with sensible defaults
24. The client must work with a standard test server (`httptest.NewServer`) for verification

## Hints

<details>
<summary>Hint 1: RoundTripper decorator pattern</summary>

```go
type RetryTransport struct {
    Base       http.RoundTripper
    MaxRetries int
    BaseDelay  time.Duration
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    var lastErr error
    var lastResp *http.Response
    for attempt := 0; attempt <= t.MaxRetries; attempt++ {
        if attempt > 0 {
            delay := t.backoff(attempt)
            select {
            case <-time.After(delay):
            case <-req.Context().Done():
                return nil, req.Context().Err()
            }
        }
        resp, err := t.Base.RoundTrip(req)
        if err == nil && resp.StatusCode < 500 {
            return resp, nil
        }
        lastResp = resp
        lastErr = err
    }
    if lastResp != nil {
        return lastResp, nil
    }
    return nil, lastErr
}
```

Note: you must clone the request body for retries since `RoundTrip` may consume it.
</details>

<details>
<summary>Hint 2: Circuit breaker state machine</summary>

```go
type CircuitState int

const (
    StateClosed CircuitState = iota
    StateOpen
    StateHalfOpen
)

type CircuitBreaker struct {
    mu            sync.Mutex
    state         CircuitState
    failures      int
    threshold     int
    lastFailure   time.Time
    openTimeout   time.Duration
}

func (cb *CircuitBreaker) Allow() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    switch cb.state {
    case StateClosed:
        return true
    case StateOpen:
        if time.Since(cb.lastFailure) > cb.openTimeout {
            cb.state = StateHalfOpen
            return true
        }
        return false
    case StateHalfOpen:
        return false // only one probe at a time
    }
    return false
}
```
</details>

<details>
<summary>Hint 3: Request body cloning for retries</summary>

```go
func cloneRequest(req *http.Request) (*http.Request, error) {
    clone := req.Clone(req.Context())
    if req.Body != nil {
        body, err := io.ReadAll(req.Body)
        if err != nil {
            return nil, err
        }
        req.Body = io.NopCloser(bytes.NewReader(body))
        clone.Body = io.NopCloser(bytes.NewReader(body))
    }
    return clone, nil
}
```

Read the body once upfront and restore it for each retry attempt.
</details>

<details>
<summary>Hint 4: Composing the transport stack</summary>

```go
func NewResilientClient(opts ...Option) *http.Client {
    cfg := defaultConfig()
    for _, opt := range opts {
        opt(&cfg)
    }

    transport := http.DefaultTransport

    // Innermost: circuit breaker (per-host)
    transport = &CircuitBreakerTransport{
        Base:      transport,
        Breakers:  make(map[string]*CircuitBreaker),
        Threshold: cfg.CBThreshold,
        Timeout:   cfg.CBTimeout,
    }

    // Middle: retry
    transport = &RetryTransport{
        Base:       transport,
        MaxRetries: cfg.MaxRetries,
        BaseDelay:  cfg.BaseDelay,
    }

    // Outermost: tracing
    transport = &TracingTransport{
        Base: transport,
        Log:  NewTraceLog(1000),
    }

    return &http.Client{Transport: transport}
}
```
</details>

## Success Criteria

- [ ] `RetryTransport` retries on 5xx and network errors with exponential backoff and jitter
- [ ] Retries stop when the context is cancelled
- [ ] POST requests are not retried by default
- [ ] `Retry-After` header is respected when present
- [ ] `CircuitBreakerTransport` opens after N consecutive failures and rejects requests immediately
- [ ] After the open timeout, a single probe request is allowed (half-open state)
- [ ] A successful probe closes the circuit; a failed probe reopens it
- [ ] Per-host circuit breakers operate independently
- [ ] `TracingTransport` generates a unique trace ID per top-level request
- [ ] Trace logs record method, URL, attempt count, status, and latency
- [ ] The transport stack composes correctly: `Tracing -> Retry -> CircuitBreaker -> Default`
- [ ] All features are demonstrated against an `httptest.NewServer` with configurable failure modes
- [ ] No data races (`go test -race`)
- [ ] The program outputs a clear demonstration of retry sequences, circuit breaker state transitions, and trace logs

## Research Resources

- [http.RoundTripper](https://pkg.go.dev/net/http#RoundTripper) -- the transport interface for request decoration
- [Exponential backoff and jitter (AWS)](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) -- backoff strategies compared
- [Circuit Breaker pattern (Martin Fowler)](https://martinfowler.com/bliki/CircuitBreaker.html) -- pattern description and state machine
- [Sony gobreaker](https://github.com/sony/gobreaker) -- production circuit breaker library (for reference)
- [hashicorp/go-retryablehttp](https://github.com/hashicorp/go-retryablehttp) -- production retry client (for reference)
- [OpenTelemetry HTTP instrumentation](https://opentelemetry.io/docs/languages/go/instrumentation/) -- production tracing approach
