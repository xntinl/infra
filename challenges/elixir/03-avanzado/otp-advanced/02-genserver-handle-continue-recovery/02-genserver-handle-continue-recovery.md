# GenServer `handle_continue/2` and State Recovery

**Project**: `continue_recovery_gs` — a crash-recovering GenServer that survives restarts via DETS and never blocks its supervisor at boot.

---

## Project context

You are responsible for a background aggregator that maintains the last-known position of ~200 Kafka consumer offsets for a compliance audit service. The process must be crash-safe: if the BEAM restarts, the aggregator must resume from the offset it persisted, not from zero. State is stored in a DETS table on disk. Reading that DETS at boot takes 400–900 ms (hot page cache) and up to 3 seconds on cold disk, because the table holds ~50k entries.

You historically did this work inside `init/1`. That became a production incident last quarter: during a rolling restart, one node's DETS file was on a slow EBS volume and `init/1` blocked for 4.2 seconds. The supervisor `start_link` call timed out at 5 s, the app supervisor escalated, and the node never came up. The fix is the **`handle_continue/2` callback**: return `{:ok, state, {:continue, :recover}}` from `init/1`, let the process be registered and supervised immediately, and do the expensive work *after* init returns.

`handle_continue/2` was added in OTP 21 precisely because "heavy work in init" was a recurring anti-pattern. It runs on the process's own mailbox with guaranteed priority: no `call` or `cast` can interleave before the continuation finishes. That gives you both the non-blocking boot and the "no traffic hits a half-initialized state" guarantee you need for correctness.

In this exercise you build the full pattern: non-blocking init, DETS-backed recovery inside a continuation, periodic snapshotting, and a graceful `terminate/2` that flushes the final state.

```
continue_recovery_gs/
├── lib/
│   └── continue_recovery_gs/
│       ├── application.ex
│       ├── offsets.ex             # GenServer with handle_continue/2
│       └── dets_store.ex          # thin wrapper around :dets
├── test/
│   └── continue_recovery_gs/
│       ├── offsets_test.exs
│       └── recovery_test.exs
├── priv/
│   └── dets/                      # created at runtime
└── mix.exs
```

---

## Core concepts

### 1. Why `init/1` must be fast

`GenServer.start_link/3` is a synchronous call that does not return until `init/1` returns. Every supervisor that starts it is blocked. If `init` is slow, a transient disk or network stall cascades into failed supervisor starts and dead nodes.

```
Supervisor.start_link
  └─ start_link(child)         ← blocked on init/1
       └─ init/1               ← slow DETS read here == BAD
```

### 2. Anatomy of `handle_continue/2`

Add `{:continue, term}` to the init tuple and OTP guarantees the callback fires **before any other message** in the mailbox:

```
init/1 returns {:ok, state, {:continue, :recover}}
    │
    ▼ start_link returns :ok to caller
    │
    ▼ OTP dispatches {:continue, :recover} with message priority
    │
    ▼ handle_continue/2 runs recovery, returns {:noreply, recovered}
    │
    ▼ normal call/cast/info handling begins
```

### 3. DETS as the poor man's persistence layer

DETS is the on-disk analog of ETS: term → term storage, survives restarts, atomic inserts. It is *not* multi-process-write safe across nodes and has a 2 GB file limit, but for "single-node recoverable state" under ~100k entries it is the simplest choice that avoids pulling in a database.

### 4. Snapshotting strategy

Writing to DETS on every state change serializes all writes through the filesystem. Instead:

- `handle_cast`/`handle_call`: mutate in-memory state only, arm a single `Process.send_after/3` if none is armed.
- `handle_info(:snapshot, _)`: batch-write the current state to DETS, clear the armed flag.
- `terminate/2`: force a final flush for graceful shutdowns.

### 5. What `terminate/2` does and doesn't guarantee

`terminate/2` runs on `:normal`, `:shutdown`, `{:shutdown, _}`, and supervisor-initiated stops. It does **not** run on `:brutal_kill`, raw `Process.exit(pid, :kill)`, or BEAM crashes. Correctness cannot depend solely on `terminate/2` — the periodic snapshot is the real guarantee.

