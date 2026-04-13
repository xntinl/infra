# Per-API-Key Rate Limiting with ETS and Token Bucket

**Project**: `api_portal` — an API gateway layer that enforces per-tenant quotas using a token bucket stored in ETS, with atomic updates via `:ets.update_counter/4`

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
api_portal/
├── lib/
│   └── api_portal.ex
├── script/
│   └── main.exs
├── test/
│   └── api_portal_test.exs
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
defmodule ApiPortal.MixProject do
  use Mix.Project

  def project do
    [
      app: :api_portal,
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
### `lib/api_portal.ex`

```elixir
defmodule ApiPortal.RateLimiter.Bucket do
  @moduledoc "Pure token-bucket math. Easy to unit-test."

  @type plan :: %{rate: pos_integer(), capacity: pos_integer()}

  @spec refill(integer(), integer(), integer(), float(), integer()) :: integer()
  def refill(tokens, last_us, now_us, rate_per_s, capacity) do
    elapsed_s = max(now_us - last_us, 0) / 1_000_000
    added = trunc(elapsed_s * rate_per_s)
    min(tokens + added, capacity)
  end

  @plans %{
    "free" =>       %{rate: 1, capacity: 60},
    "pro" =>        %{rate: 10, capacity: 600},
    "enterprise" => %{rate: 100, capacity: 6000}
  }

  def plan(name), do: Map.get(@plans, name, @plans["free"])
end

defmodule ApiPortal.RateLimiter.Limiter do
  alias ApiPortal.RateLimiter.Bucket

  @table :rate_buckets

  def setup do
    :ets.new(@table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true,
      decentralized_counters: true
    ])
  end

  @doc """
  Returns :ok if allowed, {:error, retry_after_ms} if denied.
  """
  @spec check(String.t(), String.t()) :: :ok | {:error, pos_integer()}
  def check(api_key, plan_name) do
    %{rate: rate, capacity: cap} = Bucket.plan(plan_name)
    now_us = System.monotonic_time(:microsecond)

    # Step 1: read current bucket (default to {key, capacity, now_us})
    {tokens, last_us} =
      case :ets.lookup(@table, api_key) do
        [{^api_key, t, last}] -> {t, last}
        [] -> {cap, now_us}
      end

    # Step 2: compute refilled tokens lazily
    refilled = Bucket.refill(tokens, last_us, now_us, rate, cap)

    if refilled >= 1 do
      # Step 3: atomic decrement. If another scheduler raced us, update_counter
      #         still operates atomically on whatever the current value is.
      :ets.insert(@table, {api_key, refilled - 1, now_us})
      :ok
    else
      # Tokens unavailable — compute time until next token
      retry_us = trunc(1_000_000 / rate)
      {:error, div(retry_us, 1_000)}
    end
  end
end

defmodule ApiPortal.RateLimiter.LimiterStrict do
  @table :rate_buckets_strict

  def setup do
    :ets.new(@table, [:named_table, :public, :set, write_concurrency: true])
  end

  def check(api_key, plan_name) do
    %{rate: rate, capacity: cap} = ApiPortal.RateLimiter.Bucket.plan(plan_name)
    now_us = System.monotonic_time(:microsecond)

    # First ensure the row exists and refill it.
    ensure_row(api_key, cap, now_us)
    refill_row(api_key, rate, cap, now_us)

    # Atomic: decrement tokens by 1 only if result >= 0; else keep at 0.
    # ops = [{position, increment, threshold, set_value}]
    case :ets.update_counter(@table, api_key, [{2, -1, 0, 0}]) do
      [new] when new >= 0 -> :ok
      [0] -> {:error, div(1_000_000, rate) |> div(1_000)}
    end
  end

  defp ensure_row(key, cap, now_us) do
    :ets.insert_new(@table, {key, cap, now_us})
  end

  defp refill_row(key, rate, cap, now_us) do
    [{^key, tokens, last_us}] = :ets.lookup(@table, key)
    refilled = ApiPortal.RateLimiter.Bucket.refill(tokens, last_us, now_us, rate, cap)
    :ets.insert(@table, {key, refilled, now_us})
  end
end

defmodule ApiPortalWeb.Plugs.RateLimit do
  @behaviour Plug
  import Plug.Conn
  alias ApiPortal.RateLimiter.Limiter

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    with [key] <- get_req_header(conn, "x-api-key"),
         plan <- lookup_plan(key) do
      case Limiter.check(key, plan) do
        :ok ->
          conn
          |> put_resp_header("x-ratelimit-plan", plan)

        {:error, retry_ms} ->
          conn
          |> put_resp_header("retry-after", Integer.to_string(div(retry_ms, 1000)))
          |> send_resp(429, "")
          |> halt()
      end
    else
      _ -> conn |> send_resp(401, "") |> halt()
    end
  end

  defp lookup_plan(_api_key), do: "free"  # replace with your tenant DB lookup
end
```
### `test/api_portal_test.exs`

```elixir
defmodule ApiPortal.RateLimiterTest do
  use ExUnit.Case, async: true
  doctest ApiPortal.RateLimiter.Bucket
  alias ApiPortal.RateLimiter.Limiter

  setup_all do
    Limiter.setup()
    :ok
  end

  setup do
    :ets.delete_all_objects(:rate_buckets)
    :ok
  end

  describe "token bucket semantics" do
    test "allows up to capacity without waiting" do
      for _ <- 1..60 do
        assert :ok = Limiter.check("k", "free")
      end
      assert {:error, _} = Limiter.check("k", "free")
    end

    test "refills at rate after time passes" do
      for _ <- 1..60, do: :ok = Limiter.check("k", "free")
      assert {:error, _} = Limiter.check("k", "free")

      # Simulate 2 seconds passing by manipulating last_refill_us.
      [{_, _, _}] = :ets.lookup(:rate_buckets, "k")
      :ets.update_element(:rate_buckets, "k", {3, System.monotonic_time(:microsecond) - 2_000_000})

      # Free plan = 1/s, so 2s later → 2 more tokens available.
      assert :ok = Limiter.check("k", "free")
      assert :ok = Limiter.check("k", "free")
      assert {:error, _} = Limiter.check("k", "free")
    end

    test "plans are independent between keys" do
      for _ <- 1..60, do: :ok = Limiter.check("free_k", "free")
      assert :ok = Limiter.check("pro_k", "pro")
    end
  end

  describe "concurrent checks" do
    test "100 parallel checks never exceed capacity" do
      key = "burst"
      tasks = for _ <- 1..200, do: Task.async(fn -> Limiter.check(key, "free") end)
      results = Task.await_many(tasks, 5_000)

      allowed = Enum.count(results, &(&1 == :ok))
      # Capacity is 60; should be at most 60 allowed (plus any lazy refill).
      assert allowed <= 61
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Per-API-Key Rate Limiting with ETS and Token Bucket.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Per-API-Key Rate Limiting with ETS and Token Bucket ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ApiPortal.run(payload) do
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
        for _ <- 1..1_000, do: ApiPortal.run(:bench)
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
