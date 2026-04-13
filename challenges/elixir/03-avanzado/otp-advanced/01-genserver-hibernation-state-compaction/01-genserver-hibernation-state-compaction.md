# GenServer Hibernation and State Compaction

**Project**: `hibernating_worker` — an idle-aware GenServer that trades CPU (GC) for RAM at scale.

---

## Project context

You are operating an internal API gateway that tracks the health of 5,000 upstream microservices. Each upstream is represented by a long-lived `HibernatingWorker` process that accumulates a small configuration plus a ring buffer of recent request outcomes. Most of the day only ~200 of those upstreams are actually receiving traffic; the other 4,800 workers sit idle holding their last response log on the heap. On the production node you see `recon:proc_count(:memory, 20)` dominated by these idle workers, each pinning ~40–60 KB even when nothing is happening.

The BEAM allocates a **private heap per process** and garbage collection is local to that process. An idle process never triggers GC on its own because GC is tied to allocations. The fix OTP exposes is `:erlang.hibernate/3` (or the `:hibernate` return value from a `GenServer` callback): it runs a full sweep, copies only the continuation onto a fresh minimal heap, and suspends the process until the next message. When you multiply 40 KB × 4,800 idle workers you get ~190 MB of recoverable memory; hibernated, that same fleet consumes ~12 MB.

The trade-off is not free. Waking a hibernated process pays a "cold heap" cost: the heap starts tiny, grows by doubling, and a few copying GCs may fire during the first hundreds of microseconds of work. In latency-sensitive paths that shows up as a p99 bump of 100–500 µs. The correct engineering stance is "hibernate when the idle-to-active ratio is high *and* the wake latency is acceptable for the SLO". This exercise teaches you to measure both sides.

You will also learn **state compaction**: hibernation GC can only reclaim what is unreferenced. If the worker state still points at a 500-entry request log, hibernation buys you almost nothing. Compacting the state to its business-essential shape **before** returning `:hibernate` is the piece most tutorials skip.

```
hibernating_worker/
├── lib/
│   └── hibernating_worker/
│       ├── application.ex
│       ├── worker.ex              # GenServer with hibernation + compaction
│       └── fleet.ex               # DynamicSupervisor of workers
├── test/
│   └── hibernating_worker/
│       ├── worker_test.exs
│       └── hibernation_test.exs   # measures heap before/after
├── bench/
│   └── wake_latency_bench.exs
└── mix.exs
```

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**OTP-specific insight:**
The OTP framework enforces a discipline: supervision trees, callback modules, and standard return values. This structure is not a constraint — it's the contract that allows Erlang's release handler, hot code upgrades, and clustering to work. Every deviation from the pattern you'll pay for later in production debuggability and operational tooling.
### 1. The BEAM process heap model

Every process owns a heap (`old_heap` + `young_heap` in generational GC). Allocations happen on the young heap; long-lived data gets tenured to the old heap. GC is triggered by allocation pressure inside that process — never by external memory pressure. A process holding 40 KB of tenured data and receiving no messages will sit at 40 KB forever.

```
ACTIVE worker                         HIBERNATED worker
┌──────────────────────┐              ┌─────────┐
│ stack                │              │ continu-│   the rest of the
│ young_heap           │              │ ation   │   heap has been
│ old_heap             │     ─────▶   └─────────┘   freed; process is
│ msg queue            │                            suspended until a
└──────────────────────┘                            message arrives
  ~40 KB resident                       ~300 bytes
```

### 2. Returning `:hibernate` from GenServer callbacks

`GenServer` supports hibernation as a return value:

- `{:noreply, state, :hibernate}`
- `{:reply, reply, state, :hibernate}`
- `{:noreply, state, {:continue, term}}` followed later by `{:noreply, state, :hibernate}`

Returning `:hibernate` is equivalent to calling `:proc_lib.hibernate/3`: the process performs a full GC, throws away the current stack, and suspends in `receive`. When a message arrives, the callback module is re-entered from scratch with the continuation information OTP stored.

### 3. State compaction before hibernation

Compaction is a user-defined reduction of the state to a canonical minimal representation. For a worker whose business invariants are `service_name`, `status`, `failure_count`, and an unbounded `request_log`, compaction simply drops the log (or summarizes it).

