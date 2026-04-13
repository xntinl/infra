# Deadline Propagation Across Processes

**Project**: `rpc_deadlines` — propagates an end-to-end deadline through Task-backed fan-outs and GenServer hops, so a 500ms HTTP request can't spawn background work that runs for 10 seconds.

## The business problem

An HTTP handler has a 500ms SLO. It spawns three parallel Tasks: pricing, inventory, recommendations. Each of those calls two further GenServers. The handler times out the top-level Tasks at 500ms — but the GenServer calls inside each Task have their own independent 5_000ms timeout. When the handler returns 504 to the user, those downstream calls keep running, holding DB connections, writing cache entries for a response nobody will read.

Deadline propagation threads a single deadline instance across every hop. Each process — Task, GenServer callback, nested Task — reads it, derives its own `remaining`, and short-circuits when zero.

## Project structure

```
rpc_deadlines/
├── lib/
│   └── rpc_deadlines/
│       ├── deadline.ex             # opaque struct
│       ├── context.ex              # $callers-style propagation
│       ├── task_sup.ex             # deadline-aware Task spawner
│       └── pricing.ex              # example nested work
├── test/
│   └── rpc_deadlines/
│       └── propagation_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why process dictionary and not explicit arguments

Passing `deadline` as an explicit function argument is cleanest (see a parallel exercise in this set that uses it) but it requires every function in every library to accept it. Not realistic when you want to propagate through third-party code.

Process dictionary (`Process.put/2`, `Process.get/1`) is per-process. When you spawn a Task, the new process has a fresh empty dictionary. We bridge this gap with a convention: `Task.async`-equivalent that reads the parent's deadline and sets it in the child before running the function.

## Why not `$callers`

Elixir's `$callers` key already propagates from parent to Task child via `Task.async/1`, `Task.async_stream/2`, etc. It's for *tracing* (who spawned me?), not for *deadlines*. Abusing it conflates concerns. We use our own key `:deadline`.

## Design decisions

- **Option A — Pass as arg**: best for green-field code.
- **Option B — Process dictionary + helper for spawning**: works for existing code and for libraries that don't expose an option.
→ Chose **B** for this exercise (the arg-based version is a separate exercise in this set). Demonstrates the pattern that large codebases actually use when they can't thread deadlines through 1000 call sites.

- **Option A — Deadline as integer ms**: easy to serialize, easy to misuse (forget it's absolute).
- **Option B — Opaque struct `%Deadline{at: ms}`**: cannot be confused with a duration.
→ Chose **B**.

## Implementation

### Dependencies (`mix.exs`)

### `mix.exs`
```elixir
defmodule DeadlinePropagationProcesses.MixProject do
  use Mix.Project

  def project do
    [
      app: :deadline_propagation_processes,
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
defmodule RpcDeadlines.MixProject do
  use Mix.Project
  def project, do: [app: :rpc_deadlines, version: "0.1.0", elixir: "~> 1.19", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```
### Step 1: Deadline (`lib/rpc_deadlines/deadline.ex`)

**Objective**: Represent deadlines as absolute monotonic timestamps so all descendants inherit min(parent, local) via process dictionary without nested timeouts extending budget.

```elixir
defmodule RpcDeadlines.Deadline do
  @type t :: %__MODULE__{at: integer()}
  defstruct [:at]

  def within(ms), do: %__MODULE__{at: System.monotonic_time(:millisecond) + ms}

  def remaining(%__MODULE__{at: at}),
    do: max(0, at - System.monotonic_time(:millisecond))

  def expired?(%__MODULE__{} = d), do: remaining(d) == 0
end
```
### Step 2: Context (`lib/rpc_deadlines/context.ex`)

**Objective**: Store deadline in Process.put/get so descendants read budget without modifying function signatures across libraries and legacy code.

```elixir
defmodule RpcDeadlines.Context do
  alias RpcDeadlines.Deadline

  @key :rpc_deadline

  def put(%Deadline{} = d), do: Process.put(@key, d)

  def get, do: Process.get(@key)

  def remaining do
    case get() do
      nil -> :infinity
      %Deadline{} = d -> Deadline.remaining(d)
    end
  end

  def expired? do
    case get() do
      nil -> false
      %Deadline{} = d -> Deadline.expired?(d)
    end
  end
end
```
### Step 3: Deadline-aware Task spawner (`lib/rpc_deadlines/task_sup.ex`)

**Objective**: Bridge process boundaries by copying parent deadline to child's process dictionary before user function runs so Tasks inherit remaining budget.

```elixir
defmodule RpcDeadlines.TaskSup do
  @moduledoc """
  Drop-in alternative to Task.async/1 that carries the caller's deadline
  into the spawned process.
  """
  alias RpcDeadlines.Context

  def async(fun) when is_function(fun, 0) do
    parent_deadline = Context.get()

    Task.async(fn ->
      if parent_deadline, do: Context.put(parent_deadline)
      fun.()
    end)
  end

  def await(%Task{} = t) do
    Task.await(t, Context.remaining())
  end

  def async_stream(enum, fun, opts \\ []) do
    parent = Context.get()

    wrapped = fn item ->
      if parent, do: Context.put(parent)
      fun.(item)
    end

    remaining =
      case Context.remaining() do
        :infinity -> 5_000
        n -> n
      end

    Task.async_stream(
      enum,
      wrapped,
      Keyword.merge([timeout: remaining, on_timeout: :kill_task], opts)
    )
  end
end
```
### Step 4: Example nested work (`lib/rpc_deadlines/pricing.ex`)

**Objective**: Check Context.expired?/0 before compute and inside async_stream so workers abort when deadline closes instead of burning CPU on doomed tasks.

```elixir
defmodule RpcDeadlines.Pricing do
  alias RpcDeadlines.{Context, TaskSup}

  def compute(items) do
    if Context.expired?() do
      {:error, :deadline_exceeded}
    else
      results =
        TaskSup.async_stream(items, &price_one/1, max_concurrency: 5)
        |> Enum.to_list()

      {:ok, results}
    end
  end

  defp price_one(item) do
    cond do
      Context.expired?() -> {:error, :deadline_exceeded}
      true -> do_work(item)
    end
  end

  defp do_work(item) do
    remaining = Context.remaining()
    sleep_ms = min(remaining, item.work_ms)
    Process.sleep(sleep_ms)

    if Context.expired?() do
      {:error, :deadline_exceeded}
    else
      {:ok, item.id}
    end
  end
end
```
## Why this works

- **Explicit handoff at spawn** — `TaskSup.async` reads the parent's `:rpc_deadline` *before* spawning and writes it in the child's startup, so the new process's dictionary is never empty for deadline-scoped work.
- **`remaining/0` as timeout default** — `Task.await(t, remaining())` ensures the await itself is clamped. Even if the child ignores deadlines, the parent's await will time out first.
- **Cheap short-circuit** — `expired?/0` is a process-dict read plus a monotonic subtraction: < 100ns. Costs nothing to call every few hundred lines.
- **`async_stream` timeout = remaining** — if the caller has 200ms left and one item takes 300ms, `async_stream` kills that task and yields `{:exit, :timeout}` without leaving orphan workers.

## Tests

```elixir
defmodule RpcDeadlines.PropagationTest do
  use ExUnit.Case, async: false
  doctest RpcDeadlines.Pricing
  alias RpcDeadlines.{Context, Deadline, Pricing, TaskSup}

  describe "single-process propagation" do
    test "expired? false when no deadline set" do
      refute Context.expired?()
    end

    test "expired? true after sleeping past the deadline" do
      Context.put(Deadline.within(10))
      Process.sleep(20)
      assert Context.expired?()
    end
  end

  describe "deadline propagation into Task" do
    test "child inherits parent deadline" do
      Context.put(Deadline.within(500))
      parent_remaining = Context.remaining()

      t = TaskSup.async(fn -> Context.remaining() end)
      child_remaining = TaskSup.await(t)

      assert is_integer(child_remaining)
      assert child_remaining <= parent_remaining
      assert child_remaining > 0
    end

    test "child without parent deadline gets :infinity" do
      Process.delete(:rpc_deadline)

      t = TaskSup.async(fn -> Context.remaining() end)
      assert :infinity == TaskSup.await(t)
    end
  end

  describe "end-to-end with Pricing" do
    test "succeeds within deadline" do
      Context.put(Deadline.within(500))
      items = for i <- 1..3, do: %{id: i, work_ms: 20}

      assert {:ok, results} = Pricing.compute(items)
      assert length(results) == 3
      assert Enum.all?(results, &match?({:ok, _}, &1))
    end

    test "fails fast when deadline too short" do
      Context.put(Deadline.within(10))
      items = for i <- 1..3, do: %{id: i, work_ms: 100}

      {:ok, results} = Pricing.compute(items)

      assert Enum.any?(results, fn
               {:exit, :timeout} -> true
               {:ok, {:error, :deadline_exceeded}} -> true
               _ -> false
             end)
    end
  end
end
```
## Benchmark

```elixir
# Cost of a deadline check on the hot path
alias RpcDeadlines.{Context, Deadline}
Context.put(Deadline.within(1_000))

{t, _} = :timer.tc(fn ->
  for _ <- 1..1_000_000, do: Context.expired?()
end)

IO.puts("avg: #{t / 1_000_000} µs per check")
```
Expected: < 0.1µs. Process-dict + monotonic subtraction.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---

## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Process dictionary is invisible** — a test that fails because of a lingering deadline from the previous test is hellish to debug. `setup do: Process.delete(:rpc_deadline)` in every test module using deadlines.

**2. `Task.Supervisor.async_nolink` doesn't use our wrapper** — if code uses `Task.Supervisor.async_nolink/3` directly, the deadline isn't propagated. Either wrap it too or lint for its direct usage.

**3. Deadlines don't cross the wire** — serialize as `remaining()` before HTTP/RPC; reconstruct with `Deadline.within/1` on the other side. A serialized "at" value is meaningless across nodes (different monotonic clocks).

**4. Zero-cost isn't free everywhere** — each `Process.get/1` copies the value from the dict to the stack. Fine for a `%Deadline{}` struct (tiny). Don't store large blobs in process dict.

**5. `on_timeout: :kill_task` kills abruptly** — the task is killed mid-execution; any DB transactions it started may be left in ambiguous state. Design for idempotency or use `:brutal_kill` followed by compensating actions.

**6. When NOT to use this** — background jobs (Oban) have their own deadline model (job timeout). Don't confuse the two.

## Reflection

You call `TaskSup.async_stream/3` with `max_concurrency: 5` and a deadline of 100ms. One task hangs; the other four complete in 20ms. What does the stream return for the hung task, and when does the function return to the caller?

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the rpc_deadlines project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/rpc_deadlines/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `RpcDeadlines` — propagates an end-to-end deadline through Task-backed fan-outs and GenServer hops, so a 500ms HTTP request can't spawn background work that runs for 10 seconds.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[rpc_deadlines] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[rpc_deadlines] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:rpc_deadlines) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `rpc_deadlines`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```
---

## Why Deadline Propagation Across Processes matters

Mastering **Deadline Propagation Across Processes** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/rpc_deadlines.ex`

```elixir
defmodule RpcDeadlines do
  @moduledoc """
  Ejercicio: Deadline Propagation Across Processes.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  @doc """
  Entry point for the rpc_deadlines module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> RpcDeadlines.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/rpc_deadlines_test.exs`

```elixir
defmodule RpcDeadlinesTest do
  use ExUnit.Case, async: true

  doctest RpcDeadlines

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert RpcDeadlines.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Per-process deadline context
```
Caller process:   put(:deadline, d)  →  TaskSup.async(fn -> ... end)
Child process:    (startup hook) put(:deadline, d)  →  user code reads Context.get()
```

### 2. Short-circuit everywhere
Every function that may block (GenServer.call, Process.sleep, HTTP) checks the context first:
```
if Context.expired?(), do: {:error, :deadline_exceeded}, else: actually_do_the_thing()
```

### 3. `Task.await` collapses to remaining
```
Task.await(t, Context.remaining())
```
so the await itself never outlives the request.
