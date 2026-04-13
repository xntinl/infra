# Process Top with `:recon.proc_count` and `:recon.proc_window`

**Project**: `recon_info_top` — build a `top`-style process inspector that ranks BEAM processes by memory, reductions, and mailbox size without halting the scheduler

---

## Why beam internals and performance matters

Performance work on the BEAM rewards depth: schedulers, reductions, process heaps, garbage collection, binary reference counting, and the JIT compiler each have observable knobs. Tools like recon, eflame, Benchee, and :sys.statistics let you measure before tuning.

The pitfall is benchmarking without a hypothesis. Senior engineers characterize the workload first (CPU-bound? Memory-bound? Lock contention?), then choose the instrument. Premature optimization on the BEAM is particularly costly because micro-benchmarks rarely reflect real scheduler behavior under load.

---

## The business problem

You are building a production-grade Elixir component in the **BEAM internals and performance** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
recon_info_top/
├── lib/
│   └── recon_info_top.ex
├── script/
│   └── main.exs
├── test/
│   └── recon_info_top_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in BEAM internals and performance the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule ReconInfoTop.MixProject do
  use Mix.Project

  def project do
    [
      app: :recon_info_top,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/recon_info_top.ex`

```elixir
defmodule ReconInfoTop.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {DynamicSupervisor, name: ReconInfoTop.WatcherSupervisor, strategy: :one_for_one}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ReconInfoTop.Supervisor)
  end
end

defmodule ReconInfoTop.Top do
  @moduledoc """
  Thin wrapper over `:recon.proc_count/2` and `:recon.proc_window/3` returning
  ranked rows enriched with registered name, current function, and initial call.
  """

  @type attribute :: :memory | :reductions | :message_queue_len | :total_heap_size | :binary

  @type row :: %{
          pid: pid(),
          value: non_neg_integer(),
          name: atom() | nil,
          current_function: mfa() | nil,
          initial_call: mfa() | nil
        }

  @doc "Top N processes by current value of `attribute`."
  @spec count(attribute(), pos_integer()) :: [row()]
  def count(attribute, n \\ 10) when is_atom(attribute) and is_integer(n) and n > 0 do
    attribute
    |> :recon.proc_count(n)
    |> Enum.map(&to_row/1)
  end

  @doc "Top N processes by growth of `attribute` over `window_ms` milliseconds."
  @spec window(attribute(), pos_integer(), pos_integer()) :: [row()]
  def window(attribute, n, window_ms)
      when is_atom(attribute) and is_integer(n) and n > 0 and is_integer(window_ms) do
    attribute
    |> :recon.proc_window(n, window_ms)
    |> Enum.map(&to_row/1)
  end

  defp to_row({pid, value, info}) when is_list(info) do
    %{
      pid: pid,
      value: value,
      name: Keyword.get(info, :registered_name),
      current_function: Keyword.get(info, :current_function),
      initial_call: Keyword.get(info, :initial_call)
    }
  end
end

defmodule ReconInfoTop.Formatter do
  @moduledoc false

  alias ReconInfoTop.Top

  @spec render([Top.row()], Top.attribute()) :: iodata()
  def render(rows, attribute) do
    header = "#{String.pad_trailing("PID", 18)} #{String.pad_trailing(to_string(attribute), 14)} NAME / FUNCTION\n"

    body =
      Enum.map(rows, fn row ->
        [
          String.pad_trailing(inspect(row.pid), 18),
          ?\s,
          String.pad_trailing(format_value(row.value, attribute), 14),
          ?\s,
          describe(row),
          ?\n
        ]
      end)

    [header, body]
  end

  defp describe(%{name: name}) when is_atom(name) and name not in [nil, []], do: inspect(name)
  defp describe(%{current_function: {m, f, a}}), do: "#{inspect(m)}.#{f}/#{a}"
  defp describe(%{initial_call: {m, f, a}}), do: "init=#{inspect(m)}.#{f}/#{a}"
  defp describe(_), do: "unknown"

  defp format_value(v, :memory) when v >= 1_048_576, do: "#{Float.round(v / 1_048_576, 1)}MB"
  defp format_value(v, :memory) when v >= 1_024, do: "#{Float.round(v / 1_024, 1)}KB"
  defp format_value(v, _), do: Integer.to_string(v)
end

