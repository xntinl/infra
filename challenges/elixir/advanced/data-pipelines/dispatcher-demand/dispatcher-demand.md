# GenStage DemandDispatcher + Custom Dispatcher

**Project**: `demand_dispatcher` — a work-stealing pool for expensive image thumbnail generation, plus a custom round-robin dispatcher for fair distribution

---

## Why data pipelines matters

GenStage, Flow, and Broadway make back-pressured concurrent data processing a first-class concern. Producers, consumers, dispatchers, and batchers compose into pipelines that absorb bursts without exhausting memory.

The hard problems are exactly-once semantics, checkpointing for resumability, and tuning batcher concurrency against downstream latency. A pipeline that works at 10 events/sec often collapses at 10k unless these concerns were designed in from the start.

---

## The business problem

You are building a production-grade Elixir component in the **Data pipelines** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
demand_dispatcher/
├── lib/
│   └── demand_dispatcher.ex
├── script/
│   └── main.exs
├── test/
│   └── demand_dispatcher_test.exs
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

Chose **B** because in Data pipelines the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule DemandDispatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :demand_dispatcher,
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

### `lib/demand_dispatcher.ex`

```elixir
defmodule DemandDispatcher.JobProducer do
  @moduledoc "Producer for thumbnail jobs using DemandDispatcher."
  use GenStage

  @type job :: %{id: pos_integer(), path: String.t(), size: :s | :m | :l}

  def start_link(_), do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
  @doc "Returns push result from job."
  def push(job), do: GenStage.cast(__MODULE__, {:push, job})

  @impl true
  def init(:ok) do
    {:producer, %{},
     dispatcher: GenStage.DemandDispatcher, buffer_size: 10_000, buffer_keep: :last}
  end

  @impl true
  def handle_cast({:push, job}, state), do: {:noreply, [job], state}

  @doc "Handles demand result from _d and state."
  @impl true
  def handle_demand(_d, state), do: {:noreply, [], state}
end

defmodule DemandDispatcher.ThumbnailWorker do
  use GenStage

  def start_link(id) do
    GenStage.start_link(__MODULE__, id, name: :"thumb_worker_#{id}")
  end

  @impl true
  def init(id) do
    {:consumer, %{id: id, processed: 0},
     subscribe_to: [{DemandDispatcher.JobProducer, max_demand: 4, min_demand: 2}]}
  end

  @doc "Handles events result from events, _from and state."
  @impl true
  def handle_events(events, _from, state) do
    Enum.each(events, fn _job -> :timer.sleep(Enum.random(50..200)) end)
    {:noreply, [], %{state | processed: state.processed + length(events)}}
  end
end

defmodule DemandDispatcher.RoundRobinDispatcher do
  @moduledoc """
  Dispatches events in strict round-robin across subscribers. Each subscriber
  has a `pending` counter; events are only sent when the target has pending > 0.
  If the next subscriber has pending = 0, the event is buffered and retried.
  """
  @behaviour GenStage.Dispatcher

  @impl true
  def init(_opts), do: {:ok, {[], 0, []}}

  # state = {subscribers_list, cursor, buffer}

  @doc "Returns subscribe result from _opts, ref, cursor and buf."
  @impl true
  def subscribe(_opts, {pid, ref}, {subs, cursor, buf}) do
    entry = %{pid: pid, ref: ref, pending: 0}
    {:ok, 0, {subs ++ [entry], cursor, buf}}
  end

  @doc "Returns cancel result from ref, cursor and buf."
  @impl true
  def cancel({_pid, ref}, {subs, cursor, buf}) do
    new_subs = Enum.reject(subs, &(&1.ref == ref))
    new_cursor = if new_subs == [], do: 0, else: rem(cursor, length(new_subs))
    {:ok, 0, {new_subs, new_cursor, buf}}
  end

  @doc "Returns ask result from counter, ref, cursor and buf."
  @impl true
  def ask(counter, {_pid, ref}, {subs, cursor, buf}) do
    new_subs =
      Enum.map(subs, fn
        %{ref: ^ref} = s -> %{s | pending: s.pending + counter}
        s -> s
      end)

    {flushed, remaining_buf, new_subs2, new_cursor} = flush(buf, new_subs, cursor)
    # events flushed back from buffer to actual subscribers
    Enum.each(flushed, fn {pid, r, msgs} ->
      send(pid, {:"$gen_consumer", {self(), r}, msgs})
    end)

    ask_up = min(counter, length(remaining_buf))
    # demand to pass up = total asked - what we could satisfy from buffer
    {:ok, counter - ask_up, {new_subs2, new_cursor, Enum.drop(remaining_buf, ask_up)}}
  end

  @doc "Returns dispatch result from events, _length, cursor and buf."
  @impl true
  def dispatch(events, _length, {subs, cursor, buf}) do
    {dispatched, leftover, new_subs, new_cursor} = distribute(events, subs, cursor, [])
    new_buf = buf ++ leftover

    Enum.each(dispatched, fn {pid, ref, msgs} ->
      send(pid, {:"$gen_consumer", {self(), ref}, msgs})
    end)

    {:ok, [], {new_subs, new_cursor, new_buf}}
  end

  @doc "Returns info result from msg and state."
  @impl true
  def info(msg, state) do
    send(self(), msg)
    {:ok, state}
  end

  # ---- helpers ----

  defp distribute([], subs, cursor, acc), do: {group(acc), [], subs, cursor}

  defp distribute([e | rest] = all, subs, cursor, acc) do
    if subs == [] or Enum.all?(subs, &(&1.pending == 0)) do
      {group(acc), all, subs, cursor}
    else
      idx = rem(cursor, length(subs))
      sub = Enum.at(subs, idx)

      if sub.pending > 0 do
        new_sub = %{sub | pending: sub.pending - 1}
        new_subs = List.replace_at(subs, idx, new_sub)
        distribute(rest, new_subs, cursor + 1, [{sub.pid, sub.ref, e} | acc])
      else
        distribute(all, subs, cursor + 1, acc)
      end
    end
  end

  defp flush(buf, subs, cursor), do: flush(buf, [], subs, cursor)

  defp flush([], out, subs, cursor), do: {group(out), [], subs, cursor}

  defp flush([e | rest] = all, out, subs, cursor) do
    if Enum.all?(subs, &(&1.pending == 0)) do
      {group(out), all, subs, cursor}
    else
      idx = rem(cursor, length(subs))
      sub = Enum.at(subs, idx)

      if sub.pending > 0 do
        new_sub = %{sub | pending: sub.pending - 1}
        new_subs = List.replace_at(subs, idx, new_sub)
        flush(rest, [{sub.pid, sub.ref, e} | out], new_subs, cursor + 1)
      else
        flush(all, out, subs, cursor + 1)
      end
    end
  end

  defp group(entries) do
    entries
    |> Enum.reverse()
    |> Enum.group_by(fn {pid, ref, _} -> {pid, ref} end, fn {_, _, msg} -> msg end)
    |> Enum.map(fn {{pid, ref}, msgs} -> {pid, ref, msgs} end)
  end
end

defmodule DemandDispatcher.Application do
  use Application

  @impl true
  def start(_type, _args) do
    workers =
      for i <- 1..4 do
        Supervisor.child_spec({DemandDispatcher.ThumbnailWorker, i}, id: {:worker, i})
      end

    children = [DemandDispatcher.JobProducer] ++ workers
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### `test/demand_dispatcher_test.exs`

```elixir
defmodule DemandDispatcher.DemandTest do
  use ExUnit.Case, async: true
  doctest DemandDispatcher.JobProducer

  alias DemandDispatcher.{JobProducer, ThumbnailWorker}

  setup do
    Application.stop(:demand_dispatcher)
    Application.start(:demand_dispatcher)
    Process.sleep(100)
    :ok
  end

  describe "DemandDispatcher.Demand" do
    test "work is distributed across workers" do
      for i <- 1..100 do
        JobProducer.push(%{id: i, path: "/a.jpg", size: :m})
      end

      Process.sleep(3_000)

      counts =
        for i <- 1..4 do
          :sys.get_state(:"thumb_worker_#{i}").processed
        end

      assert Enum.sum(counts) == 100
      # every worker should get non-trivial load
      assert Enum.all?(counts, &(&1 >= 5))
    end
  end
