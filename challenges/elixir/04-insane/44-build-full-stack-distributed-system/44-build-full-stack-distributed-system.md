# 44. Build a Full-Stack Distributed System

## Context

This is the capstone exercise. You have built every component in isolation across exercises 29–43: a metrics collector, a profiler, a test framework, a web framework, an API gateway, a load balancer, a streaming server, a blockchain, a P2P file sharer, a service mesh, a WebAssembly interpreter, an event sourcing framework, a macro DSL system, and an inference engine.

Now you integrate them into a single coherent production system under one umbrella. The scenario: an imaginary SaaS platform handles user-submitted analytics jobs. A client submits a job via REST → the API Gateway authenticates and rate-limits → routes to a Coordinator → the Coordinator stores state in a persistent Storage layer → publishes events to a Stream Processor → all components emit telemetry → a multi-region simulation adds geo-routing. The system must sustain 50,000 req/s at P99 under 50ms on a laptop.

There are no shortcuts. Every subsystem must be wired with real error handling, circuit breakers, health checks, and graceful shutdown. The goal is not a demo; it is a system you would hand to an SRE on call at 3am.

## Why this is harder than the sum of its parts

Individual components fail cleanly in isolation. In a composed system, failure cascades: the Storage layer's compaction pauses reads → the Gateway's circuit breaker trips → the Queue backs up → the Stream Processor's late event watermarks drift. Identifying and breaking these cascades requires understanding each component's failure modes AND the contract it offers to its callers.

The 50k RPS target is also non-trivial in Elixir. It requires avoiding process-per-request overhead in hot paths, using ETS for shared state rather than GenServer mailboxes, and batching telemetry emission. This is where theoretical knowledge of the BEAM becomes practical necessity.

## Project Structure

```
platform/
├── mix.exs
├── config/
│   ├── config.exs
│   └── test.exs
├── lib/
│   ├── platform/
│   │   ├── gateway/
│   │   │   ├── router.ex          # Plug pipeline: auth → rate_limit → route
│   │   │   ├── auth.ex            # JWT verification (HS256, no library)
│   │   │   ├── rate_limiter.ex    # Token bucket per API key in ETS
│   │   │   └── circuit_breaker.ex # Per-service circuit breaker
│   │   ├── coordinator/
│   │   │   ├── raft.ex            # Leader election: heartbeats, vote requests
│   │   │   ├── leader.ex          # Task assignment and tracking
│   │   │   └── worker.ex          # Task execution worker
│   │   ├── storage/
│   │   │   ├── lsm.ex             # LSM-tree: memtable + SSTable + WAL
│   │   │   ├── cache.ex           # ETS L1 cache with TTL
│   │   │   └── store.ex           # Public API: get/put/delete/write_batch/scan
│   │   ├── queue/
│   │   │   ├── job.ex             # Job struct: id, payload, priority, attempts
│   │   │   ├── scheduler.ex       # Priority queue + visibility timeout
│   │   │   └── dead_letter.ex     # DLQ for exhausted jobs
│   │   ├── stream/
│   │   │   ├── window.ex          # Time-based and count-based windows
│   │   │   ├── operators.ex       # filter, map, reduce, join
│   │   │   └── processor.ex       # GenStage-based pipeline
│   │   ├── telemetry/
│   │   │   ├── collector.ex       # :telemetry handler, reservoir sampling
│   │   │   ├── aggregator.ex      # 1s/10s/1m window aggregation
│   │   │   └── prometheus.ex      # Text exposition format serializer
│   │   ├── tracing/
│   │   │   ├── span.ex            # Span struct: trace_id, span_id, parent_id
│   │   │   ├── context.ex         # Process dictionary propagation
│   │   │   └── store.ex           # ETS span storage per trace_id
│   │   ├── region/
│   │   │   ├── transport.ex       # Inter-region call with artificial latency
│   │   │   └── router.ex          # Geo-routing: read local, write primary
│   │   └── cli/
│   │       ├── status.ex          # mix system.status
│   │       ├── benchmark.ex       # mix system.benchmark
│   │       └── drain.ex           # mix system.drain
│   └── platform.ex                # Application supervisor
├── test/
│   ├── gateway_test.exs
│   ├── coordinator_test.exs
│   ├── storage_test.exs
│   ├── queue_test.exs
│   ├── stream_test.exs
│   ├── telemetry_test.exs
│   ├── tracing_test.exs
│   └── integration_test.exs
└── bench/
    └── load_test.exs              # 50k RPS end-to-end benchmark
```