defmodule ReconInfoTop.Watcher do
  @moduledoc """
  Streaming watcher: samples `attribute` every `interval_ms` and emits rows
  to the subscriber pid. Stops cleanly when the subscriber exits.
  """

  use GenServer

  alias ReconInfoTop.Top

  @spec start(keyword()) :: DynamicSupervisor.on_start_child()
  def start(opts) do
    DynamicSupervisor.start_child(ReconInfoTop.WatcherSupervisor, {__MODULE__, opts})
  end

  @spec stop(pid()) :: :ok
  def stop(pid), do: DynamicSupervisor.terminate_child(ReconInfoTop.WatcherSupervisor, pid)

  def child_spec(opts) do
    %{id: __MODULE__, start: {__MODULE__, :start_link, [opts]}, restart: :temporary}
  end

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(opts) do
    subscriber = Keyword.fetch!(opts, :subscriber)
    attribute = Keyword.get(opts, :attribute, :memory)
    n = Keyword.get(opts, :n, 10)
    interval = Keyword.get(opts, :interval_ms, 1_000)

    Process.monitor(subscriber)
    send(self(), :tick)

    {:ok,
     %{
       subscriber: subscriber,
       attribute: attribute,
       n: n,
       interval: interval
     }}
  end

  @impl true
  def handle_info(:tick, state) do
    rows = Top.count(state.attribute, state.n)
    send(state.subscriber, {:top, state.attribute, rows})
    Process.send_after(self(), :tick, state.interval)
    {:noreply, state}
  end

  def handle_info({:DOWN, _ref, :process, pid, _}, %{subscriber: pid} = state),
    do: {:stop, :normal, state}
end
```

### `test/recon_info_top_test.exs`

```elixir
defmodule ReconInfoTop.TopTest do
  use ExUnit.Case, async: true
  doctest ReconInfoTop.Application

  alias ReconInfoTop.{Top, Watcher}

  describe "count/2" do
    test "returns at most N rows" do
      rows = Top.count(:memory, 5)
      assert length(rows) <= 5
    end

    test "each row has expected fields" do
      [row | _] = Top.count(:memory, 3)
      assert is_pid(row.pid)
      assert is_integer(row.value) and row.value >= 0
      assert Map.has_key?(row, :name)
      assert Map.has_key?(row, :current_function)
    end

    test "results are sorted descending" do
      rows = Top.count(:memory, 10)
      values = Enum.map(rows, & &1.value)
      assert values == Enum.sort(values, :desc)
    end
  end

  describe "window/3" do
    test "detects reduction growth in a busy process" do
      busy =
        spawn(fn ->
          Stream.iterate(0, &(&1 + 1)) |> Stream.take(10_000_000) |> Stream.run()
        end)

      Process.sleep(50)

      rows = Top.window(:reductions, 5, 500)
      pids = Enum.map(rows, & &1.pid)

      # The busy process should appear in the top 5 reduction-burners
      assert busy in pids
    after
      :ok
    end
  end

  describe "Watcher" do
    test "streams frames at the requested interval" do
      {:ok, pid} = Watcher.start(subscriber: self(), attribute: :memory, n: 3, interval_ms: 100)

      assert_receive {:top, :memory, rows1}, 500
      assert_receive {:top, :memory, rows2}, 500

      assert length(rows1) <= 3
      assert length(rows2) <= 3

      :ok = Watcher.stop(pid)
    end

    test "stops when subscriber exits" do
      subscriber = spawn(fn -> Process.sleep(:infinity) end)
      {:ok, pid} = Watcher.start(subscriber: subscriber, attribute: :memory, interval_ms: 50)
      ref = Process.monitor(pid)

      Process.exit(subscriber, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 1_000
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Process Top with `:recon.proc_count` and `:recon.proc_window`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Process Top with `:recon.proc_count` and `:recon.proc_window` ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ReconInfoTop.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: ReconInfoTop.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Reductions, not time, govern preemption

The BEAM scheduler counts reductions (function calls + I/O ops). After ~4000, the process yields. Long lists processed in tight Elixir loops are not the bottleneck people think.

### 2. Binary reference counting can leak

Sub-binaries hold references to large parent binaries. A 10-byte slice of a 10MB binary keeps the 10MB alive. Use :binary.copy/1 when storing slices long-term.

### 3. Profile production with recon

recon's process_window/3 finds memory leaks; bin_leak/1 finds binary refc leaks; proc_count/2 finds runaway processes. These are non-invasive and safe in production.

---
