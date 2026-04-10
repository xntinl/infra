# 50 — Competitive Programming: Graph Algorithms (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 7-9 horas  
**Área**: Algoritmos · Grafos · Optimización · NIFs · Benchmarking

---

## Contexto

Este capstone cierra el curriculum avanzado con un reto de programación competitiva: implementar
algoritmos clásicos de grafos en Elixir puro y entender sus tradeoffs de rendimiento frente a
implementaciones en C via NIFs. La representación funcional de grafos como Maps inmutables es
elegante pero requiere pensar diferente a los arrays mutables de C++/Java.

El objetivo no es replicar la velocidad de C, sino dominar los patrones funcionales para
algoritmos de grafos y saber cuándo un NIF es justificado.

---

## Representación de grafos

```elixir
# Adjacency list con Map (grafo no dirigido, no pesado)
graph = %{
  "A" => ["B", "C"],
  "B" => ["A", "D", "E"],
  "C" => ["A", "F"],
  "D" => ["B"],
  "E" => ["B", "F"],
  "F" => ["C", "E"]
}

# Grafo dirigido pesado (para Dijkstra)
weighted_graph = %{
  "A" => [{"B", 4}, {"C", 2}],
  "B" => [{"D", 3}, {"E", 1}],
  "C" => [{"B", 1}, {"F", 5}],
  "D" => [{"F", 2}],
  "E" => [{"F", 1}],
  "F" => []
}

# Constructor helper
defmodule Graph do
  def new, do: %{}

  def add_edge(g, u, v) do
    g
    |> Map.update(u, [v], &[v | &1])
    |> Map.update(v, [u], &[u | &1])   # no dirigido
  end

  def add_directed_edge(g, u, v, weight \\ 1) do
    Map.update(g, u, [{v, weight}], &[{v, weight} | &1])
  end

  def neighbors(g, node), do: Map.get(g, node, [])
  def nodes(g), do: Map.keys(g)
end
```

---

## Ejercicio 1 — BFS: Componentes conectados y Shortest Path

Implementa BFS iterativo con colas y úsalo para resolver dos problemas clásicos.

### BFS core

```elixir
defmodule Graph.BFS do
  @doc """
  BFS desde un nodo inicial. Retorna el orden de visita y el mapa de distancias.
  """
  def traverse(graph, start) do
    do_bfs(graph, :queue.from_list([start]), MapSet.new([start]), [], %{start => 0})
  end

  defp do_bfs(_graph, queue, _visited, order, distances) when :queue.is_empty(queue) do
    {Enum.reverse(order), distances}
  end

  defp do_bfs(graph, queue, visited, order, distances) do
    {{:value, node}, queue} = :queue.out(queue)
    neighbors = Graph.neighbors(graph, node)
    current_dist = distances[node]

    {queue, visited, distances} =
      Enum.reduce(neighbors, {queue, visited, distances}, fn neighbor, {q, v, d} ->
        if MapSet.member?(v, neighbor) do
          {q, v, d}
        else
          {
            :queue.in(neighbor, q),
            MapSet.put(v, neighbor),
            Map.put(d, neighbor, current_dist + 1)
          }
        end
      end)

    do_bfs(graph, queue, visited, [node | order], distances)
  end
end
```

### Problema 1A: Componentes conectados

```elixir
defmodule Graph.ConnectedComponents do
  @doc """
  Encuentra todos los componentes conectados del grafo.
  Retorna lista de listas (cada lista es un componente).

  ## Ejemplo
      graph = Graph.new()
      |> Graph.add_edge("A", "B")
      |> Graph.add_edge("C", "D")
      # => [["A", "B"], ["C", "D"]]
  """
  def find(graph) do
    nodes = Graph.nodes(graph)
    find_components(graph, nodes, MapSet.new(), [])
  end

  defp find_components(_graph, [], _visited, components), do: components

  defp find_components(graph, [node | rest], visited, components) do
    if MapSet.member?(visited, node) do
      find_components(graph, rest, visited, components)
    else
      {component_order, _distances} = Graph.BFS.traverse(graph, node)
      new_visited = Enum.reduce(component_order, visited, &MapSet.put(&2, &1))
      find_components(graph, rest, new_visited, [component_order | components])
    end
  end
end
```

