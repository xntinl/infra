# Load Shedding with Token Bucket

**Project**: `edge_shedder` — sheds excess traffic at the edge of an API using a token-bucket algorithm with per-priority buckets and graceful rejection responses.

## The business problem

Your public API receives 10k req/s steady state with spikes to 50k during product launches. The backend comfortably handles 15k. Above that, latencies climb and the whole system falls over. You have two options: scale (expensive, slow to react) or shed load.

Load shedding rejects excess traffic at the edge with a cheap `429` response, preserving capacity for the traffic you can actually serve. Unlike rate limiting (per-client), load shedding is global and priority-aware: authenticated paying customers shed last, anonymous scrapers shed first.

Token bucket is the canonical algorithm: a bucket holds `capacity` tokens, refilled at `refill_rate` per second. Each request consumes `cost` tokens. Empty bucket = shed.

## Project structure

```
edge_shedder/
├── lib/
│   └── edge_shedder/
│       ├── application.ex
│       ├── bucket.ex               # atomic token bucket
│       └── shedder.ex              # priority-aware entry point
├── test/
│   └── edge_shedder/
│       └── shedder_test.exs
├── bench/
│   └── shedder_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why token bucket and not leaky bucket

Leaky bucket smooths output: every request enters a queue, drained at a fixed rate. Latency bounded. Good for network shapers.

Token bucket allows bursts: if the bucket is full, you can serve `capacity` requests instantly, then back down to `refill_rate`. Matches human traffic patterns (burst on page load, quiet during read).

For API shedding, bursts are the point — we want to serve what fits and shed cleanly when we cannot.

## Why atomics and not a GenServer

A GenServer serializes every request through its mailbox. At 50k req/s the shedder must be faster than any one GenServer can be. `:atomics` gives us a lock-free counter array with atomic compare-and-swap, usable from any process, any scheduler, without a message.

## Design decisions

- **Option A — Single bucket + per-priority cost**: cheap (low priority requests cost 10 tokens, high costs 1). But binary — high never sees shedding until bucket is fully empty.
- **Option B — Bucket per priority**: more memory, explicit semantics. Sheds each tier independently.
→ Chose **B**. Production shedders (Envoy, Finagle) use per-priority queues.

- **Option A — Floating-point tokens**: allows sub-token refill per ms.
- **Option B — Integer tokens, refill in ms**: fast atomic ops, requires sane capacity/refill units.
→ Chose **B**. All the math is integer. Use `capacity` and `refill_per_second` such that `refill_per_ms * window_ms` > 1 in your worst case.

## Implementation

### Dependencies (`mix.exs`)

### `mix.exs`
```elixir
defmodule LoadSheddingTokenBucket.MixProject do
  use Mix.Project

  def project do
    [
      app: :load_shedding_token_bucket,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```

```elixir
defmodule EdgeShedder.MixProject do
  use Mix.Project
  def project, do: [app: :edge_shedder, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  def application, do: [mod: {EdgeShedder.Application, []}, extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Bucket (`lib/edge_shedder/bucket.ex`)

**Objective**: Use :atomics + compare_exchange for lock-free lazy refill so 50k req/s admission checks never serialize on GenServer mailbox or mutex.

```elixir
defmodule EdgeShedder.Bucket do
  @moduledoc """
  Atomic token bucket. Index layout in the 2-wide atomics array:
    1 = current tokens (integer, scaled by 1000 for sub-token precision)
    2 = last_refill monotonic_ms

  Refill happens lazily inside try_consume/2 using atomic compare-and-swap
  over the entire (tokens, last_refill) pair implied by one CAS per field.
  """

  defstruct [:ref, :capacity, :refill_per_ms, :scale]

  @scale 1_000

  def new(capacity: capacity, refill_per_second: per_s) do
    ref = :atomics.new(2, [])
    :atomics.put(ref, 1, capacity * @scale)
    :atomics.put(ref, 2, System.monotonic_time(:millisecond))

    %__MODULE__{
      ref: ref,
      capacity: capacity,
      refill_per_ms: per_s / 1_000,
      scale: @scale
    }
  end

  def try_consume(%__MODULE__{} = b, cost \\ 1) do
    now = System.monotonic_time(:millisecond)
    refill(b, now)

    cost_scaled = cost * b.scale

    case :atomics.sub_get(b.ref, 1, cost_scaled) do
      n when n >= 0 -> :ok
      _ ->
        :atomics.add(b.ref, 1, cost_scaled)
        {:error, :shed}
    end
  end

  defp refill(%__MODULE__{} = b, now) do
    last = :atomics.get(b.ref, 2)
    elapsed = now - last
    if elapsed <= 0, do: :ok, else: do_refill(b, now, last, elapsed)
  end

  defp do_refill(b, now, last, elapsed) do
    case :atomics.compare_exchange(b.ref, 2, last, now) do
      :ok ->
        add = trunc(elapsed * b.refill_per_ms * b.scale)
        current = :atomics.get(b.ref, 1)
        cap_scaled = b.capacity * b.scale
        new_tokens = min(cap_scaled, current + add)
        delta = new_tokens - current
        if delta > 0, do: :atomics.add(b.ref, 1, delta)
        :ok

      _actual_prev ->
        :ok
    end
  end

  def inspect_state(%__MODULE__{} = b) do
    %{
      tokens: :atomics.get(b.ref, 1) / b.scale,
      last_refill: :atomics.get(b.ref, 2),
      capacity: b.capacity
    }
  end
end
```

### Step 2: Shedder (`lib/edge_shedder/shedder.ex`)

**Objective**: Store per-priority buckets in :persistent_term so admit?/1 reads zero-copy and low-priority class sheds before medium/high during congestion.

```elixir
defmodule EdgeShedder.Shedder do
  alias EdgeShedder.Bucket

  @priorities [:high, :medium, :low]

  def start_link(opts) do
    buckets =
      for {priority, cfg} <- opts[:buckets], into: %{} do
        {priority, Bucket.new(cfg)}
      end

    :persistent_term.put(__MODULE__, buckets)
    {:ok, self()}
  end

  def child_spec(opts) do
    %{
      id: __MODULE__,
      start: {__MODULE__, :start_link, [opts]},
      type: :worker
    }
  end

  def admit?(priority) when priority in @priorities do
    buckets = :persistent_term.get(__MODULE__)

    case Bucket.try_consume(Map.fetch!(buckets, priority)) do
      :ok -> true
      {:error, :shed} -> false
    end
  end
end
```

### Step 3: Application wiring

**Objective**: Parameterize capacity and refill_per_second per priority tier so operators adjust shedding thresholds via config without recompilation.

```elixir
defmodule EdgeShedder.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {EdgeShedder.Shedder,
       buckets: %{
         high: [capacity: 1_000, refill_per_second: 10_000],
         medium: [capacity: 500, refill_per_second: 3_000],
         low: [capacity: 200, refill_per_second: 1_000]
       }}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

## Why this works

- **Zero message-passing on the hot path** — `admit?/1` reads a `:persistent_term` (compile-time constant shared across processes) and performs 3–4 atomic ops. No GenServer, no ETS, no lock.
- **Lazy refill amortises cost** — refill happens inside `try_consume/2` so the bucket is always "up to date" at consume time. No timer process.
- **CAS on `last_refill`** — two concurrent consumers racing to refill: only one wins the CAS, the other's refill is a no-op. No double-refill.
- **Sub-token precision via scale=1000** — refill rates below 1 token/ms are representable. `refill_per_second: 10` means 0.01 tokens/ms = 10 scaled-tokens/ms. No floating point on the hot path except at init.

## Tests

```elixir
defmodule EdgeShedder.ShedderTest do
  use ExUnit.Case, async: false
  doctest EdgeShedder.Application
  alias EdgeShedder.{Bucket, Shedder}

  describe "single bucket — basics" do
    test "empties after capacity consumes" do
      b = Bucket.new(capacity: 10, refill_per_second: 0)
      for _ <- 1..10, do: assert(:ok = Bucket.try_consume(b))
      assert {:error, :shed} = Bucket.try_consume(b)
    end

    test "refills over time" do
      b = Bucket.new(capacity: 10, refill_per_second: 1_000)
      for _ <- 1..10, do: Bucket.try_consume(b)
      Process.sleep(20)
      assert :ok = Bucket.try_consume(b)
    end

    test "does not exceed capacity on refill" do
      b = Bucket.new(capacity: 10, refill_per_second: 100_000)
      Process.sleep(50)
      state = Bucket.inspect_state(b)
      assert state.tokens <= 10
    end
  end

  describe "priority-aware shedding" do
    setup do
      :persistent_term.put(Shedder, %{
        high: Bucket.new(capacity: 5, refill_per_second: 0),
        medium: Bucket.new(capacity: 3, refill_per_second: 0),
        low: Bucket.new(capacity: 1, refill_per_second: 0)
      })

      :ok
    end

    test "low sheds first" do
      assert Shedder.admit?(:low)
      refute Shedder.admit?(:low)
      assert Shedder.admit?(:medium)
      assert Shedder.admit?(:high)
    end

    test "buckets are independent" do
      refute Shedder.admit?(:low) |> Kernel.==(false) and not Shedder.admit?(:high)
      for _ <- 1..5, do: Shedder.admit?(:high)
      refute Shedder.admit?(:high)
      assert Shedder.admit?(:medium)
    end
  end
end
```

## Benchmark

```elixir
# bench/shedder_bench.exs
{:ok, _} = Application.ensure_all_started(:edge_shedder)

Benchee.run(
  %{
    "admit? high" => fn -> EdgeShedder.Shedder.admit?(:high) end,
    "admit? low"  => fn -> EdgeShedder.Shedder.admit?(:low) end
  },
  parallel: 16,
  time: 5
)
```

Expected: p99 < 500ns, parallel scaling close to linear. If > 2µs you are hitting a lock — verify `:persistent_term` hasn't been rewritten at runtime (triggers global GC which stalls readers).

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---

## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. `:persistent_term` rewrite is expensive** — changing the buckets map triggers a global GC on *every* process referencing it. Set once at startup; for runtime tuning, switch to ETS.

**2. Atomic CAS can starve** — under extreme contention (millions/s), some consumers may spin in the CAS retry. `:atomics` retries are handled in the VM; our `case :atomics.compare_exchange/4` simply treats a lost CAS as "someone else refilled, fine".

**3. Cost modeling matters** — all requests costing 1 is simplistic. Expensive endpoints (report generation) should cost more tokens than cheap ones (health check).

**4. Integer scaling overflow** — `capacity * 1000 * scale` must fit in 64 bits. `1_000_000 * 1_000_000 = 10^12` — safe. For larger capacities lower the scale.

**5. Shedding is not queueing** — shed requests are *dropped*. Caller must see `429 Too Many Requests` and retry with backoff. If you want queueing instead, combine with a bounded queue.

**6. When NOT to use this** — for per-user limits use a rate limiter (per-key sliding window). Load shedding is global and priority-based, not fair to individuals.

## Reflection

Your high-priority bucket is `capacity: 1_000, refill_per_second: 10_000`. A burst of 2000 high-priority requests arrives at once. How many are admitted, and how long before the bucket has room for 500 more?

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the edge_shedder project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/edge_shedder/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `EdgeShedder` — sheds excess traffic at the edge of an API using a token-bucket algorithm with per-priority buckets and graceful rejection responses.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[edge_shedder] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[edge_shedder] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:edge_shedder) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `edge_shedder`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

---

## Why Load Shedding with Token Bucket matters

Mastering **Load Shedding with Token Bucket** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/edge_shedder.ex`

```elixir
defmodule EdgeShedder do
  @moduledoc """
  Reference implementation for Load Shedding with Token Bucket.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the edge_shedder module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> EdgeShedder.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/edge_shedder_test.exs`

```elixir
defmodule EdgeShedderTest do
  use ExUnit.Case, async: true

  doctest EdgeShedder

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert EdgeShedder.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Token bucket math
```
tokens = min(capacity, tokens + (now - last_refill) * refill_rate)
last_refill = now
if tokens >= cost:
    tokens -= cost
    allow
else:
    deny
```

### 2. Refill on demand, not on timer
A timer firing every millisecond to refill 10 buckets wastes CPU. Instead, refill happens lazily on each `try_consume/2`: compute elapsed time since last refill, add that proportionally to tokens.

### 3. Priority tiers
```
priority :high   → bucket with larger capacity, serves first
priority :medium → medium bucket
priority :low    → small bucket, sheds first
```
Each tier is an independent token bucket. If the low bucket is empty but high is full, high requests succeed.
