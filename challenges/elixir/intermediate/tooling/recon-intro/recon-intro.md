# Production diagnostics with `:recon`

**Project**: `recon_intro` — a small app with a deliberately misbehaving
process so you can practice the four or five `:recon` calls that save
outages at 3am.

---

## Why recon intro matters

`:recon` is Fred Hébert's battle-hardened production inspection library.
It's the tool you reach for when:

- `:observer` is not available (headless node, Alpine container).
- You need to spot the biggest memory consumer right now.
- You want to trace a specific function call pattern under load without
  blowing up the VM.
- You're looking for leaky processes (growing mailbox, inflated heap).

`:recon` is designed to be safe on live production nodes: every function
has guardrails (rate-limited tracing, sampled inspection, time-boxed
queries). It's standard equipment on Nerves, WhatsApp, Heroku, and most
BEAM shops.

This exercise spawns a "bad" process — one with a growing mailbox and a
bloating heap — and walks you through `:recon`'s core API to find it.

---

## Project structure

```
recon_intro/
├── lib/
│   └── recon_intro.ex
├── script/
│   └── main.exs
├── test/
│   └── recon_intro_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:recon.proc_count/2` — top-N processes by a metric

```elixir
:recon.proc_count(:memory, 5)
# [{#PID<0.xxx.0>, 10_485_760, [registered_name: ReconIntro.BloatedWorker, ...]}, ...]
```
Sorts EVERY live process by `:memory`, `:reductions`, `:message_queue_len`,
`:total_heap_size`, etc. Returns the top N. The last element of each
tuple is a keyword list of identifying info so you can tell who the PID
IS, not just its number.

### 2. `:recon.proc_window/3` — rate, not total

```elixir
:recon.proc_window(:reductions, 5, 1_000)   # top 5 by reductions in 1s
```
`proc_count/2` shows cumulative totals (which favors old processes).
`proc_window/3` shows ACTIVITY over a time window — who is busy *right
now*. This is the one you want for "what's hogging CPU?".

### 3. `:recon.info/1` — everything about one process

```elixir
:recon.info(pid)
```
Returns a structured report: memory, mailbox, stack, links, monitors,
current function, dictionary. Safer than `Process.info/1` because it
truncates gigantic messages and binary references so you don't kill
your own node formatting the output.

### 4. `:recon_alloc` — allocator inspection

When memory is growing but no single process is big, the culprit is often
the allocators (large binaries, ETS, code, atoms). `:recon_alloc.memory/1`
summarizes by category; `:recon_alloc.fragmentation/1` shows fragmented
carriers.

### 5. `:recon_trace.calls/2` — safe tracing

```elixir
:recon_trace.calls({Enum, :map, :_}, 10)
```
Traces up to 10 calls to `Enum.map/*` then AUTO-STOPS. Unlike raw `:dbg`,
you cannot accidentally burn your node — `:recon_trace` rate-limits and
time-limits every trace.

---

## Why recon and not raw `:dbg` + `Process.info`

`Process.info/1` on a pid holding a large binary returns the binary
inline, which IEx then tries to render — a classic "debug tool brought
down the node I was debugging" story. `:dbg` has no rate limiting: a
wildcard match on a hot function will flood the node with messages
faster than the tracer can drain them. `:recon` wraps both with
truncation and rate limits that make every function safe to call on a
suffering production node.

---

## Design decisions

**Option A — Pure OTP tools (`:erlang.process_info`, `:dbg`, `:erlang.memory`)**
- Pros: Zero dependencies; always available; the lowest-level truth.
- Cons: Each tool is dangerous (binary bombs, trace floods, manual
  sorting); you reimplement the safe wrappers yourself on every outage.

**Option B — `:recon` as a standard dep** (chosen)
- Pros: Battle-tested wrappers; consistent output shape; rate-limited
  tracing; allocator diagnostics (`:recon_alloc`) that OTP doesn't
  package; the idioms match Fred Hébert's *Erlang in Anger*.
- Cons: One extra dep; another vocabulary to learn (`proc_count` vs
  `proc_window`, etc.).

→ Chose **B** because the cost of a misused raw `:dbg` on a production
  node vastly exceeds the 300KB of `:recon`. Every shop running BEAM in
  production ships `:recon` as standard equipment.

