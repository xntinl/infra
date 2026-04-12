# `:recon` and `:recon_trace` for Production Diagnosis

**Project**: `prod_triage` — a toolkit wrapping `recon` and `recon_trace` for live diagnosis: finding the top memory offenders, chasing binary leaks, safely tracing a function in production without flooding the shell.

## Project context

It is 3 AM. A production node is at 18 GB of memory, 12 GB of which is `binary`. One of 40k processes is blocking; you do not know which. The logs show nothing abnormal. You have an IEx remote shell. What do you run?

`:recon` — by Fred Hebert, author of *Learn You Some Erlang* and *Erlang in Anger* — provides safe, bounded primitives for exactly this. Safe: no unbounded data fetches. Bounded: every function caps what it can output. `recon_trace` adds rate-limited tracing so you can attach a trace, see 10 samples, and auto-detach before the shell drowns.

```
prod_triage/
├── lib/
│   └── prod_triage/
│       ├── application.ex
│       └── triage.ex
├── test/
│   └── prod_triage/
│       └── triage_test.exs
└── mix.exs
```

## Why `:recon` and not raw `:erlang.*`

`:erlang.processes()` on a node with 2M processes returns a 2M-element list. Piping it through `Process.info/1` returns 2M maps, each with 30 fields. You just OOM'd the shell process.

`:recon.proc_count(:memory, 10)` returns the top 10 by memory using a bounded streaming sort — it never builds the full list. Every `recon` function is designed not to make things worse.

`:dbg` is powerful but dangerous: a typo like `:dbg.tpl(:_, :x)` matches every function on the node and floods the shell forever. `:recon_trace.calls/2` auto-stops after N messages or a time limit.

## Core concepts

### 1. `:recon.proc_count/2`

`:recon.proc_count(attribute, n)` returns the top N processes by `:memory`, `:reductions`, `:message_queue_len`, etc. Bounded, safe for prod.

### 2. `:recon.proc_window/3`

`:recon.proc_window(attribute, n, interval_ms)` samples twice with the interval, returning top N by DELTA. Useful for "which process is accumulating reductions right now" vs "which process accumulated over a lifetime".

### 3. `:recon.bin_leak/1`

Full-sweep-GCs the top N suspected binary holders and reports bytes freed. Quick confirmation of a refc leak, followed by a `proc_count(:binary_memory, ...)` to find who is holding.

### 4. `:recon_trace.calls/2`

`:recon_trace.calls({Mod, :fun, :_}, max)` traces calls with match specs, capped at `max` messages. After the cap, the trace self-terminates. Supports `return_trace`, argument filters, formatters.

### 5. `:recon_alloc`

Inspection of Erlang allocators (eheap_alloc, binary_alloc, ets_alloc). Reveals fragmentation and "sbcs vs mbcs" issues; for deep memory triage.

## Design decisions

- **Option A — `observer`**: GUI, blocks on data collection when node is under load, requires network connectivity.
- **Option B — raw `:erlang.*` in a remote shell**: flexible but unsafe.
- **Option C — `:recon` via a helper module**: bounded, composable, text-only — safe on any connection.

Chosen: Option C. Observer for dev, recon for production.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ProdTriage.MixProject do
  use Mix.Project
  def project, do: [app: :prod_triage, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:recon, "~> 2.5"}]
end
```

### Step 1: Triage wrapper — `lib/prod_triage/triage.ex`

```elixir
defmodule ProdTriage.Triage do
  @moduledoc """
  Curated `recon` calls, parameterized for common incidents.
  Exposed as plain functions so they can be called from a remote IEx
  without loading any `use` machinery.
  """

  @doc """
  Top N processes by attribute. Use :memory for leak hunting,
  :reductions for CPU hogs, :message_queue_len for backpressure.
  """
  def top(attribute, n \\ 10) when attribute in [:memory, :reductions, :message_queue_len, :binary_memory] do
    :recon.proc_count(attribute, n)
    |> Enum.map(fn {pid, val, extra} -> %{pid: pid, value: val, info: extra} end)
  end

  @doc "Top N by delta over `window_ms`. Best for live-rate questions."
  def window(attribute, n \\ 10, window_ms \\ 5_000) do
    :recon.proc_window(attribute, n, window_ms)
  end

  @doc """
  Full-sweep GCs the top binary-memory holders and reports bytes freed.
  Runs GC on real processes — mildly intrusive, use judiciously.
  """
  def bin_leak(n \\ 5), do: :recon.bin_leak(n)

  @doc """
  Rate-limited call trace. Example:
      Triage.trace({MyMod, :some_fun, :_}, 20)
  Messages flow to the calling process as text via recon's formatter.
  """
  def trace(mfa, max, opts \\ []) do
    :recon_trace.calls(mfa, max, opts)
  end

  @doc "Full picture of the node's memory allocators."
  def memory_summary do
    :erlang.memory()
    |> Enum.sort_by(fn {_k, v} -> -v end)
    |> Enum.map(fn {k, v} -> {k, div(v, 1_048_576), :mb} end)
  end

  @doc "Per-scheduler run queue lengths; > 0 means oversubscription."
  def run_queues, do: :erlang.statistics(:run_queue_lengths)