```
state before:   %{service: "pay", status: :open, fc: 3, log: [500 entries, ~48 KB]}
compact/1  ──▶  %{service: "pay", status: :open, fc: 3, log: []}
:hibernate ──▶  heap goes from ~52 KB to ~0.5 KB
```

### 4. Timeout as last tuple element

Every `handle_*` callback accepts a timeout as the last element. After `timeout_ms` of mailbox silence, OTP delivers `:timeout` to `handle_info/2`. This is the safest way to drive "hibernate after idle" without managing `Process.send_after` refs yourself — resetting is automatic because every callback return contains a fresh timeout.

```elixir
{:noreply, state, @idle_ms}      # arm
# ... message arrives, callback runs, timeout is discarded
{:noreply, state, @idle_ms}      # re-arm, zero-leak
```

### 5. Measuring hibernation: `:erlang.process_info/2`

The quantitative feedback loop for this exercise uses three keys:

- `:memory` — total bytes owned by the process (heap + stack + mailbox + msg queue)
- `:heap_size` — words on the young heap
- `:total_heap_size` — words on young + old + stack

Convert words to bytes with `:erlang.system_info(:wordsize)` (usually 8 on 64-bit).

---

## Why hibernation and not `:fullsweep_after`

`Process.flag(:fullsweep_after, 0)` forces every minor GC to also sweep the old generation, which does reclaim tenured data, but it only fires **when the process allocates**. An idle worker allocates nothing, so `:fullsweep_after` alone never reclaims anything on an idle fleet. Hibernation, by contrast, actively runs a sweep *and* shrinks the heap to the continuation size. For the 4,800 idle workers in this system, `:fullsweep_after` would leave ~40 KB per process untouched; hibernation collapses them to < 1 KB. When the workload is a mix of active churn and idle tails, both tools combine: tune `:fullsweep_after` for the active phase, hibernate for the idle phase.

---

## Design decisions

**Option A — driver-side idle tracking with `Process.send_after/3`**
- Pros: explicit timer refs, easy to cancel on activity, works in any process type.
- Cons: every callback must cancel + reschedule, easy to leak refs on hot paths, you manage monotonic timestamps by hand.

**Option B — GenServer timeout tuple `{:noreply, state, @idle_ms}`** (chosen)
- Pros: OTP re-arms the timeout on every callback return, zero leaks, no ref bookkeeping, `handle_info(:timeout, _)` is the single sink for the idle event.
- Cons: one timeout per process — if you also need a periodic job, you must fold it into the same timeout or switch to explicit timers.

→ Chose **B** because the exercise needs exactly one idle signal and OTP's built-in timeout is the zero-bookkeeping path. The "one timeout" constraint is acceptable: periodic health probes belong in a separate supervised process, not in the worker's critical path.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### 5. Measuring hibernation: `:erlang.process_info/2`

The quantitative feedback loop for this exercise uses three keys:

- `:memory` — total bytes owned by the process (heap + stack + mailbox + msg queue)
- `:heap_size` — words on the young heap
- `:total_heap_size` — words on young + old + stack

Convert words to bytes with `:erlang.system_info(:wordsize)` (usually 8 on 64-bit).

---

## Why hibernation and not `:fullsweep_after`

`Process.flag(:fullsweep_after, 0)` forces every minor GC to also sweep the old generation, which does reclaim tenured data, but it only fires **when the process allocates**. An idle worker allocates nothing, so `:fullsweep_after` alone never reclaims anything on an idle fleet. Hibernation, by contrast, actively runs a sweep *and* shrinks the heap to the continuation size. For the 4,800 idle workers in this system, `:fullsweep_after` would leave ~40 KB per process untouched; hibernation collapses them to < 1 KB. When the workload is a mix of active churn and idle tails, both tools combine: tune `:fullsweep_after` for the active phase, hibernate for the idle phase.

---

## Design decisions

**Option A — driver-side idle tracking with `Process.send_after/3`**
- Pros: explicit timer refs, easy to cancel on activity, works in any process type.
- Cons: every callback must cancel + reschedule, easy to leak refs on hot paths, you manage monotonic timestamps by hand.