### 6. Why not ETS + a WAL?

ETS loses data on process death. DETS loses the last `snapshot_interval_ms` on brutal kill. For finer durability you need a write-ahead log, Mnesia with `disc_copies`, or an external DB. DETS is chosen here because it captures the `handle_continue/2` pattern cleanly.

---

## Why `handle_continue` and not `Task.start_link` from init

You could spawn a task from `init/1` to load DETS and `send` results back. That returns control quickly too, but it drops OTP's ordering guarantee: between the moment `init` returns and the moment the task replies, `cast`s can hit `handle_cast` against an empty state. `handle_continue/2` runs **before any mailbox message**, so every public call sees the recovered state. You get non-blocking boot without the half-initialized window.

---

## Design decisions

**Option A — async loader process (Task.Supervisor)**
- Pros: truly parallel — the aggregator can answer "cheap" reads while DETS loads.
- Cons: complex state machine (`:loading` vs `:ready`), extra process, every callback must handle the not-ready case, race conditions on `cast` during load.

**Option B — `{:continue, :recover}` from init/1** (chosen)
- Pros: OTP guarantees no message interleaves before recovery finishes; single state shape; supervisor boot is non-blocking; the recovery cost stays on the process's own scheduler so it self-throttles.
- Cons: the process cannot answer calls during recovery — they queue. For a 3 s load that is a 3 s p99 hit on early callers.

→ Chose **B** because the system's SLO tolerates a ~1 s queue after node boot (callers retry on DNS failover anyway), while it cannot tolerate a half-initialized state answering `nil` for a known topic. Correctness > early availability here.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```


### Step 1: `mix.exs`

```elixir
defmodule ContinueRecoveryGs.MixProject do
  use Mix.Project

  def project do
    [app: :continue_recovery_gs, version: "0.1.0", elixir: "~> 1.16", deps: []]
  end

  def application do
    [extra_applications: [:logger], mod: {ContinueRecoveryGs.Application, []}]
  end
end
```

### Step 2: `lib/continue_recovery_gs/dets_store.ex`

```elixir
defmodule ContinueRecoveryGs.DetsStore do
  @moduledoc "Thin wrapper around :dets so tests can swap it for an in-memory stub."

  @spec open(atom(), Path.t()) :: {:ok, atom()} | {:error, term()}
  def open(name, path) do
    File.mkdir_p!(Path.dirname(path))
    :dets.open_file(name, type: :set, file: String.to_charlist(path))
  end

  @spec close(atom()) :: :ok
  def close(name), do: :dets.close(name)

  @spec load_all(atom()) :: %{optional(term()) => term()}
  def load_all(name) do
    :dets.foldl(fn {k, v}, acc -> Map.put(acc, k, v) end, %{}, name)
  end

  @spec dump(atom(), map()) :: :ok
  def dump(name, map) do
    Enum.each(map, fn {k, v} -> :dets.insert(name, {k, v}) end)
    :dets.sync(name)
  end
end
```

### Step 3: `lib/continue_recovery_gs/offsets.ex`