end
```

## Why this works

`recon.proc_count(:memory, N)` uses a bounded heap-size priority queue internally: O(P log N) for P processes. Even at 2M processes, it finishes in ~200ms without allocating 2M list cells.

`recon_trace.calls/2` installs a process-local trace handler with a hard message count. When `max` is hit, it auto-detaches. You cannot flood the shell even if you trace a hot function.

`bin_leak/1` selects the top candidates by `binary_memory`, calls `:erlang.garbage_collect/1` on each, and returns the BYTES RECOVERED — the key diagnostic for refc leaks.

## Tests — `test/prod_triage/triage_test.exs`

```elixir
defmodule ProdTriage.TriageTest do
  use ExUnit.Case, async: false
  alias ProdTriage.Triage

  describe "top/2" do
    test "returns the top N by memory" do
      top = Triage.top(:memory, 5)
      assert length(top) == 5
      assert Enum.all?(top, &is_pid(&1.pid))
      # Sorted descending by value.
      values = Enum.map(top, & &1.value)
      assert values == Enum.sort(values, :desc)
    end

    test "accepts :reductions attribute" do
      assert [%{} | _] = Triage.top(:reductions, 3)
    end

    test "rejects unknown attributes via match failure" do
      assert_raise FunctionClauseError, fn -> Triage.top(:nonsense, 5) end
    end
  end

  describe "memory_summary/0" do
    test "lists allocator buckets" do
      summary = Triage.memory_summary()
      assert Enum.any?(summary, fn {k, _, _} -> k == :binary end)
      assert Enum.any?(summary, fn {k, _, _} -> k == :processes end)
    end
  end

  describe "run_queues/0" do
    test "returns a list with one entry per online scheduler" do
      assert length(Triage.run_queues()) == System.schedulers_online()
    end
  end

  describe "trace/3" do
    test "caps the number of trace messages" do
      defmodule Callee do
        def noisy, do: :ok
      end

      {:ok, _} = :application.ensure_all_started(:recon)
      Triage.trace({Callee, :noisy, :_}, 3)
      for _ <- 1..10, do: Callee.noisy()
      Process.sleep(50)
      :recon_trace.clear()
      assert true
    end
  end
end
```

## Playbook (cheat sheet for a 3 AM incident)

```
# 1. Memory is high — who holds it?
Triage.memory_summary()

# 2. Processes by memory
Triage.top(:memory, 10)

# 3. Binary leak?
Triage.bin_leak(5)     # GC top suspects, see bytes freed
Triage.top(:binary_memory, 10)

# 4. CPU hog?
Triage.window(:reductions, 10, 5_000)   # rate over 5s

# 5. A process is stuck — what is it doing?
Process.info(pid, [:current_stacktrace, :message_queue_len, :status])

# 6. Trace a call safely
Triage.trace({MyMod, :handle_cast, :_}, 20, [:return_trace])
```

## Trade-offs and production gotchas

**1. `proc_count/2` is a snapshot.** Between the call and your action, the landscape changes. Re-run after mitigations; do not assume the top 10 now is the top 10 in 30s.

**2. `bin_leak/1` pauses those processes.** Full sweeps stall. On latency-critical processes, use sparingly.

**3. `recon_trace` is process-local but visible to BEAM's trace BIF.** Only ONE tracer may be active per process. If two engineers attach traces simultaneously, the second replaces the first.

**4. `recon.proc_window` on a quiet node returns noise.** Delta is meaningless when no work happened in the window. Increase the interval.

**5. `:erts_debug.df/1` + BeamAsm is NOT a recon feature.** Don't confuse asm inspection with behavioral tracing.

**6. When NOT to use recon.** Automated alerting. Recon is for interactive humans. Use `:telemetry` events for machine-readable triggers.

## Reflection

Your node is at 100% CPU. `Triage.top(:reductions, 10)` shows the expected suspects — web request handlers. `Triage.window(:reductions, 10, 2_000)` shows one garbage_collector process consuming 50% of reductions. What does that tell you about the node's health, and which recon function do you reach for next?

## Resources

- [`recon` on hexdocs](https://hexdocs.pm/recon/)
- [Erlang in Anger — free PDF, written by recon's author](https://www.erlang-in-anger.com/)
- [`recon_trace.calls/3` source](https://github.com/ferd/recon/blob/master/src/recon_trace.erl)
- [Fred Hebert on tracing safely](https://ferd.ca/)