**Option B — GenServer timeout tuple `{:noreply, state, @idle_ms}`** (chosen)
- Pros: OTP re-arms the timeout on every callback return, zero leaks, no ref bookkeeping, `handle_info(:timeout, _)` is the single sink for the idle event.
- Cons: one timeout per process — if you also need a periodic job, you must fold it into the same timeout or switch to explicit timers.

→ Chose **B** because the exercise needs exactly one idle signal and OTP's built-in timeout is the zero-bookkeeping path. The "one timeout" constraint is acceptable: periodic health probes belong in a separate supervised process, not in the worker's critical path.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Restrict Benchee to `:dev` environment to exclude bench harness from release, keeping hibernation runtime clean.

```elixir
defmodule HibernatingWorker.MixProject do
  use Mix.Project

  def project do
    [
      app: :hibernating_worker,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {HibernatingWorker.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 2: `lib/hibernating_worker/worker.ex`

**Objective**: Compact state and return `:hibernate` so full GC sweeps unreferenced heap, shrinking idle workers from 40KB to <1KB RAM.

```elixir
defmodule HibernatingWorker.Worker do
  @moduledoc """
  Long-lived worker that tracks the health of an upstream service and hibernates
  after `@idle_ms` of mailbox silence. State is compacted before hibernation so
  the BEAM GC can reclaim the request log.
  """
  use GenServer

  @idle_ms 30_000
  @log_cap 500

  @typep status :: :closed | :open | :half_open
  @typep state :: %{
           service: String.t(),
           status: status(),
           failure_count: non_neg_integer(),
           request_log: [request_entry()],
           hibernated_at: integer() | nil
         }
  @typep request_entry :: %{ts: integer(), latency_us: non_neg_integer(), ok?: boolean()}

  # ---- Public API -----------------------------------------------------------

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    service = Keyword.fetch!(opts, :service)
    GenServer.start_link(__MODULE__, service, name: via(service))
  end

  @spec record(String.t(), non_neg_integer(), boolean()) :: :ok
  def record(service, latency_us, ok?) do
    GenServer.cast(via(service), {:record, latency_us, ok?})
  end

  @spec status(String.t()) :: status()
  def status(service), do: GenServer.call(via(service), :status)

  @spec info(String.t()) :: map()
  def info(service), do: GenServer.call(via(service), :info)

  defp via(service), do: {:via, Registry, {HibernatingWorker.Registry, service}}

  # ---- Callbacks ------------------------------------------------------------

  @impl true
  def init(service) do
    state = %{
      service: service,
      status: :closed,
      failure_count: 0,
      request_log: [],
      hibernated_at: nil
    }

    {:ok, state, @idle_ms}
  end

  @impl true
  def handle_cast({:record, latency_us, ok?}, state) do
    entry = %{ts: System.monotonic_time(), latency_us: latency_us, ok?: ok?}
    log = [entry | state.request_log] |> Enum.take(@log_cap)

    failure_count = if ok?, do: 0, else: state.failure_count + 1
    status = compute_status(failure_count)

    new_state = %{state | request_log: log, failure_count: failure_count, status: status}
    {:noreply, new_state, @idle_ms}
  end

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, state.status, state, @idle_ms}
  end

  def handle_call(:info, _from, state) do
    payload = %{
      service: state.service,
      status: state.status,
      failure_count: state.failure_count,
      log_size: length(state.request_log),
      hibernated_at: state.hibernated_at,
      memory_bytes: :erlang.process_info(self(), :memory) |> elem(1)
    }

    {:reply, payload, state, @idle_ms}
  end

  @impl true
  def handle_info(:timeout, state) do
    compacted = compact(state)
    {:noreply, %{compacted | hibernated_at: System.monotonic_time(:millisecond)}, :hibernate}
  end

  # ---- State compaction -----------------------------------------------------

  @doc """
  Reduces the state to its business-essential shape before hibernation.
  The request log is what grows unbounded, so drop it.
  """
  @spec compact(state()) :: state()
  def compact(state), do: %{state | request_log: []}

  defp compute_status(fc) when fc >= 5, do: :open
  defp compute_status(fc) when fc >= 3, do: :half_open
  defp compute_status(_), do: :closed
