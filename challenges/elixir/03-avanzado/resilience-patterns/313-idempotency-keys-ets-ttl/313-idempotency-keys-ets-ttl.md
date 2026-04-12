# Idempotency Keys with ETS and TTL

**Project**: `charge_idempotency` — an idempotency layer for a payments API that dedups retried requests within a TTL window using ETS and a lightweight sweeper.

## Project context

Your payments endpoint accepts `POST /charges` with an `Idempotency-Key` header. Clients retry on network errors — mobile apps especially. Without dedup, a retried charge double-bills the customer.

The rule: if an idempotency key has been seen within the last 24 hours, return the cached response for that key instead of processing again. Beyond 24 hours the key expires and the next request is treated as fresh.

Stripe's model. Production-grade. This exercise implements it with ETS for storage, a sweeper process for TTL cleanup, and an `ensure_once/3` helper that handles the "first request" / "duplicate request" distinction atomically.

```
charge_idempotency/
├── lib/
│   └── charge_idempotency/
│       ├── application.ex
│       ├── store.ex                # ETS-backed store with TTL
│       └── service.ex              # example: ensure_once on a charge op
├── test/
│   └── charge_idempotency/
│       └── store_test.exs
├── bench/
│   └── store_bench.exs
└── mix.exs
```

## Why ETS and not Redis

Redis is a valid choice for cross-node dedup. For single-node or session-affine workloads, ETS gives you 100x lower latency and no network dependency. If your load balancer pins clients to nodes (Phoenix Presence, sticky LB), ETS covers you; if not, use Redis/Postgres.

## Why separate record for in-flight vs. completed

A naive implementation inserts only on success. Problem: two concurrent identical requests both see "no key" and both process. Correct: insert a sentinel `:in_flight` atomically on first arrival; duplicates see `:in_flight` and block/wait; on completion replace with the response.

## Core concepts

### 1. Atomic "first arrival" via `:ets.insert_new/2`
```
:ets.insert_new(:ik, {key, :in_flight, expires_at})
```
Returns `true` for the winner, `false` for the loser. Unlike `:ets.insert/2` (which overwrites), `insert_new` only inserts if absent.

### 2. TTL via `expires_at` column + sweeper
ETS has no native TTL. Each entry stores `expires_at`. A sweeper GenServer runs every N seconds and `:ets.select_delete/2`s all entries where `expires_at < now`.

### 3. Waiters for in-flight dedup
If a duplicate arrives while the original is still processing, we don't reject — we wait. Simplest implementation: duplicate polls until state changes (every 10ms, bounded by timeout). More sophisticated: subscribe and receive.

## Design decisions

- **Option A — Store only the response**: duplicates process twice if they arrive simultaneously.
- **Option B — Store `:in_flight` then response**: duplicates correctly wait.
→ Chose **B**. Simultaneous duplicates are the whole point of idempotency.

