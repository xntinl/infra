# Fallback Chains with `with` and Tagged Tuples

**Project**: `profile_resolver` — resolves a user profile from a chain of sources (cache → primary DB → read replica → upstream API → stale cache) using `with` for linear flow and tagged tuples for source attribution.

## Project context

The profile service backs a login flow. On every sign-in you need the user's plan, feature flags, and display name. The cache is fast but may miss; the primary DB is authoritative but occasionally degraded; the replica is eventually consistent; the upstream identity API is slow but authoritative on failover; the stale cache is a last-resort that returns data up to 1 hour old with a warning flag.

Naively: nested `case` or `try/rescue`. Pyramid of doom. Errors get swallowed. Source attribution is lost.

The idiomatic solution: represent each source as a function returning `{:ok, value, source}` or `{:error, reason}`. Compose with `with`, falling through the `else` arm to the next source. The caller gets the value *and* knows which source produced it, which matters for observability and for deciding whether to refresh the cache.

```
profile_resolver/
├── lib/
│   └── profile_resolver/
│       ├── resolver.ex             # the chain
│       ├── sources/
│       │   ├── cache.ex
│       │   ├── primary.ex
│       │   ├── replica.ex
│       │   ├── upstream.ex
│       │   └── stale_cache.ex
│       └── profile.ex
├── test/
│   └── profile_resolver/
│       └── resolver_test.exs
└── mix.exs
```

## Why `with` and not nested case

Nested `case` pyramids grow one level per source. Five sources become five nested cases, 25 lines of indentation. `with` is linear:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
with {:error, :miss} <- Cache.get(id),
     {:error, _} <- Primary.get(id),
     {:error, _} <- Replica.get(id),
     {:error, _} <- Upstream.get(id) do
  StaleCache.get(id)
end
```

Each clause is one source. Reading order mirrors execution order. Maintainers can insert a new source with one additional line.

## Why tagged tuples and not exceptions

Exceptions for control flow is expensive (stack unwind) and makes errors invisible in the signature. Tagged tuples `{:ok, _}` / `{:error, _}` make success/failure an explicit part of the return contract. `with` pattern-matches on them natively.

Additionally, tagging successes by source (`{:ok, profile, :primary}`) lets the caller decide: "we got this from `:stale_cache`, log a warning and enqueue a refresh job".

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
### 1. Uniform source signature
Every source implements `get(id) :: {:ok, profile, source_tag} | {:error, reason}`. The tag is baked into the success tuple; the reason is specific enough to distinguish retriable from non-retriable.

### 2. Chain as inverted pattern
```
with {:error, _} <- first_source() do
  with {:error, _} <- second_source() do
    ...
  end
end
```
Odd twist: `with` usually unwraps successes. Here we unwrap *errors* and stop the chain on first success — the fallback pattern.

### 3. Short-circuit to stale
The final clause (outside the `with`) returns the stale result if every upstream failed. This is a policy decision: you'd rather serve slightly-old data than no data.

## Design decisions

- **Option A — Chain via `Enum.reduce_while`**: functional, but obscures which source produced the hit.
- **Option B — Explicit `with`**: verbose but readable and preserves source tags.
→ Chose **B**. A resolver is read 100x for every time it's written; maintainability dominates.

- **Option A — Parallel fan-out to all sources**: fastest first wins (hedged). Saves tail latency at the cost of upstream load.
- **Option B — Sequential**: only call the next source if the previous failed. Saves load.
→ Chose **B** (this exercise). Hedged requests are a separate pattern — implemented elsewhere in this set. Sequential is the correct default for most caches.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ProfileResolver.MixProject do
  use Mix.Project
  def project, do: [app: :profile_resolver, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```

### Step 1: Profile struct (`lib/profile_resolver/profile.ex`)

**Objective**: Tag profiles with source provenance and staleness so callers route to refresh jobs or warn users based on data freshness without re-querying ancestry.

```elixir
defmodule ProfileResolver.Profile do
  defstruct [:id, :name, :plan, :flags, source: nil, stale?: false]

  @type t :: %__MODULE__{
          id: String.t(),
          name: String.t(),
          plan: atom(),
          flags: map(),
          source: atom() | nil,
          stale?: boolean()
        }
end
```