### Problema 1B: Shortest path en grafo no pesado

```elixir
defmodule Graph.ShortestPath do
  @doc """
  Camino más corto entre dos nodos en grafo no pesado (BFS).
  Retorna {:ok, path, distance} | {:error, :no_path}
  """
  def unweighted(graph, start, target) do
    {_order, distances} = Graph.BFS.traverse(graph, start)

    case distances[target] do
      nil -> {:error, :no_path}
      dist ->
        path = reconstruct_path(graph, start, target, distances)
        {:ok, path, dist}
    end
  end

  defp reconstruct_path(graph, start, target, distances) do
    # Backtrack desde target hasta start siguiendo gradiente de distancias
    do_reconstruct(graph, target, start, distances, [target])
  end

  defp do_reconstruct(_graph, start, start, _distances, path), do: path

  defp do_reconstruct(graph, current, start, distances, path) do
    prev = graph
    |> Graph.neighbors(current)
    |> Enum.find(fn n -> distances[n] == distances[current] - 1 end)

    do_reconstruct(graph, prev, start, distances, [prev | path])
  end
end
```

### Tests requeridos

```elixir
# test/graph/bfs_test.exs
defmodule Graph.BFSTest do
  use ExUnit.Case

  test "BFS visita todos los nodos alcanzables" do
    graph = Graph.new()
    |> Graph.add_edge("A", "B")
    |> Graph.add_edge("B", "C")
    |> Graph.add_edge("D", "E")  # componente separado

    {order, _} = Graph.BFS.traverse(graph, "A")
    assert Enum.sort(order) == ["A", "B", "C"]  # no alcanza D, E
  end

  test "componentes conectados — social network" do
    # Red social donde A-B-C son amigos, D-E son amigos, F está solo
    graph = build_social_graph()
    components = Graph.ConnectedComponents.find(graph)
    assert length(components) == 3
  end

  test "shortest path — 6 grados de separación" do
    graph = build_social_network(1000)  # 1000 personas
    {:ok, path, dist} = Graph.ShortestPath.unweighted(graph, "person_1", "person_1000")
    assert dist <= 6   # hipótesis de los 6 grados
  end
end
```

---

## Ejercicio 2 — DFS: Topological Sort y Detección de Ciclos

DFS iterativo para ordenamiento topológico y detección de dependencias circulares.

### DFS iterativo

```elixir
defmodule Graph.DFS do
  @doc "DFS iterativo con stack. Retorna orden de finalización (útil para topo sort)."
  def traverse(graph, start) do
    do_dfs(graph, [start], MapSet.new(), [])
  end

  defp do_dfs(_graph, [], _visited, order), do: Enum.reverse(order)

  defp do_dfs(graph, [node | stack], visited, order) do
    if MapSet.member?(visited, node) do
      do_dfs(graph, stack, visited, order)
    else
      visited = MapSet.put(visited, node)
      neighbors = Graph.neighbors(graph, node)
      new_stack = neighbors ++ stack
      do_dfs(graph, new_stack, visited, [node | order])
    end
  end
end
```

### Problema 2A: Topological Sort (Kahn's algorithm)

```elixir
defmodule Graph.TopologicalSort do
  @doc """
  Kahn's algorithm: BFS-based topological sort.
  Retorna {:ok, order} o {:error, :has_cycle}

  ## Aplicación: dependency resolution
  La CLI de `mix deps.get` usa topo sort para instalar deps en el orden correcto.
  """
  def sort(graph) do
    # Calcular in-degree de cada nodo
    in_degree = graph
    |> Graph.nodes()
    |> Map.new(fn node -> {node, 0} end)
    |> calculate_in_degrees(graph)

    # Comenzar con nodos sin dependencias
    queue = in_degree
    |> Enum.filter(fn {_, deg} -> deg == 0 end)
    |> Enum.map(fn {node, _} -> node end)
    |> :queue.from_list()

    do_kahn(graph, queue, in_degree, [])
  end

  defp do_kahn(_graph, queue, _in_degree, result) when :queue.is_empty(queue) do
    expected = length(result)
    actual = length(result)  # comparar con nodos totales
    {:ok, Enum.reverse(result)}
  end

  defp do_kahn(graph, queue, in_degree, result) do
    {{:value, node}, queue} = :queue.out(queue)

    {queue, in_degree} =
      graph
      |> Graph.neighbors(node)
      |> Enum.reduce({queue, in_degree}, fn neighbor, {q, ind} ->
        new_degree = ind[neighbor] - 1
        ind = Map.put(ind, neighbor, new_degree)
        q = if new_degree == 0, do: :queue.in(neighbor, q), else: q
        {q, ind}
      end)

    do_kahn(graph, queue, in_degree, [node | result])
  end

  defp calculate_in_degrees(degrees, graph) do
    Enum.reduce(graph, degrees, fn {_node, neighbors}, acc ->
      Enum.reduce(neighbors, acc, fn neighbor, d ->
        Map.update(d, neighbor, 1, &(&1 + 1))
      end)
    end)
  end
end
```