- **Option A — Sweep on every read (lazy)**: expired entries still get returned briefly; sweep amortizes.
- **Option B — Dedicated sweeper process**: reads are fast; cleanup is centralized.
→ Chose **B** + explicit expiration check on read. Defense in depth.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ChargeIdempotency.MixProject do
  use Mix.Project
  def project, do: [app: :charge_idempotency, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  def application, do: [mod: {ChargeIdempotency.Application, []}, extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Application

```elixir
defmodule ChargeIdempotency.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {ChargeIdempotency.Store, ttl_ms: 24 * 60 * 60 * 1_000, sweep_ms: 60_000}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Store (`lib/charge_idempotency/store.ex`)

```elixir
defmodule ChargeIdempotency.Store do
  use GenServer

  @table :charge_idempotency

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc """
  Executes `fun` exactly once for the given key within the TTL.
  Duplicates within TTL return the cached result without executing `fun`.
  """
  def ensure_once(key, timeout_ms, fun) when is_function(fun, 0) do
    now = now_ms()
    expires_at = now + ttl()

    case :ets.insert_new(@table, {key, :in_flight, expires_at}) do
      true ->
        execute_and_store(key, fun, expires_at)

      false ->
        wait_for_completion(key, timeout_ms, now + timeout_ms)
    end
  end

  defp execute_and_store(key, fun, expires_at) do
    try do
      result = fun.()
      :ets.insert(@table, {key, {:done, result}, expires_at})
      {:ok, result}
    rescue
      e ->
        :ets.delete(@table, key)
        reraise e, __STACKTRACE__
    end
  end

  defp wait_for_completion(key, _timeout_ms, deadline) do
    case :ets.lookup(@table, key) do
      [{^key, {:done, result}, expires_at}] ->
        if expires_at > now_ms(), do: {:ok, result}, else: {:error, :expired}

      [{^key, :in_flight, _}] ->
        if now_ms() >= deadline do
          {:error, :timeout}
        else
          Process.sleep(10)
          wait_for_completion(key, nil, deadline)
        end

      [] ->
        {:error, :evicted}
    end
  end

  # ---------- lifecycle ----------

  @impl true
  def init(opts) do
    ttl = Keyword.fetch!(opts, :ttl_ms)
    sweep = Keyword.fetch!(opts, :sweep_ms)

    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true, write_concurrency: true])
    :persistent_term.put({__MODULE__, :ttl_ms}, ttl)
    Process.send_after(self(), :sweep, sweep)

    {:ok, %{ttl_ms: ttl, sweep_ms: sweep}}
  end

  @impl true
  def handle_info(:sweep, state) do
    now = now_ms()

    # match_spec: delete entries where expires_at < now
    spec = [{{:_, :_, :"$1"}, [{:<, :"$1", now}], [true]}]
    :ets.select_delete(@table, spec)

    Process.send_after(self(), :sweep, state.sweep_ms)
    {:noreply, state}
  end

  defp now_ms, do: System.monotonic_time(:millisecond)
  defp ttl, do: :persistent_term.get({__MODULE__, :ttl_ms})
end
```

### Step 3: Example service (`lib/charge_idempotency/service.ex`)

```elixir
defmodule ChargeIdempotency.Service do
  alias ChargeIdempotency.Store

  def charge(idempotency_key, amount) do
    Store.ensure_once(idempotency_key, 5_000, fn ->
      do_charge(amount)
    end)
  end

  defp do_charge(amount) do
    %{charged_at: System.system_time(:millisecond), amount: amount, status: :ok}
  end
end
```

## Why this works

- **`insert_new/2` is atomic** — the BEAM guarantees exactly one process sees `true`. The loser sees `false` and waits. No TOCTOU race.
- **`:in_flight` sentinel + waiters** — concurrent duplicates don't fall through to the work function; they observe `:in_flight` and poll until `:done`.
- **Crash safety via `rescue` + delete** — if `fun.()` raises, we delete the sentinel so the next retry can try again. Without this, a single transient crash would permanently "dedup" the key.
- **Sweeper is independent** — the read path doesn't depend on the sweeper running on time. Even a minutes-late sweep is correct because `ensure_once` checks `expires_at` itself.
- **Match spec for bulk delete** — `:ets.select_delete/2` operates inside ETS without copying the table to a heap list. Scales to millions of entries.

## Tests

```elixir
defmodule ChargeIdempotency.StoreTest do
  use ExUnit.Case, async: false
  alias ChargeIdempotency.Store

  setup do
    :ets.delete_all_objects(:charge_idempotency)
    :ok
  end

  describe "first call" do
    test "executes the function and returns the result" do
      assert {:ok, 42} = Store.ensure_once("k1", 1_000, fn -> 42 end)
    end
  end

  describe "duplicate call within TTL" do
    test "returns the cached result without executing" do
      {:ok, _} = Agent.start_link(fn -> 0 end, name: :side_effect)

      fun = fn ->
        Agent.update(:side_effect, &(&1 + 1))
        :computed
      end

      assert {:ok, :computed} = Store.ensure_once("k_dup", 1_000, fun)
      assert {:ok, :computed} = Store.ensure_once("k_dup", 1_000, fun)
      assert {:ok, :computed} = Store.ensure_once("k_dup", 1_000, fun)

      assert 1 == Agent.get(:side_effect, & &1)
    end
  end

  describe "concurrent duplicates" do
    test "only one execution across concurrent callers" do
      {:ok, _} = Agent.start_link(fn -> 0 end, name: :concurrent_side)

      slow_fun = fn ->
        Agent.update(:concurrent_side, &(&1 + 1))
        Process.sleep(50)
        :done
      end

      tasks =
        for _ <- 1..20 do
          Task.async(fn -> Store.ensure_once("k_concurrent", 2_000, slow_fun) end)
        end

      results = Task.await_many(tasks, 5_000)

      assert Enum.all?(results, &match?({:ok, :done}, &1))
      assert 1 == Agent.get(:concurrent_side, & &1)
    end
  end

  describe "crash safety" do
    test "exception clears the sentinel so next call retries" do
      attempts = :counters.new(1, [])

      fun = fn ->
        n = :counters.add(attempts, 1, 1) |> then(fn _ -> :counters.get(attempts, 1) end)
        if n == 1, do: raise("boom"), else: :recovered
      end

      assert_raise RuntimeError, fn -> Store.ensure_once("k_crash", 1_000, fun) end
      assert {:ok, :recovered} = Store.ensure_once("k_crash", 1_000, fun)
    end
  end