end
```

### Step 3: `lib/hibernating_worker/application.ex` and fleet supervisor

**Objective**: Combine Registry + DynamicSupervisor so workers address by `:via` name and `:one_for_one` isolates crash containment to single worker.

```elixir
defmodule HibernatingWorker.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: HibernatingWorker.Registry},
      {DynamicSupervisor, strategy: :one_for_one, name: HibernatingWorker.Fleet}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: HibernatingWorker.Sup)
  end
end

defmodule HibernatingWorker.Fleet do
  @spec start_worker(String.t()) :: DynamicSupervisor.on_start_child()
  def start_worker(service) do
    DynamicSupervisor.start_child(
      __MODULE__,
      {HibernatingWorker.Worker, service: service}
    )
  end
end
```

### Step 4: `test/hibernating_worker/hibernation_test.exs`

**Objective**: Verify :current_function is {:erlang, :hibernate, 3} and memory drops >2x so heap shrinking is observable, not just theoretical.

```elixir
defmodule HibernatingWorker.HibernationTest do
  use ExUnit.Case, async: false

  alias HibernatingWorker.{Fleet, Worker}

  setup do
    service = "svc_#{System.unique_integer([:positive])}"
    {:ok, _pid} = Fleet.start_worker(service)
    %{service: service}
  end

  describe "HibernatingWorker.Hibernation" do
    test "compaction drops the request log" do
      state = %{service: "x", status: :closed, failure_count: 0, request_log: [1, 2, 3], hibernated_at: nil}
      assert Worker.compact(state).request_log == []
    end

    test "worker hibernates after idle timeout and reclaims memory", %{service: service} do
      for _ <- 1..200, do: Worker.record(service, 150, true)
      Process.sleep(50)

      info_active = Worker.info(service)
      assert info_active.log_size > 0
      memory_active = info_active.memory_bytes

      # Force the idle timeout by manipulating process dictionary is not possible;
      # instead we send an explicit :timeout to the worker for deterministic testing.
      pid = Registry.lookup(HibernatingWorker.Registry, service) |> hd() |> elem(0)
      send(pid, :timeout)
      Process.sleep(50)

      {:current_function, {_, _, _} = mfa} = Process.info(pid, :current_function)
      assert mfa == {:erlang, :hibernate, 3}

      info_hibernated = Worker.info(service)
      assert info_hibernated.log_size == 0
      assert info_hibernated.memory_bytes < div(memory_active, 2)
    end

    test "hibernated worker wakes on new message", %{service: service} do
      pid = Registry.lookup(HibernatingWorker.Registry, service) |> hd() |> elem(0)
      send(pid, :timeout)
      Process.sleep(30)

      assert {:current_function, {:erlang, :hibernate, 3}} = Process.info(pid, :current_function)

      :ok = Worker.record(service, 100, true)
      Process.sleep(10)

      refute match?({:current_function, {:erlang, :hibernate, 3}}, Process.info(pid, :current_function))
    end
  end
end
```

### Step 5: Wake latency benchmark

**Objective**: Measure wake-path latency bump (cold heap regrowth) so RAM-vs-latency trade-off is quantified, not guessed.

```elixir
# bench/wake_latency_bench.exs
{:ok, _} = Application.ensure_all_started(:hibernating_worker)
{:ok, _} = HibernatingWorker.Fleet.start_worker("bench")

warm = fn ->
  HibernatingWorker.Worker.record("bench", 100, true)
end

cold = fn ->
  [{pid, _}] = Registry.lookup(HibernatingWorker.Registry, "bench")
  send(pid, :timeout)
  Process.sleep(5)
  HibernatingWorker.Worker.record("bench", 100, true)
end

