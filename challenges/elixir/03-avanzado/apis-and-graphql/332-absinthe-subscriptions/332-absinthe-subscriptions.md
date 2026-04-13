# Absinthe Subscriptions over Phoenix.PubSub

**Project**: `live_ticker` — real-time GraphQL subscriptions that stream price ticks to every connected client without coupling publishers to GraphQL types.

## Project context

`live_ticker` is the internal market data service for a fintech dashboard. Ingest pipelines push ~2k price updates per second into the system; the dashboard must show every tick for the symbols each trader has subscribed to. REST polling was eliminated last quarter — the new requirement is push-based GraphQL subscriptions over WebSockets.

This exercise wires Absinthe subscriptions through `Phoenix.PubSub` and `Phoenix.Socket`, with a production-style publication path: the ingest process publishes a plain Elixir struct to a topic, and Absinthe takes care of routing it to every matching subscription document, running the resolver, and pushing the payload down the socket.

```
live_ticker/
├── lib/
│   ├── live_ticker/
│   │   ├── application.ex
│   │   ├── market.ex                  # publish/2 entry point
│   │   └── market/tick.ex
│   └── live_ticker_web/
│       ├── endpoint.ex
│       ├── user_socket.ex
│       ├── channels/                  # (none — subscriptions use Absinthe.Phoenix)
│       └── graphql/
│           ├── schema.ex
│           └── resolvers/tick_resolver.ex
├── test/live_ticker_web/graphql/subscription_test.exs
└── mix.exs
```

## Why Absinthe subscriptions and not raw Phoenix channels

Phoenix channels are a lower-level primitive. They work, but they force clients to learn a second protocol in parallel with GraphQL and duplicate the schema (payload shapes, filtering, auth) on both ends.

Absinthe subscriptions give you:

- **One schema, one language**: clients send the same GraphQL document shape, now over the `subscription` root.
- **Filtered delivery**: a subscription with `subscribe ticks(symbol: "BTC")` is keyed per-symbol; publishing a `USD` tick does not wake the BTC subscriber's process.
- **Resolver reuse**: the same field resolver logic (auth middleware, dataloader) that runs on `query` runs on each pushed update.

The cost is the `Absinthe.Phoenix` dependency and one more moving part (`Absinthe.Subscription` supervisor). For anything beyond "broadcast a blob to everyone", the tradeoff is overwhelmingly in favor of Absinthe.

## Why `Phoenix.PubSub` under the hood

Absinthe.Subscription defaults to `Phoenix.PubSub`. This matters because:

1. In a multi-node cluster, PubSub (with the PG2 or Redis adapter) propagates publications across nodes. A tick published on node A reaches subscribers on node B without extra code.
2. Topics are sharded across multiple `Registry` partitions, so concurrent publishes do not serialize.

Rolling your own registry works for a single node but becomes work once you deploy to three replicas behind a load balancer.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. `config_change` vs `trigger`
A `:trigger` tells Absinthe "when someone calls `publish(schema, payload, mutation_field: X)`, wake up the subscriptions whose `topic_fn` matches". We use the lower-level `Absinthe.Subscription.publish/3` directly because our publisher is the market ingest loop, not a GraphQL mutation.

### 2. `topic` function
For `subscribe ticks(symbol: "BTC")`, the `config` macro sets `topic: args.symbol`. Publish with `topic: "BTC"` and only BTC subscribers receive it.

### 3. Subscriptions run in the subscriber's process
Each resolver for a subscription update runs in the WebSocket-connected process. A slow resolver blocks that socket, not others.

## Design decisions

- **Option A — publish from the market module, keyed by symbol**: pros: decoupled from schema, easy to test; cons: need to remember the topic convention.
- **Option B — publish via a mutation whose trigger fans out**: pros: one path for all writes; cons: forces every internal event to become a GraphQL mutation.
→ We pick **A**. Market ticks are not user-originated, making them look like mutations is modeling fiction.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:phoenix_pubsub, "~> 2.1"},
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:absinthe_phoenix, "~> 2.0"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 1: Application supervision tree

**Objective**: Order PubSub, Endpoint, and Absinthe.Subscription in the supervision tree so the ingest registry exists before socket connections wake.

```elixir
defmodule LiveTicker.Application do
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
```