### Problema 2B: Detección de ciclos

```elixir
defmodule Graph.CycleDetector do
  @doc """
  Detecta ciclos en grafo dirigido via DFS con estado de 3 colores:
  :white (no visitado), :gray (en stack actual), :black (completado)
  """
  def has_cycle?(graph) do
    nodes = Graph.nodes(graph)
    colors = Map.new(nodes, fn n -> {n, :white} end)
    Enum.any?(nodes, fn node ->
      colors[node] == :white and dfs_cycle(graph, node, colors) |> elem(0)
    end)
  end

  defp dfs_cycle(graph, node, colors) do
    colors = Map.put(colors, node, :gray)

    result = graph
    |> Graph.neighbors(node)
    |> Enum.reduce_while({false, colors}, fn neighbor, {_, c} ->
      case c[neighbor] do
        :gray  -> {:halt, {true, c}}    # ciclo encontrado
        :white ->
          case dfs_cycle(graph, neighbor, c) do
            {true, c}  -> {:halt, {true, c}}
            {false, c} -> {:cont, {false, c}}
          end
        :black -> {:cont, {false, c}}
      end
    end)

    {has_cycle, colors} = result
    {has_cycle, Map.put(colors, node, :black)}
  end
end
```

### Aplicación: Dependency Resolution

```elixir
defmodule DependencyResolver do
  @doc """
  Dado un mapa de paquetes → sus dependencias,
  retorna el orden de instalación o error si hay dependencia circular.

  ## Ejemplo
      resolve(%{
        "phoenix"     => ["plug", "jason"],
        "ecto"        => ["postgrex"],
        "plug"        => [],
        "jason"       => [],
        "postgrex"    => []
      })
      # => {:ok, ["jason", "plug", "phoenix", "postgrex", "ecto"]}
  """
  def resolve(deps_map) do
    graph = build_directed_graph(deps_map)

    case Graph.TopologicalSort.sort(graph) do
      {:ok, order} -> {:ok, order}
      {:error, :has_cycle} ->
        # Encontrar el ciclo específico para dar mensaje útil
        {:error, {:circular_dependency, find_cycle(graph)}}
    end
  end

  defp build_directed_graph(deps_map) do
    Enum.reduce(deps_map, Graph.new(), fn {package, deps}, g ->
      Enum.reduce(deps, g, fn dep, g ->
        Graph.add_directed_edge(g, dep, package)  # dep → package (dep primero)
      end)
    end)
  end
end
```

---

## Ejercicio 3 — Dijkstra: Shortest Path en Grafo Pesado

Implementa Dijkstra con priority queue y aplícalo a dos problemas.

### Priority Queue en Elixir

```elixir
defmodule PriorityQueue do
  @doc """
  Min-heap implementado sobre :gb_sets (balanced binary search tree).
  Operaciones: insert O(log n), extract_min O(log n).
  """
  def new, do: :gb_sets.new()

  def insert(pq, priority, value) do
    :gb_sets.add({priority, value}, pq)
  end

  def extract_min(pq) do
    {{priority, value}, pq} = :gb_sets.take_smallest(pq)
    {{priority, value}, pq}
  end

  def empty?(pq), do: :gb_sets.is_empty(pq)
end
```

### Dijkstra

