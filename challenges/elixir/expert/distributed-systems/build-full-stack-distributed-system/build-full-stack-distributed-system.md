# Full-Stack Distributed System: Production-Grade SaaS Platform

**Project**: `platform` — production-grade distributed system with API gateway, consensus-based coordination, persistent storage, job queue, stream processing, and observability. Target: **50,000 req/s sustained at p99 < 50ms** on a single BEAM cluster.

---

## Why full-stack distributed systems matter

Individual distributed-systems primitives (Raft, gossip, CRDTs) are fundamental but isolated. In production, the *composition* is what fails: Raft under a gossip-driven topology, circuit breakers tripping on a rate-limited gateway, the job queue backpressuring a Phoenix endpoint, the storage compactor pausing at the wrong moment. This exercise is about building all six major subsystems as one cooperating mesh so the failure modes emerge.

The 50k RPS target is deliberately aggressive for a single machine. It forces you to avoid process-per-request on the hot path (use ETS + `:ets.update_counter/3`), batch telemetry, keep the gateway stateless, and ensure backpressure propagates from disk through the queue to the client. These constraints are *exactly* what production Elixir architects debate.

References:
- [Release It! Patterns for Resilient Software, Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book, ch. 21 Handling Overload](https://sre.google/sre-book/handling-overload/)
- [Designing Data-Intensive Applications, Kleppmann](https://dataintensive.net/)

---

## The business problem

You are building the backend for a SaaS analytics product. Clients submit analytics jobs via HTTPS. Each request must:

1. Authenticate (JWT HS256) and be rate-limited per tenant (1000 req/min default).
2. Be routed through a coordinator that assigns work to a worker pool and persists job metadata.
3. Publish an async event on a durable queue so the stream processor can update dashboards.
4. Emit traces + metrics so an SRE can debug p99 regressions.
5. Survive node crashes: leader election must recover within 5 seconds; the queue must not lose acknowledged jobs.
6. Refuse traffic (503 with `Retry-After`) when backpressure accumulates rather than silently buffering.

Partial service degradation (storage compaction causing 500ms read stalls) must not cause cascading 5xx on the gateway. Tail-latency amplification is the primary enemy.

---

## Project structure

```
platform/
├── lib/
│   ├── platform.ex
│   └── platform/
│       ├── application.ex         # root supervisor tree, rest_for_one
│       ├── gateway.ex             # Plug-based API: auth, rate limit, routing
│       ├── coordinator.ex         # GenServer: job assignment + storage writes
│       ├── storage.ex             # ETS-backed KV with periodic snapshots
│       ├── queue.ex               # durable on-disk ring buffer with ack
│       ├── processor.ex           # streaming consumer (GenStage style)
│       ├── raft_simple.ex         # leader election only, no log replication
│       ├── circuit_breaker.ex     # per-service 3-state breaker
│       ├── rate_limiter.ex        # ETS token bucket, atomic update_counter
│       ├── telemetry.ex           # reservoir-sampled percentiles
│       └── health.ex              # liveness + readiness probes
├── script/
│   └── main.exs                   # stress test: 50k RPS + chaos injection
├── test/
│   └── platform_test.exs          # e2e, fault injection, linearizability
└── mix.exs
```

---

## Design decisions

**Option A — synchronous request flow (all blocking)**
- Pros: simple to reason about, client sees errors immediately.
- Cons: tail-latency amplification. Any backend stall multiplies into p99 breaches; one slow dependency starves the pool.

**Option B — hybrid (sync gateway, async queue for side effects)** (chosen)
- Pros: decouples ingestion latency from pipeline latency; natural backpressure point at queue depth; allows graceful shedding.
- Cons: eventual consistency on downstream dashboards; client cannot observe full-pipeline completion without polling.

**Option C — fully asynchronous (client submits, polls for result)**
- Pros: maximum decoupling and throughput.
- Cons: UX is worse for interactive requests; doubles the surface area (submit API + poll API + webhook fallback).

Chose **B**. The SaaS use case has two distinct latency budgets: "accept the job" (<50ms) and "dashboard updated" (seconds). Coupling them under A creates false urgency; C creates a second API unnecessarily.

**Rate limiter — ETS atomic counters vs GenServer**: a GenServer serializes every check through one mailbox. At 50k RPS the mailbox is the bottleneck. ETS `:update_counter/3` with `:write_concurrency` gives atomic increment in ~100ns. A single background process refills tokens on a 100ms tick. We accept a brief over-limit burst during refill lag in exchange for staying under 1µs per check.

**Simplified Raft for leader election only**: full Raft with log replication has quadratic message cost on config changes. For leader-of-coordinator we only need election + heartbeat; the leader's state can be reconstructed from the storage snapshot, so log replication is wasted work.

References:
- [In Search of an Understandable Consensus Algorithm (Raft), Ongaro & Ousterhout](https://raft.github.io/raft.pdf)
- [Vitter's Algorithm R for reservoir sampling](https://www.cs.umd.edu/~samir/498/vitter.pdf)

---

## Implementation

### `mix.exs`

```elixir
defmodule Platform.MixProject do
  use Mix.Project

  def project do
    [
      app: :platform,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Platform.Application, []}
    ]
  end

  defp deps do
    [
      {:plug, "~> 1.15"},
      {:plug_cowboy, "~> 2.6"},
      {:jason, "~> 1.4"},
      {:telemetry, "~> 1.2"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/platform.ex`

```elixir
defmodule Platform do
  @moduledoc """
  Full-stack distributed platform façade.

  The Platform module exposes the public API: submit/1 accepts a job, authenticates,
  rate-limits, persists, enqueues, and returns a short-lived request ID. Operators
  interact with the system through this module and `Platform.Health`.

  ## Constraints
    * Sustained target: 50_000 req/s at p99 < 50ms on a 16-core machine.
    * Bounded request: max body 256 KiB, max JSON depth 32, max execution 250ms.
    * Tenant isolation: one tenant's abuse cannot degrade another's p99.

  ## Failure semantics
    * If the coordinator is unavailable, submit/1 returns `{:error, :coordinator_down}`.
    * If the circuit breaker for storage is open, submit/1 returns `{:error, :shed_load}`
      with a `Retry-After` suggestion derived from current queue depth.
    * Acknowledged submits are durable: once `{:ok, request_id}` is returned the
      job survives node restart.
  """

  alias Platform.{Coordinator, Gateway, RateLimiter, Telemetry}

  @type tenant_id :: binary()
  @type request_id :: binary()
  @type submit_error ::
          :invalid_token
          | :rate_limited
          | :payload_too_large
          | :coordinator_down
          | :shed_load
          | {:validation, term()}

  @max_body_bytes 262_144
  @max_json_depth 32

  @doc """
  Submits an analytics job. Returns a request identifier for polling.

  ## Examples

      iex> Platform.submit(%{tenant: "t1", token: "valid", payload: %{"q" => "sum"}})
      {:ok, _request_id}

      iex> Platform.submit(%{tenant: "t1", token: "bad"})
      {:error, :invalid_token}
  """
  @spec submit(map()) :: {:ok, request_id()} | {:error, submit_error()}
  def submit(%{tenant: tenant, token: token, payload: payload} = req)
      when is_binary(tenant) and is_binary(token) and is_map(payload) do
    started_at = System.monotonic_time(:microsecond)

    with :ok <- validate_size(payload),
         :ok <- validate_depth(payload),
         {:ok, _claims} <- Gateway.verify_token(tenant, token),
         :ok <- RateLimiter.check(tenant),
         {:ok, request_id} <- Coordinator.enqueue(tenant, payload) do
      elapsed = System.monotonic_time(:microsecond) - started_at
      Telemetry.record(:submit_ok, elapsed, tenant: tenant)
      {:ok, request_id}
    else
      {:error, reason} = err ->
        elapsed = System.monotonic_time(:microsecond) - started_at
        Telemetry.record(:submit_err, elapsed, tenant: tenant, reason: reason)
        err
    end
  end

  def submit(_), do: {:error, {:validation, :malformed_request}}

  @spec validate_size(map()) :: :ok | {:error, :payload_too_large}
  defp validate_size(payload) do
    if :erlang.external_size(payload) > @max_body_bytes do
      {:error, :payload_too_large}
    else
      :ok
    end
  end

  @spec validate_depth(term()) :: :ok | {:error, :payload_too_deep}
  defp validate_depth(term), do: depth(term, 0)

  defp depth(_, d) when d > @max_json_depth, do: {:error, :payload_too_deep}
  defp depth(m, d) when is_map(m), do: Enum.reduce_while(m, :ok, fn {_, v}, _ -> step(v, d) end)
  defp depth(l, d) when is_list(l), do: Enum.reduce_while(l, :ok, fn v, _ -> step(v, d) end)
  defp depth(_, _), do: :ok

  defp step(v, d) do
    case depth(v, d + 1) do
      :ok -> {:cont, :ok}
      err -> {:halt, err}
    end
  end
end
```
### `test/platform_test.exs`

```elixir
defmodule PlatformTest do
  use ExUnit.Case, async: true
  doctest Platform

  alias Platform.{Gateway, RateLimiter, Coordinator, CircuitBreaker}

  setup do
    start_supervised!(Platform.Application)
    Gateway.install_test_token("t1", "valid")
    :ok
  end

  describe "submit/1 happy path" do
    test "returns {:ok, request_id} for valid request" do
      assert {:ok, id} = Platform.submit(%{tenant: "t1", token: "valid", payload: %{"q" => 1}})
      assert is_binary(id)
      assert byte_size(id) >= 16
    end
  end

  describe "submit/1 input validation" do
    test "rejects oversized payload" do
      big = %{"blob" => :binary.copy(<<0>>, 300_000)}
      assert {:error, :payload_too_large} =
               Platform.submit(%{tenant: "t1", token: "valid", payload: big})
    end

    test "rejects over-deep payload" do
      nested = Enum.reduce(1..40, %{}, fn _, acc -> %{"a" => acc} end)
      assert {:error, :payload_too_deep} =
               Platform.submit(%{tenant: "t1", token: "valid", payload: nested})
    end

    test "rejects malformed input" do
      assert {:error, {:validation, _}} = Platform.submit(%{})
    end
  end

  describe "rate limiting" do
    test "enforces per-tenant quota" do
      RateLimiter.configure("t_burst", rps: 5, burst: 5)
      Gateway.install_test_token("t_burst", "tok")
      req = %{tenant: "t_burst", token: "tok", payload: %{}}

      Enum.each(1..5, fn _ -> assert {:ok, _} = Platform.submit(req) end)
      assert {:error, :rate_limited} = Platform.submit(req)
    end
  end

  describe "circuit breaker" do
    test "opens on repeated storage failures and sheds load" do
      CircuitBreaker.force_open(:storage)
      assert {:error, :shed_load} =
               Platform.submit(%{tenant: "t1", token: "valid", payload: %{"q" => 1}})
    end
  end

  describe "tenant isolation" do
    test "abuser on t_a does not degrade t_b" do
      RateLimiter.configure("t_a", rps: 100, burst: 100)
      RateLimiter.configure("t_b", rps: 100, burst: 100)
      Gateway.install_test_token("t_a", "a")
      Gateway.install_test_token("t_b", "b")

      # flood t_a past its quota
      for _ <- 1..200, do: Platform.submit(%{tenant: "t_a", token: "a", payload: %{}})

      assert {:ok, _} = Platform.submit(%{tenant: "t_b", token: "b", payload: %{}})
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for the platform. Spins up the supervision tree,
  drives 50k requests/s across multiple tenants for 30 seconds, then injects
  three chaos scenarios: coordinator crash, storage latency spike, network
  partition. Exits non-zero if p99 breaches the SLO.
  """

  @target_rps 50_000
  @duration_s 30
  @slo_p99_us 50_000
  @slo_error_rate 0.001

  def main do
    {:ok, _} = Application.ensure_all_started(:platform)
    configure_tenants()

    IO.puts("=== Platform stress test: #{@target_rps} RPS for #{@duration_s}s ===")
    baseline = run_phase(:baseline, @target_rps, @duration_s)

    IO.puts("\n=== Chaos: coordinator crash during steady load ===")
    chaos_crash_coordinator(self())
    crash_phase = run_phase(:crash, @target_rps, 15)

    IO.puts("\n=== Chaos: storage latency spike 500ms ===")
    chaos_storage_stall(500)
    stall_phase = run_phase(:stall, @target_rps, 10)

    IO.puts("\n=== Chaos: simulated 30% packet loss ===")
    chaos_packet_loss(0.30)
    loss_phase = run_phase(:loss, 20_000, 10)

    report([baseline, crash_phase, stall_phase, loss_phase])
  end

  defp configure_tenants do
    for i <- 1..10 do
      tenant = "bench_t#{i}"
      Platform.RateLimiter.configure(tenant, rps: 10_000, burst: 10_000)
      Platform.Gateway.install_test_token(tenant, "tok#{i}")
    end
  end

  defp run_phase(name, target_rps, seconds) do
    started = System.monotonic_time(:millisecond)
    interval_us = div(1_000_000, target_rps)
    parent = self()
    workers = System.schedulers_online() * 4
    per_worker = div(target_rps, workers)

    tasks =
      for w <- 1..workers do
        Task.async(fn -> worker_loop(w, per_worker, seconds, interval_us, parent) end)
      end

    results = Enum.flat_map(tasks, &Task.await(&1, (seconds + 5) * 1000))
    elapsed_ms = System.monotonic_time(:millisecond) - started

    percentiles = compute_percentiles(results)
    err = Enum.count(results, &match?({:err, _}, &1)) / max(length(results), 1)

    IO.puts("""
      phase=#{name}
      sent=#{length(results)} err=#{Float.round(err * 100, 3)}%
      p50=#{percentiles.p50}µs p95=#{percentiles.p95}µs p99=#{percentiles.p99}µs p999=#{percentiles.p999}µs
      throughput=#{round(length(results) / (elapsed_ms / 1000))}/s
    """)

    Map.merge(percentiles, %{phase: name, error_rate: err, samples: length(results)})
  end

  defp worker_loop(w, count, seconds, interval_us, parent) do
    tenant = "bench_t#{rem(w - 1, 10) + 1}"
    token = "tok#{rem(w - 1, 10) + 1}"
    deadline = System.monotonic_time(:millisecond) + seconds * 1000

    Enum.reduce_while(1..count * seconds, [], fn _, acc ->
      if System.monotonic_time(:millisecond) >= deadline do
        {:halt, acc}
      else
        t0 = System.monotonic_time(:microsecond)
        result = Platform.submit(%{tenant: tenant, token: token, payload: payload()})
        elapsed = System.monotonic_time(:microsecond) - t0
        busy_wait(interval_us - elapsed)
        {:cont, [tag(result, elapsed) | acc]}
      end
    end)
  end

  defp busy_wait(us) when us <= 0, do: :ok
  defp busy_wait(us), do: Process.sleep(max(div(us, 1000), 1))

  defp payload, do: %{"q" => :rand.uniform(1000), "ts" => System.system_time(:second)}
  defp tag({:ok, _}, us), do: {:ok, us}
  defp tag({:error, r}, us), do: {:err, {r, us}}

  defp compute_percentiles(results) do
    lats = for r <- results, do: elem(r, 1) |> extract()
    sorted = Enum.sort(lats)
    n = length(sorted)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(sorted, div(n, 2)),
         p95: Enum.at(sorted, div(n * 95, 100)),
         p99: Enum.at(sorted, div(n * 99, 100)),
         p999: Enum.at(sorted, min(div(n * 999, 1000), n - 1))
       }
  end

  defp extract({_, us}) when is_integer(us), do: us
  defp extract(us) when is_integer(us), do: us

  defp chaos_crash_coordinator(parent) do
    spawn(fn ->
      Process.sleep(2000)
      Platform.Coordinator |> Process.whereis() |> Process.exit(:kill)
      send(parent, :coord_killed)
    end)
  end

  defp chaos_storage_stall(ms) do
    Platform.Storage.inject_stall(ms, duration_ms: 5_000)
  end

  defp chaos_packet_loss(ratio) do
    Platform.Coordinator.inject_drop(ratio, duration_ms: 5_000)
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      slo_p99 = p.p99 <= 50_000
      slo_err = p.error_rate <= 0.001
      status = if slo_p99 and slo_err, do: "PASS", else: "FAIL"
      IO.puts("#{p.phase}: #{status} (p99=#{p.p99}µs, err=#{Float.round(p.error_rate * 100, 3)}%)")
    end)

    bad = Enum.any?(phases, fn p -> p.p99 > 50_000 or p.error_rate > 0.001 end)
    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
---

## Error Handling and Recovery

The platform classifies failures on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide).

### Critical failures (halt subsystem, surface to SRE)

| Condition | Detection | Response |
|---|---|---|
| Storage corruption (checksum mismatch on replay) | `Platform.Storage.recover/0` fails | Refuse boot, alert via PagerDuty, preserve WAL for forensic analysis |
| Raft term divergence > 5 without election | Heartbeat scheduler notices stale term | Step down leader, enter safe mode (reject writes), page SRE |
| Queue disk exhaustion | `:file.position` returns `{:error, :enospc}` | Stop accepting new enqueues, drain in-flight, emit SEV1 alert |
| Supervisor reaches `max_restarts` | BEAM default behavior | Process exits non-zero; orchestrator (systemd/k8s) reschedules |

### Recoverable failures (retry with backoff, circuit-break downstream)

| Condition | Policy | Bounds |
|---|---|---|
| Coordinator RPC timeout | Exponential backoff with 50ms base, jitter 0-20% | Max 3 attempts, max 2s total |
| Storage read after compaction pause | Retry with fresh snapshot | Max 2 attempts, fail open (serve stale) |
| Processor lag > 5s | Backpressure upstream (queue refuses enqueue) | Shed load until lag < 1s |
| Rate-limit breach | Return 429 with `Retry-After` | Client responsibility to back off |
| Auth token within 5s of expiry | Refresh transparently | One attempt; fail closed on error |

### Recovery protocol on node restart

1. **Snapshot replay**: Storage reads the last full snapshot (persisted every 1 minute) then replays WAL entries with seq > snapshot_seq. Checksums verified per entry.
2. **Raft rejoin**: Node starts as follower, waits for heartbeat. If no heartbeat in `election_timeout` (randomized 150-300ms), becomes candidate.
3. **Queue replay**: The queue scans the on-disk ring buffer for unacked entries (written but not marked consumed). These are re-delivered to the processor; idempotency on the consumer side is required.
4. **Circuit breaker cold start**: All breakers start in `:half_open` to probe dependency health before accepting traffic, preventing thundering herd.
5. **Health gate**: Gateway refuses `/v1/submit` until `/healthz/ready` returns 200. Liveness passes once the supervisor is up; readiness requires storage + coordinator + queue all healthy.

### Bulkheads

- Rate limiter runs under its own 1-for-1 supervisor. If it crashes, the gateway falls back to a coarse global limit (1000 req/s total) rather than rejecting all traffic.
- Telemetry emission is batched every 100ms. Buffer is bounded at 10k events; overflow drops oldest with a `dropped_events` counter.
- Processor runs `max_demand: 500` to bound memory; a slow consumer blocks the queue, not the gateway.

---

## Performance Targets

| Metric | Target | Measurement |
|---|---|---|
| Sustained ingest | **50,000 req/s** | `script/main.exs` baseline phase over 30s |
| Submit latency p50 | **< 2 ms** | end-to-end, JWT verify → enqueue ack |
| Submit latency p99 | **< 50 ms** | same, under 50k concurrent |
| Submit latency p99.9 | **< 200 ms** | includes GC pauses, compaction |
| Error rate under baseline | **< 0.1 %** | excludes 429s, counts 5xx only |
| Leader re-election | **< 5 s** | from kill to new leader accepting writes |
| Queue enqueue ack | **< 500 µs** | local fsync disabled; durability via replication |
| Telemetry overhead | **< 1 %** | on-CPU time of Platform.Telemetry |
| Memory per idle connection | **< 50 KiB** | Cowboy default plus plug pipeline |
| Cold start to serving | **< 3 s** | supervisor ready + warm snapshot loaded |

**Baselines we should beat**:
- Nginx + Postgres pipeline typically saturates at 20-30k RPS per core before network saturates; our BEAM-native pipeline should reach 50k because we avoid cross-process JSON serialization within the node.
- A naive `GenServer`-based rate limiter tops out at ~15k RPS (one process = bottleneck). ETS-based must clear 500k checks/s per core.

---

## Key concepts

### 1. Cascading failure is the default; isolation is the work
Under load, any slow dependency (storage compaction, partition re-election) causes every upstream component to accumulate pending requests. Mailboxes grow, GC thrashes, and the system degrades non-linearly. The cure is not "making things faster"; it is *refusing work when downstream is unhealthy* — circuit breakers, bulkheads, load shedding. A p99 SLO is maintained by rejecting the right 1% rather than slowing the other 99%.

### 2. Backpressure must propagate to the edge
If the queue is full but the gateway keeps accepting requests, memory grows unboundedly until OOM. Every hot path must expose a bounded buffer with a visible pressure signal. In this system, `Coordinator.enqueue/2` returns `{:error, :shed_load}` when the queue depth exceeds the watermark; the gateway converts that to `503 Retry-After`.

### 3. Tenant isolation ≠ tenant fairness
A rate limiter that caps each tenant at a fixed budget provides isolation. But under global overload, two small tenants starving alongside one large abuser is fairness. Implement *both*: hard per-tenant caps (isolation) and global weighted fair queuing above 80% utilization (fairness).

### 4. The leader election is not the interesting part
Election takes seconds; steady-state leadership runs for hours. The interesting engineering is in what happens *during* a re-election: inflight writes must neither be lost nor double-applied. Acks returned before term commit are unsafe; the coordinator must hold client responses until the leader has a quorum.

### 5. Observability is a correctness property, not a nice-to-have
In a 50k RPS system, the only way to know you hit the SLO is to measure. Reservoir sampling (Vitter's R) gives bounded-memory p99 estimates within ±1% at 95% confidence with 1000 samples — cheap enough to run on every request.

### 6. "Simple" building blocks hide complexity
`:global.register_name/2` looks simple and is correct under ideal conditions; it fails silently under asymmetric partitions. For coordinator-of-leader we implement a small Raft explicitly so the failure mode is defined (partition minority cannot win) rather than latent.

---

## Why Full-Stack Distributed System matters

Mastering **Full-Stack Distributed System** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.