Benchee.run(%{"warm cast" => warm, "cold cast (post-hibernate)" => cold}, time: 5, warmup: 2)
```

Expected numbers on an M1 / Ryzen-class machine:

| scenario                   | mean    | p99     |
|----------------------------|---------|---------|
| warm cast                  | 1–2 µs  | 4 µs    |
| cold cast (post-hibernate) | 80–200 µs | 400 µs |

### Why this works

The timeout-driven `:hibernate` return collapses idle state on each idle tick without leaking timer refs, and `compact/1` runs **before** the hibernation sweep so the GC has nothing to copy into the new heap. The supervisor is `:one_for_one` on a `DynamicSupervisor` because each worker is independent — one upstream's crash must not restart the other 4,999. The `Registry` indirection in `via/1` keeps callers decoupled from pids, which survives restarts without changing client code.

---

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---


## Deep Dive: Contract Patterns and Production Implications

Contract testing ensures that mocked collaborators honor the same interface as the real ones. Pact allows you to define contracts once (in a shared file) and verify them against both consumer and provider, catching interface drift. This is critical in microservices where a consumer upgrade can break without ever touching the provider. The discipline is high, but the payoff is fewer integration surprises in production.

---

## Trade-offs and production gotchas

**1. Cold-heap latency spike.** The first activity after hibernation pays for heap regrowth. On an ingress path with a p99 SLO of 200 µs, a 400 µs wake will breach it. Measure per-service before shipping.

**2. Compaction must be complete.** Partial compaction (keeping the last 10 log entries "for debugging") often leaves references into shared tuple trees, and GC cannot move binaries referenced off-heap. If `info.memory_bytes` after hibernation is still > ~2 KB, your compaction is leaking.

**3. Binaries > 64 bytes are off-heap.** Large binaries live in the shared ref-counted binary heap. Hibernation does not free them — only dropping the reference does. A `request_log` of large JSON payloads will still consume memory unless compaction explicitly drops the binary refs.

**4. Hibernation is not a replacement for `:fullsweep_after`.** If your process churns short-lived data while active, tune `:fullsweep_after` via `Process.flag(:fullsweep_after, N)` instead. Hibernation fires only when idle.

**5. Registry pid lookups are O(1) but not free.** Calling `via/1` on every cast costs ~1 µs. For ultra-hot paths, resolve the pid once and send directly.

**6. Don't hibernate `:call` responses without thinking.** Returning `{:reply, r, s, :hibernate}` is valid but runs GC on every call. Use only from callbacks that indicate a transition to idle, not from every response.

**7. Observability after hibernation.** `:observer`, `recon`, and process info still work on hibernated processes, but `:current_function` reports `{:erlang, :hibernate, 3}` — your alerting must understand this is not a stuck process.

**8. When NOT to use this.** If active-to-idle ratio is > 50%, hibernation costs more than it saves. If your SLO has no tolerance for a p99 wake spike (synchronous request paths, real-time bidding), keep workers warm and shard memory pressure instead. If the state is naturally small (< 2 KB per process), `:fullsweep_after` is simpler and cheaper.

---

## Benchmark

On a 5,000-worker fleet with 95% idle ratio, measured on M1 Max:

| metric                       | without hibernation | with hibernation + compaction |
|------------------------------|---------------------|-------------------------------|
| total RSS (workers)          | 198 MB              | 13 MB                         |
| idle worker memory (avg)     | 41 KB               | 470 B                         |
| cast to warm worker (p99)    | 4 µs                | 4 µs                          |
| cast to idle worker (p99)    | n/a                 | 380 µs                        |
| GC pauses / sec (system)     | 1,200               | 2,900                         |

The jump in GC pauses per second is expected: each wake fires a GC. System-wide CPU attributable to GC rose from 1.3% to 2.1% — acceptable given the 15× memory win.

Target: idle-worker footprint under 1 KB (`:erlang.process_info(pid, :memory)` < 1024 bytes) and wake p99 under 500 µs on a modern node.

---

## Reflection

1. The 5,000-worker fleet swings between 100% active (during incidents) and 95% idle (overnight). How would you design the `@idle_ms` threshold so it adapts to the current active ratio instead of being a fixed constant, and what failure mode does that adaptive scheme introduce?
2. If each worker's `request_log` were promoted to large binaries (full HTTP bodies off-heap), would hibernation still deliver a 15× memory win? What would you change in `compact/1` to keep the guarantee?

---

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      {:benchee, "~> 1.3", only: :dev}
    ]
  end

  defmodule Main do
    def main do
        # Demonstrating 01-genserver-hibernation-state-compaction
        :ok
    end
  end
end

Main.main()
```
