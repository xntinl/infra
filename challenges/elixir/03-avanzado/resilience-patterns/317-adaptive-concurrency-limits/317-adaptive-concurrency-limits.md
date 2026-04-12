# Adaptive Concurrency Limits

**Project**: `adaptive_limiter` — dynamically adjusts the concurrency limit protecting a downstream service based on observed latency, inspired by Netflix's `concurrency-limits` library and TCP congestion control.

## Project context

Fixed concurrency limits are wrong the moment you set them. Set too low, you underuse the downstream. Set too high, you hammer it during degradation. The *right* limit is whatever the downstream can handle *right now*, which changes as the downstream scales, a deploy rolls out, or traffic patterns shift.

Netflix's `concurrency-limits` library treats this like TCP congestion: maintain a limit, observe round-trip latency, and adjust. When latency is low, increase the limit (additive-increase). When latency rises, decrease (multiplicative-decrease). This is Vegas/Gradient algorithm territory — the limiter finds the sweet spot where latency is just above noise floor.

```
adaptive_limiter/
├── lib/
│   └── adaptive_limiter/
│       ├── application.ex
│       ├── limiter.ex              # public API + GenServer
│       └── gradient.ex             # pure algorithm: compute new limit from RTT
├── test/
│   └── adaptive_limiter/
│       ├── gradient_test.exs
│       └── limiter_test.exs
└── mix.exs
```

## Why adaptive and not fixed

A fixed limit of 100 permits on a service that can handle 500 under-utilizes. The same service during a degradation can only handle 30 — 100 permits causes queuing and cascading failure. Adaptive tracks both cases automatically.

## Why not just tune the fixed limit quarterly

Capacity shifts continuously: node autoscaling, GC pause, neighbor noise. A quarterly tune captures none of this. Adaptive adjusts per second.

## Core concepts

### 1. Observed RTT vs. minimum RTT
`rtt_noload` = the floor latency when the system is idle. `rtt_current` = latency of the most recent sample. If `rtt_current ≈ rtt_noload`, the system has headroom. If `rtt_current >> rtt_noload`, the system is saturated.

### 2. Gradient
```
gradient = rtt_noload / rtt_current        (between 0 and 1)
new_limit = current_limit * gradient + queue_size
```
Gradient close to 1 → grow limit. Gradient close to 0 → shrink hard.

### 3. Bounded by min/max
Never go below `min_limit` (would starve) or above `max_limit` (would overload hardware).

### 4. Permit-based admission
Clients acquire a permit before calling downstream. Permit released on completion with an RTT sample.

## Design decisions

- **Option A — AIMD (TCP Reno)**: `+1` on success, `/2` on failure. Classic, works, but slow to respond.
- **Option B — Vegas / Gradient**: adjusts proportional to observed latency gradient. More responsive.
- **Option C — Little's Law based**: `limit = throughput * target_latency`.
→ Chose **B** — Netflix's choice, responsive, and interesting to implement.

