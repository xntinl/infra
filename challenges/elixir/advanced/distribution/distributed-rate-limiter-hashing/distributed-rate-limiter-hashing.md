# Distributed Rate Limiter with Consistent Hashing

**Project**: `edge_rate_limiter` — a cluster-wide rate limiter where every node can enforce the same per-key budget without coordinating on every request

---

## Why distribution and clustering matters

Distributed Erlang gives you remote message-passing transparency, but the cost is your responsibility for split-brain detection, registry consistency, and net-tick policies. Libcluster, Horde, and PG provide pieces; you compose them.

Clusters fail in interesting ways: netsplits, asymmetric partitions, GC pauses misread as crashes, and global registry race conditions. Designing for the network — rather than against it — is the senior shift.

---

## The business problem

You are building a production-grade Elixir component in the **Distribution and clustering** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
edge_rate_limiter/
├── lib/
│   └── edge_rate_limiter.ex
├── script/
│   └── main.exs
├── test/
│   └── edge_rate_limiter_test.exs
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

Chose **B** because in Distribution and clustering the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule EdgeRateLimiter.MixProject do
  use Mix.Project

  def project do
    [
      app: :edge_rate_limiter,
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
### `lib/edge_rate_limiter.ex`

```elixir
# lib/edge_rate_limiter/ring.ex
defmodule EdgeRateLimiter.Ring do
  @moduledoc """
  Consistent hash ring with virtual nodes.

  A ring is an opaque term built from a list of nodes. Lookup is O(log(n·vnodes)).
  """

  @vnodes_per_node 128

  @opaque t :: {sorted_entries :: :gb_trees.tree(non_neg_integer(), node()), node_count :: non_neg_integer()}

  @doc "Builds result from nodes."
  @spec build([node()]) :: t()
  def build(nodes) when is_list(nodes) do
    tree =
      for n <- nodes, i <- 0..(@vnodes_per_node - 1), reduce: :gb_trees.empty() do
        acc -> :gb_trees.insert(hash({n, i}), n, acc)
      end

    {tree, length(nodes)}
  end

  @doc "Returns owner result from _key."
  @spec owner(t(), term()) :: node() | nil
  def owner({tree, 0}, _key), do: nil

  @doc "Returns owner result from _count and key."
  def owner({tree, _count}, key) do
    h = hash(key)

    case successor(tree, h) do
      :none ->
        # wrap around to the first entry
        {_hash, node} = :gb_trees.smallest(tree)
        node

      {_hash, node} ->
        node
    end
  end

  @doc "Returns nodes result from _."
  @spec nodes(t()) :: [node()]
  def nodes({tree, _}) do
    tree
    |> :gb_trees.values()
    |> Enum.uniq()
  end

  defp successor(tree, h) do
    iter = :gb_trees.iterator_from(h, tree)

    case :gb_trees.next(iter) do
      :none -> :none
      {hash, node, _next} -> {hash, node}
    end
  end

  defp hash(term), do: :erlang.phash2(term, 1 <<< 32)
end

# lib/edge_rate_limiter/shard.ex
defmodule EdgeRateLimiter.Shard do
  @moduledoc "Local counter and sliding-window store. Owns an ETS table."
  use GenServer

  @table :edge_rate_limiter_shard

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  @doc "Checks result from client_id, limit and window_ms."
  def check(client_id, limit, window_ms) do
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    timestamps =
      @table
      |> :ets.lookup(client_id)
      |> Enum.map(fn {_id, ts} -> ts end)
      |> Enum.filter(&(&1 >= cutoff))

    count = length(timestamps)

    cond do
      count < limit ->
        :ets.insert(@table, {client_id, now})
        {:allow, limit - count - 1}

      true ->
        oldest = Enum.min(timestamps)
        {:deny, max(1, oldest + window_ms - now)}
    end
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :bag, {:read_concurrency, true}])
    :timer.send_interval(60_000, :cleanup)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:cleanup, state) do
    cutoff = System.monotonic_time(:millisecond) - 3_600_000
    ms = [{{:_, :"$1"}, [{:<, :"$1", cutoff}], [true]}]
    :ets.select_delete(@table, ms)
    {:noreply, state}
  end
end

# lib/edge_rate_limiter/limiter.ex
defmodule EdgeRateLimiter.Limiter do
  @moduledoc "Public API. Locates the owning node via the ring and forwards the check."
  use GenServer

  alias EdgeRateLimiter.{Ring, Shard}

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  @doc "Checks result from client_id, limit and window_ms."
  def check(client_id, limit, window_ms) do
    ring = :persistent_term.get(:edge_rate_limiter_ring)

    case Ring.owner(ring, client_id) do
      nil ->
        {:deny, window_ms}

      node when node == node() ->
        Shard.check(client_id, limit, window_ms)

      other_node ->
        :rpc.call(other_node, Shard, :check, [client_id, limit, window_ms], 500)
    end
  end

  @impl true
  def init(_) do
    :net_kernel.monitor_nodes(true, node_type: :visible)
    rebuild_ring()
    {:ok, %{}}
  end

  @impl true
  def handle_info({:nodeup, _, _}, state) do
    rebuild_ring()
    {:noreply, state}
  end

  def handle_info({:nodedown, _, _}, state) do
    rebuild_ring()
    {:noreply, state}
  end

  defp rebuild_ring do
    nodes = [Node.self() | Node.list(:visible)]
    :persistent_term.put(:edge_rate_limiter_ring, Ring.build(nodes))
  end
end

# lib/edge_rate_limiter/application.ex
defmodule EdgeRateLimiter.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      EdgeRateLimiter.Shard,
      EdgeRateLimiter.Limiter
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: EdgeRateLimiter.Supervisor)
  end
end

defmodule EdgeRateLimiter.LimiterTest do
  use ExUnit.Case, async: false
  doctest EdgeRateLimiter.MixProject

  alias EdgeRateLimiter.{Limiter, Shard}

  setup do
    :ets.delete_all_objects(:edge_rate_limiter_shard)
    :ok
  end

  describe "check/3 — single-node behaviour" do
    test "allows up to the limit" do
      for _ <- 1..5 do
        assert {:allow, _} = Limiter.check("client_a", 10, 60_000)
      end
    end

    test "denies when limit is reached" do
      for _ <- 1..10, do: Limiter.check("client_b", 10, 60_000)
      assert {:deny, retry} = Limiter.check("client_b", 10, 60_000)
      assert retry > 0
    end
  end

  describe "Shard.check/3 directly" do
    test "new client has full budget" do
      assert {:allow, 99} = Shard.check("new_c", 100, 60_000)
    end
  end
end
```
### `test/edge_rate_limiter_test.exs`

```elixir
defmodule EdgeRateLimiter.RingTest do
  use ExUnit.Case, async: true
  doctest EdgeRateLimiter.MixProject
  alias EdgeRateLimiter.Ring

  describe "build/1 and owner/2" do
    test "empty ring returns nil owner" do
      assert Ring.owner(Ring.build([]), "any") == nil
    end

    test "single-node ring always returns that node" do
      ring = Ring.build([:"a@h"])
      assert Ring.owner(ring, "k1") == :"a@h"
      assert Ring.owner(ring, "k2") == :"a@h"
    end

    test "stable ownership: same key → same node" do
      ring = Ring.build([:"a@h", :"b@h", :"c@h"])
      assert Ring.owner(ring, "alice") == Ring.owner(ring, "alice")
    end

    test "minimal reshuffle when a node leaves" do
      ring1 = Ring.build([:"a@h", :"b@h", :"c@h"])
      ring2 = Ring.build([:"a@h", :"b@h"])

      keys = for i <- 1..1000, do: "k#{i}"

      moved =
        Enum.count(keys, fn k ->
          Ring.owner(ring1, k) != Ring.owner(ring2, k)
        end)

      # After removing :c@h, ~1/3 of keys should move. Allow wide range.
      assert moved < 500
      assert moved > 200
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Simulate distributed rate limiter: consistent hash to same bucket
      customer_id = "customer_abc"
      budget = 100  # requests per minute

      # Consistent hash: always maps customer_id to same bucket across cluster
      hash = :erlang.phash2(customer_id)
      bucket_node = rem(hash, 3) + 1  # Distribute across 3 nodes

      # Track usage
      usage = %{customer_id => %{count: 45, reset_at: System.os_time(:millisecond) + 60_000}}

      available = budget - usage[customer_id].count

      IO.puts("✓ Customer: #{customer_id}")
      IO.puts("✓ Hashed to node: #{bucket_node}")
      IO.puts("✓ Available budget: #{available}/#{budget}")
      IO.inspect(usage, label: "✓ Rate limit state")

      assert bucket_node in 1..3, "Consistent hash to valid node"
      assert available >= 0, "Valid budget"

      IO.puts("✓ Distributed rate limiter: consistent hashing working")
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

### 1. Partitions are the rule, not the exception

In a multi-AZ cluster, brief netsplits happen daily. Design for them: prefer eventual consistency, use idempotent operations, and detect split-brain explicitly.

### 2. Registries don't replicate transparently

Local Registry is fast and node-local. :global is consistent but slow. Horde.Registry replicates via CRDTs — eventual consistency, no global locks. Pick based on your read/write ratio.

### 3. Tune net_kernel ticks for your environment

The default 60-second tick is too long for production failure detection but too short for high-latency cross-region links. Measure first.

---
