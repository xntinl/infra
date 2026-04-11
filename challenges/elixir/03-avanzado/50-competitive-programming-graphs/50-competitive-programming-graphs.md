# Graph Algorithms in Elixir

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` umbrella needs two graph-shaped problems solved:

1. **Dependency resolution**: when a client requests service `X`, the gateway must determine
   the correct order to call upstream services (some services depend on others). Circular
   dependencies must be detected and rejected at configuration load time.
2. **Route optimization**: the gateway has a weighted graph of upstream service endpoints
   (latency measurements). Dijkstra finds the lowest-latency path to a destination.

You'll implement the required graph algorithms from scratch in `gateway_core`, understand
their functional representation, and benchmark them.

Project structure for this exercise:

```
api_gateway_umbrella/apps/gateway_core/
├── lib/gateway_core/
│   └── graph/
│       ├── graph.ex                  # ← you implement this
│       ├── bfs.ex                    # ← and this
│       ├── dfs.ex                    # ← and this
│       ├── dijkstra.ex               # ← and this
│       ├── topological_sort.ex       # ← and this
│       └── cycle_detector.ex         # ← and this
├── test/gateway_core/graph/
│   ├── bfs_test.exs                  # given tests
│   ├── dijkstra_test.exs             # given tests
│   └── topological_sort_test.exs     # given tests
└── bench/graph_bench.exs
```

---

## Graph representation

```elixir
# Unweighted adjacency list (undirected)
%{
  "payments" => ["auth", "audit"],
  "auth"     => ["payments", "users"],
  "users"    => ["auth"],
  "audit"    => ["payments"]
}

# Weighted adjacency list (directed — for Dijkstra)
%{
  "gateway"  => [{"payments", 12}, {"auth", 5}],
  "payments" => [{"audit", 3}],
  "auth"     => [{"users", 8}, {"payments", 15}],
  "users"    => [],
  "audit"    => []
}
```

Why maps and not arrays? Elixir has no mutable arrays. A functional graph is a `Map` from
node to neighbor list. Lookups are O(log n). For the graph sizes in this exercise (< 10k
nodes), this is fine. For graphs with millions of nodes, you'd reach for a NIF.

---

## Implementation

### Step 1: `lib/gateway_core/graph/graph.ex`

```elixir
defmodule GatewayCore.Graph do
  @moduledoc "Functional graph representation and construction helpers."

  @type node_id :: term()
  @type weight :: number()
  @type t :: %{node_id() => [node_id()]}
  @type weighted_t :: %{node_id() => [{node_id(), weight()}]}

  def new, do: %{}

  @doc "Add an undirected edge between u and v."
  def add_edge(g, u, v) do
    g
    |> Map.update(u, [v], &[v | &1])
    |> Map.update(v, [u], &[u | &1])
  end

  @doc "Add a directed edge from u to v with optional weight."
  def add_directed_edge(g, u, v, weight \\ 1) do
    Map.update(g, u, [{v, weight}], &[{v, weight} | &1])
  end

  def neighbors(g, node), do: Map.get(g, node, [])
  def nodes(g), do: Map.keys(g)
  def size(g), do: map_size(g)
end
```

### Step 2: `lib/gateway_core/graph/bfs.ex`

```elixir
defmodule GatewayCore.Graph.BFS do
  alias GatewayCore.Graph

  @doc """
  BFS from `start`. Returns {visit_order, distance_map}.

  Uses Erlang's `:queue` for O(1) enqueue and dequeue.
  """
  @spec traverse(Graph.t(), Graph.node_id()) ::
          {[Graph.node_id()], %{Graph.node_id() => non_neg_integer()}}
  def traverse(graph, start) do
    # TODO: implement iterative BFS using :queue
    # State: queue of nodes to visit, visited MapSet, order list, distances map
    # HINT: :queue.from_list([start]) to initialize
    # HINT: :queue.out(queue) returns {{:value, node}, rest_queue} or {:empty, queue}
  end

  @doc """
  Finds all connected components. Returns a list of lists (each is one component).
  """
  @spec connected_components(Graph.t()) :: [[Graph.node_id()]]
  def connected_components(graph) do
    nodes = Graph.nodes(graph)
    # TODO: iterate over nodes, BFS each unvisited node, collect components
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

  # TODO: implement reconstruct_path/4
  # HINT: backtrack from target — at each step, find the neighbor with distance = current - 1
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
```

### Step 3: `lib/gateway_core/graph/topological_sort.ex`

```elixir
defmodule GatewayCore.Graph.TopologicalSort do
  alias GatewayCore.Graph

  @doc """
  Kahn's algorithm: BFS-based topological sort for directed graphs.

  Returns {:ok, order} or {:error, :has_cycle}.

  Gateway application: determine the call order for service dependencies.
  `mix deps.get` uses topological sort to install packages in dependency order.
  """
  @spec sort(Graph.t()) :: {:ok, [Graph.node_id()]} | {:error, :has_cycle}
  def sort(graph) do
    in_degrees = calculate_in_degrees(graph)
    queue = in_degrees
    |> Enum.filter(fn {_, deg} -> deg == 0 end)
    |> Enum.map(fn {node, _} -> node end)
    |> :queue.from_list()

    # TODO: implement Kahn's algorithm
    # For each node dequeued:
    #   1. Add to result
    #   2. For each neighbor: decrement in_degree
    #   3. If neighbor's in_degree reaches 0: enqueue it
    # If result length == node count: no cycle. Otherwise: cycle detected.
  end

  defp calculate_in_degrees(graph) do
    base = Map.new(Graph.nodes(graph), fn n -> {n, 0} end)
    Enum.reduce(graph, base, fn {_node, neighbors}, acc ->
      Enum.reduce(neighbors, acc, fn neighbor, d ->
        Map.update(d, neighbor, 1, &(&1 + 1))
      end)
    end)
  end