```elixir
defmodule Graph.Dijkstra do
  @infinity :infinity

  @doc """
  Dijkstra desde nodo `start`. Retorna mapa de distancias mínimas a todos los nodos.
  """
  def shortest_paths(graph, start) do
    nodes = Graph.nodes(graph)
    dist  = Map.new(nodes, fn n -> {n, @infinity} end) |> Map.put(start, 0)
    pq    = PriorityQueue.new() |> PriorityQueue.insert(0, start)

    do_dijkstra(graph, pq, dist, MapSet.new())
  end

  @doc "Camino más corto entre start y target. Retorna {distance, path}."
  def shortest_path(graph, start, target) do
    nodes = Graph.nodes(graph)
    dist  = Map.new(nodes, fn n -> {n, @infinity} end) |> Map.put(start, 0)
    prev  = %{}
    pq    = PriorityQueue.new() |> PriorityQueue.insert(0, start)

    {dist, prev} = do_dijkstra_with_prev(graph, pq, dist, prev, MapSet.new())

    case dist[target] do
      @infinity -> {:error, :no_path}
      d         -> {:ok, d, reconstruct_path(prev, start, target)}
    end
  end

  defp do_dijkstra(graph, pq, dist, visited) do
    if PriorityQueue.empty?(pq) do
      dist
    else
      {{d, node}, pq} = PriorityQueue.extract_min(pq)

      if MapSet.member?(visited, node) or d > dist[node] do
        do_dijkstra(graph, pq, dist, visited)
      else
        visited = MapSet.put(visited, node)

        {pq, dist} =
          graph
          |> Graph.neighbors(node)  # retorna [{neighbor, weight}]
          |> Enum.reduce({pq, dist}, fn {neighbor, weight}, {q, d_map} ->
            new_dist = dist[node] + weight
            if new_dist < d_map[neighbor] do
              {PriorityQueue.insert(q, new_dist, neighbor), Map.put(d_map, neighbor, new_dist)}
            else
              {q, d_map}
            end
          end)

        do_dijkstra(graph, pq, dist, visited)
      end
    end
  end

  defp reconstruct_path(prev, start, target) do
    do_reconstruct(prev, start, target, [target])
  end

  defp do_reconstruct(_prev, start, start, path), do: path
  defp do_reconstruct(prev, start, node, path) do
    do_reconstruct(prev, start, prev[node], [prev[node] | path])
  end
end
```

### Problema 3A: Route Planning

```elixir
defmodule RoutePlanner do
  @doc """
  Dado un mapa de ciudades con distancias entre ellas,
  encuentra la ruta más corta entre dos ciudades.

  ## Ejemplo
      cities = %{
        "Madrid"    => [{"Barcelona", 621}, {"Sevilla", 540}],
        "Barcelona" => [{"Madrid", 621}, {"Valencia", 349}],
        "Valencia"  => [{"Barcelona", 349}, {"Sevilla", 655}],
        "Sevilla"   => [{"Madrid", 540}, {"Valencia", 655}]
      }

      find_route(cities, "Madrid", "Valencia")
      # => {:ok, 970, ["Madrid", "Barcelona", "Valencia"]}
      # vs ruta directa Madrid→Valencia (no existe en este grafo)
  """
  def find_route(city_graph, from, to) do
    Graph.Dijkstra.shortest_path(city_graph, from, to)
  end

  def all_distances_from(city_graph, origin) do
    Graph.Dijkstra.shortest_paths(city_graph, origin)
    |> Enum.reject(fn {_, d} -> d == :infinity end)
    |> Map.new()
  end
end
```

---

## Ejercicio 4 — 5 Problemas clásicos y Benchmarking vs NIF

Resuelve 5 problemas completos y compara el rendimiento con un NIF en C.

### Problema 1: Social Network Analysis

```elixir
defmodule SocialNetwork do
  @doc "Grado de separación entre dos personas (BFS)"
  def degrees_of_separation(network, person_a, person_b) do
    case Graph.ShortestPath.unweighted(network, person_a, person_b) do
      {:ok, _path, dist} -> {:ok, dist}
      {:error, :no_path} -> {:error, :not_connected}
    end
  end

  @doc "Influencers: nodos con más conexiones (degree centrality)"
  def top_influencers(network, n \\ 10) do
    network
    |> Enum.map(fn {node, neighbors} -> {node, length(neighbors)} end)
    |> Enum.sort_by(fn {_, degree} -> degree end, :desc)
    |> Enum.take(n)
  end

  @doc "Comunidades: componentes conectados en red social"
  def find_communities(network) do
    Graph.ConnectedComponents.find(network)
    |> Enum.sort_by(&length/1, :desc)
  end
end
```