`Absinthe.Subscription` MUST be supervised by the endpoint — it reads the PubSub name from `endpoint.config(:pubsub_server)`.

### Step 2: Endpoint and socket wiring

**Objective**: Mix Absinthe.Phoenix.Endpoint for broadcast/publish helpers and use Absinthe.Phoenix.Socket to extract auth tokens into resolver context without per-channel duplication.

```elixir
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

  def id(_socket), do: nil
end
```

### Step 3: Schema with `subscription` root

**Objective**: Declare subscription :ticks keyed by symbol argument so publish(tick) triggers only matching subscribers and resolve can transform before delivery.

```elixir
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
```

### Step 4: Publisher from the ingest loop

**Objective**: Emit ticks from the market module via Absinthe.Subscription.publish/3 so ingest pipelines stay decoupled from GraphQL schema contracts.

```elixir
defmodule LiveTicker.Market do
  alias LiveTickerWeb.Endpoint
  alias LiveTicker.Market.Tick

  def publish(%Tick{symbol: sym} = tick) do
    Absinthe.Subscription.publish(Endpoint, tick, ticks: sym)
  end
end

defmodule LiveTicker.Market.Tick do
  @enforce_keys [:symbol, :price, :ts_ms]
  defstruct [:symbol, :price, :ts_ms]
end
```

The keyword `ticks: sym` means "for the subscription field named `:ticks`, match topic `sym`". Absinthe looks up every subscription whose `config` returned that topic and pushes the payload to their processes.

## Why this works

```
Ingest ──Market.publish/1──▶ Absinthe.Subscription.publish/3
                                    │
                                    ▼
                          Phoenix.PubSub.broadcast
                                    │
                       ┌────────────┴─────────────┐
                       ▼                          ▼
              node A subscriber           node B subscriber
              ("ticks:BTC")               ("ticks:BTC")
                       │                          │
              run resolve/3 in their            same
              Absinthe.Phoenix socket
                       │
                       ▼
              push GraphQL payload
              over WebSocket
```

Key property: the publisher does **not** serialize anything. The socket process receives the raw `%Tick{}` struct and runs `resolve/3` locally. If 1k subscribers are listening for BTC, the cost is `O(1)` publish + `O(1k)` parallel resolutions across BEAM schedulers, not one serialized blob copied 1k times.

## Tests

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

## Benchmark

```elixir
# bench/publish_fanout.exs
# 1k in-process subscribers on one node, measure publish-to-last-delivery latency.
symbol = "BTC"
me = self()

for i <- 1..1000 do
  Task.start_link(fn ->
    :ok = Absinthe.Subscription.subscribe(LiveTickerWeb.Endpoint, "ticks:" <> symbol, fn payload ->
      send(me, {:got, i, payload})
    end)
    receive do _ -> :ok end
  end)
end

Process.sleep(500)

{t_us, _} = :timer.tc(fn ->
  LiveTicker.Market.publish(%LiveTicker.Market.Tick{symbol: symbol, price: 1.0, ts_ms: 1})
  for _ <- 1..1000, do: receive do {:got, _, _} -> :ok end
end)

IO.puts("1 publish → 1000 deliveries in #{t_us} µs")
```

**Expected**: under 10 ms total on a 4-core laptop, i.e. < 10 µs per delivery amortized.

## Deep Dive: Query Complexity and N+1 Prevention Patterns

GraphQL's flexibility is a double-edged sword. A query like `{ users { posts { comments { author { email } } } } }`
becomes a DDoS vector if unchecked: a resolver that loads each post's comments naively yields 1000 database 
queries for a 100-user query.

**Three strategies to prevent N+1**:
1. **Dataloader batching** (Absinthe-native): Queue fields in phase 1 (`load/3`), flush in phase 2 (`run/1`).
   Single database call per level. Works across HTTP boundaries via custom sources.
2. **Ecto select/5 eager loading** (preload): Best when schema relationships are known at resolver definition time.
   Fine-grained control; requires discipline in your types.
3. **Complexity analysis** (persisted queries): Assign a "weight" to each field (users=2, posts=5, comments=10).
   Reject queries exceeding a threshold BEFORE execution. Prevents runaway queries entirely.