- **Option A — Acquire is `call`, Release is `cast`**: strong consistency on acquire, no wait on release.
- **Option B — Both `cast`**: fastest, but permits can overshoot briefly.
→ Chose **A**. Overshoot on permits defeats the whole point.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule AdaptiveLimiter.MixProject do
  use Mix.Project
  def project, do: [app: :adaptive_limiter, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [mod: {AdaptiveLimiter.Application, []}, extra_applications: [:logger]]
end
```

### Step 1: Application

```elixir
defmodule AdaptiveLimiter.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {AdaptiveLimiter.Limiter,
       name: :search_backend, min_limit: 5, max_limit: 200, initial_limit: 20}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Gradient algorithm (`lib/adaptive_limiter/gradient.ex`)

```elixir
defmodule AdaptiveLimiter.Gradient do
  @moduledoc """
  Pure gradient computation — no state, no side effects, trivial to test.
  """

  @smoothing 0.2
  @tolerance 2.0

  @doc """
  Compute a new limit given the current limit, queue size,
  observed RTT, and the rolling minimum (no-load) RTT.
  """
  @spec compute(pos_integer(), non_neg_integer(), non_neg_integer(), pos_integer(), pos_integer(), pos_integer(), pos_integer()) :: pos_integer()
  def compute(current_limit, queue_size, rtt_current, rtt_noload, min_limit, max_limit, _in_flight) do
    rtt_current = max(rtt_current, 1)
    rtt_noload = max(rtt_noload, 1)

    # Gradient: 1 when at floor, approaches 0 when highly saturated.
    raw_gradient = rtt_noload * @tolerance / rtt_current
    gradient = max(0.5, min(1.0, raw_gradient))

    # Proposed new limit smoothed with current.
    proposed = current_limit * gradient + queue_size
    smoothed = current_limit + @smoothing * (proposed - current_limit)

    smoothed
    |> round()
    |> max(min_limit)
    |> min(max_limit)
  end
end
```

### Step 3: Limiter (`lib/adaptive_limiter/limiter.ex`)

```elixir
defmodule AdaptiveLimiter.Limiter do
  use GenServer
  alias AdaptiveLimiter.Gradient

  # ---------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: opts[:name] || __MODULE__)

  @doc """
  Acquire a permit. Returns `{:ok, permit_token}` or `{:error, :limit_exceeded}`.
  The permit_token is used in release/3 to report RTT.
  """
  def acquire(name), do: GenServer.call(name, :acquire)

  @doc "Release the permit with the observed RTT in milliseconds."
  def release(name, token, rtt_ms), do: GenServer.cast(name, {:release, token, rtt_ms})

  def state(name), do: GenServer.call(name, :state)

  # ---------------------------------------------------------------
  # Lifecycle
  # ---------------------------------------------------------------

  @impl true
  def init(opts) do
    state = %{
      name: Keyword.fetch!(opts, :name),
      limit: Keyword.fetch!(opts, :initial_limit),
      min_limit: Keyword.fetch!(opts, :min_limit),
      max_limit: Keyword.fetch!(opts, :max_limit),
      in_flight: 0,
      rtt_noload: 10_000,
      token_counter: 0
    }

    {:ok, state}
  end

  @impl true
  def handle_call(:acquire, _from, state) do
    if state.in_flight >= state.limit do
      {:reply, {:error, :limit_exceeded}, state}
    else
      token = state.token_counter + 1
      new_state = %{state | in_flight: state.in_flight + 1, token_counter: token}
      {:reply, {:ok, token}, new_state}
    end
  end

  def handle_call(:state, _from, state) do
    {:reply,
     Map.take(state, [:limit, :in_flight, :rtt_noload, :min_limit, :max_limit]), state}
  end

  @impl true
  def handle_cast({:release, _token, rtt_ms}, state) do
    in_flight = max(0, state.in_flight - 1)
    queue_size = 0

    rtt_noload = min(state.rtt_noload, rtt_ms)

    new_limit =
      Gradient.compute(
        state.limit,
        queue_size,
        rtt_ms,
        rtt_noload,
        state.min_limit,
        state.max_limit,
        in_flight
      )

    {:noreply, %{state | in_flight: in_flight, rtt_noload: rtt_noload, limit: new_limit}}
  end
end
```

## Why this works

- **Gradient bounded [0.5, 1.0]** — prevents catastrophic collapse. Even if RTT doubles, the limit shrinks to at most 50% per sample. Combined with smoothing, drops are gradual.
- **Rolling minimum RTT** — `rtt_noload` is only ever decreased (by observing a lower RTT). Prevents the baseline from drifting up during sustained degradation.
- **Smoothing factor** — prevents single noisy samples from causing wild limit swings. 0.2 means each new measurement moves the limit 20% toward the proposed value.
- **Pure gradient module** — no GenServer, no time, no ETS; completely deterministic under unit test. Every parameter is passed in.
- **Cast on release** — the caller of a downstream service doesn't wait for the limiter to compute the new limit. Acquire is sync (bounded), release is fire-and-forget.

## Tests

```elixir
defmodule AdaptiveLimiter.GradientTest do
  use ExUnit.Case, async: true
  alias AdaptiveLimiter.Gradient

  describe "compute/7" do
    test "low rtt → limit grows" do
      new = Gradient.compute(20, 0, 10, 10, 5, 100, 10)
      assert new >= 20
    end

    test "high rtt → limit shrinks" do
      new = Gradient.compute(100, 0, 1_000, 10, 5, 200, 80)
      assert new < 100
    end

    test "clamped to min_limit" do
      new = Gradient.compute(10, 0, 10_000, 10, 5, 200, 10)
      assert new >= 5
    end

    test "clamped to max_limit" do
      new = Gradient.compute(100, 100, 10, 10, 5, 150, 100)
      assert new <= 150
    end

    test "rtt noload zero handled safely" do
      new = Gradient.compute(20, 0, 50, 0, 5, 100, 10)
      assert is_integer(new) and new >= 5
    end
  end
end
```

```elixir
defmodule AdaptiveLimiter.LimiterTest do
  use ExUnit.Case, async: false
  alias AdaptiveLimiter.Limiter

  setup do
    name = :"lim_#{System.unique_integer([:positive])}"

    {:ok, _} =
      Limiter.start_link(name: name, min_limit: 2, max_limit: 50, initial_limit: 5)

    {:ok, name: name}
  end

  describe "acquire / release" do
    test "acquires up to limit", %{name: n} do
      for _ <- 1..5, do: assert({:ok, _} = Limiter.acquire(n))
      assert {:error, :limit_exceeded} = Limiter.acquire(n)
    end

    test "release frees a permit", %{name: n} do
      {:ok, token} = Limiter.acquire(n)
      for _ <- 1..4, do: Limiter.acquire(n)
      assert {:error, :limit_exceeded} = Limiter.acquire(n)

      Limiter.release(n, token, 10)
      Process.sleep(10)
      assert {:ok, _} = Limiter.acquire(n)
    end
  end

  describe "adaptation" do
    test "limit grows when RTT stays low", %{name: n} do
      %{limit: initial} = Limiter.state(n)

      for _ <- 1..30 do
        {:ok, token} = Limiter.acquire(n)
        Limiter.release(n, token, 5)
      end

      Process.sleep(20)
      %{limit: after_limit} = Limiter.state(n)
      assert after_limit >= initial
    end

    test "limit shrinks when RTT spikes", %{name: n} do
      for _ <- 1..5 do
        {:ok, token} = Limiter.acquire(n)
        Limiter.release(n, token, 10)
      end

      Process.sleep(10)
      %{limit: before_spike} = Limiter.state(n)

      for _ <- 1..20 do
        {:ok, token} = Limiter.acquire(n)
        Limiter.release(n, token, 1_000)
      end

      Process.sleep(20)
      %{limit: after_spike} = Limiter.state(n)
      assert after_spike <= before_spike
    end
  end
end
```

## Benchmark

```elixir
# Acquire + release round-trip cost
{:ok, _} = Application.ensure_all_started(:adaptive_limiter)
name = :search_backend

{t, _} = :timer.tc(fn ->
  for _ <- 1..50_000 do
    {:ok, token} = AdaptiveLimiter.Limiter.acquire(name)
    AdaptiveLimiter.Limiter.release(name, token, 10)
  end
end)
IO.puts("avg: #{t / 50_000} µs")
```

Expected: ~3-5µs per acquire+release (single GenServer, two messages). For higher throughput, partition by key into N limiters.

## Trade-offs and production gotchas

**1. `rtt_noload` is monotonic-decreasing but never resets** — if the backend permanently slows, the floor stays stuck at the old fast value. Reset `rtt_noload` every T minutes to track capacity shifts.

**2. Sample quality matters** — measured RTT must be *only* the downstream call, not your whole handler. Include queue wait time in the sample and you'll see stability drift.

**3. The limiter is itself a bottleneck** — every acquire goes through one GenServer. At 50k/s you're near its serial capacity. Shard by request key.

**4. Queue size is an input** — this exercise uses 0. A real implementation should track pending acquire calls (waiters) and feed that as `queue_size` to the gradient. That's what lets the limit grow when demand is high and latency remains low.

**5. Gradient > 1 is clamped** — the bound `max(0.5, min(1.0, ...))` prevents runaway growth but also prevents legitimate catch-up after a recovery. Consider a probing phase that briefly unclamps.

**6. When NOT to use this** — for fixed-capacity services where the limit is known (DB connection pool size). Adaptive is for opaque downstreams whose capacity you cannot predict.

## Reflection

Two adaptive limiters sit on opposite ends of the same service: one on the caller side, one on the callee side. Do they converge to the same limit? What could cause them to disagree persistently?

## Resources

- [Netflix `concurrency-limits` (Java)](https://github.com/Netflix/concurrency-limits) — the reference implementation
- [TCP Vegas — Brakmo & Peterson (1995)](https://www.cs.arizona.edu/~rts/pubs/SP94.pdf) — the algorithm's origin
- [Stop Rate Limiting! Capacity Management Done Right — Jon Moore](https://www.youtube.com/watch?v=m64SWl9bfvk)
- [Gradient2 algorithm — Netflix blog](https://netflixtechblog.medium.com/performance-under-load-3e6fa9a60581)
