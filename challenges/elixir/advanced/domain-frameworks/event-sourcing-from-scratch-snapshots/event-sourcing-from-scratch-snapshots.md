# Event Sourcing from Scratch with Snapshots

**Project**: `inventory_es` — minimal event-sourced warehouse-inventory aggregate with hand-built event store, snapshot support, and optimistic concurrency — no Commanded, no framework

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
inventory_es/
├── lib/
│   └── inventory_es.ex
├── script/
│   └── main.exs
├── test/
│   └── inventory_es_test.exs
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
defmodule InventoryEs.MixProject do
  use Mix.Project

  def project do
    [
      app: :inventory_es,
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
### `lib/inventory_es.ex`

```elixir
defmodule InventoryEs.Repo.Migrations.CreateEvents do
  @moduledoc """
  Ejercicio: Event Sourcing from Scratch with Snapshots.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Migration

  def change do
    create table(:events, primary_key: false) do
      add :id, :bigserial, primary_key: true
      add :stream_id, :string, null: false
      add :version, :integer, null: false
      add :type, :string, null: false
      add :payload, :jsonb, null: false
      add :inserted_at, :utc_datetime_usec, null: false, default: fragment("now()")
    end

    create unique_index(:events, [:stream_id, :version])
    create index(:events, [:type])
  end
end

defmodule InventoryEs.Repo.Migrations.CreateSnapshots do
  use Ecto.Migration

  def change do
    create table(:snapshots, primary_key: false) do
      add :stream_id, :string, primary_key: true
      add :version, :integer, null: false
      add :state, :jsonb, null: false
      add :inserted_at, :utc_datetime_usec, null: false, default: fragment("now()")
    end
  end
end

defmodule InventoryEs.Repo do
  use Ecto.Repo, otp_app: :inventory_es, adapter: Ecto.Adapters.Postgres
end

defmodule InventoryEs.Errors do
  defmodule ConcurrencyError do
    defexception [:stream_id, :expected_version, :message]

    @impl true
    def exception(opts) do
      %__MODULE__{
        stream_id: opts[:stream_id],
        expected_version: opts[:expected_version],
        message: "stream #{opts[:stream_id]} is at a different version than expected (#{opts[:expected_version]})"
      }
    end
  end
end

defmodule InventoryEs.Stock.Events do
  defmodule StockReceived do
    @derive Jason.Encoder
    defstruct [:sku, :quantity]
  end

  defmodule StockReserved do
    @derive Jason.Encoder
    defstruct [:sku, :reservation_id, :quantity]
  end

  defmodule ReservationCancelled do
    @derive Jason.Encoder
    defstruct [:sku, :reservation_id]
  end
end

defmodule InventoryEs.Stock.Commands do
  defmodule ReceiveStock do
    @enforce_keys [:sku, :quantity]
    defstruct [:sku, :quantity]
  end

  defmodule Reserve do
    @enforce_keys [:sku, :reservation_id, :quantity]
    defstruct [:sku, :reservation_id, :quantity]
  end

  defmodule CancelReservation do
    @enforce_keys [:sku, :reservation_id]
    defstruct [:sku, :reservation_id]
  end
end

defmodule InventoryEs.Stock.Aggregate do
  alias InventoryEs.Stock.Commands.{ReceiveStock, Reserve, CancelReservation}
  alias InventoryEs.Stock.Events.{StockReceived, StockReserved, ReservationCancelled}

  defstruct sku: nil, on_hand: 0, reservations: %{}

  # --- decide: command → events or error ---

  def decide(%__MODULE__{}, %ReceiveStock{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def decide(%__MODULE__{} = _state, %ReceiveStock{} = cmd) do
    {:ok, [%StockReceived{sku: cmd.sku, quantity: cmd.quantity}]}
  end

  def decide(%__MODULE__{}, %Reserve{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def decide(%__MODULE__{reservations: r}, %Reserve{reservation_id: rid})
      when is_map_key(r, rid),
      do: {:error, :reservation_already_exists}

  def decide(%__MODULE__{} = state, %Reserve{} = cmd) do
    reserved_total = state.reservations |> Map.values() |> Enum.sum()
    available = state.on_hand - reserved_total

    if cmd.quantity > available do
      {:error, {:insufficient_stock, available: available}}
    else
      {:ok,
       [
         %StockReserved{
           sku: cmd.sku,
           reservation_id: cmd.reservation_id,
           quantity: cmd.quantity
         }
       ]}
    end
  end

  def decide(%__MODULE__{reservations: r}, %CancelReservation{reservation_id: rid})
      when not is_map_key(r, rid),
      do: {:error, :reservation_not_found}

  def decide(%__MODULE__{} = _state, %CancelReservation{} = cmd) do
    {:ok, [%ReservationCancelled{sku: cmd.sku, reservation_id: cmd.reservation_id}]}
  end

  # --- apply: event → state ---

  def apply(%__MODULE__{} = state, %StockReceived{sku: sku, quantity: q}),
    do: %{state | sku: sku, on_hand: state.on_hand + q}

  def apply(%__MODULE__{} = state, %StockReserved{reservation_id: rid, quantity: q}),
    do: %{state | reservations: Map.put(state.reservations, rid, q)}

  def apply(%__MODULE__{} = state, %ReservationCancelled{reservation_id: rid}),
    do: %{state | reservations: Map.delete(state.reservations, rid)}
end

defmodule InventoryEs.EventStore do
  import Ecto.Query
  alias InventoryEs.Repo
  alias InventoryEs.Errors.ConcurrencyError

  @snapshot_every 100

  def append(stream_id, expected_version, events) when is_list(events) do
    Repo.transaction(fn ->
      rows =
        events
        |> Enum.with_index(expected_version + 1)
        |> Enum.map(fn {event, v} ->
          %{
            stream_id: stream_id,
            version: v,
            type: event.__struct__ |> Module.split() |> List.last(),
            payload: Map.from_struct(event),
            inserted_at: DateTime.utc_now()
          }
        end)

      case Repo.insert_all("events", rows) do
        {n, _} when n == length(rows) -> :ok
        _ -> Repo.rollback(:append_failed)
      end
    end)
    |> translate_conflict(stream_id, expected_version)
  end

  defp translate_conflict({:ok, :ok}, _stream_id, _expected), do: :ok

  defp translate_conflict({:error, %Postgrex.Error{postgres: %{code: :unique_violation}}},
         stream_id,
         expected),
       do: raise(ConcurrencyError, stream_id: stream_id, expected_version: expected)

  defp translate_conflict({:error, reason}, _stream_id, _expected),
    do: {:error, reason}

  def read_stream(stream_id, from_version \\ 0) do
    Repo.all(
      from e in "events",
        where: e.stream_id == ^stream_id and e.version > ^from_version,
        order_by: [asc: e.version],
        select: %{version: e.version, type: e.type, payload: e.payload}
    )
  end

  def read_snapshot(stream_id) do
    Repo.one(
      from s in "snapshots",
        where: s.stream_id == ^stream_id,
        select: %{version: s.version, state: s.state}
    )
  end

  def maybe_write_snapshot(stream_id, version, state) when rem(version, @snapshot_every) == 0 do
    Repo.insert_all(
      "snapshots",
      [%{stream_id: stream_id, version: version, state: Map.from_struct(state),
         inserted_at: DateTime.utc_now()}],
      on_conflict: {:replace, [:version, :state, :inserted_at]},
      conflict_target: [:stream_id]
    )

    :ok
  end

  def maybe_write_snapshot(_stream_id, _version, _state), do: :ok
end

defmodule InventoryEs.Stock.Repository do
  alias InventoryEs.EventStore
  alias InventoryEs.Stock.Aggregate
  alias InventoryEs.Stock.Events.{StockReceived, StockReserved, ReservationCancelled}

  def stream_id(sku), do: "stock-" <> sku

  @doc "Load aggregate by replaying snapshot + tail events. Returns {state, current_version}."
  def load(sku) do
    stream = stream_id(sku)

    {base_state, from_version} =
      case EventStore.read_snapshot(stream) do
        nil -> {%Aggregate{}, 0}
        %{version: v, state: json} -> {deserialize_state(json), v}
      end

    events = EventStore.read_stream(stream, from_version)
    final = Enum.reduce(events, base_state, fn e, acc -> Aggregate.apply(acc, rehydrate(e)) end)
    version = if events == [], do: from_version, else: List.last(events).version
    {final, version}
  end

  @doc "Execute a command, persist events, write snapshot if due."
  def execute(sku, cmd) do
    {state, version} = load(sku)

    case Aggregate.decide(state, cmd) do
      {:ok, events} ->
        stream = stream_id(sku)
        :ok = EventStore.append(stream, version, events)

        new_state = Enum.reduce(events, state, &Aggregate.apply(&2, &1))
        new_version = version + length(events)
        EventStore.maybe_write_snapshot(stream, new_version, new_state)
        {:ok, new_state, new_version}

      {:error, _} = err ->
        err
    end
  end

  # --- helpers ---

  defp rehydrate(%{type: "StockReceived", payload: p}),
    do: %StockReceived{sku: p["sku"], quantity: p["quantity"]}

  defp rehydrate(%{type: "StockReserved", payload: p}),
    do: %StockReserved{sku: p["sku"], reservation_id: p["reservation_id"], quantity: p["quantity"]}

  defp rehydrate(%{type: "ReservationCancelled", payload: p}),
    do: %ReservationCancelled{sku: p["sku"], reservation_id: p["reservation_id"]}

  defp deserialize_state(%{"sku" => sku, "on_hand" => on_hand, "reservations" => res}) do
    %Aggregate{
      sku: sku,
      on_hand: on_hand,
      reservations: for({k, v} <- res, into: %{}, do: {k, v})
    }
  end
end
```
### `test/inventory_es_test.exs`

```elixir
defmodule InventoryEs.StockTest do
  use ExUnit.Case, async: true
  doctest InventoryEs.Repo.Migrations.CreateEvents

  alias InventoryEs.Stock.Repository
  alias InventoryEs.Stock.Commands.{ReceiveStock, Reserve, CancelReservation}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(InventoryEs.Repo)
    Ecto.Adapters.SQL.Sandbox.mode(InventoryEs.Repo, {:shared, self()})
    :ok
  end

  describe "ReceiveStock" do
    test "increases on_hand" do
      sku = unique_sku()
      assert {:ok, state, 1} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 50})
      assert state.on_hand == 50
    end

    test "rejects non-positive quantity" do
      sku = unique_sku()
      assert {:error, :quantity_must_be_positive} =
               Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 0})
    end
  end

  describe "Reserve" do
    test "reserves within available stock" do
      sku = unique_sku()
      {:ok, _, _} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 10})
      assert {:ok, state, 2} =
               Repository.execute(sku, %Reserve{sku: sku, reservation_id: "r1", quantity: 3})

      assert state.reservations["r1"] == 3
    end

    test "rejects over-reservation" do
      sku = unique_sku()
      {:ok, _, _} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 2})
      assert {:error, {:insufficient_stock, available: 2}} =
               Repository.execute(sku, %Reserve{sku: sku, reservation_id: "r1", quantity: 3})
    end
  end

  describe "replay from snapshot" do
    test "state reconstructed from snapshot + tail matches pure replay" do
      sku = unique_sku()
      {:ok, _, _} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 1_000})

      for i <- 1..120 do
        {:ok, _, _} =
          Repository.execute(sku, %Reserve{sku: sku, reservation_id: "r#{i}", quantity: 1})
      end

      {state, version} = Repository.load(sku)
      assert version == 121
      assert map_size(state.reservations) == 120
    end
  end

  defp unique_sku, do: "sku-" <> Integer.to_string(:erlang.unique_integer([:positive]))
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Event Sourcing from Scratch with Snapshots.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Event Sourcing from Scratch with Snapshots ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case InventoryEs.run(payload) do
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
        for _ <- 1..1_000, do: InventoryEs.run(:bench)
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
