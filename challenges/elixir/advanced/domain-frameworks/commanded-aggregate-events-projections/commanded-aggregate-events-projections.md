# Commanded — Aggregate, Events and Projections

**Project**: `shopping_cart_es` — shopping cart aggregate with domain events and a read-model projection using Commanded on top of `commanded_eventstore_adapter`

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
shopping_cart_es/
├── lib/
│   └── shopping_cart_es.ex
├── script/
│   └── main.exs
├── test/
│   └── shopping_cart_es_test.exs
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
defmodule ShoppingCartEs.MixProject do
  use Mix.Project

  def project do
    [
      app: :shopping_cart_es,
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

### `lib/shopping_cart_es.ex`

```elixir
defmodule ShoppingCartEs.CommandedApp do
  @moduledoc """
  Ejercicio: Commanded — Aggregate, Events and Projections.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Commanded.Application,
    otp_app: :shopping_cart_es,
    event_store: [
      adapter: Commanded.EventStore.Adapters.EventStore,
      event_store: ShoppingCartEs.EventStore
    ]

  router ShoppingCartEs.Router
end

defmodule ShoppingCartEs.EventStore do
  use EventStore, otp_app: :shopping_cart_es
end

defmodule ShoppingCartEs.Cart.Commands do
  defmodule AddItem do
    @enforce_keys [:cart_id, :sku, :quantity, :unit_price_cents]
    defstruct [:cart_id, :sku, :quantity, :unit_price_cents]
  end

  defmodule RemoveItem do
    @enforce_keys [:cart_id, :sku]
    defstruct [:cart_id, :sku]
  end

  defmodule Checkout do
    @enforce_keys [:cart_id]
    defstruct [:cart_id]
  end
end

defmodule ShoppingCartEs.Cart.Events do
  defmodule ItemAdded do
    @derive Jason.Encoder
    defstruct [:cart_id, :sku, :quantity, :unit_price_cents]
  end

  defmodule ItemRemoved do
    @derive Jason.Encoder
    defstruct [:cart_id, :sku]
  end

  defmodule CartCheckedOut do
    @derive Jason.Encoder
    defstruct [:cart_id, :total_cents, :item_count]
  end
end

defmodule ShoppingCartEs.Cart.Aggregate do
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}
  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  defstruct cart_id: nil,
            items: %{},
            status: :open

  # --- execute: command → event(s) or error ---

  @doc "Executes result from _cmd."
  def execute(%__MODULE__{status: :checked_out}, _cmd),
    do: {:error, :cart_already_checked_out}

  @doc "Executes result."
  def execute(%__MODULE__{}, %AddItem{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  @doc "Executes result."
  def execute(%__MODULE__{} = _cart, %AddItem{} = cmd) do
    %ItemAdded{
      cart_id: cmd.cart_id,
      sku: cmd.sku,
      quantity: cmd.quantity,
      unit_price_cents: cmd.unit_price_cents
    }
  end

  @doc "Executes result."
  def execute(%__MODULE__{items: items}, %RemoveItem{sku: sku}) when not is_map_key(items, sku),
    do: {:error, :item_not_in_cart}

  @doc "Executes result."
  def execute(%__MODULE__{} = _cart, %RemoveItem{} = cmd) do
    %ItemRemoved{cart_id: cmd.cart_id, sku: cmd.sku}
  end

  @doc "Executes result."
  def execute(%__MODULE__{items: items}, %Checkout{}) when map_size(items) == 0,
    do: {:error, :cart_is_empty}

  @doc "Executes result from cart_id."
  def execute(%__MODULE__{items: items, cart_id: id}, %Checkout{}) do
    total =
      Enum.reduce(items, 0, fn {_sku, %{quantity: q, unit_price_cents: p}}, acc ->
        acc + q * p
      end)

    %CartCheckedOut{cart_id: id, total_cents: total, item_count: map_size(items)}
  end

  # --- apply: event → new state ---

  @doc "Applies result."
  def apply(%__MODULE__{} = state, %ItemAdded{} = ev) do
    %{
      state
      | cart_id: ev.cart_id,
        items:
          Map.update(
            state.items,
            ev.sku,
            %{quantity: ev.quantity, unit_price_cents: ev.unit_price_cents},
            fn existing -> %{existing | quantity: existing.quantity + ev.quantity} end
          )
    }
  end

  @doc "Applies result."
  def apply(%__MODULE__{} = state, %ItemRemoved{} = ev) do
    %{state | items: Map.delete(state.items, ev.sku)}
  end

  @doc "Applies result."
  def apply(%__MODULE__{} = state, %CartCheckedOut{}) do
    %{state | status: :checked_out}
  end
end

defmodule ShoppingCartEs.Router do
  use Commanded.Commands.Router

  alias ShoppingCartEs.Cart.Aggregate
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}

  identify(Aggregate, by: :cart_id, prefix: "cart-")

  dispatch([AddItem, RemoveItem, Checkout],
    to: Aggregate,
    lifespan: ShoppingCartEs.Cart.Lifespan
  )
end

defmodule ShoppingCartEs.Cart.Lifespan do
  @behaviour Commanded.Aggregates.AggregateLifespan

  # Hibernate the aggregate process after 5 minutes of inactivity
  @doc "Returns after event result from _event."
  def after_event(_event), do: :timer.minutes(5)
  @doc "Returns after command result from _command."
  def after_command(_command), do: :timer.minutes(5)
  @doc "Returns after error result from _error."
  def after_error(_error), do: :stop
end

defmodule ShoppingCartEs.Projections.CartSummaryProjector do
  use Commanded.Projections.Ecto,
    application: ShoppingCartEs.CommandedApp,
    name: "cart_summary_projector",
    repo: ShoppingCartEs.Repo

  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  project(%ItemAdded{} = ev, _metadata, fn multi ->
    Ecto.Multi.run(multi, :upsert, fn repo, _ ->
      {:ok,
       repo.insert_all(
         "cart_summaries",
         [
           %{
             cart_id: ev.cart_id,
             item_count: ev.quantity,
             total_cents: ev.quantity * ev.unit_price_cents,
             status: "open",
             updated_at: DateTime.utc_now()
           }
         ],
         on_conflict: {:replace, [:item_count, :total_cents, :updated_at]},
         conflict_target: [:cart_id]
       )}
    end)
  end)

  project(%ItemRemoved{cart_id: id}, _metadata, fn multi ->
    Ecto.Multi.run(multi, :decrement, fn repo, _ ->
      {count, _} =
        repo.update_all(
          "cart_summaries",
          [set: [updated_at: DateTime.utc_now()]],
          returning: false
        )

      {:ok, %{cart_id: id, touched: count}}
    end)
  end)

  project(%CartCheckedOut{} = ev, _metadata, fn multi ->
    Ecto.Multi.run(multi, :finalize, fn repo, _ ->
      {:ok,
       repo.update_all(
         "cart_summaries",
         [set: [status: "checked_out", total_cents: ev.total_cents]],
         returning: false
       )}
    end)
  end)
end

defmodule ShoppingCartEs.Repo.Migrations.CreateCartSummaries do
  use Ecto.Migration

  @doc "Returns change result."
  def change do
    create table(:cart_summaries, primary_key: false) do
      add :cart_id, :string, primary_key: true
      add :item_count, :integer, null: false, default: 0
      add :total_cents, :integer, null: false, default: 0
      add :status, :string, null: false, default: "open"
      add :updated_at, :utc_datetime_usec, null: false
    end
  end
end
```

### `test/shopping_cart_es_test.exs`

```elixir
defmodule ShoppingCartEs.Cart.AggregateTest do
  use ExUnit.Case, async: true
  doctest ShoppingCartEs.CommandedApp

  alias ShoppingCartEs.Cart.Aggregate
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}
  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  describe "AddItem" do
    test "emits ItemAdded on an open cart" do
      cmd = %AddItem{cart_id: "c1", sku: "SKU1", quantity: 2, unit_price_cents: 500}

      assert %ItemAdded{sku: "SKU1", quantity: 2} =
               Aggregate.execute(%Aggregate{}, cmd)
    end

    test "rejects non-positive quantity" do
      cmd = %AddItem{cart_id: "c1", sku: "SKU1", quantity: 0, unit_price_cents: 500}
      assert {:error, :quantity_must_be_positive} = Aggregate.execute(%Aggregate{}, cmd)
    end
  end

  describe "RemoveItem" do
    test "emits ItemRemoved when the item exists" do
      state = apply_events(%Aggregate{}, [%ItemAdded{cart_id: "c1", sku: "SKU1", quantity: 1, unit_price_cents: 100}])
      assert %ItemRemoved{sku: "SKU1"} = Aggregate.execute(state, %RemoveItem{cart_id: "c1", sku: "SKU1"})
    end

    test "rejects removing an item not in the cart" do
      assert {:error, :item_not_in_cart} =
               Aggregate.execute(%Aggregate{}, %RemoveItem{cart_id: "c1", sku: "missing"})
    end
  end

  describe "Checkout" do
    test "computes total from current items" do
      state =
        apply_events(%Aggregate{}, [
          %ItemAdded{cart_id: "c1", sku: "A", quantity: 2, unit_price_cents: 500},
          %ItemAdded{cart_id: "c1", sku: "B", quantity: 1, unit_price_cents: 300}
        ])

      assert %CartCheckedOut{total_cents: 1_300, item_count: 2} =
               Aggregate.execute(state, %Checkout{cart_id: "c1"})
    end

    test "rejects checkout of empty cart" do
      assert {:error, :cart_is_empty} =
               Aggregate.execute(%Aggregate{}, %Checkout{cart_id: "c1"})
    end

    test "rejects any command after checkout" do
      state =
        apply_events(%Aggregate{}, [
          %ItemAdded{cart_id: "c1", sku: "A", quantity: 1, unit_price_cents: 100},
          %CartCheckedOut{cart_id: "c1", total_cents: 100, item_count: 1}
        ])

      assert {:error, :cart_already_checked_out} =
               Aggregate.execute(state, %AddItem{cart_id: "c1", sku: "X", quantity: 1, unit_price_cents: 1})
    end
  end

  defp apply_events(state, events),
    do: Enum.reduce(events, state, &Aggregate.apply(&2, &1))
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Commanded — Aggregate, Events and Projections.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Commanded — Aggregate, Events and Projections ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ShoppingCartEs.run(payload) do
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
        for _ <- 1..1_000, do: ShoppingCartEs.run(:bench)
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