end
```

## Benchmark

```elixir
# bench/store_bench.exs
{:ok, _} = Application.ensure_all_started(:charge_idempotency)

Benchee.run(
  %{
    "first call (insert_new)" => fn ->
      key = "bench_#{:rand.uniform(1_000_000)}"
      ChargeIdempotency.Store.ensure_once(key, 1_000, fn -> :ok end)
    end,
    "cached call (lookup)" => fn ->
      ChargeIdempotency.Store.ensure_once("bench_fixed", 1_000, fn -> :ok end)
    end
  },
  parallel: 8,
  time: 5
)
```

Expected: first call ~5µs, cached call ~1µs. If cached call > 10µs you are hitting the sweeper or spending time in GenServer.

## Trade-offs and production gotchas

**1. TTL collision with retry windows** — TTL must exceed your maximum client retry window. Stripe uses 24h because mobile clients may retry for hours.

**2. Response size in memory** — ETS holds full responses. A 1KB response × 1M keys = 1GB. For large responses store a hash/reference and put the body in S3/Redis.

**3. Waiter timeout interaction** — if `ensure_once(..., 5_000, ...)` is called while the original is taking 10s, waiter returns `{:error, :timeout}` even though the original will eventually succeed. Client must retry (safely — still idempotent!).

**4. No cross-node consistency** — two nodes running independent ETS tables do not share keys. For multi-node, use a shared store.

**5. Sentinel leak on VM crash** — if the BEAM dies, the ETS table is destroyed. No leak. If the *process* dies but the BEAM is alive, the table also dies (owned by the GenServer). Supervisor restart creates a fresh empty table. This is acceptable: duplicates during a rare restart are the least of your problems.

**6. When NOT to use this** — for cross-region or multi-tenant SaaS with heavy traffic, Redis or a DB column with a unique index is more appropriate.

## Reflection

A client sends 3 concurrent identical charge requests. The `fun.()` executes once and returns `:ok` in 100ms. What does each caller observe, in what order, and what is the latency each sees?

## Resources

- [Stripe API idempotency](https://stripe.com/docs/api/idempotent_requests)
- [`:ets.select_delete/2` — Erlang docs](https://www.erlang.org/doc/man/ets.html#select_delete-2)
- [`:ets.insert_new/2` — Erlang docs](https://www.erlang.org/doc/man/ets.html#insert_new-2)
- [Exactly-once delivery — Confluent blog](https://www.confluent.io/blog/exactly-once-semantics-are-possible-heres-how-apache-kafka-does-it/)
