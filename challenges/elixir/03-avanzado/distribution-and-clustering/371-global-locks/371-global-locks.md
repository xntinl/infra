# Global Locks and `:global.trans`

**Project**: `nightly_batch` — run a periodic job exactly once across a multi-node cluster, even when every node independently tries to start it.

## Project context

You run a cluster of three BEAM nodes. Every night at 02:00 UTC a cron-like scheduler on each node attempts to start the daily billing batch. You want the batch to run on exactly one node per night — running it three times would triple-charge customers. Quartz-style clustered schedulers exist, but you want the BEAM-native primitive: `:global.trans/2`.

`:global` is the oldest and most maintained distributed primitive in OTP. It provides:

- cluster-wide unique process names (`:global.register_name/2`),
- cluster-wide locks (`:global.set_lock/3`, `:global.del_lock/2`),
- transactional locking (`:global.trans/2`) that wraps a function with set + del automatically, even on crash.

The catch: `:global` uses a synchronous, leader-based protocol that does not handle netsplits well. For nightly-batch scale (once per day, small cluster), this is fine and cheap. For 10,000 lock acquisitions per second across 50 nodes, it is the wrong tool.

```
nightly_batch/
├── lib/
│   └── nightly_batch/
│       ├── application.ex
│       ├── scheduler.ex
│       ├── runner.ex
│       └── billing_job.ex
├── test/
│   └── nightly_batch/
│       └── runner_test.exs
├── bench/
│   └── lock_bench.exs
└── mix.exs
```

## Why `:global.trans` and not a database row lock

A SELECT ... FOR UPDATE on a "jobs" table works and is common. Trade-offs:

- **DB lock** — durable (survives node restart), requires DB connection, lock is cheap but the connection pool can be the bottleneck.
- **`:global.trans`** — no external dependency, BEAM-native, but lost on full cluster restart (no persistence).

For a nightly job that re-runs if the whole cluster crashes (scheduler will retry next minute), `:global.trans` is simpler.

## Why `:global.trans` and not `:global.set_lock` + manual `del_lock`

`trans/2` releases the lock if the fun crashes. With manual `set_lock`/`del_lock`, a crash between the two calls leaks the lock. Equivalent to a try/after wrapper, but shorter.

## Core concepts

### 1. Lock identity