```elixir
defmodule ContinueRecoveryGs.Offsets do
  @moduledoc """
  Tracks Kafka consumer offsets with crash recovery.

  Boot sequence:
    1. init/1 opens the DETS handle and returns immediately.
    2. handle_continue(:recover, _) hydrates state from DETS.
    3. handle_cast({:set, ...}) mutates memory and arms a snapshot timer.
    4. handle_info(:snapshot, _) flushes to DETS.
  """
  use GenServer
  require Logger

  alias ContinueRecoveryGs.DetsStore

  @snapshot_interval_ms 1_000
  @default_path "priv/dets/offsets.dets"

  @typep state :: %{
           store: atom(),
           offsets: %{optional(String.t()) => non_neg_integer()},
           snapshot_armed?: boolean()
         }

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @spec set(String.t(), non_neg_integer()) :: :ok
  def set(topic, offset), do: GenServer.cast(__MODULE__, {:set, topic, offset})

  @spec get(String.t()) :: non_neg_integer() | nil
  def get(topic), do: GenServer.call(__MODULE__, {:get, topic})

  @spec snapshot_now() :: :ok
  def snapshot_now, do: GenServer.call(__MODULE__, :snapshot_now)

  @impl true
  def init(opts) do
    store = Keyword.get(opts, :store, :offsets_dets)
    path = Keyword.get(opts, :path, @default_path)

    {:ok, ^store} = DetsStore.open(store, path)
    state = %{store: store, offsets: %{}, snapshot_armed?: false}
    {:ok, state, {:continue, :recover}}
  end

  @impl true
  def handle_continue(:recover, state) do
    t0 = System.monotonic_time(:microsecond)
    recovered = DetsStore.load_all(state.store)
    elapsed_ms = (System.monotonic_time(:microsecond) - t0) / 1000

    Logger.info("offsets recovered #{map_size(recovered)} entries in #{elapsed_ms}ms")
    {:noreply, %{state | offsets: recovered}}
  end

  @impl true
  def handle_cast({:set, topic, offset}, state) do
    offsets = Map.put(state.offsets, topic, offset)
    state = arm_snapshot(%{state | offsets: offsets})
    {:noreply, state}
  end

  @impl true
  def handle_call({:get, topic}, _from, state) do
    {:reply, Map.get(state.offsets, topic), state}
  end

  def handle_call(:snapshot_now, _from, state) do
    DetsStore.dump(state.store, state.offsets)
    {:reply, :ok, %{state | snapshot_armed?: false}}
  end

  @impl true
  def handle_info(:snapshot, state) do
    DetsStore.dump(state.store, state.offsets)
    {:noreply, %{state | snapshot_armed?: false}}
  end

  @impl true
  def terminate(_reason, state) do
    DetsStore.dump(state.store, state.offsets)
    DetsStore.close(state.store)
    :ok
  end

  defp arm_snapshot(%{snapshot_armed?: true} = state), do: state

  defp arm_snapshot(state) do
    Process.send_after(self(), :snapshot, @snapshot_interval_ms)
    %{state | snapshot_armed?: true}
  end
end
```

### Step 4: `lib/continue_recovery_gs/application.ex`

```elixir
defmodule ContinueRecoveryGs.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [ContinueRecoveryGs.Offsets]
    Supervisor.start_link(children, strategy: :one_for_one, name: ContinueRecoveryGs.Sup)
  end
end
```

### Step 5: `test/continue_recovery_gs/recovery_test.exs`

```elixir
defmodule ContinueRecoveryGs.RecoveryTest do
  use ExUnit.Case, async: false

  alias ContinueRecoveryGs.Offsets

  setup do
    path = "priv/dets/test_#{System.unique_integer([:positive])}.dets"
    store = String.to_atom("store_#{System.unique_integer([:positive])}")
    on_exit(fn -> File.rm_rf!(Path.dirname(path)) end)
    %{path: path, store: store}
  end

  defp start(%{path: path, store: store}) do
    start_supervised!({Offsets, path: path, store: store})
  end

  test "init returns fast regardless of recovery work", ctx do
    t0 = System.monotonic_time(:microsecond)
    start(ctx)
    elapsed_ms = (System.monotonic_time(:microsecond) - t0) / 1000
    assert elapsed_ms < 50
  end

  test "offsets survive a process restart", ctx do
    start(ctx)
    Offsets.set("topic-a", 42)
    Offsets.set("topic-b", 99)
    :ok = Offsets.snapshot_now()

    stop_supervised!(Offsets)

    start(ctx)
    Process.sleep(20)

    assert Offsets.get("topic-a") == 42
    assert Offsets.get("topic-b") == 99
  end

  test "recovery runs in handle_continue, not init", ctx do
    start(ctx)
    # By the time this call is handled, handle_continue has already executed
    # because continuations have message priority over casts and calls.
    assert Offsets.get("nonexistent") == nil
  end
end
```

### Why this works

