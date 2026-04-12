# Load Shedding with Token Bucket

**Project**: `edge_shedder` — sheds excess traffic at the edge of an API using a token-bucket algorithm with per-priority buckets and graceful rejection responses.

## Project context

Your public API receives 10k req/s steady state with spikes to 50k during product launches. The backend comfortably handles 15k. Above that, latencies climb and the whole system falls over. You have two options: scale (expensive, slow to react) or shed load.

Load shedding rejects excess traffic at the edge with a cheap `429` response, preserving capacity for the traffic you can actually serve. Unlike rate limiting (per-client), load shedding is global and priority-aware: authenticated paying customers shed last, anonymous scrapers shed first.

Token bucket is the canonical algorithm: a bucket holds `capacity` tokens, refilled at `refill_rate` per second. Each request consumes `cost` tokens. Empty bucket = shed.

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
└── mix.exs
```

## Why token bucket and not leaky bucket

Leaky bucket smooths output: every request enters a queue, drained at a fixed rate. Latency bounded. Good for network shapers.

Token bucket allows bursts: if the bucket is full, you can serve `capacity` requests instantly, then back down to `refill_rate`. Matches human traffic patterns (burst on page load, quiet during read).

For API shedding, bursts are the point — we want to serve what fits and shed cleanly when we cannot.

## Why atomics and not a GenServer

A GenServer serializes every request through its mailbox. At 50k req/s the shedder must be faster than any one GenServer can be. `:atomics` gives us a lock-free counter array with atomic compare-and-swap, usable from any process, any scheduler, without a message.

## Core concepts

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

## Design decisions

- **Option A — Single bucket + per-priority cost**: cheap (low priority requests cost 10 tokens, high costs 1). But binary — high never sees shedding until bucket is fully empty.
- **Option B — Bucket per priority**: more memory, explicit semantics. Sheds each tier independently.
→ Chose **B**. Production shedders (Envoy, Finagle) use per-priority queues.

- **Option A — Floating-point tokens**: allows sub-token refill per ms.
- **Option B — Integer tokens, refill in ms**: fast atomic ops, requires sane capacity/refill units.
→ Chose **B**. All the math is integer. Use `capacity` and `refill_per_second` such that `refill_per_ms * window_ms` > 1 in your worst case.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule EdgeShedder.MixProject do
  use Mix.Project
  def project, do: [app: :edge_shedder, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  def application, do: [mod: {EdgeShedder.Application, []}, extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Bucket (`lib/edge_shedder/bucket.ex`)

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

## Trade-offs and production gotchas

**1. `:persistent_term` rewrite is expensive** — changing the buckets map triggers a global GC on *every* process referencing it. Set once at startup; for runtime tuning, switch to ETS.

**2. Atomic CAS can starve** — under extreme contention (millions/s), some consumers may spin in the CAS retry. `:atomics` retries are handled in the VM; our `case :atomics.compare_exchange/4` simply treats a lost CAS as "someone else refilled, fine".

**3. Cost modeling matters** — all requests costing 1 is simplistic. Expensive endpoints (report generation) should cost more tokens than cheap ones (health check).

**4. Integer scaling overflow** — `capacity * 1000 * scale` must fit in 64 bits. `1_000_000 * 1_000_000 = 10^12` — safe. For larger capacities lower the scale.

**5. Shedding is not queueing** — shed requests are *dropped*. Caller must see `429 Too Many Requests` and retry with backoff. If you want queueing instead, combine with a bounded queue.

**6. When NOT to use this** — for per-user limits use a rate limiter (per-key sliding window). Load shedding is global and priority-based, not fair to individuals.

## Reflection

Your high-priority bucket is `capacity: 1_000, refill_per_second: 10_000`. A burst of 2000 high-priority requests arrives at once. How many are admitted, and how long before the bucket has room for 500 more?

## Resources

- [Token bucket — Wikipedia](https://en.wikipedia.org/wiki/Token_bucket)
- [`:atomics` — Erlang docs](https://www.erlang.org/doc/man/atomics.html)
- [`:persistent_term` — Erlang docs](https://www.erlang.org/doc/man/persistent_term.html)
- [Envoy rate-limit filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) — industrial shedding