---

## Implementation

### `mix.exs`

```elixir
defmodule ReconIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :recon_intro,
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
    []
  end
end
```
### Step 1: Create the project with `recon` as a dependency

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new recon_intro --sup
cd recon_intro
```

Add `:recon` to `mix.exs`:

### Step 2: The culprit — `lib/recon_intro/bloated_worker.ex`

**Objective**: Provide The culprit — `lib/recon_intro/bloated_worker.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.

```elixir
defmodule ReconIntro.BloatedWorker do
  @moduledoc """
  Deliberately misbehaves so we can spot it with `:recon`:

    * grows its state every tick (heap bloat),
    * never drains its mailbox if external senders pile up.

  Use `stuff/0` from IEx to push it harder.
  """

  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Simulates external pressure — sends many messages that we never read."
  @spec stuff(pos_integer()) :: :ok
  def stuff(n \\ 10_000) do
    for _ <- 1..n, do: send(__MODULE__, {:noise, :crypto.strong_rand_bytes(1_024)})
    :ok
  end

  @impl true
  def init(_opts) do
    # A big initial blob so the process is visibly fat from the start.
    schedule()
    {:ok, %{blobs: [], ticks: 0}}
  end

  @impl true
  def handle_info(:tick, %{blobs: blobs, ticks: t} = state) do
    # Append 256KB of junk each tick → heap grows without bound.
    new_blob = :crypto.strong_rand_bytes(256 * 1_024)
    schedule()
    {:noreply, %{state | blobs: [new_blob | blobs], ticks: t + 1}}
  end

  # Deliberately DO NOT match {:noise, _} so those messages accumulate in the
  # mailbox — simulating a slow consumer being overwhelmed.

  defp schedule, do: Process.send_after(self(), :tick, 500)
end
```
### `lib/recon_intro/application.ex`

**Objective**: Wire `application.ex` to start the runtime wiring needed so the tool under study has something real to inspect, format, or report on.

```elixir
defmodule ReconIntro.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [ReconIntro.BloatedWorker]
    Supervisor.start_link(children, strategy: :one_for_one, name: ReconIntro.Supervisor)
  end
end
```
Remember to wire it in `mix.exs`:

```elixir
def application do
  [
    extra_applications: [:logger],
    mod: {ReconIntro.Application, []}
  ]
end
```
### `lib/recon_intro.ex`

**Objective**: Edit `recon_intro.ex` — convenience wrappers, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

```elixir
defmodule ReconIntro do
  @moduledoc """
  Thin wrappers around `:recon` that make it pleasant to call from IEx.

  Run `iex -S mix` and then:

      iex> ReconIntro.top_memory()
      iex> ReconIntro.top_busy(1_000)
      iex> ReconIntro.info_worker()

  Use `ReconIntro.BloatedWorker.stuff(50_000)` to pile pressure on the worker
  and see numbers move.
  """

  @doc "Top processes by heap memory."
  @spec top_memory(pos_integer()) :: list()
  def top_memory(n \\ 5), do: :recon.proc_count(:memory, n)

  @doc "Top processes by reductions in the last `window_ms` milliseconds."
  @spec top_busy(pos_integer(), pos_integer()) :: list()
  def top_busy(window_ms \\ 1_000, n \\ 5), do: :recon.proc_window(:reductions, n, window_ms)

  @doc "Top processes by message queue length (pending messages)."
  @spec top_inbox(pos_integer()) :: list()
  def top_inbox(n \\ 5), do: :recon.proc_count(:message_queue_len, n)

  @doc "Detailed info about the bloated worker."
  @spec info_worker() :: list()
  def info_worker do
    case Process.whereis(ReconIntro.BloatedWorker) do
      nil -> raise "BloatedWorker not running"
      pid -> :recon.info(pid)
    end
  end
end
```
### Step 5: `test/recon_intro_test.exs`

