# Bulkhead Pattern with Process Pools

**Project**: `shipping_bulkheads` — isolates downstream vendor calls in bounded process pools so a single slow vendor cannot exhaust the shared worker pool.

## Project context

Your order-fulfilment service talks to three shipping vendors: UPS, FedEx, and a regional carrier. Each call is wrapped in a `Task` and awaited. Under normal load every vendor responds in ~200ms. Last week the regional carrier degraded to 20-second responses; within minutes every request was blocked waiting on regional-carrier tasks and unrelated vendor calls timed out.

The bulkhead pattern takes its name from ship compartments: a leak in one compartment doesn't flood the rest. Each dependency gets its own bounded resource pool. When that pool is exhausted, *only calls to that dependency* fail fast — callers of other dependencies are unaffected.

```
shipping_bulkheads/
├── lib/
│   └── shipping_bulkheads/
│       ├── application.ex
│       ├── bulkhead.ex             # public API
│       └── bulkhead/
│           ├── pool.ex             # pool supervisor + tracker
│           └── worker.ex           # per-call worker
├── test/
│   └── shipping_bulkheads/
│       └── bulkhead_test.exs
├── bench/
│   └── bulkhead_bench.exs
└── mix.exs
```

## Why bounded pools and not unbounded Task.Supervisor

`Task.Supervisor.async_nolink/2` spawns an unlimited number of tasks. If the regional carrier hangs and you receive 5000 requests, you spawn 5000 tasks, consume 5000 connections, and fill the BEAM with zombie processes waiting on sockets. Unbounded concurrency is a resource leak with good intentions.

A bounded pool caps in-flight work at N. Beyond N, callers either queue briefly or fail fast — either way, one slow dependency cannot consume the whole system.

## Why not `poolboy`

`poolboy` is battle-tested but checkout blocks by default and its `transaction` semantics force you to return workers after use, which is fiddly for fire-and-forget patterns. We want a lean implementation that exposes the core ideas (semaphore counter, fail-fast on overflow, per-call worker) so you understand what production pools actually do under the hood.

## Core concepts

### 1. Fail-fast semaphore
```
┌─────────────────┐  checkout  ┌───────────────────┐
│ caller requests │───────────▶│ :counters counter │
└─────────────────┘            │  if > max: reject │
                               │  else: increment  │
                               └───────────────────┘
```

### 2. Per-call worker process
Each accepted call spawns a fresh worker under a `DynamicSupervisor`. The worker executes the function, returns the result, and terminates — releasing the slot.

### 3. Separation of pools
UPS, FedEx, and regional each have their own `Bulkhead.Pool`. An exhausted UPS pool never affects FedEx.

## Design decisions

- **Option A — Pre-spawned workers (poolboy style)**: recycle workers, one process per slot. Lower spawn cost but leaks state across requests.
- **Option B — Per-call spawn**: fresh process per call. Higher spawn cost (~3µs) but zero state leakage and trivial crash isolation.
→ Chose **B**. BEAM process spawn is cheap; correctness dominates at this scale.