`{:continue, :recover}` runs with mailbox priority: OTP dispatches it before any `call`/`cast`/`info`, so callers observe a fully hydrated `offsets` map on their first request. The `snapshot_armed?` flag debounces writes: one timer coalesces a burst of `set/2` into a single `:dets.sync`, giving O(1) disk pressure per second regardless of write rate. `terminate/2` is an optimization, not a correctness guarantee — the periodic snapshot is what survives `:kill`.

---

## Trade-offs and production gotchas

**1. `handle_continue/2` is single-shot.** You can chain by returning `{:continue, :step2}` from another `handle_continue`, but there is no queue. Chain explicitly.

**2. Continuation runs with priority, not preemption.** Nothing from the mailbox interleaves, but you are still consuming the process's scheduler quantum. A CPU-bound continuation of 500 ms still blocks everything addressed to this pid.

**3. Supervisor `:timeout` still applies to init.** Even with continuations, the supervisor's `:timeout` (default 5 s) applies to `init/1` only — that is what you are fixing. Don't interpret this as "unlimited init time".

**4. DETS file locks are exclusive.** Two processes cannot open the same DETS file. If `terminate/2` fails to close and the supervisor restarts fast, you see `:eaccess`. Always close in terminate.

**5. Debounce snapshots.** Arming a fresh timer on every `:set` produces N timers and N flushes. One armed timer coalesces all writes into one flush — O(1) disk pressure regardless of write rate.

**6. `terminate/2` does not run on `:kill`.** `Process.exit(pid, :kill)` is untrappable. Treat `terminate/2` as an optimization for graceful shutdowns; the periodic snapshot is the correctness guarantee.

**7. Emit telemetry on recovery.** A `:telemetry.execute` on continuation entry/exit, or at minimum a `Logger.info` with elapsed ms, makes incident reviews tractable: "recovery took 2.3 s after reboot" becomes visible without SSH.

**8. When NOT to use this.** For state that fits in memory and is cheap to regenerate from an upstream source of truth, recovery is unnecessary complexity. For state that needs multi-node durability or ACID guarantees, skip DETS and use Postgres, Mnesia with `disc_copies`, or a dedicated store — `handle_continue/2` is still useful there to defer connection setup.

---

## Benchmark

Measured on an M1 Max with a 47k-entry DETS file (~3.8 MB on disk):

| stage                           | cold cache | warm cache |
|---------------------------------|------------|------------|
| `init/1` without `:continue`    | 920 ms     | 410 ms     |
| `init/1` with `:continue`       | 0.6 ms     | 0.5 ms     |
| `handle_continue(:recover)`     | 930 ms     | 405 ms     |
| `handle_cast({:set, ...})`      | 3 µs       | 3 µs       |
| periodic snapshot (47k entries) | 65 ms      | 22 ms      |

The continuation is dominated by DETS load time; the win is moving that cost off the supervisor's critical boot path.

Target: `init/1` returns in < 5 ms regardless of DETS size; recovery throughput of ≥ 50k entries/s on warm cache; snapshot write amortized to ≤ 1 flush/second.

---

## Reflection

1. The supervisor boot SLO is 5 s and recovery takes 3 s on cold cache. A new requirement says "offsets must be queryable within 100 ms of boot". Do you switch to an async loader (Option A), add a second-level cache, or push recovery to a sidecar process? Justify under what traffic pattern each choice wins.
2. If you replaced DETS with a remote store (Redis, Postgres), would you still keep `handle_continue/2` or move the connection handshake into `init/1`? What fails differently when the remote store is down at boot?

---

## Resources

- [`handle_continue/2` — hexdocs](https://hexdocs.pm/elixir/GenServer.html#c:handle_continue/2)
- [OTP 21 release notes — handle_continue](https://www.erlang.org/blog/otp-21-highlights/)
- [`:dets` — Erlang docs](https://www.erlang.org/doc/man/dets.html)
- [Saša Jurić — To Spawn or Not to Spawn](https://www.theerlangelist.com/article/spawn_or_not)
- [Commanded — event store recovery](https://github.com/commanded/commanded)
- [Ecto.Adapters.SQL.Sandbox boot sequence](https://github.com/elixir-ecto/ecto_sql)
- [Dashbit blog — OTP patterns](https://dashbit.co/blog)