**Objective**: Write `recon_intro_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule ReconIntroTest do
  use ExUnit.Case, async: false

  doctest ReconIntro

  describe "core functionality" do
    test "top_memory/1 returns a list with at least the worker" do
      results = ReconIntro.top_memory(20)
      assert is_list(results)
      assert length(results) > 0

      # Each entry is {pid, value, attrs_list}.
      for {pid, value, attrs} <- results do
        assert is_pid(pid)
        assert is_integer(value)
        assert is_list(attrs)
      end
    end

    test "top_inbox/1 catches the BloatedWorker after we stuff it" do
      ReconIntro.BloatedWorker.stuff(5_000)
      # Give the scheduler a moment before we measure.
      Process.sleep(50)

      [{_pid, qlen, attrs} | _] = ReconIntro.top_inbox(5)
      assert qlen > 0

      # One of the top inbox-loaded processes should be our worker.
      names =
        ReconIntro.top_inbox(20)
        |> Enum.flat_map(fn {_pid, _q, attrs} -> List.wrap(attrs[:registered_name]) end)

      assert ReconIntro.BloatedWorker in names
    end

    test "info_worker/0 returns a non-empty info list" do
      info = ReconIntro.info_worker()
      assert is_list(info)
      assert length(info) > 0
    end
  end
end
```
### Step 6: Drive it from IEx

**Objective**: Drive it from IEx.

```bash
iex -S mix
iex> ReconIntro.top_memory()
iex> ReconIntro.BloatedWorker.stuff(50_000)
iex> ReconIntro.top_inbox()     # BloatedWorker now at the top
iex> ReconIntro.info_worker()   # drill into it

# Memory by allocator category:
iex> :recon_alloc.memory(:usage)

# Safely trace 5 calls to `String.downcase/1` across the node:
iex> :recon_trace.calls({String, :downcase, :_}, 5)
iex> String.downcase("HELLO")
# the trace prints once, auto-stops after 5.
```

### Why this works

