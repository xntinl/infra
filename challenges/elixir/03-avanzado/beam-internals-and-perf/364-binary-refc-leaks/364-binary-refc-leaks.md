# Binary Reference Counting and Memory Leaks

**Project**: `binary_leak_lab` — reproduces the classic "binary leak": a process that reads many large binaries, holds a small reference to each, and never triggers a major GC so refc binaries pile up in the shared pool.

## Project context

Your app parses 10 MB log files. A GenServer reads each file, extracts a timestamp, stores only the timestamp, and replies. Two weeks later the node is OOM. `observer` shows `memory_used.binary` at 12 GB while all process heaps sum to < 500 MB. No process holds the 10 MB file — but the refc binaries live on, anchored by sub-binaries pointing into them.

This is the canonical Erlang memory leak. Understanding it requires knowing the difference between heap binaries (≤ 64 bytes, copied) and refc binaries (> 64 bytes, reference-counted in a shared pool).

```
binary_leak_lab/
├── lib/
│   └── binary_leak_lab/
│       ├── leaker.ex
│       └── non_leaker.ex
├── test/
│   └── binary_leak_lab/
│       └── leak_test.exs
├── bench/
│   └── leak_bench.exs
└── mix.exs
```

## Why refc binaries can leak

Refc binaries are allocated in a **shared pool** outside any process heap. A process holds a small ProcBin structure (24 bytes) pointing into the pool. When all ProcBins for a given refc binary are collected, the refcount drops to 0 and the pool entry is freed.

The catch: ProcBins are released only during GC. A process that never triggers a major GC never releases them. A sub-binary (a slice via `binary:part/2` or pattern `<<_::binary, x::binary>>`) ALSO holds a ProcBin pointing at the original — so extracting a 4-byte timestamp from a 10 MB binary still anchors the whole 10 MB.

**Why doesn't the VM detect this?** The VM has no way to know the 10 MB is "dead" without running GC. Without memory pressure on the process's own heap, no GC runs.

## Core concepts

### 1. Heap vs refc binaries

- **Heap binary**: size ≤ 64 bytes. Copied inline into the process heap. Behaves like any other term.
- **Refc binary**: size > 64 bytes. In the shared pool. Accessible via ProcBin on the process heap.
- **Sub-binary**: a slice of a refc binary. Holds a ProcBin referencing the original.
- **Match context**: temporary structure used during binary pattern matching; optimizes repeated matches.

### 2. The `:binary.copy/1` escape hatch

`:binary.copy(sub_binary)` returns a NEW binary with only the bytes you need. Small enough, it goes on the heap (or becomes a refc of exactly the right size). The original 10 MB ref is released when the process next GCs.

### 3. `:recon.bin_leak/1`

Fred Hebert's `recon` library has a single function that forces GC on the top N processes by binary references and reports the reduction. Production-grade triage.

### 4. `erlang:memory/1`

`:erlang.memory(:binary)` reports total refc pool size. `:erlang.memory(:processes)` is heap-only. If the former is huge and the latter is small, you have a binary leak.

## Design decisions

- **Option A — `:binary.copy/1` at extraction point**: always safe; small allocation cost.
- **Option B — `:erlang.garbage_collect/1` periodically**: crude; stalls the process.
- **Option C — `hibernate` after each request**: fine for GenServer, full sweep included.
- **Option D — spawn a short-lived worker per file, let it die**: death releases everything.

