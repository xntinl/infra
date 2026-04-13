# Full-Stack Distributed System

**Project**: `platform` — Production-grade distributed system with API gateway, consensus-based coordination, persistent storage, job queue, stream processing, and observability, sustainable at 50,000 req/s P99 < 50ms.

---

## Project context

Your team is building a SaaS analytics platform. A client submits a job via REST. The API Gateway authenticates and rate-limits the request, then routes it to a Coordinator. The Coordinator stores state in persistent Storage and publishes events to a Stream Processor. All components emit telemetry. A multi-region simulation adds geo-routing. The system must sustain 50,000 req/s at P99 under 50ms on a laptop — no shortcuts, every subsystem has real error handling, circuit breakers, health checks, and graceful shutdown.

This is not a demo. It is a system you would hand to an SRE on call at 3am.

---

## Why this is harder than the sum of its parts

Individual components fail cleanly in isolation. In a composed system, failure cascades: the Storage layer's compaction pauses reads, the Gateway's circuit breaker trips, the Queue backs up, the Stream Processor's late event watermarks drift. Identifying and breaking these cascades requires understanding each component's failure modes AND the contract it offers to its callers.

The 50k RPS target is non-trivial in Elixir. It requires avoiding process-per-request overhead in hot paths, using ETS for shared state rather than GenServer mailboxes, and batching telemetry emission.

---

## Design decisions

**Option A — monolithic BEAM cluster with libcluster**
- Pros: simple deployment, trivial RPC, shared observability.
- Cons: language lock-in, single-runtime blast radius.

**Option B — polyglot services over gRPC with explicit contracts** (chosen)
- Pros: teams can pick their stack, explicit versioning.
- Cons: network tax, tracing harder, more operational surface.

→ Chose **B** because the exercise is explicitly about what BEAM clustering gives you — it's the whole point.

---

## Key Concepts: Distributed System Architecture and Failure Modes

A distributed system's architecture determines its failure modes. The key decisions are:

1. **Synchrony model**: Are calls blocking (synchronous) or non-blocking (asynchronous)?
2. **Replication strategy**: Is state replicated for HA, and if so, how?
3. **Cascading failure prevention**: What circuit breakers and bulkheads prevent one component's failure from taking down others?

**Synchronous architecture** (blocking calls):
- Pros: simple to reason about; client sees failures immediately.
- Cons: tail latency: if any backend service is slow, the entire request is slow; resource coupling.

**Asynchronous architecture** (queue-based):
- Pros: decoupling; slow backend doesn't block client; can retry independently.
- Cons: eventual consistency; client doesn't know if the request succeeded until much later.

Your platform uses a hybrid: the API Gateway accepts requests synchronously (client expects a response), then publishes events asynchronously to the queue for processing. This decouples client latency from backend latency while maintaining the illusion of synchronous processing for the client.

**Production insight**: Cascading failure is the number-one failure mode in production systems. Here's how it starts:
1. Storage compaction pauses reads for 500ms.
2. Coordinator's RPC to Storage times out (default 5 seconds).
3. Coordinator retries, creating a thundering herd of requests back to Storage.
4. Storage is now overloaded, compactions take longer, more timeouts.
5. Coordinator circuit breaker trips, rejecting new requests.
6. Queue backs up because Coordinator isn't accepting jobs.
7. Gateway's queue fills up, starts rejecting requests.
8. Clients get 503 Service Unavailable.

Breaking this cascade requires:
- **Short timeouts**: fail fast instead of waiting 5 seconds.
- **Exponential backoff with jitter**: retries don't synchronized.
- **Circuit breakers**: fail open instead of retrying when a dependency is down.
- **Bulkheads**: isolate failure to one subsystem. The Queue doesn't call Storage directly; only the Coordinator does.

---

## Project Structure

```
platform/
├── lib/
│   └── platform/
│       ├── application.ex             # supervisor: gateway, coordinator, storage, queue, processor
│       ├── gateway.ex                 # API server: auth, rate limit, circuit breaker
│       ├── coordinator.ex             # state machine: accepts jobs, publishes events
│       ├── storage.ex                 # in-memory KV with snapshots
│       ├── queue.ex                   # job queue with priority and retry
│       ├── processor.ex               # stream processor: consumes events
│       ├── raft_simple.ex             # leader election (no log replication)
│       ├── circuit_breaker.ex         # per-service failure handling
│       ├── telemetry.ex               # metrics collection and sampling
│       └── health_check.ex            # readiness and liveness probes
├── test/
│   └── platform/
│       ├── gateway_test.exs           # rate limiting, auth, circuit breaker
│       ├── coordinator_test.exs       # state consistency under concurrent loads
│       ├── storage_test.exs           # snapshot correctness
│       ├── queue_test.exs             # job ordering and retry logic
│       ├── processor_test.exs         # event consumption and ordering
│       ├── end_to_end_test.exs        # full request flow with failures
│       └── fault_injection_test.exs   # cascading failure scenarios
├── bench/
│   └── platform_bench.exs             # throughput at 50k RPS
├── simulation/
│   └── chaos.ex                       # network partitions, delays, crashes
└── mix.exs
```

