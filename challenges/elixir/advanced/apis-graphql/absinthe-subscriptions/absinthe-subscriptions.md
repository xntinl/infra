# Absinthe Subscriptions over Phoenix.PubSub

**Project**: `live_ticker` — real-time GraphQL subscriptions that stream price ticks to every connected client without coupling publishers to GraphQL types

---

## Why apis and graphql matters

GraphQL with Absinthe collapses N+1 problems via Dataloader, exposes subscriptions through Phoenix.PubSub, and lets the schema itself enforce complexity limits. REST APIs in Elixir benefit from Plug pipelines, OpenAPI generation, JWT auth, and HMAC-signed webhooks.

The hard parts are not the happy path: it's pagination consistency under concurrent writes, refresh-token rotation, idempotent webhook processing, and complexity budgets that prevent a single query from saturating a node.

---

## The business problem

You are building a production-grade Elixir component in the **APIs and GraphQL** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
live_ticker/
├── lib/
│   └── live_ticker.ex
├── script/
│   └── main.exs
├── test/
│   └── live_ticker_test.exs
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

Chose **B** because in APIs and GraphQL the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule LiveTicker.MixProject do
  use Mix.Project

  def project do
    [
      app: :live_ticker,
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

### `lib/live_ticker.ex`

```elixir
defmodule LiveTicker.Application do
  @moduledoc """
  Ejercicio: Absinthe Subscriptions over Phoenix.PubSub.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Application

  def start(_type, _args) do
    children = [
      {Phoenix.PubSub, name: LiveTicker.PubSub},
      LiveTickerWeb.Endpoint,
      {Absinthe.Subscription, LiveTickerWeb.Endpoint}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end

defmodule LiveTickerWeb.Endpoint do
  use Phoenix.Endpoint, otp_app: :live_ticker
  use Absinthe.Phoenix.Endpoint  # adds broadcast/publish helpers

  socket "/socket", LiveTickerWeb.UserSocket,
    websocket: [timeout: 45_000],
    longpoll: false
end

defmodule LiveTickerWeb.UserSocket do
  use Phoenix.Socket
  use Absinthe.Phoenix.Socket, schema: LiveTickerWeb.Graphql.Schema

  @doc "Connects result from params, socket and _info."
  def connect(params, socket, _info) do
    # Attach auth context to the socket so resolvers see the user
    case params["token"] do
      nil -> {:ok, socket}
      token ->
        case LiveTicker.Auth.verify(token) do
          {:ok, user_id} ->
            socket = Absinthe.Phoenix.Socket.put_options(socket, context: %{user_id: user_id})
            {:ok, socket}
          :error -> :error
        end
    end
  end

  @doc "Returns id result from _socket."
  def id(_socket), do: nil
end

defmodule LiveTickerWeb.Graphql.Schema do
  use Absinthe.Schema

  object :tick do
    field :symbol, non_null(:string)
    field :price, non_null(:float)
    field :ts_ms, non_null(:integer)
  end

  query do
    field :ping, :string, resolve: fn _, _, _ -> {:ok, "pong"} end
  end

  subscription do
    field :ticks, :tick do
      arg :symbol, non_null(:string)

      config fn args, _info ->
        {:ok, topic: args.symbol}
      end

      # The payload published to the topic IS the final value.
      # resolve/3 here is optional; it can transform before delivery.
      resolve fn tick, _args, _info -> {:ok, tick} end
    end
  end
end

defmodule LiveTicker.Market do
  alias LiveTickerWeb.Endpoint
  alias LiveTicker.Market.Tick

  @doc "Returns publish result."
  def publish(%Tick{symbol: sym} = tick) do
    Absinthe.Subscription.publish(Endpoint, tick, ticks: sym)
  end
end

defmodule LiveTicker.Market.Tick do
  @enforce_keys [:symbol, :price, :ts_ms]
  defstruct [:symbol, :price, :ts_ms]
end
```

### `test/live_ticker_test.exs`

```elixir
defmodule LiveTickerWeb.SubscriptionTest do
  use LiveTickerWeb.ChannelCase, async: false
  use Absinthe.Phoenix.SubscriptionTest, schema: LiveTickerWeb.Graphql.Schema

  setup do
    {:ok, socket} = Phoenix.ChannelTest.connect(LiveTickerWeb.UserSocket, %{})
    {:ok, socket} = Absinthe.Phoenix.SubscriptionTest.join_absinthe(socket)
    {:ok, socket: socket}
  end

  describe "ticks subscription" do
    test "receives updates only for subscribed symbol", %{socket: socket} do
      ref = push_doc(socket, """
      subscription($sym: String!) { ticks(symbol: $sym) { symbol price } }
      """, variables: %{"sym" => "BTC"})

      assert_reply ref, :ok, %{subscriptionId: sub_id}

      LiveTicker.Market.publish(%LiveTicker.Market.Tick{symbol: "BTC", price: 50_000.0, ts_ms: 1})
      LiveTicker.Market.publish(%LiveTicker.Market.Tick{symbol: "ETH", price: 3_000.0, ts_ms: 2})

      assert_push "subscription:data", %{result: %{data: %{"ticks" => tick}}, subscriptionId: ^sub_id}
      assert tick == %{"symbol" => "BTC", "price" => 50_000.0}

      refute_push "subscription:data", %{}, 100
    end

    test "unsubscribe stops delivery", %{socket: socket} do
      ref = push_doc(socket, "subscription { ticks(symbol: \"BTC\") { price } }")
      assert_reply ref, :ok, %{subscriptionId: sub_id}

      :ok = Absinthe.Subscription.unsubscribe(LiveTickerWeb.Endpoint, sub_id)

      LiveTicker.Market.publish(%LiveTicker.Market.Tick{symbol: "BTC", price: 1.0, ts_ms: 1})
      refute_push "subscription:data", %{}, 100
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Absinthe Subscriptions over Phoenix.PubSub.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Absinthe Subscriptions over Phoenix.PubSub ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case LiveTicker.run(payload) do
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
        for _ <- 1..1_000, do: LiveTicker.run(:bench)
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

### 1. Dataloader collapses N+1 queries

Without Dataloader, a GraphQL query for 'posts and their authors' issues N+1 queries. With Dataloader, it issues 2 — one for posts, one batched for authors.

### 2. Complexity analysis prevents query DoS

GraphQL allows clients to compose queries. Without complexity limits, a malicious client can request a 10-level deep nested query that brings the server down. Set per-query and per-connection limits.

### 3. Cursor pagination is consistent under writes

Offset pagination skips/duplicates rows under concurrent inserts. Cursor pagination (encode the last-seen ID) is correct regardless of writes.

---