### Step 2: Source behaviour and implementations

**Objective**: Define uniform @callback get/1 signature across all sources so `with` can chain them linearly without nested case pyramids or source-specific dispatch logic.

```elixir
defmodule ProfileResolver.Sources.Source do
  @callback get(String.t()) ::
              {:ok, ProfileResolver.Profile.t()} | {:error, term()}
end
```

```elixir
defmodule ProfileResolver.Sources.Cache do
  @behaviour ProfileResolver.Sources.Source
  alias ProfileResolver.Profile

  def get(id) do
    case :persistent_term.get({__MODULE__, id}, :miss) do
      :miss -> {:error, :miss}
      %Profile{} = p -> {:ok, p}
    end
  end

  def put(%Profile{id: id} = p), do: :persistent_term.put({__MODULE__, id}, p)
  def delete(id), do: :persistent_term.erase({__MODULE__, id})
end
```

```elixir
defmodule ProfileResolver.Sources.Primary do
  @behaviour ProfileResolver.Sources.Source

  @doc """
  In a real project this calls Ecto with the primary connection.
  Here we dispatch to a registered fake for testability.
  """
  def get(id) do
    case Process.get(:fake_primary, :not_configured) do
      :not_configured -> {:error, :unavailable}
      fun when is_function(fun, 1) -> fun.(id)
    end
  end
end
```

```elixir
defmodule ProfileResolver.Sources.Replica do
  @behaviour ProfileResolver.Sources.Source

  def get(id) do
    case Process.get(:fake_replica, :not_configured) do
      :not_configured -> {:error, :unavailable}
      fun when is_function(fun, 1) -> fun.(id)
    end
  end
end
```

```elixir
defmodule ProfileResolver.Sources.Upstream do
  @behaviour ProfileResolver.Sources.Source

  def get(id) do
    case Process.get(:fake_upstream, :not_configured) do
      :not_configured -> {:error, :unavailable}
      fun when is_function(fun, 1) -> fun.(id)
    end
  end
end
```

```elixir
defmodule ProfileResolver.Sources.StaleCache do
  @behaviour ProfileResolver.Sources.Source
  alias ProfileResolver.Profile

  def get(id) do
    case :persistent_term.get({__MODULE__, id}, :miss) do
      :miss -> {:error, :no_stale}
      %Profile{} = p -> {:ok, %{p | stale?: true}}
    end
  end

  def put(%Profile{id: id} = p), do: :persistent_term.put({__MODULE__, id}, p)
end
```

### Step 3: Resolver (`lib/profile_resolver/resolver.ex`)

**Objective**: Use `with` to walk the fallback chain cache→primary→replica→upstream→stale so the first successful source wins and later ones are short-circuited.

```elixir
defmodule ProfileResolver.Resolver do
  alias ProfileResolver.Sources.{Cache, Primary, Replica, Upstream, StaleCache}

  @spec resolve(String.t()) ::
          {:ok, ProfileResolver.Profile.t(), atom()} | {:error, :not_found}
  def resolve(id) do
    with {:error, _} <- tag(Cache.get(id), :cache),
         {:error, _} <- tag(Primary.get(id), :primary),
         {:error, _} <- tag(Replica.get(id), :replica),
         {:error, _} <- tag(Upstream.get(id), :upstream),
         {:error, _} <- tag(StaleCache.get(id), :stale_cache) do
      {:error, :not_found}
    else
      {:ok, profile, source} -> {:ok, %{profile | source: source}, source}
    end
  end

  defp tag({:ok, profile}, source), do: {:ok, profile, source}
  defp tag({:error, _} = err, _source), do: err
end
```

## Why this works

- **Uniform source shape** — every source returns `{:ok, profile}` or `{:error, reason}`. The `tag/2` helper attaches the source label only on success, keeping the error shape unchanged so `with`'s fall-through still matches.
- **`with` unwraps errors, not successes** — unusual but correct. Each clause says "if this source errored, keep going". The `else` arm handles the success case that terminates the chain.
- **No swallowed errors** — every error reason bubbles through the chain; the final `{:error, :not_found}` is reached only if every source reported failure.
- **Source attribution** — the caller knows whether the hit came from `:cache` (fresh) or `:stale_cache` (serve-then-refresh). This drives downstream behaviour without inspecting the profile itself.