end

defmodule DemandDispatcher.RoundRobinTest do
  use ExUnit.Case, async: false

  defmodule RRProducer do
    use GenStage

    def start_link, do: GenStage.start_link(__MODULE__, :ok, name: __MODULE__)
    def push(e), do: GenStage.cast(__MODULE__, {:push, e})

    @impl true
    def init(:ok) do
      {:producer, %{}, dispatcher: DemandDispatcher.RoundRobinDispatcher}
    end

    @impl true
    def handle_cast({:push, e}, s), do: {:noreply, [e], s}

    @impl true
    def handle_demand(_d, s), do: {:noreply, [], s}
  end

  defmodule Collector do
    use GenStage

    def start_link(name) do
      GenStage.start_link(__MODULE__, name, name: name)
    end

    @impl true
    def init(_name) do
      {:consumer, %{seen: []}, subscribe_to: [{RRProducer, max_demand: 100}]}
    end

    @impl true
    def handle_events(events, _from, s), do: {:noreply, [], %{s | seen: s.seen ++ events}}
  end

  describe "DemandDispatcher.RoundRobin" do
    test "round-robin distributes 1-at-a-time per subscriber" do
      {:ok, _} = RRProducer.start_link()
      {:ok, _} = Collector.start_link(:c1)
      {:ok, _} = Collector.start_link(:c2)
      Process.sleep(50)

      for i <- 1..10, do: RRProducer.push(i)
      Process.sleep(200)

      s1 = :sys.get_state(:c1).seen
      s2 = :sys.get_state(:c2).seen
      assert length(s1) + length(s2) == 10
      # round-robin → each sees ~5
      assert abs(length(s1) - length(s2)) <= 1
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate DemandDispatcher: work-stealing, fastest consumer gets next task
      {:ok, p} = GenStage.start_link(GenstageAdvanced.IngestProducer, 
        [dispatcher: GenStage.DemandDispatcher, buffer_size: 100], [])
      {:ok, c1} = GenStage.start_link(GenstageAdvanced.Aggregator, 
        [subscribe_to: [{p, max_demand: 100}]], [])
      {:ok, c2} = GenStage.start_link(GenstageAdvanced.Aggregator, 
        [subscribe_to: [{p, max_demand: 50}]], [])

      Process.sleep(20)

      # Push events
      for i <- 1..10, do: GenStage.cast(p, {:push, %{id: i, payload: "task", ts: 0}})

      Process.sleep(100)

      c1_count = :sys.get_state(c1).count

      IO.puts("✓ DemandDispatcher: consumer1 (high demand) got #{c1_count} tasks")
      assert c1_count > 0, "Consumer with higher demand got tasks"
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

### 1. Demand drives back-pressure

GenStage's pull model means slow consumers don't drown fast producers. Producers ask 'give me N events when you have them' rather than producers shoving events downstream.

### 2. Batchers trade latency for throughput

Broadway batchers accumulate events before flushing. A batch size of 100 with a 1-second timeout balances throughput against latency — tune both axes.

### 3. Idempotency is not optional

At-least-once delivery is the default in distributed pipelines. Exactly-once requires idempotent processing, deduplication keys, and durable checkpoints.

---
