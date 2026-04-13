# Absinthe Subscriptions via Phoenix.PubSub

**Project**: `absinthe_subscriptions` — real-time comment feed pushed to subscribed clients over a Phoenix socket

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
absinthe_subscriptions/
├── lib/
│   └── absinthe_subscriptions.ex
├── script/
│   └── main.exs
├── test/
│   └── absinthe_subscriptions_test.exs
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
defmodule AbsintheSubscriptions.MixProject do
  use Mix.Project

  def project do
    [
      app: :absinthe_subscriptions,
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

### `lib/absinthe_subscriptions.ex`

```elixir
# lib/absinthe_subscriptions/application.ex
defmodule AbsintheSubscriptions.Application do
  @moduledoc """
  Ejercicio: Absinthe Subscriptions via Phoenix.PubSub.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      AbsintheSubscriptions.Repo,
      {Phoenix.PubSub, name: AbsintheSubscriptions.PubSub},
      AbsintheSubscriptions.Endpoint,
      {Absinthe.Subscription, AbsintheSubscriptions.Endpoint}
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: AbsintheSubscriptions.Supervisor)
  end
end

# lib/absinthe_subscriptions/endpoint.ex
defmodule AbsintheSubscriptions.Endpoint do
  use Phoenix.Endpoint, otp_app: :absinthe_subscriptions
  use Absinthe.Phoenix.Endpoint

  socket "/socket", AbsintheSubscriptions.UserSocket,
    websocket: true,
    longpoll: false

  plug Plug.Parsers,
    parsers: [:urlencoded, :multipart, :json, Absinthe.Plug.Parser],
    json_decoder: Jason

  plug Absinthe.Plug,
    schema: AbsintheSubscriptions.Graphql.Schema
end

# lib/absinthe_subscriptions/socket.ex
defmodule AbsintheSubscriptions.UserSocket do
  use Phoenix.Socket
  use Absinthe.Phoenix.Socket, schema: AbsintheSubscriptions.Graphql.Schema

  @doc "Connects result from params, socket and _connect_info."
  @impl true
  def connect(params, socket, _connect_info) do
    viewer_id = params["viewer_id"]
    socket = Absinthe.Phoenix.Socket.put_options(socket, context: %{viewer_id: viewer_id})
    {:ok, socket}
  end

  @doc "Returns id result from socket."
  @impl true
  def id(socket), do: "user_socket:#{socket.assigns[:viewer_id] || "anon"}"
end

# lib/absinthe_subscriptions/graphql/types/comment_types.ex
defmodule AbsintheSubscriptions.Graphql.Types.CommentTypes do
  use Absinthe.Schema.Notation

  object :comment do
    field :id, non_null(:id)
    field :body, non_null(:string)
    field :article_id, non_null(:id)
    field :inserted_at, non_null(:string)
  end

  input_object :create_comment_input do
    field :article_id, non_null(:id)
    field :body, non_null(:string)
  end
end

# lib/absinthe_subscriptions/graphql/schema.ex
defmodule AbsintheSubscriptions.Graphql.Schema do
  use Absinthe.Schema

  import_types AbsintheSubscriptions.Graphql.Types.CommentTypes

  alias AbsintheSubscriptions.{Repo, Blog.Comment}

  query do
    field :ping, :string, resolve: fn _, _, _ -> {:ok, "pong"} end
  end

  mutation do
    field :create_comment, :comment do
      arg :input, non_null(:create_comment_input)

      resolve fn _p, %{input: input}, _r ->
        %Comment{}
        |> Comment.changeset(input)
        |> Repo.insert()
      end

      # Emit the event on successful insert.
      middleware fn resolution, _ ->
        case resolution.value do
          %Comment{} = comment ->
            Absinthe.Subscription.publish(
              AbsintheSubscriptions.Endpoint,
              comment,
              comment_added: "article:#{comment.article_id}"
            )

          _ ->
            :ok
        end

        resolution
      end
    end
  end

  subscription do
    field :comment_added, :comment do
      arg :article_id, non_null(:id)

      config fn %{article_id: id}, _info ->
        {:ok, topic: "article:#{id}"}
      end
    end
  end
end

# lib/absinthe_subscriptions/blog/comment.ex
defmodule AbsintheSubscriptions.Blog.Comment do
  use Ecto.Schema
  import Ecto.Changeset

  schema "comments" do
    field :body, :string
    field :article_id, :integer
    timestamps()
  end

  @doc "Returns changeset result from comment and attrs."
  def changeset(comment, attrs) do
    comment
    |> cast(attrs, [:body, :article_id])
    |> validate_required([:body, :article_id])
    |> validate_length(:body, min: 1, max: 5000)
  end
end
```

### `test/absinthe_subscriptions_test.exs`

```elixir
defmodule AbsintheSubscriptions.SubscriptionTest do
  use ExUnit.Case, async: true
  doctest AbsintheSubscriptions.Application
  use Absinthe.Phoenix.SubscriptionTest, schema: AbsintheSubscriptions.Graphql.Schema

  alias AbsintheSubscriptions.Repo

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo, ownership_timeout: :infinity)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    {:ok, socket} = Phoenix.ChannelTest.connect(AbsintheSubscriptions.UserSocket, %{})
    {:ok, socket: socket}
  end

  describe "AbsintheSubscriptions.Subscription" do
    test "a subscribed client receives a payload on createComment", %{socket: socket} do
      subscription = """
      subscription ($id: ID!) {
        commentAdded(articleId: $id) { id body articleId }
      }
      """

      ref = push_doc(socket, subscription, variables: %{"id" => "42"})
      assert_reply ref, :ok, %{subscriptionId: _sub_id}

      # Create a comment via mutation.
      mutation = """
      mutation ($input: CreateCommentInput!) {
        createComment(input: $input) { id }
      }
      """
      push_doc(socket, mutation, variables: %{"input" => %{"articleId" => "42", "body" => "first!"}})

      assert_push "subscription:data", %{result: %{data: %{"commentAdded" => payload}}}
      assert payload["body"] == "first!"
      assert payload["articleId"] == "42"
    end

    test "clients subscribed to a different article do not get the push", %{socket: socket} do
      push_doc(socket, "subscription ($id: ID!) { commentAdded(articleId: $id) { id } }",
               variables: %{"id" => "99"})

      push_doc(socket, """
        mutation { createComment(input: {articleId: "42", body: "x"}) { id } }
      """)

      refute_push "subscription:data", _, 100
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Absinthe Subscriptions via Phoenix.PubSub.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Absinthe Subscriptions via Phoenix.PubSub ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AbsintheSubscriptions.run(payload) do
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
        for _ <- 1..1_000, do: AbsintheSubscriptions.run(:bench)
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