## Implementation milestones (abbreviated)

### Why ETS for the Gateway hot path

At 50k RPS, a GenServer rate limiter becomes a bottleneck — every request serializes through one process mailbox. ETS with `:ets.update_counter/3` provides atomic increment without a process boundary. The operation is O(1) and takes ~100ns. Token refill is done by a single background process that resets counters on a timer.

### Simplified Raft for leader election (not log replication)

`:global.register_name/2` uses a two-phase locking protocol with no progress guarantee under network partition. Raft provides linearizable leader election with bounded election timeout. The simplified Raft implemented here handles leader election only (no log replication), sufficient to demonstrate failure recovery: leader dies, a follower is elected in under 5 seconds.

### Reservoir sampling for percentiles

Storing every latency observation is O(n) space per metric per window. At 50k RPS over a 1-minute window, that is 3 million samples. Reservoir sampling (Vitter's Algorithm R) maintains a fixed-size sample (1000 observations) with equal probability of inclusion. The P99 estimate from 1000 samples has a confidence interval of ±1% at 95% confidence.

---

## Trade-off analysis

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Rate limiter | ETS + background refill | Trades consistency for latency: allows brief over-limit bursts during refill lag, but stays under 1µs per check |
| Auth | Manual JWT + HMAC-SHA256 | Avoids external library; HS256 is not secure for long-term keys but fine for short-lived tokens in this context |
| Circuit breaker | Per-service state machine | Simple to understand; alternative (bulkhead pools) requires more complex thread pool management |
| Storage | In-memory with periodic snapshots | No durability; fine for an exercise where clients are aware. Production would use RocksDB or PostgreSQL |
| Telemetry | Sampled (1 in 100 requests) | Reduces overhead; trades granularity for speed. Alternative: batch events asynchronously |
| Leader election | Simplified Raft (election only) | Full Raft has log replication overhead. This subset is sufficient for leader failover |

---

## Common production mistakes

**1. Not isolating the rate limiter from the rest of the gateway**

The rate limiter is in a hot path. If the rate limiter's background refill process crashes, the limiter stops refilling tokens and starts rejecting all requests. Use a separate Supervisor tree for the rate limiter, and make its failures non-fatal to the Gateway. If the limiter fails, fall back to a coarser-grained rate limit (e.g., reject 1% of requests) rather than rejecting everything.

**2. Circuit breaker transitions not resetting state**

If a circuit breaker trips and then the dependency recovers, the circuit breaker must transition to `:half_open` and send probe requests. If the probe fails, it goes back to `:open`. If the probe succeeds, it goes back to `:closed` and resets the failure counter. Not resetting the counter means it stays open even after recovery.

**3. Auth token expiration not checked on every request**

A token expires at timestamp T. The client makes a request at T-1ms (before expiry) and the request is slow (GC pause, network delay, storage lag). By the time the request is processed at T+500ms, the token is expired. But the auth check was already done at T-1ms, so the request is allowed. Mitigation: re-check token expiration on every transition to a new handler, or use a grace period (e.g., allow requests up to 5 seconds after expiration).

**4. Queue not draining before shutdown**

When the platform shuts down, the Queue should gracefully drain: stop accepting new jobs, wait for in-flight jobs to complete, then shut down. If you just kill the process, jobs are lost. Implement a `:terminate` callback that sets a flag, waits for workers to finish, then exits.

**5. Telemetry batching losing data on crash**

If you batch telemetry events and crash before sending the batch, the events are lost. Use a durable queue (e.g., write to disk) for critical metrics. For non-critical metrics, accept the loss but document it.

---

## Key metrics to measure

1. **P50, P99, P99.9 latency**: histogram of request latency per endpoint.
2. **Error rate**: % of requests that failed (4xx, 5xx).
3. **Circuit breaker state**: is any breaker open? If so, how long?
4. **Queue depth**: how many jobs are waiting?
5. **Storage compaction time**: how long does each compaction take?

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 44-build-full-stack-distributed-system ========")
  IO.puts("Build Full Stack Distributed System")
  IO.puts("")
  
  {:ok, _} = Platform.start_link([])
  IO.puts("Platform started")
  
  {:ok, _} = Platform.Gateway.start_link([])
  IO.puts("Gateway started")
  
  {:ok, result} = Platform.Gateway.submit_request(%{user_id: "user123", data: "test"})
  IO.puts("Request result: #{inspect(result)}")
  
  IO.puts("Run: mix test")
end
```