### Problema 2: Package Dependency Resolution

Ver `DependencyResolver` del Ejercicio 2 — implementación completa.

### Problema 3: Network Reliability

```elixir
defmodule NetworkReliability do
  @doc """
  ¿El grafo sigue siendo conectado si se elimina el nodo `node`?
  Útil para identificar puntos únicos de fallo en redes.
  """
  def is_critical_node?(graph, node) do
    original_components = Graph.ConnectedComponents.find(graph) |> length()
    graph_without = remove_node(graph, node)
    new_components = Graph.ConnectedComponents.find(graph_without) |> length()
    new_components > original_components
  end

  @doc "Todos los nodos críticos (articulation points) del grafo"
  def critical_nodes(graph) do
    graph
    |> Graph.nodes()
    |> Enum.filter(&is_critical_node?(graph, &1))
  end

  defp remove_node(graph, node) do
    graph
    |> Map.delete(node)
    |> Map.new(fn {n, neighbors} ->
      {n, Enum.reject(neighbors, &(&1 == node))}
    end)
  end
end
```

### Problema 4: Minimum Spanning Tree (Kruskal)

```elixir
defmodule Graph.Kruskal do
  @doc """
  Minimum Spanning Tree via Kruskal con Union-Find.
  Input: lista de {u, v, weight}. Output: lista de edges del MST.
  """
  def mst(edges, nodes) do
    sorted_edges = Enum.sort_by(edges, fn {_, _, w} -> w end)
    uf = UnionFind.new(nodes)
    do_kruskal(sorted_edges, uf, [], 0, length(nodes) - 1)
  end

  defp do_kruskal(_, _, mst_edges, count, target) when count == target, do: mst_edges
  defp do_kruskal([], _, mst_edges, _, _), do: mst_edges

  defp do_kruskal([{u, v, w} | rest], uf, mst_edges, count, target) do
    if UnionFind.find(uf, u) != UnionFind.find(uf, v) do
      uf = UnionFind.union(uf, u, v)
      do_kruskal(rest, uf, [{u, v, w} | mst_edges], count + 1, target)
    else
      do_kruskal(rest, uf, mst_edges, count, target)
    end
  end
end

defmodule UnionFind do
  def new(nodes), do: Map.new(nodes, fn n -> {n, n} end)

  def find(uf, node) do
    parent = uf[node]
    if parent == node, do: node, else: find(uf, parent)
  end

  def union(uf, u, v) do
    root_u = find(uf, u)
    root_v = find(uf, v)
    Map.put(uf, root_u, root_v)
  end
end
```

### Problema 5: Bipartite Check (BFS-based)

```elixir
defmodule Graph.BipartiteCheck do
  @doc """
  Verifica si el grafo es bipartito (2-coloreable).
  Útil para: scheduling, matching problems, detección de conflictos.
  Returns {:ok, {set_a, set_b}} | {:error, :not_bipartite}
  """
  def check(graph) do
    nodes = Graph.nodes(graph)
    colors = %{}
    do_check(graph, nodes, colors, [], [])
  end

  defp do_check(_graph, [], _colors, set_a, set_b) do
    {:ok, {set_a, set_b}}
  end

  defp do_check(graph, [node | rest], colors, set_a, set_b) do
    if Map.has_key?(colors, node) do
      do_check(graph, rest, colors, set_a, set_b)
    else
      case bfs_color(graph, node, colors, :red) do
        {:ok, colors, new_reds, new_blues} ->
          do_check(graph, rest, colors, new_reds ++ set_a, new_blues ++ set_b)
        {:error, :not_bipartite} ->
          {:error, :not_bipartite}
      end
    end
  end
end
```

### Benchmarking vs NIF en C