Chosen: Option D for file-processing workers (dying cleans up automatically); Option A elsewhere.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule BinaryLeakLab.MixProject do
  use Mix.Project
  def project, do: [app: :binary_leak_lab, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:recon, "~> 2.5"}, {:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: The leaker — `lib/binary_leak_lab/leaker.ex`

**Objective**: Extract sub-binary timestamp from large blob, storing it uncopied so refc parent anchors entire binary until GC.

```elixir
defmodule BinaryLeakLab.Leaker do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok), do: {:ok, %{timestamps: []}}

  def ingest(blob) when is_binary(blob), do: GenServer.call(__MODULE__, {:ingest, blob})

  @impl true
  def handle_call({:ingest, blob}, _from, state) do
    # Extract an 8-byte "timestamp" from the head. This creates a sub-binary
    # that pins the ENTIRE blob in the refc pool.
    <<ts::binary-size(8), _rest::binary>> = blob
    {:reply, :ok, %{state | timestamps: [ts | state.timestamps]}}
  end
end
```

### Step 2: The non-leaker — `lib/binary_leak_lab/non_leaker.ex`

**Objective**: Copy extracted timestamp via :binary.copy/1 so fresh heap binary breaks sub-binary link to original blob.

```elixir
defmodule BinaryLeakLab.NonLeaker do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok), do: {:ok, %{timestamps: []}}

  def ingest(blob) when is_binary(blob), do: GenServer.call(__MODULE__, {:ingest, blob})

  @impl true
  def handle_call({:ingest, blob}, _from, state) do
    <<ts::binary-size(8), _rest::binary>> = blob
    # :binary.copy/1 breaks the sub-binary link: ts becomes a fresh 8-byte
    # heap binary, the original blob becomes eligible for release.
    safe_ts = :binary.copy(ts)
    {:reply, :ok, %{state | timestamps: [safe_ts | state.timestamps]}}
  end
end
```

## Why this works

`:binary.copy/1` allocates a fresh binary of the target bytes. For 8 bytes, it is stored on the heap (inline, not refc). The sub-binary that pointed into the 10 MB blob is no longer referenced, so on the next GC the ProcBin is freed and the refc binary can be released.

Without the copy, `state.timestamps` is a list of sub-binaries, each holding a ProcBin to a different 10 MB blob. After processing 100 files the process "only" uses 500 KB of heap (100 sub-binaries + 8 bytes each of header), but the refc pool holds 1 GB.

## Tests — `test/binary_leak_lab/leak_test.exs`

```elixir
defmodule BinaryLeakLab.LeakTest do
  use ExUnit.Case, async: false

  defp big_blob(mb), do: :crypto.strong_rand_bytes(mb * 1_024 * 1_024)

  defp binary_memory_mb, do: div(:erlang.memory(:binary), 1_024 * 1_024)

  describe "the leaker" do
    test "refc pool grows proportionally to blobs ingested" do
      {:ok, _} = BinaryLeakLab.Leaker.start_link([])
      before = binary_memory_mb()

      for _ <- 1..20, do: BinaryLeakLab.Leaker.ingest(big_blob(1))

      :erlang.garbage_collect(Process.whereis(BinaryLeakLab.Leaker))
      after_ = binary_memory_mb()

      # Leak: refc memory should still be high despite GC.
      assert after_ - before >= 10
    end
  end

  describe "the non-leaker" do
    test ":binary.copy/1 allows refc release on GC" do
      {:ok, _} = BinaryLeakLab.NonLeaker.start_link([])
      for _ <- 1..20, do: BinaryLeakLab.NonLeaker.ingest(big_blob(1))

      before_gc = binary_memory_mb()
      :erlang.garbage_collect(Process.whereis(BinaryLeakLab.NonLeaker))
      :timer.sleep(100)
      after_gc = binary_memory_mb()

      assert after_gc <= before_gc
    end
  end
end
```

## Benchmark — `bench/leak_bench.exs`

```elixir
defmodule Blob do
  def one_mb, do: :crypto.strong_rand_bytes(1_024 * 1_024)
end

{:ok, l} = BinaryLeakLab.Leaker.start_link([])
{:ok, n} = BinaryLeakLab.NonLeaker.start_link([])

for _ <- 1..100, do: BinaryLeakLab.Leaker.ingest(Blob.one_mb())
:erlang.garbage_collect(l)
IO.puts("leaker refc:     #{div(:erlang.memory(:binary), 1024 * 1024)} MB")

for _ <- 1..100, do: BinaryLeakLab.NonLeaker.ingest(Blob.one_mb())
:erlang.garbage_collect(n)
IO.puts("non-leaker refc: #{div(:erlang.memory(:binary), 1024 * 1024)} MB")
```

**Expected**: leaker refc ~100 MB, non-leaker refc ~0 MB.

## Deep Dive: BEAM Scheduler Tuning and Memory Profiling in Production

The BEAM scheduler is not "magic" — it's a preemptive work-stealing scheduler that divides CPU time 
into reductions (bytecode instructions). Understanding scheduler tuning is critical when you suspect 
latency spikes in production.

**Key concepts**:
- **Reductions budget**: By default, a process gets ~2000 reductions before yielding to another process.
  Heavy CPU work (binary matching, list recursion) can exhaust the budget and cause tail latency.
- **Dirty schedulers**: If a process does CPU-intensive work (crypto, compression, numerical), it blocks 
  the main scheduler. Use dirty NIFs or `spawn_opt(..., [{:fullsweep_after, 0}])` for GC tuning.
- **Heap tuning per process**: `Process.flag(:min_heap_size, ...)` reserves heap upfront, reducing GC 
  pauses. Measure; don't guess.

**Memory profiling workflow**:
1. Run `recon:memory/0` in iex; identify top 10 memory consumers by type (atoms, binaries, ets).
2. If binaries dominate, check for refc binary leaks (binary held by process that should have been freed).
3. Use `eprof` or `fprof` for function-level CPU attribution; `recon:proc_window/3` for process memory trends.

**Production pattern**: Deploy with `+K true` (async IO), `-env ERL_MAX_PORTS 65536` (port limit), 
`+T 9` (async threads). Measure GC time with `erlang:statistics(garbage_collection)` — if >5% of uptime, 
tune heap or reduce allocation pressure. Never assume defaults are optimal for YOUR workload.

---

## Advanced Considerations

Understanding BEAM internals at production scale requires deep knowledge of scheduler behavior, memory models, and garbage collection dynamics. The soft real-time guarantees of BEAM only hold under specific conditions — high system load, uneven process distribution across schedulers, or GC pressure can break predictable latency completely. Monitor `erlang:statistics(run_queue)` in production to catch scheduler saturation before it degrades latency significantly. The difference between immediate, offheap, and continuous GC garbage collection strategies can significantly impact tail latencies in systems with millions of messages per second and sustained memory pressure.

Process reductions and the reduction counter affect scheduler fairness fundamentally. A process that runs for extended periods without yielding can starve other processes, even though the scheduler treats it fairly by reduction count per scheduling interval. This is especially critical in pipelines processing large data structures or performing recursive computations where yielding points are infrequent and difficult to predict. The BEAM's preemption model is deterministic per reduction, making performance testing reproducible but sometimes hiding race conditions that only manifest under specific load patterns and GC interactions.

The interaction between ETS, Mnesia, and process message queues creates subtle bottlenecks in distributed systems. ETS reads don't block other processes, but writes require acquiring locks; understanding when your workload transitions from read-heavy to write-heavy is crucial for capacity planning. Port drivers and NIFs bypass the BEAM scheduler entirely, which can lead to unexpected priority inversions if not carefully managed. Always profile with `eprof` and `fprof` in realistic production-like environments before deployment to catch performance surprises.


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. `:binary.copy/1` has a cost.** Copying 1 MB takes ~100µs. If the whole binary is needed, copy is wasteful. Copy only the extracted slice.

**2. Pattern match bodies may still pin.** `<<_::binary-size(100), rest::binary>>` where you keep `rest` does NOT release the first 100 bytes; `rest` is still a sub-binary. Copy `rest` too if you need only part of it.

**3. `binary_part/3` is an alias that behaves identically.** Do not assume it copies. It doesn't.

**4. GenServer state with sub-binaries is the #1 production leak pattern.** `state = %{history: [sub_binary, ...]}` accumulates anchors. Audit every `state` field that can hold binaries.

**5. `:recon.bin_leak(5)` in production.** It runs full-sweep GC on the top 5 suspects and reports bytes freed. Use in a triage, never as a cron job (full sweeps stall processes).

**6. When NOT to worry.** Short-lived processes (< 1s lifetime) GC or die before the pool matters. Workers that die after each request are naturally leak-proof.

## Reflection

A coworker proposes running `:erlang.garbage_collect(pid)` on every GenServer callback to prevent leaks. Why is this the wrong fix, and what correct patterns cover 90% of cases?

## Resources

- [Erlang binaries — erlang.org](https://www.erlang.org/doc/efficiency_guide/binaryhandling.html)
- [Refc binary leak — Erlang in Anger](https://www.erlang-in-anger.com/)
- [`recon.bin_leak/1` — hexdocs](https://hexdocs.pm/recon/recon.html#bin_leak/1)
- [The BEAM Book — binaries chapter](https://blog.stenmans.org/theBeamBook/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