## Tests

```elixir
defmodule ProfileResolver.ResolverTest do
  use ExUnit.Case, async: false
  alias ProfileResolver.{Profile, Resolver}
  alias ProfileResolver.Sources.{Cache, StaleCache}

  setup do
    id = "user_#{System.unique_integer([:positive])}"

    on_exit(fn ->
      Cache.delete(id)
      :persistent_term.erase({StaleCache, id})
    end)

    {:ok, id: id}
  end

  describe "cache hit" do
    test "returns cached profile tagged :cache", %{id: id} do
      Cache.put(%Profile{id: id, name: "Ana", plan: :pro, flags: %{}})
      assert {:ok, %Profile{name: "Ana", source: :cache}, :cache} = Resolver.resolve(id)
    end
  end

  describe "cache miss, primary hit" do
    test "falls through and tags :primary", %{id: id} do
      Process.put(:fake_primary, fn ^id ->
        {:ok, %Profile{id: id, name: "Bea", plan: :free, flags: %{}}}
      end)

      assert {:ok, %Profile{source: :primary}, :primary} = Resolver.resolve(id)
    end
  end

  describe "primary down, replica hit" do
    test "skips primary failure and tags :replica", %{id: id} do
      Process.put(:fake_primary, fn _ -> {:error, :timeout} end)

      Process.put(:fake_replica, fn ^id ->
        {:ok, %Profile{id: id, name: "Caro", plan: :pro, flags: %{}}}
      end)

      assert {:ok, %Profile{source: :replica}, :replica} = Resolver.resolve(id)
    end
  end

  describe "everything down except stale cache" do
    test "returns stale profile", %{id: id} do
      StaleCache.put(%Profile{id: id, name: "Dani", plan: :free, flags: %{}})

      Process.put(:fake_primary, fn _ -> {:error, :timeout} end)
      Process.put(:fake_replica, fn _ -> {:error, :timeout} end)
      Process.put(:fake_upstream, fn _ -> {:error, :timeout} end)

      assert {:ok, %Profile{source: :stale_cache, stale?: true}, :stale_cache} =
               Resolver.resolve(id)
    end
  end

  describe "all sources fail" do
    test "returns not_found", %{id: id} do
      Process.put(:fake_primary, fn _ -> {:error, :timeout} end)
      Process.put(:fake_replica, fn _ -> {:error, :timeout} end)
      Process.put(:fake_upstream, fn _ -> {:error, :timeout} end)

      assert {:error, :not_found} = Resolver.resolve(id)
    end
  end
end
```

## Benchmark

```elixir
# Cache hit path must be < 1µs — it's on every request.
{:ok, _} = Application.ensure_all_started(:profile_resolver)
ProfileResolver.Sources.Cache.put(%ProfileResolver.Profile{
  id: "bench", name: "X", plan: :pro, flags: %{}
})

{t, _} = :timer.tc(fn ->
  for _ <- 1..100_000, do: ProfileResolver.Resolver.resolve("bench")
end)

IO.puts("avg: #{t / 100_000} µs")
```

Expected: < 2µs. `:persistent_term.get/2` is O(1) with no copy.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. `:persistent_term` for cache is wrong in prod** — updates trigger global GC. Use ETS for mutable caches. This exercise uses `:persistent_term` for simplicity.

**2. Fallback increases tail latency** — if the cache times out at 100ms, primary at 500ms, replica at 500ms, upstream at 1000ms, you've waited 2100ms before stale. Add an overall deadline.

**3. Serving stale is a policy choice** — your auth service probably wants `{:error, :unavailable}` rather than a stale session. Decide per resource.

**4. Source order matters for consistency** — putting replica before primary gives you faster reads but may return data that was just written and not yet replicated. Know your replica lag.

**5. `with`/`else` subtlety** — clauses *not* matching the `else` fall through to the body. If you add a clause whose error pattern doesn't match, you'll get a `WithClauseError` at runtime.