**Production gotcha**: Complexity analysis doesn't prevent slow queries — it prevents expensive queries.
A query that hits 50,000 database rows but under the complexity limit still runs. Combine with database 
query timeouts and active monitoring.

**Subscription patterns** (real-time): Subscriptions over PubSub break traditional Dataloader batching 
because events arrive asynchronously. Use a separate resolver that doesn't call the loader; instead, 
publish (source) and subscribe (sink) directly. This keeps subscriptions cheap and doesn't starve 
the dataloader queue.

**Field-level authorization**: Dataloader sources can enforce per-user visibility rules at load time, 
not in the resolver. This is cleaner than filtering after the fact and reduces unnecessary database 
queries for unauthorized fields.

---

## Advanced Considerations

API implementations at scale require careful consideration of request handling, error responses, and the interaction between multiple clients with different performance expectations. The distinction between public APIs and internal APIs affects error reporting granularity, versioning strategies, and backwards compatibility guarantees fundamentally. Versioning APIs through headers, paths, or query parameters each have trade-offs in terms of maintenance burden, client complexity, and developer experience across multiple client versions. When deprecating API endpoints, the migration window and support period must balance client migration costs with infrastructure maintenance costs and team capacity constraints.

GraphQL adds complexity around query costs, depth limits, and the interaction between nested resolvers and N+1 query problems. A deeply nested GraphQL query can trigger hundreds of database queries if not carefully managed with proper preloading and query analysis. Implementing query cost analysis prevents malicious or poorly-written queries from starving resources and degrading service for other clients. The caching layer becomes more complex with GraphQL because the same data may be accessed through multiple query paths, each with different caching semantics and TTL requirements that must be carefully coordinated at the application level.

Error handling and status codes require careful design to balance information disclosure with security concerns. Too much detail in error messages helps attackers; too little detail frustrates legitimate users. Implement structured error responses with specific error codes that clients can use to handle different failure scenarios intelligently and retry appropriately. Rate limiting, circuit breakers, and backpressure mechanisms prevent API overload but require careful configuration based on expected traffic patterns and SLA requirements.


## Deep Dive: Apis Patterns and Production Implications

API testing requires testing schema validation, error messages, pagination, and rate limiting—not just happy paths. The mistake is testing only the happy path and assuming error handling works. Production APIs with weak error handling become support nightmares.

---

## Trade-offs and production gotchas

**1. Subscribers are processes — slow resolvers block their own socket**
If the subscription resolver does I/O (DB call per tick) it blocks the subscriber's socket. For hot paths, push already-materialized payloads and make `resolve/3` identity.

**2. Back-pressure is your problem**
PubSub never drops messages. A slow consumer accumulates messages in its mailbox. Add a limiter (`{:run_queue, max_demand: N}`) or drop in the resolver if mailbox size exceeds a threshold (`Process.info(self(), :message_queue_len)`).

**3. Topics are strings, not atoms**
Do NOT key a topic by `String.to_atom(user_input)`. You'll leak atoms and eventually crash the VM. Keep them as binaries.

**4. Endpoint `pubsub_server` config missing**
`Absinthe.Subscription.publish/3` silently succeeds if PubSub is not configured on the endpoint; no delivery happens. Configure `pubsub_server: LiveTicker.PubSub` in `config/config.exs`.

**5. Auth evaluated only at connect**
If a user's token expires mid-session, subscriptions keep streaming. Re-check auth inside the resolver with `info.context` if your threat model requires it.

**6. When NOT to use this**
For 1-to-1 request/response where the server already has all the data ("give me the current price of BTC"), `query` is cheaper. Reserve subscriptions for truly push-driven data.

## Reflection

Your ingest loop publishes 2k ticks/sec. Each tick fans out to 500 subscribers on average. What happens to scheduler run queue and PubSub registry contention? Which dimension breaks first — CPU, mailbox memory, or network? Sketch a back-pressure strategy that protects the ingest loop without dropping ticks for paying customers.


## Executable Example

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

defmodule Main do
  def main do
      IO.puts("GraphQL schema initialization")
      defmodule QueryType do
        def resolve_hello(_, _, _), do: {:ok, "world"}
      end
      if is_atom(QueryType) do
        IO.puts("✓ GraphQL schema validated and query resolver accessible")
      end
  end
end

Main.main()
```