end
```

### Step 4: `lib/gateway_core/graph/cycle_detector.ex`

```elixir
defmodule GatewayCore.Graph.CycleDetector do
  alias GatewayCore.Graph

  @doc """
  Detects cycles in a directed graph using 3-color DFS.

  Colors:
    :white — not yet visited
    :gray  — currently in the DFS stack (ancestor)
    :black — fully processed

  A back edge (gray → gray) indicates a cycle.
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

  # TODO: implement dfs/3 with 3-color marking
  # Returns {:cycle, colors} or {:ok, colors}
  defp dfs(graph, node, colors) do
    colors = Map.put(colors, node, :gray)
    result = graph
    |> Graph.neighbors(node)
    |> Enum.reduce_while({:ok, colors}, fn neighbor, {:ok, c} ->
      case c[neighbor] do
        :gray  -> {:halt, {:cycle, c}}
        :white ->
          case dfs(graph, neighbor, c) do
            {:cycle, _} = cycle -> {:halt, cycle}
            {:ok, c}            -> {:cont, {:ok, c}}
          end
        :black -> {:cont, {:ok, c}}
      end
    end)

    case result do
      {:cycle, c}  -> {:cycle, c}
      {:ok, c}     -> {:ok, Map.put(c, node, :black)}
    end
  end
end
```

### Step 5: `lib/gateway_core/graph/dijkstra.ex`

```elixir
defmodule GatewayCore.Graph.Dijkstra do
  alias GatewayCore.Graph

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

  # TODO: implement do_dijkstra/4
  # For each node extracted from the priority queue (min distance first):
  #   1. Skip if already visited
  #   2. For each neighbor {n, w}: if dist[node] + w < dist[n], update dist[n] and enqueue

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

  # TODO: implement do_dijkstra_with_prev/5 — same as above but also tracks prev map
  defp do_dijkstra_with_prev(graph, pq, dist, prev, visited) do
    # HINT: when updating dist[neighbor], also set prev[neighbor] = node
  end

  defp reconstruct(prev, start, target) do
    do_reconstruct(prev, start, target, [target])
  end

  defp do_reconstruct(_prev, start, start, path), do: path
  defp do_reconstruct(prev, start, node, path) do
    do_reconstruct(prev, start, prev[node], [prev[node] | path])
  end
end
```

### Step 6: Gateway application — dependency resolver

```elixir
# lib/gateway_core/service_dependency_resolver.ex
defmodule GatewayCore.ServiceDependencyResolver do
  alias GatewayCore.Graph
  alias GatewayCore.Graph.{TopologicalSort, CycleDetector}

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
      {:error, {:circular, find_cycle(graph)}}
    else
      {:ok, sorted} = TopologicalSort.sort(graph)
      {:ok, sorted}
    end
  end

  defp build_graph(service_deps) do
    Enum.reduce(service_deps, Graph.new(), fn {service, deps}, g ->
      # Ensure service node exists even with no deps
      g = Map.put_new(g, service, [])
      Enum.reduce(deps, g, fn dep, g ->
        Graph.add_directed_edge(g, dep, service)
      end)
    end)
  end

  defp find_cycle(graph) do
    # TODO: trace one cycle path (DFS starting from first :gray node encountered)
    # Return the list of nodes forming the cycle
    []
  end
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/gateway_core/graph/bfs_test.exs
defmodule GatewayCore.Graph.BFSTest do
  use ExUnit.Case

  alias GatewayCore.Graph
  alias GatewayCore.Graph.BFS

  test "visits all reachable nodes" do
    g = Graph.new()
    |> Graph.add_edge("a", "b")
    |> Graph.add_edge("b", "c")
    |> Graph.add_edge("d", "e")   # separate component

    {order, _} = BFS.traverse(g, "a")
    assert Enum.sort(order) == ["a", "b", "c"]
  end

  test "connected_components finds isolated components" do
    g = Graph.new()
    |> Graph.add_edge("a", "b")
    |> Graph.add_edge("c", "d")
    |> Map.put("e", [])           # isolated node

    components = BFS.connected_components(g)
    assert length(components) == 3
  end

  test "shortest_path returns correct hop count" do
    g = Graph.new()
    |> Graph.add_edge("a", "b")
    |> Graph.add_edge("b", "c")
    |> Graph.add_edge("a", "c")   # shortcut: a→c in 1 hop

    assert {:ok, ["a", "c"], 1} = BFS.shortest_path(g, "a", "c")
  end

  test "returns no_path for disconnected nodes" do
    g = Graph.new()
    |> Graph.add_edge("a", "b")
    |> Map.put("z", [])

    assert {:error, :no_path} = BFS.shortest_path(g, "a", "z")
  end
end
```

```elixir
# test/gateway_core/graph/topological_sort_test.exs
defmodule GatewayCore.Graph.TopologicalSortTest do
  use ExUnit.Case

  alias GatewayCore.Graph
  alias GatewayCore.Graph.TopologicalSort

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
    {:ok, order} = GatewayCore.ServiceDependencyResolver.resolve(deps)
    assert Enum.find_index(order, &(&1 == "users")) <
           Enum.find_index(order, &(&1 == "auth"))
    assert Enum.find_index(order, &(&1 == "auth")) <
           Enum.find_index(order, &(&1 == "payments"))
  end

  test "resolver detects circular dependency" do
    deps = %{"a" => ["b"], "b" => ["c"], "c" => ["a"]}
    assert {:error, {:circular, _}} = GatewayCore.ServiceDependencyResolver.resolve(deps)
  end
end
```

```elixir
# test/gateway_core/graph/dijkstra_test.exs
defmodule GatewayCore.Graph.DijkstraTest do
  use ExUnit.Case

  alias GatewayCore.Graph.Dijkstra

  @graph %{
    "a" => [{"b", 4}, {"c", 2}],
    "b" => [{"d", 3}],
    "c" => [{"b", 1}, {"d", 10}],
    "d" => []
  }

  test "finds shortest path" do
    # a→c (2) + c→b (1) + b→d (3) = 6  vs  a→b (4) + b→d (3) = 7
    assert {:ok, 6, ["a", "c", "b", "d"]} = Dijkstra.shortest_path(@graph, "a", "d")
  end

  test "returns no_path for unreachable node" do
    g = %{"a" => [{"b", 1}], "b" => [], "z" => []}
    assert {:error, :no_path} = Dijkstra.shortest_path(g, "a", "z")
  end
end
```

### Step 8: Benchmark

```elixir
# bench/graph_bench.exs
Mix.install([{:benchee, "~> 1.3"}])

alias GatewayCore.Graph
alias GatewayCore.Graph.{BFS, Dijkstra}

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

```bash
mix run bench/graph_bench.exs
```

**Expected result**: BFS on 1k nodes < 2ms; on 10k nodes < 30ms. If your BFS is 10× slower,
check whether you're accidentally copying the entire visited set on every iteration instead
of using `MapSet` accumulation inside `reduce`.

---

## Trade-off analysis

| Aspect | Map-based functional graph | :array ETS-backed graph | NIF (Rust/C) |
|--------|---------------------------|------------------------|-------------|
| Mutability | immutable (new map per update) | mutable (in-place) | mutable |
| Memory | proportional to node count | proportional | proportional |
| BFS 10k nodes | ~20ms | ~5ms | ~0.5ms |
| Parallelism | multiple readers, functional | concurrent ETS reads | unsafe without care |
| Code simplicity | high | medium | low |

Reflection: the functional graph creates a new `MapSet` on every BFS step. Where does the
GC pressure show up in the Benchee memory_time output? At what graph size would you switch
to an ETS-backed representation?

---

## Common production mistakes

**1. Recursive DFS without tail-call optimization**
The cycle detector uses recursive DFS. For graphs with deep paths (thousands of nodes),
recursive DFS overflows the process stack. Elixir's default process stack is ~1MB.
Iterative DFS with an explicit stack is the correct approach for large graphs.

**2. Not handling disconnected graphs in topological sort**
Kahn's algorithm initializes from nodes with in-degree 0. If your graph has isolated nodes
not reachable from any zero-degree node (shouldn't happen, but defensive code matters),
they'll be missing from the result. Always compare `length(result)` with `map_size(graph)`.

**3. Using `List.last/1` for priority queue minimum**
`List.last/1` is O(n). For Dijkstra's priority queue, use `:gb_sets` (balanced BST) or
`:gb_trees` — both provide O(log n) minimum extraction.

**4. Forgetting that BFS distances are hop counts, not weights**
`BFS.shortest_path` gives the minimum number of hops. For weighted graphs, this is wrong —
use Dijkstra. Mixing them up produces silently incorrect results.

---

## Resources

- [`:queue` module — Erlang/OTP](https://www.erlang.org/doc/man/queue.html) — functional FIFO queue
- [`:gb_sets` module — Erlang/OTP](https://www.erlang.org/doc/man/gb_sets.html) — balanced BST used as priority queue
- [Introduction to Algorithms — CLRS](https://mitpress.mit.edu/9780262046305/introduction-to-algorithms/) — BFS, DFS, Dijkstra (chapters 22–24)
- [libgraph](https://github.com/bitwalker/libgraph) — production-grade graph library for Elixir (study after implementing yourself)