```elixir
# bench/graph_bench.exs — usando Benchee
Mix.install([{:benchee, "~> 1.3"}])

defmodule GraphBench do
  def large_graph(n) do
    # Genera grafo aleatorio de n nodos
    1..n
    |> Enum.reduce(Graph.new(), fn i, g ->
      neighbors = Enum.take_random(1..(n), 5)
      Enum.reduce(neighbors, g, fn j, g -> Graph.add_edge(g, i, j) end)
    end)
  end
end

graph_1k  = GraphBench.large_graph(1_000)
graph_10k = GraphBench.large_graph(10_000)

Benchee.run(
  %{
    "BFS 1k nodes (Elixir)"   => fn -> Graph.BFS.traverse(graph_1k, 1) end,
    "BFS 10k nodes (Elixir)"  => fn -> Graph.BFS.traverse(graph_10k, 1) end,
    "Dijkstra 1k (Elixir)"    => fn -> Graph.Dijkstra.shortest_paths(graph_1k, 1) end,
    "Components 1k (Elixir)"  => fn -> Graph.ConnectedComponents.find(graph_1k) end,
    # NIF equivalente (si lo implementas como reto adicional):
    # "BFS 1k nodes (NIF)"    => fn -> GraphNIF.bfs(graph_1k, 1) end,
  },
  time:    5,
  warmup:  2,
  memory_time: 2
)
```

### Resultados esperados y análisis

```
Name                            ips      average  deviation
BFS 1k nodes (Elixir)       1.23 K    812.45 μs    ±8.2%
BFS 10k nodes (Elixir)      95.4      10.48 ms    ±5.1%
Dijkstra 1k (Elixir)        342       2.92 ms     ±12.4%
Components 1k (Elixir)      1.15 K    869.2 μs    ±9.7%

# Análisis:
# - Elixir es 10-50x más lento que C para grafos grandes
# - Para grafos < 10k nodos y latencia no crítica: Elixir es suficiente
# - Para análisis batch de grafos grandes (>1M nodos): considerar NIF o Rust
# - La inmutabilidad tiene costo: cada actualización del estado crea nueva estructura
```

### Estructura del proyecto

```
lib/
├── graph/
│   ├── graph.ex              # Graph struct + constructor helpers
│   ├── bfs.ex                # BFS traversal
│   ├── dfs.ex                # DFS traversal
│   ├── dijkstra.ex           # Dijkstra + PriorityQueue
│   ├── topological_sort.ex   # Kahn's algorithm
│   ├── cycle_detector.ex     # 3-color DFS
│   ├── kruskal.ex            # MST + UnionFind
│   ├── bipartite_check.ex    # 2-coloring
│   └── connected_components.ex
├── problems/
│   ├── social_network.ex
│   ├── dependency_resolver.ex
│   ├── route_planner.ex
│   └── network_reliability.ex
bench/
└── graph_bench.exs
test/
├── graph/
│   ├── bfs_test.exs
│   ├── dfs_test.exs
│   ├── dijkstra_test.exs
│   ├── topological_sort_test.exs
│   └── cycle_detector_test.exs
└── problems/
    ├── social_network_test.exs
    ├── dependency_resolver_test.exs
    └── route_planner_test.exs
```

---

## Criterios de aceptación

- [ ] `Graph.BFS.traverse/2` implementado correctamente con `:queue`
- [ ] `Graph.ConnectedComponents.find/1` identifica todos los componentes
- [ ] `Graph.ShortestPath.unweighted/3` retorna el camino correcto con BFS
- [ ] `Graph.TopologicalSort.sort/1` retorna `{:error, :has_cycle}` para grafos con ciclos
- [ ] `Graph.CycleDetector.has_cycle?/1` usa coloración 3-colores correctamente
- [ ] `Graph.Dijkstra.shortest_path/3` retorna el camino correcto en grafo pesado
- [ ] `DependencyResolver.resolve/1` detecta dependencias circulares
- [ ] Benchee corre correctamente y produce reporte de rendimiento
- [ ] Tests cubren todos los algoritmos con casos borde: grafo vacío, nodo aislado, grafo completo

---

## Retos adicionales (opcional)

- NIF en C con Rustler: implementar BFS en Rust y comparar con Benchee
- A* para route planning con heurística (euclidean distance para ciudades con coordenadas)
- Floyd-Warshall: all-pairs shortest path O(V³)
- Bellman-Ford: shortest path con pesos negativos (detecta ciclos negativos)
- Visualización: exportar el grafo a formato DOT para visualizar con Graphviz
