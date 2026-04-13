# Build Your Own Event Bus

**Project**: `event_bus_built` — in-VM pub/sub with Registry, delivery guarantees, and subscriber isolation

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
event_bus_built/
├── lib/
│   └── event_bus_built.ex
├── script/
│   └── main.exs
├── test/
│   └── event_bus_built_test.exs
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

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule EventBusBuilt.MixProject do
  use Mix.Project

  def project do
    [
      app: :event_bus_built,
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
### `lib/event_bus_built.ex`

```elixir
defmodule EventBus.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry,
       keys: :duplicate,
       name: EventBus.Registry,
       partitions: System.schedulers_online()},
      {DynamicSupervisor, name: EventBus.SubscriberSupervisor, strategy: :one_for_one},
      EventBus.Outbox,
      EventBus.Telemetry
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EventBus.Supervisor)
  end
end

defmodule EventBus.Bus do
  @moduledoc """
  Public API of the event bus. All calls are non-blocking.
  Delivery mode is chosen at subscribe time.
  """

  alias EventBus.{Subscriber, Outbox}

  @type topic :: String.t()
  @type pattern :: String.t()
  @type mode :: :fire_and_forget | :at_least_once_local

  @doc "Returns publish result from topic and payload."
  @spec publish(topic, term()) :: :ok
  def publish(topic, payload) when is_binary(topic) do
    delivery_id = System.unique_integer([:monotonic, :positive])

    envelope = %{
      id: delivery_id,
      topic: topic,
      payload: payload,
      ts: System.system_time(:microsecond)
    }

    :telemetry.execute([:event_bus, :publish], %{count: 1}, %{topic: topic})

    Registry.dispatch(EventBus.Registry, :all, fn subscribers ->
      Enum.each(subscribers, fn {pid, %{pattern: pattern, mode: mode}} ->
        if topic_match?(topic, pattern), do: deliver(pid, envelope, mode)
      end)
    end)

    :ok
  end

  @doc "Returns subscribe result from pattern, handler_mod and mode."
  @spec subscribe(pattern, module(), mode) :: {:ok, pid()} | {:error, term()}
  def subscribe(pattern, handler_mod, mode \\ :fire_and_forget)
      when is_binary(pattern) and is_atom(handler_mod) do
    DynamicSupervisor.start_child(
      EventBus.SubscriberSupervisor,
      {Subscriber, pattern: pattern, handler: handler_mod, mode: mode}
    )
  end

  @doc "Returns unsubscribe result from pid."
  @spec unsubscribe(pid()) :: :ok
  def unsubscribe(pid) when is_pid(pid) do
    DynamicSupervisor.terminate_child(EventBus.SubscriberSupervisor, pid)
  end

  defp deliver(pid, envelope, :fire_and_forget), do: send(pid, {:event, envelope})

  defp deliver(pid, envelope, :at_least_once_local) do
    Outbox.record(envelope, pid)
    send(pid, {:event_with_ack, envelope})
  end

  @doc "Returns whether topic match holds from topic and pattern."
  def topic_match?(topic, pattern) do
    match_segments(String.split(topic, "."), String.split(pattern, "."))
  end

  defp match_segments(_, ["#"]), do: true
  defp match_segments([], []), do: true
  defp match_segments([], _), do: false
  defp match_segments(_, []), do: false
  defp match_segments([_ | rest_t], ["*" | rest_p]), do: match_segments(rest_t, rest_p)
  defp match_segments([seg | rest_t], [seg | rest_p]), do: match_segments(rest_t, rest_p)
  defp match_segments(_, _), do: false
end

defmodule EventBus.Subscriber do
  @moduledoc """
  Supervised wrapper around a user-provided handler module.

  The handler module must export `handle_event/1` returning `:ok` or
  `{:error, reason}`. On `{:error, _}` in at-least-once mode the envelope
  remains in the outbox for redelivery.
  """

  use GenServer

  alias EventBus.Outbox

  @lag_threshold 1_000
  @lag_check_interval_ms 5_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(opts) do
    pattern = Keyword.fetch!(opts, :pattern)
    handler = Keyword.fetch!(opts, :handler)
    mode = Keyword.get(opts, :mode, :fire_and_forget)

    {:ok, _} = Registry.register(EventBus.Registry, :all, %{pattern: pattern, mode: mode})
    Process.send_after(self(), :check_lag, @lag_check_interval_ms)

    {:ok, %{handler: handler, mode: mode, pattern: pattern}}
  end

  @impl true
  def handle_info({:event, envelope}, state) do
    safe_dispatch(state.handler, envelope)
    {:noreply, state}
  end

  def handle_info({:event_with_ack, envelope}, state) do
    case safe_dispatch(state.handler, envelope) do
      :ok -> Outbox.ack(envelope.id, self())
      _ -> :keep_in_outbox
    end

    {:noreply, state}
  end

  def handle_info(:check_lag, state) do
    {:message_queue_len, len} = Process.info(self(), :message_queue_len)

    if len > @lag_threshold do
      :telemetry.execute(
        [:event_bus, :subscriber, :lag],
        %{queue_len: len},
        %{pattern: state.pattern, handler: state.handler}
      )
    end

    Process.send_after(self(), :check_lag, @lag_check_interval_ms)
    {:noreply, state}
  end

  defp safe_dispatch(handler, envelope) do
    handler.handle_event(envelope)
  rescue
    exception ->
      :telemetry.execute(
        [:event_bus, :subscriber, :exception],
        %{count: 1},
        %{handler: handler, reason: Exception.message(exception)}
      )

      {:error, exception}
  end
end

defmodule EventBus.Outbox do
  @moduledoc """
  Per-subscriber outbox backed by ETS. Supports at-least-once-local delivery.

  Key: `{subscriber_pid, delivery_id}`. On ack the entry is deleted.
  Entries older than `@retention_ms` are swept periodically.
  """

  use GenServer

  @table :event_bus_outbox
  @retention_ms 5 * 60 * 1_000
  @sweep_interval_ms 30_000

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Returns record result from envelope and pid."
  @spec record(map(), pid()) :: :ok
  def record(envelope, pid) do
    :ets.insert(@table, {{pid, envelope.id}, envelope, System.monotonic_time(:millisecond)})
    :ok
  end

  @doc "Returns ack result from delivery_id and pid."
  @spec ack(non_neg_integer(), pid()) :: :ok
  def ack(delivery_id, pid) do
    :ets.delete(@table, {pid, delivery_id})
    :ok
  end

  @doc "Returns pending result from pid."
  @spec pending(pid()) :: [map()]
  def pending(pid) do
    :ets.match_object(@table, {{pid, :_}, :_, :_})
    |> Enum.map(fn {_key, envelope, _ts} -> envelope end)
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:sweep, state) do
    cutoff = System.monotonic_time(:millisecond) - @retention_ms
    ms = [{{:_, :_, :"$1"}, [{:<, :"$1", cutoff}], [true]}]
    count = :ets.select_delete(@table, ms)

    if count > 0,
      do: :telemetry.execute([:event_bus, :outbox, :swept], %{count: count}, %{})

    Process.send_after(self(), :sweep, @sweep_interval_ms)
    {:noreply, state}
  end
end

defmodule EventBus.Telemetry do
  @moduledoc false
  use GenServer

  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    events = [
      [:event_bus, :publish],
      [:event_bus, :subscriber, :lag],
      [:event_bus, :subscriber, :exception],
      [:event_bus, :outbox, :swept]
    ]

    :telemetry.attach_many("event-bus-logger", events, &process_request/4, nil)
    {:ok, %{}}
  end

  @doc "Handles result from event, measurements, metadata and _config."
  def process_request(event, measurements, metadata, _config) do
    Logger.debug("#{inspect(event)} #{inspect(measurements)} #{inspect(metadata)}")
  end
end

defmodule EventBus.OutboxTest do
  use ExUnit.Case, async: false
  doctest EventBusBuilt.MixProject

  alias EventBus.Outbox

  describe "core functionality" do
    test "record then ack removes entry" do
      envelope = %{id: 42, topic: "t", payload: :p, ts: 0}
      Outbox.record(envelope, self())
      assert [^envelope] = Outbox.pending(self())

      Outbox.ack(42, self())
      assert [] = Outbox.pending(self())
    end
  end

  # test/event_bus/delivery_test.exs
  defmodule EventBus.DeliveryTest do
    use ExUnit.Case, async: false

    alias EventBus.{Bus, Outbox}

    defmodule FailingHandler do
      def handle_event(_), do: {:error, :transient}
    end

    test "at_least_once_local keeps envelope on error" do
      {:ok, pid} = Bus.subscribe("retry.*", FailingHandler, :at_least_once_local)
      Bus.publish("retry.now", :payload)

      Process.sleep(50)
      assert length(Outbox.pending(pid)) >= 1
    end
  end

  # bench/bus_bench.exs
  defmodule NoopHandler do
    def handle_event(_), do: :ok
  end
end

Enum.each(1..500, fn _ -> EventBus.Bus.subscribe("order.*", NoopHandler) end)
Enum.each(1..500, fn _ -> EventBus.Bus.subscribe("invoice.*", NoopHandler) end)

Benchee.run(
  %{
    "publish matching 500" => fn -> EventBus.Bus.publish("order.placed", :payload) end,
    "publish matching 0"   => fn -> EventBus.Bus.publish("unknown.topic", :payload) end
  },
  time: 5, warmup: 2, parallel: 4
)
```
### `test/event_bus_built_test.exs`

```elixir
defmodule EventBus.BusTest do
  use ExUnit.Case, async: true
  doctest EventBusBuilt.MixProject

  alias EventBus.Bus

  defmodule TestHandler do
    def handle_event(%{payload: {:forward, pid, tag}}) do
      send(pid, {tag, :received})
      :ok
    end

    def handle_event(_), do: :ok
  end

  setup do
    on_exit(fn ->
      EventBus.SubscriberSupervisor
      |> DynamicSupervisor.which_children()
      |> Enum.each(fn {_, pid, _, _} ->
        DynamicSupervisor.terminate_child(EventBus.SubscriberSupervisor, pid)
      end)
    end)

    :ok
  end

  describe "subscribe/publish" do
    test "delivers exact-topic messages" do
      {:ok, _} = Bus.subscribe("order.placed", TestHandler)
      Bus.publish("order.placed", {:forward, self(), :got})
      assert_receive {:got, :received}, 200
    end

    test "honors * wildcard" do
      {:ok, _} = Bus.subscribe("order.*", TestHandler)
      Bus.publish("order.cancelled", {:forward, self(), :wild})
      assert_receive {:wild, :received}, 200
    end

    test "honors # catch-all" do
      {:ok, _} = Bus.subscribe("#", TestHandler)
      Bus.publish("anything.else.here", {:forward, self(), :all})
      assert_receive {:all, :received}, 200
    end

    test "non-matching topics are not delivered" do
      {:ok, _} = Bus.subscribe("invoice.*", TestHandler)
      Bus.publish("order.placed", {:forward, self(), :nope})
      refute_receive {:nope, :received}, 100
    end
  end

  describe "isolation" do
    defmodule Crasher do
      def handle_event(_), do: raise("boom")
    end

    test "crashing handler does not affect other subscribers" do
      {:ok, _} = Bus.subscribe("boom.*", Crasher)
      {:ok, _} = Bus.subscribe("boom.*", TestHandler)

      Bus.publish("boom.one", {:forward, self(), :survived})

      assert_receive {:survived, :received}, 200
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Build Your Own Event Bus.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Build Your Own Event Bus ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case EventBusBuilt.run(payload) do
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
        for _ <- 1..1_000, do: EventBusBuilt.run(:bench)
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

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