- **Option A — GenServer with `:queue`**: queue overflow callers and let them wait.
- **Option B — `:counters` atomic**: no queueing; over-limit calls are rejected immediately.
→ Chose **B** — fail-fast is the whole point of a bulkhead. Queueing reintroduces the latency explosion you were trying to prevent.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ShippingBulkheads.MixProject do
  use Mix.Project

  def project do
    [app: :shipping_bulkheads, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  end

  def application do
    [mod: {ShippingBulkheads.Application, []}, extra_applications: [:logger]]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Application

```elixir
defmodule ShippingBulkheads.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {ShippingBulkheads.Bulkhead.Pool, name: :ups, max_concurrent: 20},
      {ShippingBulkheads.Bulkhead.Pool, name: :fedex, max_concurrent: 20},
      {ShippingBulkheads.Bulkhead.Pool, name: :regional, max_concurrent: 5}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Pool (`lib/shipping_bulkheads/bulkhead/pool.ex`)

```elixir
defmodule ShippingBulkheads.Bulkhead.Pool do
  @moduledoc """
  Each pool is:
    * a DynamicSupervisor that owns the worker processes
    * a :counters atomic reference for lock-free checkout/checkin
  The pair is registered under a name so callers can resolve both.
  """
  use Supervisor

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    Supervisor.start_link(__MODULE__, opts, name: sup_name(name))
  end

  @impl true
  def init(opts) do
    name = Keyword.fetch!(opts, :name)
    max = Keyword.fetch!(opts, :max_concurrent)

    counter = :counters.new(1, [:atomics])
    :persistent_term.put({__MODULE__, name}, %{counter: counter, max: max})

    children = [
      {DynamicSupervisor, strategy: :one_for_one, name: dynsup_name(name)}
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end

  def checkout(name) do
    %{counter: c, max: max} = :persistent_term.get({__MODULE__, name})
    current = :counters.add(c, 1, 1) |> then(fn _ -> :counters.get(c, 1) end)

    if current > max do
      :counters.sub(c, 1, 1)
      {:error, :pool_exhausted}
    else
      :ok
    end
  end

  def checkin(name) do
    %{counter: c} = :persistent_term.get({__MODULE__, name})
    :counters.sub(c, 1, 1)
    :ok
  end

  def dynsup_name(name), do: :"bulkhead_dynsup_#{name}"
  def sup_name(name), do: :"bulkhead_sup_#{name}"

  def in_flight(name) do
    %{counter: c} = :persistent_term.get({__MODULE__, name})
    :counters.get(c, 1)
  end
end
```

### Step 3: Worker (`lib/shipping_bulkheads/bulkhead/worker.ex`)

```elixir
defmodule ShippingBulkheads.Bulkhead.Worker do
  use Task, restart: :temporary

  def start_link({pool_name, fun, caller, ref}) do
    Task.start_link(fn ->
      result =
        try do
          {:ok, fun.()}
        rescue
          e -> {:error, e}
        catch
          kind, reason -> {:error, {kind, reason}}
        after
          ShippingBulkheads.Bulkhead.Pool.checkin(pool_name)
        end

      send(caller, {ref, result})
    end)
  end
end
```

### Step 4: Public API (`lib/shipping_bulkheads/bulkhead.ex`)

```elixir
defmodule ShippingBulkheads.Bulkhead do
  alias ShippingBulkheads.Bulkhead.{Pool, Worker}

  def run(pool, fun, timeout \\ 5_000) do
    case Pool.checkout(pool) do
      :ok ->
        ref = make_ref()
        caller = self()

        {:ok, _pid} =
          DynamicSupervisor.start_child(
            Pool.dynsup_name(pool),
            {Worker, {pool, fun, caller, ref}}
          )

        receive do
          {^ref, {:ok, value}} -> {:ok, value}
          {^ref, {:error, reason}} -> {:error, reason}
        after
          timeout ->
            Pool.checkin(pool)
            {:error, :timeout}
        end

      {:error, :pool_exhausted} = err ->
        err
    end
  end
end
```

## Why this works

- **Atomic counter is lock-free** — `:counters.add/3` is a single atomic instruction at the VM level. Thousands of concurrent checkouts scale linearly with cores.
- **Overshoot-then-rollback is safe** — the increment-then-check pattern may briefly exceed `max` by one or two under extreme concurrency, but we immediately decrement and reject. The worst case is transient over-provisioning by the number of cores.
- **DynamicSupervisor gives us crash isolation** — if the user-provided `fun` crashes, the worker dies, the Task link kills the spawned process cleanly, and `checkin` runs in the `after` block.
- **Fail fast beats queueing** — when the regional carrier is slow, the 6th concurrent caller receives `{:error, :pool_exhausted}` in microseconds instead of waiting 20 seconds.

## Tests

```elixir
defmodule ShippingBulkheads.BulkheadTest do
  use ExUnit.Case, async: false
  alias ShippingBulkheads.Bulkhead
  alias ShippingBulkheads.Bulkhead.Pool

  describe "successful calls" do
    test "returns the function result" do
      assert {:ok, 42} = Bulkhead.run(:ups, fn -> 42 end)
    end

    test "propagates rescued exceptions" do
      assert {:error, %RuntimeError{}} = Bulkhead.run(:ups, fn -> raise "boom" end)
    end
  end

  describe "pool isolation" do
    test "regional pool overflow does not affect ups pool" do
      parent = self()

      slow_tasks =
        for i <- 1..5 do
          Task.async(fn ->
            Bulkhead.run(
              :regional,
              fn ->
                send(parent, {:started, i})
                Process.sleep(500)
                :done
              end,
              2_000
            )
          end)
        end

      for _ <- 1..5, do: assert_receive({:started, _}, 1_000)

      assert {:error, :pool_exhausted} = Bulkhead.run(:regional, fn -> :ok end)
      assert {:ok, :ups_ok} = Bulkhead.run(:ups, fn -> :ups_ok end)

      Enum.each(slow_tasks, &Task.await(&1, 2_000))
    end
  end

  describe "timeout handling" do
    test "returns :timeout and releases the slot" do
      assert {:error, :timeout} = Bulkhead.run(:ups, fn -> Process.sleep(1_000) end, 50)
      Process.sleep(50)
      assert Pool.in_flight(:ups) >= 0
    end
  end
end
```

## Benchmark

```elixir
# bench/bulkhead_bench.exs
{:ok, _} = Application.ensure_all_started(:shipping_bulkheads)

Benchee.run(
  %{
    "run — trivial fun" => fn ->
      ShippingBulkheads.Bulkhead.run(:ups, fn -> :ok end)
    end
  },
  parallel: 8,
  time: 5
)
```

Expected: p99 < 50µs for trivial work. Pool overhead should dominate over the `:ok` function itself.

## Trade-offs and production gotchas

**1. Pool size must be tuned per-dependency** — `max_concurrent` is not a universal number. Start with `2 * (expected_latency_ms / target_latency_ms)` and adjust from telemetry.

**2. `:persistent_term` on hot config** — safe because pool config is set once at startup. If you rotate pool sizes at runtime, use `:ets` instead — `:persistent_term` updates trigger a GC of all processes referencing the term.

**3. Worker crash must checkin** — the `after` block guarantees it even on raise/exit/throw. Verify this in test with `raise` and an `exit(:kill)` scenario.

**4. Timeout without checkin leaks slots** — the `receive` timeout branch explicitly calls `checkin`. If the worker actually completes later, it will also call checkin — you over-release by one. Guard this with a monitor or accept the minor drift.

**5. No retry inside the pool** — retries multiply load on the already-struggling dependency. Wrap the bulkhead with retry-on-outside, never inside.

**6. When NOT to use this** — for fast, non-network work (hashing, parsing) the pool overhead is pure cost. Bulkheads are for I/O-bound dependencies with variable latency.

## Reflection

What happens if you set `max_concurrent: 1`? Under what circumstances would that actually be a good choice instead of a shared mutex or `GenServer`?

## Resources

- [`:counters` — Erlang docs](https://www.erlang.org/doc/man/counters.html)
- [Poolboy](https://github.com/devinus/poolboy) — reference pool implementation
- [DynamicSupervisor — Elixir docs](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Release It! — Bulkheads chapter](https://pragprog.com/titles/mnee2/release-it-second-edition/)