## Why ETS for the Gateway hot path

At 50k RPS, a GenServer rate limiter becomes a bottleneck immediately — every request serializes through one process mailbox, capped at ~500k msg/s on a single core, but with scheduling overhead at scale. ETS with `:ets.update_counter/3` provides atomic increment without a process boundary. The operation is O(1) and takes ~100ns. Token refill is done by a single background process that resets counters on a timer, not per-request.

## Why Raft and not just `:global` for leader election

`:global.register_name/2` uses a two-phase locking protocol with no progress guarantee under network partition. Raft provides linearizable leader election with bounded election timeout (you configure it) and guaranteed re-election after leader failure. For a Coordinator that assigns tasks, knowing definitively who the leader is prevents duplicate task assignment.

The simplified Raft you implement here does not need log replication — only leader election. This is enough to demonstrate the failure recovery requirement (leader dies → follower elected in < 5s).

## Why reservoir sampling for percentiles

Storing every latency observation in memory is O(n) space per metric per window. At 50k RPS over a 1-minute window, that is 3 million samples. Reservoir sampling (Vitter's Algorithm R) maintains a fixed-size sample (1000 observations) with each incoming observation having equal probability of being included. The P99 estimate from 1000 samples has a confidence interval of ±1% at 95% confidence. This is accurate enough for alerting.

## Step 1 — Gateway

```elixir
defmodule Platform.Gateway.RateLimiter do
  @table :rate_limiter

  def init do
    :ets.new(@table, [:named_table, :public, :set, {:write_concurrency, true}])
  end

  @doc "Returns :ok or {:error, :rate_limited}"
  def check_and_consume(api_key, limit, window_ms) do
    now_window = div(System.monotonic_time(:millisecond), window_ms)
    key = {api_key, now_window}
    # TODO: use :ets.update_counter with a threshold check
    # HINT: :ets.update_counter(@table, key, {2, 1, limit, limit}, {key, 0})
    #       returns the new counter value; if == limit, insert succeeded at boundary
    # TODO: if counter > limit return {:error, :rate_limited}, else :ok
    # TODO: schedule cleanup for expired windows to prevent unbounded table growth
  end
end

defmodule Platform.Gateway.Auth do
  @doc "Verify HS256 JWT without external library. Returns {:ok, claims} or {:error, reason}"
  def verify_jwt(token, secret) do
    # TODO: split token into [header_b64, payload_b64, sig_b64]
    # TODO: recompute HMAC-SHA256 over "header.payload" with secret
    # TODO: compare with decoded sig_b64 using Plug.Crypto.secure_compare/2 (constant time)
    # TODO: decode payload JSON, check exp claim against System.system_time(:second)
  end
end

defmodule Platform.Gateway.CircuitBreaker do
  use GenServer

  @states [:closed, :open, :half_open]

  # State: %{service => %{state, failure_count, last_failure_at, success_count}}
  def start_link(opts), do: GenServer.start_link(__MODULE__, %{}, opts)

  @doc "Execute fun against service. Returns result or {:error, :circuit_open}"
  def call(breaker, service, fun, timeout \\ 5000) do
    # TODO: check current state for service
    # TODO: if :open and cooldown not elapsed, return {:error, :circuit_open}
    # TODO: if :half_open, allow one call through; on success → :closed; on failure → :open
    # TODO: if :closed, execute fun; on failure increment count; above threshold → :open
    # HINT: threshold = 5 failures in 10 seconds
  end
end
```

## Step 2 — Coordinator (simplified Raft)

```elixir
defmodule Platform.Coordinator.Raft do
  use GenServer

  # Each node is a GenServer. Nodes communicate via send/receive or GenServer.call.
  # State: term, voted_for, role (:follower | :candidate | :leader), votes_received
  # Leader sends heartbeats every 150ms; election timeout 300–600ms (randomized)

  def start_link(opts) do
    GenServer.start_link(__MODULE__, %{
      node_id: Keyword.fetch!(opts, :node_id),
      peers: Keyword.fetch!(opts, :peers),
      term: 0,
      voted_for: nil,
      role: :follower,
      leader: nil,
      votes: MapSet.new()
    }, opts)
  end

  def handle_info(:election_timeout, state) do
    # TODO: transition to :candidate
    # TODO: increment term, vote for self
    # TODO: send RequestVote to all peers
    # TODO: schedule new election timeout (randomized 300–600ms)
  end

  def handle_call({:request_vote, candidate_term, candidate_id}, _from, state) do
    # TODO: if candidate_term > state.term and we haven't voted: grant vote
    # TODO: reset election timeout on granting vote
    # TODO: return {:reply, {current_term, vote_granted}, new_state}
  end

  def handle_info(:send_heartbeat, %{role: :leader} = state) do
    # TODO: send AppendEntries (empty, heartbeat-only) to all peers
    # TODO: schedule next heartbeat in 150ms
  end

  def handle_cast({:append_entries, leader_term, leader_id}, state) do
    # TODO: if leader_term >= state.term, accept leader; reset election timeout
    # TODO: update state.leader
  end
end
```

## Step 3 — Storage

```elixir
defmodule Platform.Storage.Store do
  @doc "Write a key-value pair. Writes go to WAL first, then memtable."
  def put(key, value) do
    # TODO: append to WAL: {timestamp, :put, key, value}
    # TODO: write to ETS cache with TTL
    # TODO: write to LSM memtable; if memtable size > threshold, flush to SSTable
  end

  @doc "Atomic batch write. All succeed or none are visible."
  def write_batch(operations) when is_list(operations) do
    # TODO: write a batch-start marker to WAL
    # TODO: apply all operations to memtable
    # TODO: write batch-commit marker to WAL
    # TODO: on crash between start and commit, replay WAL ignores uncommitted batches
  end

  @doc "Prefix scan: return all {key, value} pairs where key starts with prefix"
  def scan(prefix) do
    # TODO: scan ETS cache first; collect matching keys
    # TODO: scan memtable; merge with cache results (cache takes precedence)
    # TODO: scan SSTables from newest to oldest; merge (newer wins)
    # TODO: return sorted list of {key, value}
  end
end
```

## Step 4 — Queue

```elixir
defmodule Platform.Queue.Scheduler do
  use GenServer

  # Internal state: three ETS tables (one per priority), visibility_timeout map
  # Job struct: %{id, payload, priority, attempts, max_attempts, next_visible_at}

  @doc "Enqueue a job at the given priority"
  def enqueue(payload, priority \\ :normal, opts \\ []) do
    # TODO: generate UUID job id
    # TODO: insert into priority ETS table with next_visible_at = now
  end

  @doc "Dequeue next available job (highest priority first). Marks as invisible."
  def dequeue(worker_id) do
    # TODO: check :high, then :normal, then :low tables
    # TODO: find job where next_visible_at <= now
    # TODO: update next_visible_at = now + visibility_timeout (30s default)
    # TODO: return {:ok, job} or {:empty}
  end

  @doc "Acknowledge successful completion"
  def ack(job_id) do
    # TODO: delete from all tables
  end

  @doc "Negative acknowledge: increment attempts, schedule retry with exponential backoff"
  def nack(job_id) do
    # TODO: find job; increment attempts
    # TODO: if attempts >= max_attempts: move to DLQ
    # TODO: else: next_visible_at = now + backoff(attempts) where backoff(n) = min(2^n * 1s, 5m)
  end

  # Visibility timeout reaper: runs every 5 seconds
  def handle_info(:reap_invisible, state) do
    # TODO: find jobs where next_visible_at < now (they timed out without ack)
    # TODO: call nack/1 for each (increments attempts, handles retry or DLQ)
    # TODO: schedule next reap
  end
end
```

## Step 5 — Stream processor

```elixir
defmodule Platform.Stream.Window do
  @doc "Time-based tumbling window. Returns events in the current window."
  def tumbling(events, window_ms) do
    # TODO: group events by floor(event.timestamp / window_ms)
    # TODO: return map of %{window_start_ms => [events]}
  end

  @doc "Count-based sliding window over a stream of events"
  def sliding(events, size) do
    # TODO: return list of lists, each of length `size`, sliding by 1
    # HINT: Enum.chunk_every(events, size, 1, :discard)
  end
end

defmodule Platform.Stream.Operators do
  @doc "Join two event streams by key within a time window"
  def join(stream_a, stream_b, key_fn, window_ms) do
    # TODO: index stream_a events by key: %{key => [events]}
    # TODO: for each event in stream_b, find matching key events in stream_a within window_ms
    # TODO: emit {event_a, event_b} pairs
  end

  @doc "Watermark-aware filter: drop events older than watermark"
  def filter_late(events, watermark_ms) do
    Enum.reject(events, fn e -> e.timestamp < watermark_ms end)
  end
end
```

## Step 6 — Telemetry and tracing

```elixir
defmodule Platform.Telemetry.Collector do
  @reservoir_size 1000

  def attach do
    events = [
      [:platform, :gateway, :request],
      [:platform, :storage, :operation],
      [:platform, :queue, :job],
      [:platform, :coordinator, :task]
    ]
    :telemetry.attach_many("platform-collector", events, &handle_event/4, %{})
  end

  def handle_event(event_name, measurements, _metadata, _config) do
    # TODO: extract duration_ms from measurements
    # TODO: update reservoir: if size < 1000, append; else replace random index
    # HINT: :rand.uniform(current_size) for Vitter's Algorithm R
    # TODO: update counters in ETS (total_count, error_count)
  end

  def percentile(event_name, p) do
    # TODO: get reservoir for event_name from ETS
    # TODO: sort; index = round(p / 100 * length(reservoir))
    # TODO: return Enum.at(sorted, index)
  end
end

defmodule Platform.Tracing.Context do
  @trace_key :platform_trace_context

  def start_trace do
    trace_id = :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower)
    span_id = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
    Process.put(@trace_key, %{trace_id: trace_id, span_id: span_id, parent_id: nil})
    trace_id
  end

  def current_trace_id do
    case Process.get(@trace_key) do
      %{trace_id: id} -> id
      nil -> nil
    end
  end

  @doc "Execute fun as a child span. Records duration and result."
  def with_span(component, operation, fun) do
    parent = Process.get(@trace_key)
    span_id = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
    # TODO: set new span context in process dictionary
    # TODO: record start time
    # TODO: call fun.()
    # TODO: record end time; compute duration_ms
    # TODO: store span in Platform.Tracing.Store (ETS keyed by trace_id)
    # TODO: restore parent span context
    # TODO: return result of fun.()
  end
end
```

## Step 7 — Region transport

```elixir
defmodule Platform.Region.Transport do
  @regions %{
    "us-east" => %{latency_ms: 0},      # primary
    "eu-west" => %{latency_ms: 80},
    "ap-south" => %{latency_ms: 140}
  }

  @doc "Call a function on a remote region. In dev, simulates latency with Process.sleep."
  def call(region, fun, timeout \\ 5000) do
    latency = get_in(@regions, [region, :latency_ms]) || 0
    if latency > 0, do: Process.sleep(latency)
    # TODO: in production this would be an Erlang distribution RPC
    # TODO: for now, execute fun.() after simulated latency
    # TODO: wrap in Task with timeout to prevent hanging
  end

  @doc "Route a request: writes go to primary, reads go to nearest region"
  def route(operation, region_header) do
    case operation do
      :write -> call("us-east", fn -> {:ok, :primary} end)
      :read -> call(region_header || "us-east", fn -> {:ok, :local} end)
    end
  end
end
```

## Given tests

```elixir
# test/gateway_test.exs
defmodule Platform.GatewayTest do
  use ExUnit.Case, async: false
  alias Platform.Gateway.{RateLimiter, Auth, CircuitBreaker}

  setup do
    RateLimiter.init()
    :ok
  end

  test "rate limiter allows requests under limit" do
    for _ <- 1..5 do
      assert :ok = RateLimiter.check_and_consume("key1", 10, 1000)
    end
  end

  test "rate limiter blocks requests over limit" do
    for _ <- 1..10 do
      RateLimiter.check_and_consume("key2", 10, 1000)
    end
    assert {:error, :rate_limited} = RateLimiter.check_and_consume("key2", 10, 1000)
  end

  test "JWT verification succeeds for valid token" do
    secret = "test_secret"
    claims = %{"sub" => "user123", "exp" => System.system_time(:second) + 3600}
    token = build_test_jwt(claims, secret)
    assert {:ok, decoded} = Auth.verify_jwt(token, secret)
    assert decoded["sub"] == "user123"
  end

  test "JWT verification fails for expired token" do
    secret = "test_secret"
    claims = %{"sub" => "user123", "exp" => System.system_time(:second) - 1}
    token = build_test_jwt(claims, secret)
    assert {:error, :expired} = Auth.verify_jwt(token, secret)
  end

  defp build_test_jwt(claims, secret) do
    header = Base.url_encode64(~s({"alg":"HS256","typ":"JWT"}), padding: false)
    payload = Base.url_encode64(Jason.encode!(claims), padding: false)
    sig = :crypto.mac(:hmac, :sha256, secret, "#{header}.#{payload}")
           |> Base.url_encode64(padding: false)
    "#{header}.#{payload}.#{sig}"
  end
end

# test/coordinator_test.exs
defmodule Platform.CoordinatorTest do
  use ExUnit.Case, async: false
  alias Platform.Coordinator.Raft

  test "leader is elected within 2 seconds in a 3-node cluster" do
    nodes = for i <- 1..3 do
      {:ok, pid} = Raft.start_link(node_id: i, peers: [], name: :"raft_#{i}")
      pid
    end
    # Give time for election
    Process.sleep(2000)
    leaders = Enum.count(nodes, fn pid -> GenServer.call(pid, :role) == :leader end)
    assert leaders == 1
    Enum.each(nodes, &GenServer.stop/1)
  end

  test "new leader elected within 5 seconds after leader crash" do
    nodes = for i <- 1..3 do
      {:ok, pid} = Raft.start_link(node_id: i, peers: [], name: :"raft2_#{i}")
      pid
    end
    Process.sleep(2000)
    leader = Enum.find(nodes, fn pid -> GenServer.call(pid, :role) == :leader end)
    GenServer.stop(leader, :kill)
    remaining = List.delete(nodes, leader)
    Process.sleep(5000)
    new_leaders = Enum.count(remaining, fn pid ->
      try do
        GenServer.call(pid, :role) == :leader
      catch
        :exit, _ -> false
      end
    end)
    assert new_leaders == 1
    Enum.each(remaining, fn pid -> try do GenServer.stop(pid) catch :exit, _ -> :ok end end)
  end
end

# test/queue_test.exs
defmodule Platform.QueueTest do
  use ExUnit.Case, async: false
  alias Platform.Queue.Scheduler

  setup do
    {:ok, _} = Scheduler.start_link(name: :test_queue)
    :ok
  end

  test "enqueue and dequeue preserves payload" do
    Scheduler.enqueue(%{action: "process_file"}, :normal)
    {:ok, job} = Scheduler.dequeue("worker-1")
    assert job.payload.action == "process_file"
  end

  test "high priority jobs dequeued before normal" do
    Scheduler.enqueue(%{id: "low"}, :low)
    Scheduler.enqueue(%{id: "high"}, :high)
    Scheduler.enqueue(%{id: "normal"}, :normal)
    {:ok, j1} = Scheduler.dequeue("w1")
    {:ok, j2} = Scheduler.dequeue("w2")
    {:ok, j3} = Scheduler.dequeue("w3")
    assert j1.payload.id == "high"
    assert j2.payload.id == "normal"
    assert j3.payload.id == "low"
  end

  test "nack increments attempts and reschedules" do
    Scheduler.enqueue(%{id: "retry_me"}, :normal)
    {:ok, job} = Scheduler.dequeue("w1")
    assert job.attempts == 0
    Scheduler.nack(job.id)
    # After backoff the job should become visible again
    Process.sleep(1500)
    {:ok, retried} = Scheduler.dequeue("w2")
    assert retried.id == job.id
    assert retried.attempts == 1
  end

  test "job moves to DLQ after max attempts" do
    Scheduler.enqueue(%{id: "doomed"}, :normal, max_attempts: 1)
    {:ok, job} = Scheduler.dequeue("w1")
    Scheduler.nack(job.id)
    Process.sleep(1500)
    # Job should not be visible in main queue
    assert {:empty} = Scheduler.dequeue("w2")
    # Should be in DLQ
    assert [_] = Platform.Queue.DeadLetter.list()
  end
end

# test/integration_test.exs
defmodule Platform.IntegrationTest do
  use ExUnit.Case, async: false

  @tag timeout: 30_000
  test "end-to-end request flow: Gateway → Storage → Queue" do
    # Start the full system
    {:ok, _} = Platform.start(:normal, [])

    # Submit a request
    response = Platform.Gateway.Router.handle_request(%{
      method: "POST",
      path: "/jobs",
      headers: %{"authorization" => "Bearer #{test_jwt()}"},
      body: %{"type" => "analysis", "data" => "sample"}
    })

    assert response.status == 202
    assert Map.has_key?(response.body, "job_id")

    # Verify job is in queue
    {:ok, job} = Platform.Queue.Scheduler.dequeue("integration-worker")
    assert job.payload["type"] == "analysis"

    # Verify trace exists
    trace_id = response.headers["x-trace-id"]
    assert trace_id != nil
    spans = Platform.Tracing.Store.get_trace(trace_id)
    assert length(spans) >= 2  # at least gateway span + storage/queue span
  end

  defp test_jwt do
    # Build a valid test JWT
    secret = Application.get_env(:platform, :jwt_secret, "test_secret")
    claims = %{"sub" => "test_user", "exp" => System.system_time(:second) + 3600}
    header = Base.url_encode64(~s({"alg":"HS256","typ":"JWT"}), padding: false)
    payload = Base.url_encode64(Jason.encode!(claims), padding: false)
    sig = :crypto.mac(:hmac, :sha256, secret, "#{header}.#{payload}")
           |> Base.url_encode64(padding: false)
    "#{header}.#{payload}.#{sig}"
  end
end
```

## Benchmark

```elixir
# bench/load_test.exs
# Run with: mix run bench/load_test.exs
# or: mix system.benchmark --rps 50000 --duration 30

defmodule Platform.Bench.LoadTest do
  @target_rps 50_000
  @duration_s 30
  @mix_read 0.70
  @mix_write 0.30

  def run do
    {:ok, _} = Platform.start(:normal, [])
    Process.sleep(500)  # let system stabilize

    IO.puts("Starting load test: #{@target_rps} RPS for #{@duration_s}s")
    IO.puts("Mix: #{trunc(@mix_read * 100)}% reads / #{trunc(@mix_write * 100)}% writes")

    start_ms = System.monotonic_time(:millisecond)
    deadline_ms = start_ms + @duration_s * 1000
    interval_us = div(1_000_000, @target_rps)

    # Use Task.async_stream for concurrent workers
    requests = Stream.repeatedly(fn ->
      if :rand.uniform() < @mix_read, do: :read, else: :write
    end)
    |> Stream.take(@target_rps * @duration_s)

    results =
      Task.async_stream(
        requests,
        fn op ->
          t0 = System.monotonic_time(:microsecond)
          result = case op do
            :read -> Platform.Storage.Store.get("bench_key_#{:rand.uniform(1000)}")
            :write -> Platform.Storage.Store.put("bench_key_#{:rand.uniform(1000)}", :rand.bytes(64))
          end
          t1 = System.monotonic_time(:microsecond)
          {result, t1 - t0}
        end,
        max_concurrency: System.schedulers_online() * 4,
        timeout: 10_000,
        ordered: false
      )
      |> Enum.to_list()

    latencies = Enum.map(results, fn {:ok, {_, us}} -> us / 1000.0 end)
    errors = Enum.count(results, fn
      {:ok, {{:error, _}, _}} -> true
      {:exit, _} -> true
      _ -> false
    end)

    sorted = Enum.sort(latencies)
    n = length(sorted)
    median = Enum.at(sorted, div(n, 2))
    p95 = Enum.at(sorted, trunc(n * 0.95))
    p99 = Enum.at(sorted, trunc(n * 0.99))
    elapsed_s = (System.monotonic_time(:millisecond) - start_ms) / 1000.0
    throughput = n / elapsed_s

    IO.puts("\n=== Results ===")
    IO.puts("Total requests: #{n}")
    IO.puts("Errors:         #{errors} (#{Float.round(errors / n * 100, 2)}%)")
    IO.puts("Throughput:     #{Float.round(throughput, 0)} req/s")
    IO.puts("Median:         #{Float.round(median, 2)} ms")
    IO.puts("P95:            #{Float.round(p95, 2)} ms")
    IO.puts("P99:            #{Float.round(p99, 2)} ms")
    IO.puts("\nTargets:")
    IO.puts("  P99 < 50ms:   #{if p99 < 50, do: "PASS", else: "FAIL (#{Float.round(p99, 1)}ms)"}")
    IO.puts("  0% errors:    #{if errors == 0, do: "PASS", else: "FAIL (#{errors} errors)"}")
    IO.puts("  50k RPS:      #{if throughput >= 50_000, do: "PASS", else: "PARTIAL (#{Float.round(throughput, 0)} RPS)"}")
  end
end

Platform.Bench.LoadTest.run()
```

## Trade-offs

| Design choice | Selected approach | Alternative | Why not the alternative |
|---|---|---|---|
| Rate limiter state | ETS `:update_counter` | GenServer mailbox | GenServer serializes at >100k/s; ETS is lock-free per-key |
| Leader election | Simplified Raft | `:global` registry | `:global` has no progress guarantee under partition |
| Percentile computation | Reservoir sampling (1000 samples) | Store all observations | 3M samples/min is unbounded memory |
| Storage cache invalidation | TTL-based expiry | Event-driven invalidation | Event-driven requires distributed coordination; TTL is simple and bounded |
| Queue visibility timeout | Periodic reaper process | Per-job timer | One timer per job at 50k RPS creates 50k active timers; reaper batches the check |
| Inter-region communication | Simulated `Process.sleep` | Real Erlang distribution | Keeps the exercise local; real distribution requires cluster setup |

## Production mistakes

**Coordinated omission in your benchmark.** If you measure latency only from when you actually send the request, you miss the queue-waiting time. The correct approach is to schedule requests at fixed intervals (`t0 + i * interval`) and measure from the scheduled time, not the actual send time. Gil Tene's "How NOT to Measure Latency" talks documents this in detail.

**Not propagating trace context through `Task.async_stream`.** When a request spawns concurrent tasks, the child tasks run in fresh processes with empty process dictionaries. The trace context stored via `Process.put` is invisible to them. You must explicitly pass `trace_id` in the fun's closure and call `Platform.Tracing.Context.set_trace(trace_id)` at the start of each task.

**Circuit breaker state shared across all callers.** A circuit breaker GenServer that holds state for all downstream services in a single process becomes a serialization point. At 50k RPS with 10 services, the breaker's mailbox receives 500k messages per second. Use one GenServer per service, or use an ETS table with atomic state transitions (`:ets.select_replace/2`).

**WAL sync policy causing write latency spikes.** Calling `:file.sync/1` after every write guarantees durability but adds ~1ms of fsync latency per write. At 50k writes/second this is impossible. Use group commit: buffer writes in the WAL for 2ms, then fsync once for the batch. This reduces fsync rate from 50k/s to 500/s with minimal durability loss.

**Raft election timeout not randomized.** If all followers use the same election timeout (e.g., 300ms), they all start an election simultaneously after a leader crash, splitting votes indefinitely. Each node must pick a random timeout in a range (e.g., 300–600ms). This is explicitly required by the Raft paper.

## Resources

- Kleppmann — "Designing Data-Intensive Applications" (entire book is the reference for this exercise)
- Ongaro & Ousterhout — "In Search of an Understandable Consensus Algorithm" (Raft paper, 2014)
- Gil Tene — "How NOT to Measure Latency" (2015 talk) — https://www.youtube.com/watch?v=lJ8ydIuPFeU
- Vitter — "Random Sampling with a Reservoir" (1985) — ACM TOMS 11(1) (Algorithm R for percentiles)
- Prometheus text exposition format — https://prometheus.io/docs/instrumenting/exposition_formats/
- W3C TraceContext specification — https://www.w3.org/TR/trace-context/
- Erlang ETS documentation — https://www.erlang.org/doc/man/ets.html (`:update_counter`, `write_concurrency`)
