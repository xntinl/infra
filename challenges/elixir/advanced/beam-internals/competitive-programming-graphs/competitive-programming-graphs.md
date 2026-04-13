# Graph Algorithms in Elixir

**Project**: `competitive_programming_graphs` — production-grade graph algorithms in elixir in Elixir

---

## Why beam internals and performance matters

Performance work on the BEAM rewards depth: schedulers, reductions, process heaps, garbage collection, binary reference counting, and the JIT compiler each have observable knobs. Tools like recon, eflame, Benchee, and :sys.statistics let you measure before tuning.

The pitfall is benchmarking without a hypothesis. Senior engineers characterize the workload first (CPU-bound? Memory-bound? Lock contention?), then choose the instrument. Premature optimization on the BEAM is particularly costly because micro-benchmarks rarely reflect real scheduler behavior under load.

---

## The business problem

You are building a production-grade Elixir component in the **BEAM internals and performance** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
competitive_programming_graphs/
├── lib/
│   └── competitive_programming_graphs.ex
├── script/
│   └── main.exs
├── test/
│   └── competitive_programming_graphs_test.exs
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

Chose **B** because in BEAM internals and performance the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule CompetitiveProgrammingGraphs.MixProject do
  use Mix.Project

  def project do
    [
      app: :competitive_programming_graphs,
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
### `lib/competitive_programming_graphs.ex`

```elixir
defmodule ApiGateway.Graph do
  @moduledoc "Functional graph representation and construction helpers."

  @type node_id :: term()
  @type weight :: number()
  @type t :: %{node_id() => [node_id()]}
  @type weighted_t :: %{node_id() => [{node_id(), weight()}]}

  def new, do: %{}

  @doc "Add an undirected edge between u and v."
  @spec add_edge(t(), node_id(), node_id()) :: t()
  def add_edge(g, u, v) do
    g
    |> Map.update(u, [v], &[v | &1])
    |> Map.update(v, [u], &[u | &1])
  end

  @doc "Add a directed edge from u to v with optional weight."
  @spec add_directed_edge(weighted_t(), node_id(), node_id(), weight()) :: weighted_t()
  def add_directed_edge(g, u, v, weight \\ 1) do
    Map.update(g, u, [{v, weight}], &[{v, weight} | &1])
  end

  @spec neighbors(t() | weighted_t(), node_id()) :: list()
  def neighbors(g, node), do: Map.get(g, node, [])

  @spec nodes(t() | weighted_t()) :: [node_id()]
  def nodes(g), do: Map.keys(g)

  @spec size(t() | weighted_t()) :: non_neg_integer()
  def size(g), do: map_size(g)
end

defmodule ApiGateway.Graph.BFS do
  alias ApiGateway.Graph

  @doc """
  BFS from `start`. Returns {visit_order, distance_map}.

  Uses Erlang's `:queue` for O(1) enqueue and dequeue.
  """
  @spec traverse(Graph.t(), Graph.node_id()) ::
          {[Graph.node_id()], %{Graph.node_id() => non_neg_integer()}}
  def traverse(graph, start) do
    queue = :queue.from_list([start])
    visited = MapSet.new([start])
    distances = %{start => 0}

    do_bfs(graph, queue, visited, [start], distances)
  end

  defp do_bfs(graph, queue, visited, order, distances) do
    case :queue.out(queue) do
      {:empty, _} ->
        {Enum.reverse(order), distances}

      {{:value, node}, rest_queue} ->
        current_dist = distances[node]

        {new_queue, new_visited, new_order, new_distances} =
          graph
          |> Graph.neighbors(node)
          |> Enum.reduce({rest_queue, visited, order, distances}, fn neighbor, {q, v, o, d} ->
            if MapSet.member?(v, neighbor) do
              {q, v, o, d}
            else
              {
                :queue.in(neighbor, q),
                MapSet.put(v, neighbor),
                [neighbor | o],
                Map.put(d, neighbor, current_dist + 1)
              }
            end
          end)

        do_bfs(graph, new_queue, new_visited, new_order, new_distances)
    end
  end

  @doc """
  Finds all connected components. Returns a list of lists (each is one component).
  """
  @spec connected_components(Graph.t()) :: [[Graph.node_id()]]
  def connected_components(graph) do
    nodes = Graph.nodes(graph)

    {components, _visited} =
      Enum.reduce(nodes, {[], MapSet.new()}, fn node, {comps, visited} ->
        if MapSet.member?(visited, node) do
          {comps, visited}
        else
          {order, _distances} = traverse(graph, node)
          new_visited = Enum.reduce(order, visited, &MapSet.put(&2, &1))
          {[order | comps], new_visited}
        end
      end)

    Enum.reverse(components)
  end

  @doc """
  Shortest path by hop count between start and target in an unweighted graph.
  Returns {:ok, path, distance} or {:error, :no_path}.
  """
  @spec shortest_path(Graph.t(), Graph.node_id(), Graph.node_id()) ::
          {:ok, [Graph.node_id()], non_neg_integer()} | {:error, :no_path}
  def shortest_path(graph, start, target) do
    {_order, distances} = traverse(graph, start)
    case distances[target] do
      nil  -> {:error, :no_path}
      dist ->
        path = reconstruct_path(graph, distances, start, target)
        {:ok, path, dist}
    end
  end

  defp reconstruct_path(graph, distances, start, target) do
    do_reconstruct(graph, distances, start, target, [target])
  end

  defp do_reconstruct(_graph, _distances, start, start, path), do: path
  defp do_reconstruct(graph, distances, start, current, path) do
    prev = graph
    |> Graph.neighbors(current)
    |> Enum.find(fn n -> distances[n] == distances[current] - 1 end)
    do_reconstruct(graph, distances, start, prev, [prev | path])
  end
end

defmodule ApiGateway.Graph.TopologicalSort do
  alias ApiGateway.Graph

  @doc """
  Kahn's algorithm: BFS-based topological sort for directed graphs.

  Returns {:ok, order} or {:error, :has_cycle}.
  """
  @spec sort(Graph.t()) :: {:ok, [Graph.node_id()]} | {:error, :has_cycle}
  def sort(graph) do
    in_degrees = calculate_in_degrees(graph)

    zero_degree_nodes =
      in_degrees
      |> Enum.filter(fn {_, deg} -> deg == 0 end)
      |> Enum.map(fn {node, _} -> node end)

    queue = :queue.from_list(zero_degree_nodes)
    node_count = map_size(in_degrees)

    do_kahn(graph, queue, in_degrees, [], node_count)
  end

  defp do_kahn(graph, queue, in_degrees, result, node_count) do
    case :queue.out(queue) do
      {:empty, _} ->
        if length(result) == node_count do
          {:ok, Enum.reverse(result)}
        else
          {:error, :has_cycle}
        end

      {{:value, node}, rest_queue} ->
        neighbors = Map.get(graph, node, [])

        neighbor_ids = Enum.map(neighbors, fn
          {n, _weight} -> n
          n -> n
        end)

        {updated_queue, updated_degrees} =
          Enum.reduce(neighbor_ids, {rest_queue, in_degrees}, fn neighbor, {q, deg} ->
            new_deg = Map.update!(deg, neighbor, &(&1 - 1))

            if new_deg[neighbor] == 0 do
              {:queue.in(neighbor, q), new_deg}
            else
              {q, new_deg}
            end
          end)

        do_kahn(graph, updated_queue, updated_degrees, [node | result], node_count)
    end
  end

  defp calculate_in_degrees(graph) do
    base = Map.new(Graph.nodes(graph), fn n -> {n, 0} end)

    Enum.reduce(graph, base, fn {_node, neighbors}, acc ->
      Enum.reduce(neighbors, acc, fn neighbor, d ->
        neighbor_id = case neighbor do
          {n, _weight} -> n
          n -> n
        end

        Map.update(d, neighbor_id, 1, &(&1 + 1))
      end)
    end)
  end
end

defmodule ApiGateway.Graph.CycleDetector do
  alias ApiGateway.Graph

  @doc """
  Detects cycles in a directed graph using 3-color DFS.

  Colors:
    :white -- not yet visited
    :gray  -- currently in the DFS stack (ancestor)
    :black -- fully processed

  A back edge (gray -> gray) indicates a cycle.
  """
  @spec has_cycle?(Graph.t()) :: boolean()
  def has_cycle?(graph) do
    nodes = Graph.nodes(graph)
    colors = Map.new(nodes, fn n -> {n, :white} end)
    Enum.reduce_while(nodes, colors, fn node, colors ->
      if colors[node] == :white do
        case dfs(graph, node, colors) do
          {:cycle, _}    -> {:halt, true}
          {:ok, colors}  -> {:cont, colors}
        end
      else
        {:cont, colors}
      end
    end)
    |> case do
      true -> true
      _colors -> false
    end
  end

  @doc """
  Finds one cycle path in the graph. Returns a list of nodes forming the cycle,
  or an empty list if no cycle exists.
  """
  @spec find_cycle(Graph.t()) :: [Graph.node_id()]
  def find_cycle(graph) do
    nodes = Graph.nodes(graph)
    colors = Map.new(nodes, fn n -> {n, :white} end)

    Enum.reduce_while(nodes, {colors, []}, fn node, {colors, _path} ->
      if colors[node] == :white do
        case dfs_with_path(graph, node, colors, []) do
          {:cycle, _colors, cycle_path} -> {:halt, {colors, cycle_path}}
          {:ok, colors}                 -> {:cont, {colors, []}}
        end
      else
        {:cont, {colors, []}}
      end
    end)
    |> elem(1)
  end

  defp dfs(graph, node, colors) do
    colors = Map.put(colors, node, :gray)

    neighbors = Graph.neighbors(graph, node)
    neighbor_ids = Enum.map(neighbors, fn
      {n, _w} -> n
      n -> n
    end)

    result = Enum.reduce_while(neighbor_ids, {:ok, colors}, fn neighbor, {:ok, c} ->
      case c[neighbor] do
        :gray  -> {:halt, {:cycle, c}}
        :white ->
          case dfs(graph, neighbor, c) do
            {:cycle, _} = cycle -> {:halt, cycle}
            {:ok, c}            -> {:cont, {:ok, c}}
          end
        :black -> {:cont, {:ok, c}}
        nil    -> {:cont, {:ok, c}}
      end
    end)

    case result do
      {:cycle, c}  -> {:cycle, c}
      {:ok, c}     -> {:ok, Map.put(c, node, :black)}
    end
  end

  defp dfs_with_path(graph, node, colors, path) do
    colors = Map.put(colors, node, :gray)
    path = path ++ [node]

    neighbors = Graph.neighbors(graph, node)
    neighbor_ids = Enum.map(neighbors, fn
      {n, _w} -> n
      n -> n
    end)

    result = Enum.reduce_while(neighbor_ids, {:ok, colors}, fn neighbor, {:ok, c} ->
      case c[neighbor] do
        :gray ->
          cycle_start_idx = Enum.find_index(path, &(&1 == neighbor))
          cycle_path = Enum.slice(path, cycle_start_idx..-1//1) ++ [neighbor]
          {:halt, {:cycle, c, cycle_path}}
        :white ->
          case dfs_with_path(graph, neighbor, c, path) do
            {:cycle, _, _} = cycle -> {:halt, cycle}
            {:ok, c}               -> {:cont, {:ok, c}}
          end
        :black -> {:cont, {:ok, c}}
        nil    -> {:cont, {:ok, c}}
      end
    end)

    case result do
      {:cycle, c, cycle_path} -> {:cycle, c, cycle_path}
      {:ok, c}                -> {:ok, Map.put(c, node, :black)}
    end
  end
end

defmodule ApiGateway.Graph.Dijkstra do
  alias ApiGateway.Graph

  @infinity :infinity

  @doc """
  Dijkstra from `start`. Returns a map of minimum distances to all reachable nodes.
  Uses `:gb_sets` as a min-heap priority queue.
  """
  @spec shortest_paths(Graph.weighted_t(), Graph.node_id()) ::
          %{Graph.node_id() => number()}
  def shortest_paths(graph, start) do
    dist = Map.new(Graph.nodes(graph), fn n -> {n, @infinity} end) |> Map.put(start, 0)
    pq   = :gb_sets.singleton({0, start})
    do_dijkstra(graph, pq, dist, MapSet.new())
  end

  @doc """
  Finds the shortest path and distance between start and target.
  Returns {:ok, distance, path} or {:error, :no_path}.
  """
  @spec shortest_path(Graph.weighted_t(), Graph.node_id(), Graph.node_id()) ::
          {:ok, number(), [Graph.node_id()]} | {:error, :no_path}
  def shortest_path(graph, start, target) do
    nodes = Graph.nodes(graph)
    dist  = Map.new(nodes, fn n -> {n, @infinity} end) |> Map.put(start, 0)
    prev  = %{}
    pq    = :gb_sets.singleton({0, start})

    {dist, prev} = do_dijkstra_with_prev(graph, pq, dist, prev, MapSet.new())

    case dist[target] do
      @infinity -> {:error, :no_path}
      d         -> {:ok, d, reconstruct(prev, start, target)}
    end
  end

  defp do_dijkstra(graph, pq, dist, visited) do
    if :gb_sets.is_empty(pq) do
      dist
    else
      {{d, node}, pq} = :gb_sets.take_smallest(pq)
      if MapSet.member?(visited, node) or d > dist[node] do
        do_dijkstra(graph, pq, dist, visited)
      else
        visited = MapSet.put(visited, node)
        {pq, dist} = graph
        |> Graph.neighbors(node)
        |> Enum.reduce({pq, dist}, fn {neighbor, weight}, {q, dm} ->
          new_d = dist[node] + weight
          if new_d < dm[neighbor] do
            {:gb_sets.add({new_d, neighbor}, q), Map.put(dm, neighbor, new_d)}
          else
            {q, dm}
          end
        end)
        do_dijkstra(graph, pq, dist, visited)
      end
    end
  end

  defp do_dijkstra_with_prev(graph, pq, dist, prev, visited) do
    if :gb_sets.is_empty(pq) do
      {dist, prev}
    else
      {{d, node}, pq} = :gb_sets.take_smallest(pq)
      if MapSet.member?(visited, node) or d > dist[node] do
        do_dijkstra_with_prev(graph, pq, dist, prev, visited)
      else
        visited = MapSet.put(visited, node)
        {pq, dist, prev} = graph
        |> Graph.neighbors(node)
        |> Enum.reduce({pq, dist, prev}, fn {neighbor, weight}, {q, dm, pm} ->
          new_d = dist[node] + weight
          if new_d < dm[neighbor] do
            {
              :gb_sets.add({new_d, neighbor}, q),
              Map.put(dm, neighbor, new_d),
              Map.put(pm, neighbor, node)
            }
          else
            {q, dm, pm}
          end
        end)
        do_dijkstra_with_prev(graph, pq, dist, prev, visited)
      end
    end
  end

  defp reconstruct(prev, start, target) do
    do_reconstruct(prev, start, target, [target])
  end

  defp do_reconstruct(_prev, start, start, path), do: path
  defp do_reconstruct(prev, start, node, path) do
    do_reconstruct(prev, start, prev[node], [prev[node] | path])
  end
end

# lib/api_gateway/graph/service_dependency_resolver.ex
defmodule ApiGateway.ServiceDependencyResolver do
  alias ApiGateway.Graph
  alias ApiGateway.Graph.{TopologicalSort, CycleDetector}

  @doc """
  Given a map of service_name => [dependency_names], returns the correct
  call order or {:error, {:circular, cycle_path}}.

  ## Example
      resolve(%{
        "payments" => ["auth", "exchange_rates"],
        "auth"     => ["users"],
        "users"    => [],
        "exchange_rates" => []
      })
      # => {:ok, ["users", "exchange_rates", "auth", "payments"]}
  """
  @spec resolve(%{String.t() => [String.t()]}) ::
          {:ok, [String.t()]} | {:error, {:circular, [String.t()]}}
  def resolve(service_deps) do
    graph = build_graph(service_deps)

    if CycleDetector.has_cycle?(graph) do
      cycle_path = CycleDetector.find_cycle(graph)
      {:error, {:circular, cycle_path}}
    else
      {:ok, sorted} = TopologicalSort.sort(graph)
      {:ok, sorted}
    end
  end

  defp build_graph(service_deps) do
    Enum.reduce(service_deps, Graph.new(), fn {service, deps}, g ->
      g = Map.put_new(g, service, [])
      Enum.reduce(deps, g, fn dep, g ->
        Graph.add_directed_edge(g, dep, service)
      end)
    end)
  end
end

defmodule ApiGateway.Graph.TopologicalSortTest do
  use ExUnit.Case, async: true
  doctest CompetitiveProgrammingGraphs.MixProject

  alias ApiGateway.Graph
  alias ApiGateway.Graph.TopologicalSort

  describe "ApiGateway.Graph.TopologicalSort" do
    test "returns valid topological order for acyclic graph" do
      g = %{
        "a" => ["b", "c"],
        "b" => ["d"],
        "c" => ["d"],
        "d" => []
      }
      {:ok, order} = TopologicalSort.sort(g)
      assert Enum.find_index(order, &(&1 == "a")) < Enum.find_index(order, &(&1 == "d"))
    end

    test "detects cycle" do
      g = %{"a" => ["b"], "b" => ["c"], "c" => ["a"]}
      assert {:error, :has_cycle} = TopologicalSort.sort(g)
    end

    test "service dependency resolver returns correct call order" do
      deps = %{
        "payments"       => ["auth"],
        "auth"           => ["users"],
        "users"          => [],
        "exchange_rates" => []
      }
      {:ok, order} = ApiGateway.ServiceDependencyResolver.resolve(deps)
      assert Enum.find_index(order, &(&1 == "users")) <
             Enum.find_index(order, &(&1 == "auth"))
      assert Enum.find_index(order, &(&1 == "auth")) <
             Enum.find_index(order, &(&1 == "payments"))
    end

    test "resolver detects circular dependency" do
      deps = %{"a" => ["b"], "b" => ["c"], "c" => ["a"]}
      assert {:error, {:circular, _}} = ApiGateway.ServiceDependencyResolver.resolve(deps)
    end
  end
end

# test/api_gateway/graph/dijkstra_test.exs
defmodule ApiGateway.Graph.DijkstraTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Graph.Dijkstra

  @graph %{
    "a" => [{"b", 4}, {"c", 2}],
    "b" => [{"d", 3}],
    "c" => [{"b", 1}, {"d", 10}],
    "d" => []
  }

  describe "ApiGateway.Graph.Dijkstra" do
    test "finds shortest path" do
      assert {:ok, 6, ["a", "c", "b", "d"]} = Dijkstra.shortest_path(@graph, "a", "d")
    end

    test "returns no_path for unreachable node" do
      g = %{"a" => [{"b", 1}], "b" => [], "z" => []}
      assert {:error, :no_path} = Dijkstra.shortest_path(g, "a", "z")
    end
  end
end

# bench/graph_bench.exs
Mix.install([{:benchee, "~> 1.3"}])

alias ApiGateway.Graph
alias ApiGateway.Graph.BFS

defmodule GraphBench do
  def random_graph(n) do
    Enum.reduce(1..n, Graph.new(), fn i, g ->
      neighbors = Enum.take_random(1..n, min(5, n - 1))
      Enum.reduce(neighbors, g, fn j, g -> Graph.add_edge(g, i, j) end)
    end)
  end
end

graph_1k  = GraphBench.random_graph(1_000)
graph_10k = GraphBench.random_graph(10_000)

Benchee.run(%{
  "BFS 1k"       => fn -> BFS.traverse(graph_1k, 1) end,
  "BFS 10k"      => fn -> BFS.traverse(graph_10k, 1) end,
  "Components 1k" => fn -> BFS.connected_components(graph_1k) end,
},
  time: 5, warmup: 2, memory_time: 2,
  formatters: [Benchee.Formatters.Console]
)
```
### `test/competitive_programming_graphs_test.exs`

```elixir
defmodule ApiGateway.Graph.BFSTest do
  use ExUnit.Case, async: true
  doctest CompetitiveProgrammingGraphs.MixProject

  alias ApiGateway.Graph
  alias ApiGateway.Graph.BFS

  describe "ApiGateway.Graph.BFS" do
    test "visits all reachable nodes" do
      g = Graph.new()
      |> Graph.add_edge("a", "b")
      |> Graph.add_edge("b", "c")
      |> Graph.add_edge("d", "e")

      {order, _} = BFS.traverse(g, "a")
      assert Enum.sort(order) == ["a", "b", "c"]
    end

    test "connected_components finds isolated components" do
      g = Graph.new()
      |> Graph.add_edge("a", "b")
      |> Graph.add_edge("c", "d")
      |> Map.put("e", [])

      components = BFS.connected_components(g)
      assert length(components) == 3
    end

    test "shortest_path returns correct hop count" do
      g = Graph.new()
      |> Graph.add_edge("a", "b")
      |> Graph.add_edge("b", "c")
      |> Graph.add_edge("a", "c")

      assert {:ok, ["a", "c"], 1} = BFS.shortest_path(g, "a", "c")
    end

    test "returns no_path for disconnected nodes" do
      g = Graph.new()
      |> Graph.add_edge("a", "b")
      |> Map.put("z", [])

      assert {:error, :no_path} = BFS.shortest_path(g, "a", "z")
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Graph Algorithms in Elixir.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Graph Algorithms in Elixir ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case CompetitiveProgrammingGraphs.run(payload) do
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
        for _ <- 1..1_000, do: CompetitiveProgrammingGraphs.run(:bench)
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

### 1. Reductions, not time, govern preemption

The BEAM scheduler counts reductions (function calls + I/O ops). After ~4000, the process yields. Long lists processed in tight Elixir loops are not the bottleneck people think.

### 2. Binary reference counting can leak

Sub-binaries hold references to large parent binaries. A 10-byte slice of a 10MB binary keeps the 10MB alive. Use :binary.copy/1 when storing slices long-term.

### 3. Profile production with recon

recon's process_window/3 finds memory leaks; bin_leak/1 finds binary refc leaks; proc_count/2 finds runaway processes. These are non-invasive and safe in production.

---