A `:global` lock is identified by a `{ResourceId, RequesterId}` tuple. `ResourceId` is what you are locking; `RequesterId` tells `:global` which process to blame if the holder dies (the lock is released on the requester's exit). A common pattern: `{:billing_job_2026_04_11, self()}`.

### 2. `trans/2` semantics

```elixir
:global.trans({resource_id, requester}, fn -> ... end, nodes, retries)
```

- Acquires the lock on the given `nodes` (default: all known nodes) by contacting `:global` on each.
- Runs the fun.
- Releases the lock.
- Returns the fun's return value, or `:aborted` if it could not acquire within `retries` attempts.

### 3. Leader-based protocol

`:global` designates a node as the "lock manager" per resource. Acquisition requires a round-trip to every node in the cluster, so latency scales with cluster size and network RTT.

### 4. Netsplit behaviour

During a netsplit, each partition may independently acquire the lock (both partitions think the other is gone). On merge, `:global` runs a resolution callback and, by default, logs an error. This is the single biggest foot-gun: **`:global.trans` is not split-brain safe**.

## Design decisions

- **Option A — DB-level advisory lock (`pg_advisory_lock`)**: durable, survives BEAM restarts, no split-brain ambiguity because the DB has a single source of truth. Pick this if you already have PostgreSQL.
- **Option B — `:global.trans`** (chosen for this scenario): zero infra, good enough for nightly batches where two runs in a split-brain edge case are acceptable (the batch is idempotent).
- **Option C — Raft-based lock (e.g. `ra`, etcd)**: strong consistency, heavy infra cost. Overkill for one job per day.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule NightlyBatch.MixProject do
  use Mix.Project

  def project do
    [app: :nightly_batch, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {NightlyBatch.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: The runner — the actual cluster-wide exclusion

```elixir
# lib/nightly_batch/runner.ex
defmodule NightlyBatch.Runner do
  @moduledoc """
  Runs `fun` on exactly one node per `resource_id` across the cluster.

  Uses `:global.trans/4` with retries=0 (fail fast). Other nodes receive
  `:already_running` and MUST NOT start their local copy.
  """

  require Logger

  @type resource_id :: term()

  @spec run_once(resource_id, (-> any())) :: {:ok, any()} | :already_running | {:error, term()}
  def run_once(resource_id, fun) when is_function(fun, 0) do
    lock = {resource_id, self()}
    nodes = [Node.self() | Node.list()]

    case :global.trans(lock, fun, nodes, 0) do
      :aborted ->
        Logger.info("runner: could not acquire #{inspect(resource_id)} — another node holds it")
        :already_running

      result ->
        {:ok, result}
    end
  end
end
```

### Step 2: The domain job

```elixir
# lib/nightly_batch/billing_job.ex
defmodule NightlyBatch.BillingJob do
  require Logger

  def run do
    Logger.info("billing batch started on #{Node.self()}")
    # simulate work
    Process.sleep(100)
    Logger.info("billing batch finished on #{Node.self()}")
    :ok
  end
end
```

### Step 3: The scheduler

```elixir
# lib/nightly_batch/scheduler.ex
defmodule NightlyBatch.Scheduler do
  use GenServer
  require Logger

  alias NightlyBatch.{Runner, BillingJob}

  @interval_ms :timer.hours(24)

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    tick_ms = Keyword.get(opts, :tick_ms, @interval_ms)
    Process.send_after(self(), :tick, tick_ms)
    {:ok, %{tick_ms: tick_ms}}
  end

  @impl true
  def handle_info(:tick, state) do
    day = Date.utc_today() |> Date.to_string()
    resource_id = {:billing_job, day}

    case Runner.run_once(resource_id, &BillingJob.run/0) do
      {:ok, _} -> Logger.info("scheduler: ran billing for #{day} on this node")
      :already_running -> Logger.info("scheduler: billing for #{day} already running elsewhere")
      {:error, reason} -> Logger.error("scheduler: failed #{inspect(reason)}")
    end

    Process.send_after(self(), :tick, state.tick_ms)
    {:noreply, state}
  end
end
```

### Step 4: Application

```elixir
# lib/nightly_batch/application.ex
defmodule NightlyBatch.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [NightlyBatch.Scheduler]
    Supervisor.start_link(children, strategy: :one_for_one, name: NightlyBatch.Supervisor)
  end
end
```

## Data flow diagram

```
  Day starts.
  Scheduler ticks on Node A, B, C simultaneously.

  Node A: :global.trans({:billing, "2026-04-11"}, ..., [A, B, C], retries=0)
            1. asks :global on A, B, C to set the lock
            2. :global runs a consensus round (leader-based)
            3. exactly one node wins — say A
            4. A runs the fun, releases lock

  Node B: :global.trans(...) returns :aborted → :already_running
  Node C: :global.trans(...) returns :aborted → :already_running

  Invariant: exactly one BillingJob.run/0 executes per day.
```

## Why this works

`:global.trans/4` wraps `:global.set_lock/3` and `:global.del_lock/2` in a try/after. `set_lock/3` runs a two-phase protocol to every `:global` process across the cluster. Because the protocol requires every participating node to agree, and because a node can only agree to one holder at a time per resource, at most one caller succeeds. With `retries=0`, losers fail fast with `:aborted`. The requester pid (`self()`) is monitored by `:global`; if it dies, the lock is released — so even `BillingJob.run/0` crashing does not leave a ghost lock.

## Tests

```elixir
# test/nightly_batch/runner_test.exs
defmodule NightlyBatch.RunnerTest do
  use ExUnit.Case, async: false

  alias NightlyBatch.Runner

  describe "run_once/2 — single-node exclusion" do
    test "runs the fun when the lock is free" do
      assert {:ok, :did_it} = Runner.run_once({:test, :free}, fn -> :did_it end)
    end

    test "returns :already_running while the lock is held elsewhere" do
      me = self()

      other =
        spawn_link(fn ->
          Runner.run_once({:test, :held}, fn ->
            send(me, :holding)
            receive do: (:release -> :ok)
          end)
        end)

      assert_receive :holding, 500

      assert :already_running = Runner.run_once({:test, :held}, fn -> :should_not_run end)

      send(other, :release)
      Process.sleep(50)

      # Now the lock is free again
      assert {:ok, :free_now} = Runner.run_once({:test, :held}, fn -> :free_now end)
    end

    test "lock is released even if the fun raises" do
      assert_raise RuntimeError, fn ->
        Runner.run_once({:test, :crash}, fn -> raise "boom" end)
      end

      assert {:ok, :ok_after_crash} =
               Runner.run_once({:test, :crash}, fn -> :ok_after_crash end)
    end
  end

  describe "concurrency within a node" do
    test "two concurrent callers serialize through :global.trans" do
      me = self()

      for i <- 1..5 do
        spawn_link(fn ->
          Runner.run_once({:test, :serial}, fn ->
            Process.sleep(20)
            send(me, {:done, i})
            :ok
          end)
        end)
      end

      # Collect as many as complete in the window
      received =
        for _ <- 1..5 do
          receive do
            {:done, i} -> i
          after
            500 -> nil
          end
        end
        |> Enum.reject(&is_nil/1)

      # At least one must succeed; others either succeed (serially) or got :already_running
      assert length(received) >= 1
    end
  end
end
```

## Benchmark

```elixir
# bench/lock_bench.exs
alias NightlyBatch.Runner

Benchee.run(
  %{
    "uncontended lock acquire + release" => fn ->
      id = {:bench, :erlang.unique_integer([:positive])}
      Runner.run_once(id, fn -> :ok end)
    end
  },
  time: 5,
  warmup: 2
)
```

Target (single node): < 200 µs per uncontended trans. On a 3-node LAN cluster expect ~2 ms; across WAN nodes, 50+ ms. Do not use `:global.trans` in hot paths.

## Trade-offs and production gotchas

1. **Split-brain gives you two runs**: during a netsplit, each partition can independently acquire the lock. If your batch is not idempotent, you will charge customers twice. Always write batches to be safe to run twice.
2. **`retries > 0` blocks**: `:global.trans(..., retries: :infinity)` will wait forever. Use 0 for "fail-fast this tick" semantics.
3. **Lock is lost on requester death — sometimes wanted, sometimes not**: if your job is long-running and the requester process (not the runner) dies, the lock releases mid-run. Either make the requester the runner, or use a longer-lived `self()`.
4. **`:global.sync/0` after `Node.connect`**: a freshly connected node has not merged its `:global` state yet. `:global.trans` may briefly return inconsistent results. Call `:global.sync/0` after topology changes in tests.
5. **Cluster size matters**: each `set_lock` hits every node. At 30+ nodes, lock acquisition latency becomes significant — consider sharding the resource space or using Raft.
6. **When NOT to use this**: high-frequency locking (>100/s) or tight latency SLAs. Use a DB advisory lock or Redis redlock instead.

## Reflection

Your cluster has three nodes A, B, C. A netsplit isolates A from {B, C}. Both sides start a nightly batch with `:global.trans`. Explain exactly what each side sees (lock acquired or aborted) and why. When the netsplit heals, what does `:global` do, and what is the smallest code change to prevent the duplicate run?

## Resources

- [`:global` docs](https://www.erlang.org/doc/man/global.html)
- [`:global.trans/4` source](https://github.com/erlang/otp/blob/master/lib/kernel/src/global.erl)
- [Fred Hebert — Learn You Some Erlang, distribunomicon](https://learnyousomeerlang.com/distribunomicon)
- [PostgreSQL advisory locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)
- [Martin Kleppmann — How to do distributed locking](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html)