`:recon.proc_count/2` sorts every live process by a BEAM-native metric
(memory, reductions, mailbox), then returns the top N with identifying
attributes so you can resolve a pid to "the process named X running
function Y". `proc_window/3` does the same but computes deltas over a
time window — distinguishing "process that's old and big" from "process
that's busy right now". Combined with `:recon.info/1` (truncated,
binary-safe process detail) you get a diagnostic loop that works on a
struggling production node without tipping it over.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule ReconIntro.BloatedWorker do
    @moduledoc """
    Deliberately misbehaves so we can spot it with `:recon`:

      * grows its state every tick (heap bloat),
      * never drains its mailbox if external senders pile up.

    Use `stuff/0` from IEx to push it harder.
    """

    use GenServer

    def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

    @doc "Simulates external pressure — sends many messages that we never read."
    @spec stuff(pos_integer()) :: :ok
    def stuff(n \\ 10_000) do
      for _ <- 1..n, do: send(__MODULE__, {:noise, :crypto.strong_rand_bytes(1_024)})
      :ok
    end

    @impl true
    def init(_opts) do
      # A big initial blob so the process is visibly fat from the start.
      schedule()
      {:ok, %{blobs: [], ticks: 0}}
    end

    @impl true
    def handle_info(:tick, %{blobs: blobs, ticks: t} = state) do
      # Append 256KB of junk each tick → heap grows without bound.
      new_blob = :crypto.strong_rand_bytes(256 * 1_024)
      schedule()
      {:noreply, %{state | blobs: [new_blob | blobs], ticks: t + 1}}
    end

    # Deliberately DO NOT match {:noise, _} so those messages accumulate in the
    # mailbox — simulating a slow consumer being overwhelmed.

    defp schedule, do: Process.send_after(self(), :tick, 500)
  end

  def main do
    IO.puts("=== Recon Demo ===
  ")
  
    # Demo: Recon tracing
  IO.puts("1. recon_trace for lightweight tracing")
  IO.puts("2. recon.info - system info")
  IO.puts("3. Alternative to :dbg")

  IO.puts("
  ✓ Recon demo completed!")
  end

end

Main.main()
```
## Benchmark

`:timer.tc(fn -> :recon.proc_count(:memory, 5) end)` on a node with 10k
processes typically returns in under 10 ms. The cost scales linearly
with the live process count. Target: well under 50 ms on healthy nodes
(tens of thousands of processes). If it takes longer, the node is
already in trouble and `:recon` is doing its job — surfacing it.

---

## Trade-offs and production gotchas

**1. `:recon.proc_count(:memory, _)` vs `:recon.proc_window(:memory, _, _)`**
`proc_count` is the right call for memory (a level, not a rate).
`proc_window` is the right call for reductions / reductions per second
(an activity rate). Mixing them up is the #1 misread of recon output.

**2. `:recon.info/1` is the SAFE `Process.info/1`**
Plain `Process.info(pid)` on a process holding a 2GB binary returns a
tuple with the binary inside. IEx then tries to print it. You crash your
OWN node. `:recon.info/1` truncates. Always prefer it on production.

**3. `:recon_trace` is safer than `:dbg` — but it is still TRACING**
Even rate-limited, tracing on a hot path has overhead. Don't leave traces
running overnight. Match patterns as tightly as possible: `{M, F, A}` with
specific arity, not `{M, :_, :_}`.

**4. `:recon_alloc` is the answer to "memory is growing but no process is fat"**
If `:recon.proc_count(:memory, 50)` shows nothing big but `:erlang.memory/0`
is climbing, the culprit is usually binaries (off-heap) or ETS. Run
`:recon_alloc.memory(:usage)` and `:recon_alloc.fragmentation(:current)`
to find it.

**5. `:recon` requires the node to be RUNNING**
Obvious, but: if a node OOMs, `:recon` went down with it. For post-mortem
analysis on a crashed node you need crash dumps (see `erl_crash.dump`
analyzer in Erlang docs or `crashdump_viewer`).

**6. When NOT to use `:recon`**
- For long-term monitoring → `:telemetry` + TSDB. `:recon` is ad-hoc and
  interactive.
- For CI — there's nothing interesting running.
- If you have `:observer` and a GUI, prefer it; it's less error-prone
  because it forces you to click rather than remember call shapes.

---

## Reflection

- Your monitoring alerts at 3am: `:erlang.memory(:total)` is climbing
  at 50 MB/min. `ReconIntro.top_memory(20)` shows nothing above 20 MB.
  What's your next three commands, and what would each of them rule in
  or out?
- You decide to keep a `:recon_trace.calls/2` running on a hot path in
  production to collect data for a bug report. What failure modes does
  that decision create, and what guardrails would you add (time limit,
  rate limit, call-site scope) before leaving it running unattended?

## Resources

- [`:recon` — main docs](https://hexdocs.pm/recon/) and the [Recon User's Guide](https://ferd.github.io/recon/)
- ["Stuff Goes Bad: Erlang in Anger" — Fred Hébert](https://www.erlang-in-anger.com/) — the book. Free PDF.
- [`:recon.info/1`](https://hexdocs.pm/recon/recon.html#info-1)
- [`:recon.proc_count/2` and `proc_window/3`](https://hexdocs.pm/recon/recon.html#proc_count-2)
- [`:recon_trace`](https://hexdocs.pm/recon/recon_trace.html) — safe tracing
- [`:recon_alloc`](https://hexdocs.pm/recon/recon_alloc.html) — allocator diagnostics

## Deep Dive

Elixir's tooling ecosystem extends beyond the language into DevOps, profiling, and observability. Understanding each tool's role prevents misuse and false optimizations.

**Mix tasks and releases:**
Custom mix tasks (`mix myapp.setup`, `mix myapp.migrate`) encapsulate operational knowledge. Tasks run in the host environment (not the compiled app), so they're ideal for setup, teardown, or scripting. Releases, built with `mix release`, create self-contained OTP applications deployable without Elixir installed. They're immutable: no source code changes after release — all config comes from environment variables or runtime files.

**Debugging and profiling tools:**
- `:observer` (GUI): real-time process tree, metrics, and port inspection
- `Recon`: production-safe introspection (stable even under high load)
- `:eprof`: function-level timing; lower overhead than `:fprof`
- `:fprof`: detailed trace analysis; use only in staging

**Profiling approaches:**
Ceiling profiling (e.g., "which modules consume CPU?") is cheap; go there first with `perf` or `eprof`. Floor profiling (e.g., "which lines in this function are slow?") is expensive; reserve for specific functions. In production, prefer metrics (Prometheus, New Relic) over profiling — continuous profiling has overhead. Store profiling data for post-mortem analysis, not real-time dashboards.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/recon_intro_test.exs`

```elixir
defmodule ReconIntroTest do
  use ExUnit.Case, async: true

  doctest ReconIntro

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ReconIntro.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