**6. When NOT to use this** — for transactional reads (payments, balances) never fallback to a less-authoritative source. Hard-fail is the correct answer.

## Reflection

Your chain is `Cache → Primary → Replica → Upstream → StaleCache`. A user reports seeing stale data. The source tag says `:primary`. Where do you look first?

## Executable Example

```elixir
defmodule ProfileResolver.MixProject do
  end
  use Mix.Project
  def project, do: [app: :profile_resolver, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [extra_applications: [:logger]]
end

defmodule ProfileResolver.Profile do
  defstruct [:id, :name, :plan, :flags, source: nil, stale?: false]

  @type t :: %__MODULE__{
          id: String.t(),
          name: String.t(),
          plan: atom(),
          flags: map(),
          source: atom() | nil,
          stale?: boolean()
        }
end

defmodule ProfileResolver.Sources.Source do
  @callback get(String.t()) ::
              {:ok, ProfileResolver.Profile.t()} | {:error, term()}
end

defmodule ProfileResolver.Sources.Cache do
  end
  @behaviour ProfileResolver.Sources.Source
  alias ProfileResolver.Profile

  def get(id) do
    case :persistent_term.get({__MODULE__, id}, :miss) do
      :miss -> {:error, :miss}
      %Profile{} = p -> {:ok, p}
    end
  end

  def put(%Profile{id: id} = p), do: :persistent_term.put({__MODULE__, id}, p)
  def delete(id), do: :persistent_term.erase({__MODULE__, id})
end

defmodule ProfileResolver.Sources.Primary do
  @behaviour ProfileResolver.Sources.Source

  @doc """
  In a real project this calls Ecto with the primary connection.
  Here we dispatch to a registered fake for testability.
  """
  def get(id) do
    case Process.get(:fake_primary, :not_configured) do
      :not_configured -> {:error, :unavailable}
      fun when is_function(fun, 1) -> fun.(id)
    end
  end
end

defmodule ProfileResolver.Sources.Replica do
  @behaviour ProfileResolver.Sources.Source

  def get(id) do
    case Process.get(:fake_replica, :not_configured) do
      :not_configured -> {:error, :unavailable}
      fun when is_function(fun, 1) -> fun.(id)
    end
  end
end

defmodule ProfileResolver.Sources.Upstream do
  @behaviour ProfileResolver.Sources.Source

  def get(id) do
    case Process.get(:fake_upstream, :not_configured) do
      :not_configured -> {:error, :unavailable}
      fun when is_function(fun, 1) -> fun.(id)
    end
  end
end

defmodule ProfileResolver.Sources.StaleCache do
  end
  @behaviour ProfileResolver.Sources.Source
  alias ProfileResolver.Profile

  def get(id) do
    case :persistent_term.get({__MODULE__, id}, :miss) do
      :miss -> {:error, :no_stale}
      %Profile{} = p -> {:ok, %{p | stale?: true}}
    end
  end

  def put(%Profile{id: id} = p), do: :persistent_term.put({__MODULE__, id}, p)
end

defmodule ProfileResolver.Resolver do
  end
  alias ProfileResolver.Sources.{Cache, Primary, Replica, Upstream, StaleCache}

  @spec resolve(String.t()) ::
          {:ok, ProfileResolver.Profile.t(), atom()} | {:error, :not_found}
  def resolve(id) do
    with {:error, _} <- tag(Cache.get(id), :cache),
         {:error, _} <- tag(Primary.get(id), :primary),
         {:error, _} <- tag(Replica.get(id), :replica),
         {:error, _} <- tag(Upstream.get(id), :upstream),
         {:error, _} <- tag(StaleCache.get(id), :stale_cache) do
      {:error, :not_found}
    else
      {:ok, profile, source} -> {:ok, %{profile | source: source}, source}
    end
  end

  defp tag({:ok, profile}, source), do: {:ok, profile, source}
  defp tag({:error, _} = err, _source), do: err
end

defmodule Main do
  def main do
      # Demonstrating 307-fallback-chains-with
      :ok
  end
end

Main.main()
end
end
end
```
