# Graph Database with Cypher-Like Query Language

**Project**: `graphdb` — a property graph database with Cypher-like query language and traversal algorithms

---

## Project context

You are building `graphdb`, a property graph database in Elixir with its own query language parser. The database stores nodes and directed edges with arbitrary properties, supports BFS/DFS traversal, executes Dijkstra's algorithm, and parses a meaningful subset of Cypher.

Project structure:

```
graphdb/
├── lib/
│   └── graphdb/
│       ├── application.ex           # database supervisor
│       ├── database.ex              # public API: create_node, create_edge, neighbors, query
│       ├── storage.ex               # three ETS tables: nodes, edges, adjacency
│       ├── traversal.ex             # BFS, DFS with configurable depth and direction
│       ├── dijkstra.ex              # shortest path: weighted and unweighted
│       ├── parser.ex                # Cypher-like query parser (recursive descent)
│       ├── planner.ex               # query plan from parsed AST
│       ├── executor.ex              # executes query plan against storage
│       └── transaction.ex           # diff log for rollback: inverse operations
├── test/
│   └── graphdb/
│       ├── storage_test.exs         # CRUD, neighbor lookups
│       ├── traversal_test.exs       # BFS/DFS correctness, depth limiting
│       ├── dijkstra_test.exs        # shortest path, disconnected graph
│       ├── query_test.exs           # Cypher-like query execution
│       └── transaction_test.exs    # commit and rollback correctness
├── bench/
│   └── graphdb_bench.exs
└── mix.exs
```

---

## The problem

Relational databases represent relationships as foreign keys — finding all people who follow a given person within 3 hops requires multiple self-joins that are expensive to plan and execute. Graph databases represent relationships natively as edges: traversal is a first-class operation, not a query optimization problem. A 3-hop traversal in a property graph is O(degree^3) — the query engine walks the adjacency lists directly.

---

## Why this design

**Three ETS tables**: nodes (`{id, labels, props}`), edges (`{id, from, to, type, props}`), adjacency (`{from_id, to_id, edge_id, type}`). The adjacency table is the key to O(degree) neighbor lookups — `:ets.match_object/2` with the `from_id` field bound returns all outgoing edges without scanning all edges. Add a second adjacency table for incoming edges to support bidirectional traversal.

**Recursive descent for the query parser**: Cypher is a context-free grammar with recursive patterns (`MATCH`, `WHERE`, `RETURN`). A hand-rolled recursive descent parser processes the token stream and builds an AST that the planner and executor consume.

**Transaction diff log for rollback**: instead of MVCC (expensive for graph operations), record the inverse of every mutation: `{:delete_node, id}` for a `CREATE NODE`, `{:delete_edge, id}` for a `CREATE EDGE`. On rollback, apply inverses in reverse order. This is correct for within-transaction rollback but does not support concurrent readers seeing a consistent snapshot.

---

## Design decisions

**Option A — Adjacency matrix (dense)**
- Pros: O(1) edge lookup; fast matrix algebra.
- Cons: O(V²) memory — impossible past a few thousand nodes.

**Option B — Adjacency list with per-vertex ETS tables** (chosen)
- Pros: O(V + E) memory; scales to millions of vertices; traversal is natural.
- Cons: edge-existence checks are O(deg(v)) without a secondary index.

→ Chose **B** because real graph databases are sparse by construction; paying O(V²) memory for dense-matrix ergonomics isn't acceptable for any non-trivial dataset.

## Implementation milestones

### Step 1: Create the project

**Objective**: Generate `--sup` skeleton so storage, traversal workers, and query executor share one supervision tree from boot.


```bash
mix new graphdb --sup
cd graphdb
mkdir -p lib/graphdb test/graphdb bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Keep deps to `:benchee` only; ETS, `:queue`, and `:gb_sets` ship with OTP and cover every storage and traversal primitive.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Storage layer

**Objective**: Back nodes, edges, and bidirectional adjacency lists with four ETS tables so traversal is O(deg) and reverse lookups are free.


```elixir
# lib/graphdb/storage.ex
defmodule GraphDB.Storage do
  @moduledoc """
  ETS-backed storage for nodes, edges, and adjacency lists.

  Four ETS tables:
    - nodes:   {id, labels, props}         — :set
    - edges:   {id, from_id, to_id, type, props} — :set
    - adj_out: {from_id, to_id, edge_id, type}   — :bag
    - adj_in:  {to_id, from_id, edge_id, type}   — :bag
  """

  @nodes     :graphdb_nodes
  @edges     :graphdb_edges
  @adj_out   :graphdb_adj_out
  @adj_in    :graphdb_adj_in

  @doc "Initializes the four ETS tables."
  @spec init() :: :ok
  def init do
    :ets.new(@nodes,   [:named_table, :public, :set])
    :ets.new(@edges,   [:named_table, :public, :set])
    :ets.new(@adj_out, [:named_table, :public, :bag])
    :ets.new(@adj_in,  [:named_table, :public, :bag])
    :ok
  end

  @doc "Clears and reinitializes all tables."
  @spec reset() :: :ok
  def reset do
    for table <- [@nodes, @edges, @adj_out, @adj_in] do
      try do
        :ets.delete(table)
      rescue
        ArgumentError -> :ok
      end
    end
    init()
  end

  @doc "Creates a node. Returns the generated node ID."
  @spec create_node(list(), map()) :: pos_integer()
  def create_node(labels, props) do
    id = :erlang.unique_integer([:positive])
    :ets.insert(@nodes, {id, labels, props})
    id
  end

  @doc "Creates an edge between two nodes."
  @spec create_edge(pos_integer(), pos_integer(), atom(), map()) :: pos_integer()
  def create_edge(from_id, to_id, type, props) do
    id = :erlang.unique_integer([:positive])
    :ets.insert(@edges, {id, from_id, to_id, type, props})
    :ets.insert(@adj_out, {from_id, to_id, id, type})
    :ets.insert(@adj_in, {to_id, from_id, id, type})
    id
  end

  @doc "Returns the node tuple {id, labels, props} or nil."
  @spec get_node(pos_integer()) :: tuple() | nil
  def get_node(id) do
    case :ets.lookup(@nodes, id) do
      [node] -> node
      [] -> nil
    end
  end

  @doc "Returns the edge tuple or nil."
  @spec get_edge(pos_integer()) :: tuple() | nil
  def get_edge(id) do
    case :ets.lookup(@edges, id) do
      [edge] -> edge
      [] -> nil
    end
  end

  @doc "Returns all outgoing neighbors of node_id as [{to_id, edge_id, type}]."
  @spec outgoing(pos_integer()) :: [{pos_integer(), pos_integer(), atom()}]
  def outgoing(node_id) do
    :ets.match_object(@adj_out, {node_id, :_, :_, :_})
    |> Enum.map(fn {_from, to, eid, type} -> {to, eid, type} end)
  end

  @doc "Returns all incoming neighbors of node_id as [{from_id, edge_id, type}]."
  @spec incoming(pos_integer()) :: [{pos_integer(), pos_integer(), atom()}]
  def incoming(node_id) do
    :ets.match_object(@adj_in, {node_id, :_, :_, :_})
    |> Enum.map(fn {_to, from, eid, type} -> {from, eid, type} end)
  end

  @doc "Returns neighbors in both directions."
  @spec neighbors(pos_integer(), :out | :in | :both) :: [{pos_integer(), pos_integer(), atom()}]
  def neighbors(node_id, :both), do: outgoing(node_id) ++ incoming(node_id)
  def neighbors(node_id, :out),  do: outgoing(node_id)
  def neighbors(node_id, :in),   do: incoming(node_id)

  @doc "Returns all node tuples."
  @spec all_nodes() :: [tuple()]
  def all_nodes, do: :ets.tab2list(@nodes)

  @doc "Returns all nodes matching a label."
  @spec nodes_by_label(atom()) :: [tuple()]
  def nodes_by_label(label) do
    :ets.tab2list(@nodes)
    |> Enum.filter(fn {_id, labels, _props} -> label in labels end)
  end
end
```

### Step 4: Traversal algorithms

**Objective**: Run BFS with an Erlang `:queue` and DFS with an explicit stack so traversal stays tail-recursive and cycle-safe under depth limits.


```elixir
# lib/graphdb/traversal.ex
defmodule GraphDB.Traversal do
  @moduledoc """
  BFS and DFS traversal algorithms with configurable depth and direction.
  """

  @doc """
  BFS traversal starting from start_node_id.
  Returns [{node_id, depth}] in BFS order, up to max_depth.
  """
  @spec bfs(pos_integer(), non_neg_integer(), :out | :in | :both) :: [{pos_integer(), non_neg_integer()}]
  def bfs(start_node_id, max_depth, direction \\ :out) do
    queue = :queue.in({start_node_id, 0}, :queue.new())
    visited = MapSet.new([start_node_id])
    bfs_loop(queue, visited, max_depth, direction, [{start_node_id, 0}])
  end

  defp bfs_loop(queue, visited, max_depth, direction, acc) do
    case :queue.out(queue) do
      {:empty, _} ->
        acc

      {{:value, {node_id, depth}}, rest_queue} ->
        if depth >= max_depth do
          bfs_loop(rest_queue, visited, max_depth, direction, acc)
        else
          neighbors = GraphDB.Storage.neighbors(node_id, direction)

          {new_queue, new_visited, new_acc} =
            Enum.reduce(neighbors, {rest_queue, visited, acc}, fn {neighbor_id, _eid, _type}, {q, vis, a} ->
              if MapSet.member?(vis, neighbor_id) do
                {q, vis, a}
              else
                new_q = :queue.in({neighbor_id, depth + 1}, q)
                new_vis = MapSet.put(vis, neighbor_id)
                new_a = a ++ [{neighbor_id, depth + 1}]
                {new_q, new_vis, new_a}
              end
            end)

          bfs_loop(new_queue, new_visited, max_depth, direction, new_acc)
        end
    end
  end

  @doc """
  DFS traversal starting from start_node_id.
  Returns [node_id] in DFS order, up to max_depth.
  """
  @spec dfs(pos_integer(), non_neg_integer(), :out | :in | :both) :: [pos_integer()]
  def dfs(start_node_id, max_depth, direction \\ :out) do
    dfs_loop([{start_node_id, 0}], MapSet.new(), max_depth, direction, [])
    |> Enum.reverse()
  end

  defp dfs_loop([], _visited, _max_depth, _direction, acc), do: acc

  defp dfs_loop([{node_id, depth} | rest], visited, max_depth, direction, acc) do
    if MapSet.member?(visited, node_id) do
      dfs_loop(rest, visited, max_depth, direction, acc)
    else
      new_visited = MapSet.put(visited, node_id)
      new_acc = [node_id | acc]

      if depth < max_depth do
        neighbors = GraphDB.Storage.neighbors(node_id, direction)
        children = Enum.map(neighbors, fn {nid, _eid, _type} -> {nid, depth + 1} end)
        dfs_loop(children ++ rest, new_visited, max_depth, direction, new_acc)
      else
        dfs_loop(rest, new_visited, max_depth, direction, new_acc)
      end
    end
  end
end
```

### Step 5: Dijkstra's shortest path

**Objective**: Drive Dijkstra with a `:gb_sets` priority queue so shortest path stays O((V+E) log V) on weighted graphs.


```elixir
# lib/graphdb/dijkstra.ex
defmodule GraphDB.Dijkstra do
  @moduledoc """
  Shortest path using Dijkstra's algorithm with :gb_sets as a priority queue.

  For unweighted graphs, all edges have weight 1.
  For weighted graphs, the weight_fn extracts weight from edge props.
  """

  @doc """
  Returns the shortest path from source to target as a list of node IDs.
  weight_fn receives the edge props map and returns a numeric weight.
  """
  @spec shortest_path(pos_integer(), pos_integer(), (map() -> number())) ::
          {:ok, [pos_integer()]} | {:error, :no_path}
  def shortest_path(source, target, weight_fn \\ fn _props -> 1 end) do
    dist = %{source => 0}
    prev = %{}
    pq = :gb_sets.singleton({0, source})

    case dijkstra_loop(pq, dist, prev, target, weight_fn) do
      {:found, final_prev} ->
        path = reconstruct_path(final_prev, source, target)
        {:ok, path}

      :not_found ->
        {:error, :no_path}
    end
  end

  defp dijkstra_loop(pq, dist, prev, target, weight_fn) do
    if :gb_sets.is_empty(pq) do
      :not_found
    else
      {{current_dist, current_node}, rest_pq} = :gb_sets.take_smallest(pq)

      if current_node == target do
        {:found, prev}
      else
        if current_dist > Map.get(dist, current_node, :infinity) do
          dijkstra_loop(rest_pq, dist, prev, target, weight_fn)
        else
          neighbors = GraphDB.Storage.outgoing(current_node)

          {new_pq, new_dist, new_prev} =
            Enum.reduce(neighbors, {rest_pq, dist, prev}, fn {neighbor_id, edge_id, _type}, {q, d, p} ->
              {_, _, _, _, edge_props} = GraphDB.Storage.get_edge(edge_id)
              weight = weight_fn.(edge_props)
              alt = current_dist + weight

              if alt < Map.get(d, neighbor_id, :infinity) do
                new_q = :gb_sets.add({alt, neighbor_id}, q)
                new_d = Map.put(d, neighbor_id, alt)
                new_p = Map.put(p, neighbor_id, current_node)
                {new_q, new_d, new_p}
              else
                {q, d, p}
              end
            end)

          dijkstra_loop(new_pq, new_dist, new_prev, target, weight_fn)
        end
      end
    end
  end

  defp reconstruct_path(prev, source, target) do
    do_reconstruct(prev, source, target, [target])
  end

  defp do_reconstruct(_prev, source, source, path), do: path

  defp do_reconstruct(prev, source, current, path) do
    predecessor = Map.fetch!(prev, current)
    do_reconstruct(prev, source, predecessor, [predecessor | path])
  end
end
```

### Step 6: Cypher-like query parser and executor

**Objective**: Recursive-descent parse Cypher-like MATCH/WHERE/RETURN into an AST so the executor plans traversals without re-tokenizing per query.


```elixir
# lib/graphdb/parser.ex
defmodule GraphDB.Parser do
  @moduledoc """
  Recursive descent parser for a Cypher-like query language.

  Supported syntax:
    MATCH (n:Label) RETURN n
    MATCH (n:Label) WHERE n.prop > value RETURN n.prop
    MATCH (n:Label)-[:TYPE]->(m:Label) WHERE n.prop = value RETURN m.prop
  """

  @doc "Parses a Cypher-like query string into an AST map."
  @spec parse(String.t()) :: {:ok, map()} | {:error, String.t()}
  def parse(query) do
    tokens = tokenize(query)
    parse_match(tokens)
  end

  defp tokenize(query) do
    query
    |> String.replace("(", " ( ")
    |> String.replace(")", " ) ")
    |> String.replace("[", " [ ")
    |> String.replace("]", " ] ")
    |> String.replace("->", " -> ")
    |> String.replace("-", " - ")
    |> String.replace(":", " : ")
    |> String.replace(".", " . ")
    |> String.replace(",", " , ")
    |> String.replace("'", " ' ")
    |> String.split(~r/\s+/, trim: true)
  end

  defp parse_match(["MATCH" | rest]) do
    {match_pattern, rest} = parse_pattern(rest)

    {where_clause, rest} =
      case rest do
        ["WHERE" | wr] -> parse_where(wr)
        _ -> {nil, rest}
      end

    {return_clause, _rest} =
      case rest do
        ["RETURN" | rr] -> parse_return(rr)
        _ -> {nil, []}
      end

    {:ok, %{
      match: match_pattern,
      where: where_clause,
      return: return_clause
    }}
  end

  defp parse_match(_), do: {:error, "expected MATCH"}

  defp parse_pattern(tokens) do
    {node, rest} = parse_node_pattern(tokens)

    case rest do
      ["-" | edge_rest] ->
        {edge, rest2} = parse_edge_pattern(["-" | edge_rest])
        {target, rest3} = parse_node_pattern(rest2)
        {%{type: :path, source: node, edge: edge, target: target}, rest3}

      _ ->
        {%{type: :node_only, node: node}, rest}
    end
  end

  defp parse_node_pattern(["(" | rest]) do
    {var_name, rest} = parse_identifier(rest)

    {label, rest} =
      case rest do
        [":" | label_rest] ->
          {lbl, r} = parse_identifier(label_rest)
          {String.to_atom(lbl), r}
        _ -> {nil, rest}
      end

    [")" | rest] = rest
    {%{var: var_name, label: label}, rest}
  end

  defp parse_edge_pattern(["-", "[" | rest]) do
    {edge_info, rest} =
      case rest do
        [":" | type_rest] ->
          {type_name, r} = parse_identifier(type_rest)
          {%{type: String.to_atom(type_name)}, r}
        _ ->
          {%{type: nil}, rest}
      end

    ["]" | rest] = rest
    ["-", ">" | rest] = rest
    {edge_info, rest}
  end

  defp parse_edge_pattern(["-", "-", ">" | rest]) do
    {%{type: nil}, rest}
  end

  defp parse_where(tokens) do
    {left, rest} = parse_prop_access(tokens)

    {op, rest} =
      case rest do
        [">" | r] -> {:>, r}
        ["<" | r] -> {:<, r}
        ["=" | r] -> {:==, r}
        [">=" | r] -> {:>=, r}
        ["<=" | r] -> {:<=, r}
      end

    {value, rest} = parse_value(rest)
    {%{left: left, op: op, right: value}, rest}
  end

  defp parse_prop_access(tokens) do
    {var, rest} = parse_identifier(tokens)
    case rest do
      ["." | prop_rest] ->
        {prop, rest2} = parse_identifier(prop_rest)
        {%{var: var, prop: String.to_atom(prop)}, rest2}
      _ ->
        {%{var: var, prop: nil}, rest}
    end
  end

  defp parse_value(["'" | rest]) do
    {str_parts, ["'" | rest2]} = Enum.split_while(rest, fn t -> t != "'" end)
    {Enum.join(str_parts, " "), rest2}
  end

  defp parse_value([token | rest]) do
    case Integer.parse(token) do
      {n, ""} -> {n, rest}
      _ ->
        case Float.parse(token) do
          {f, ""} -> {f, rest}
          _ -> {token, rest}
        end
    end
  end

  defp parse_return(tokens) do
    parts = parse_return_items(tokens, [])
    {parts, []}
  end

  defp parse_return_items([], acc), do: Enum.reverse(acc)
  defp parse_return_items(tokens, acc) do
    {item, rest} = parse_prop_access(tokens)
    case rest do
      ["," | r] -> parse_return_items(r, [item | acc])
      _ -> Enum.reverse([item | acc])
    end
  end

  defp parse_identifier([token | rest]) do
    {token, rest}
  end
end
```

```elixir
# lib/graphdb/executor.ex
defmodule GraphDB.Executor do
  @moduledoc """
  Executes a parsed Cypher-like query against the storage layer.
  """

  @doc "Executes an AST and returns a list of result maps."
  @spec execute(map()) :: [map()]
  def execute(%{match: match, where: where, return: return_items}) do
    bindings = match_pattern(match)

    filtered =
      if where do
        Enum.filter(bindings, fn binding -> evaluate_where(where, binding) end)
      else
        bindings
      end

    project_results(filtered, return_items)
  end

  defp match_pattern(%{type: :node_only, node: %{var: var, label: label}}) do
    nodes =
      if label do
        GraphDB.Storage.nodes_by_label(label)
      else
        GraphDB.Storage.all_nodes()
      end

    Enum.map(nodes, fn {id, labels, props} ->
      %{var => %{id: id, labels: labels, props: props}}
    end)
  end

  defp match_pattern(%{type: :path, source: src, edge: edge, target: tgt}) do
    source_nodes =
      if src.label do
        GraphDB.Storage.nodes_by_label(src.label)
      else
        GraphDB.Storage.all_nodes()
      end

    Enum.flat_map(source_nodes, fn {src_id, src_labels, src_props} ->
      outgoing = GraphDB.Storage.outgoing(src_id)

      outgoing
      |> Enum.filter(fn {_to, _eid, type} ->
        edge.type == nil or edge.type == type
      end)
      |> Enum.flat_map(fn {to_id, _eid, _type} ->
        case GraphDB.Storage.get_node(to_id) do
          {^to_id, to_labels, to_props} ->
            if tgt.label == nil or tgt.label in to_labels do
              [%{
                src.var => %{id: src_id, labels: src_labels, props: src_props},
                tgt.var => %{id: to_id, labels: to_labels, props: to_props}
              }]
            else
              []
            end
          nil -> []
        end
      end)
    end)
  end

  defp evaluate_where(%{left: %{var: var, prop: prop}, op: op, right: value}, binding) do
    node_data = Map.get(binding, var)

    node_value =
      if prop do
        get_in(node_data, [:props, prop])
      else
        node_data
      end

    case op do
      :>  -> node_value > value
      :<  -> node_value < value
      :== -> node_value == value
      :>= -> node_value >= value
      :<= -> node_value <= value
    end
  end

  defp project_results(bindings, nil) do
    Enum.map(bindings, fn binding ->
      Map.new(binding, fn {var, node_data} -> {var, node_data.props} end)
    end)
  end

  defp project_results(bindings, return_items) do
    Enum.map(bindings, fn binding ->
      Map.new(return_items, fn
        %{var: var, prop: nil} ->
          {var, Map.get(binding, var)}

        %{var: var, prop: prop} ->
          key = "#{var}.#{prop}"
          node_data = Map.get(binding, var)
          value = get_in(node_data, [:props, prop])
          {key, value}
      end)
    end)
  end
end
```

### Step 7: Database — public API

**Objective**: Front storage, traversal, Dijkstra, and parser behind one GenServer so callers never see ETS tables or direct module state.


```elixir
# lib/graphdb/database.ex
defmodule GraphDB.Database do
  use GenServer

  @moduledoc """
  Public API for the graph database.
  Delegates to Storage, Traversal, Dijkstra, and query Parser/Executor.
  """

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(_opts) do
    GraphDB.Storage.reset()
    {:ok, %{}}
  end
end

defmodule GraphDB do
  @moduledoc "Top-level convenience API for the graph database."

  @spec create_node(GenServer.server(), keyword()) :: {:ok, pos_integer()}
  def create_node(_db, opts) do
    labels = Keyword.get(opts, :labels, [])
    props = Keyword.get(opts, :props, %{})
    id = GraphDB.Storage.create_node(labels, props)
    {:ok, id}
  end

  @spec create_edge(GenServer.server(), keyword()) :: {:ok, pos_integer()}
  def create_edge(_db, opts) do
    from = Keyword.fetch!(opts, :from)
    to = Keyword.fetch!(opts, :to)
    type = Keyword.fetch!(opts, :type)
    props = Keyword.get(opts, :props, %{})
    id = GraphDB.Storage.create_edge(from, to, type, props)
    {:ok, id}
  end

  @spec bfs(GenServer.server(), keyword()) :: [{pos_integer(), non_neg_integer()}]
  def bfs(_db, opts) do
    start = Keyword.fetch!(opts, :start)
    max_depth = Keyword.get(opts, :max_depth, 10)
    direction = Keyword.get(opts, :direction, :out)
    GraphDB.Traversal.bfs(start, max_depth, direction)
  end

  @spec shortest_path(GenServer.server(), keyword()) :: {:ok, [pos_integer()]} | {:error, :no_path}
  def shortest_path(_db, opts) do
    from = Keyword.fetch!(opts, :from)
    to = Keyword.fetch!(opts, :to)
    weight_fn = Keyword.get(opts, :weight_fn, fn _props -> 1 end)
    GraphDB.Dijkstra.shortest_path(from, to, weight_fn)
  end

  @spec query(GenServer.server(), String.t()) :: [map()]
  def query(_db, query_string) do
    {:ok, ast} = GraphDB.Parser.parse(query_string)
    GraphDB.Executor.execute(ast)
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Lock BFS depth ordering and Cypher semantics so later query-planner rewrites cannot silently break traversal invariants.


```elixir
# test/graphdb/traversal_test.exs
defmodule GraphDB.TraversalTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, db} = GraphDB.Database.start_link()
    {:ok, db: db}
  end

  test "BFS returns nodes in correct depth order", %{db: db} do
    {:ok, a} = GraphDB.create_node(db, labels: [:N], props: %{name: "A"})
    {:ok, b} = GraphDB.create_node(db, labels: [:N], props: %{name: "B"})
    {:ok, c} = GraphDB.create_node(db, labels: [:N], props: %{name: "C"})
    {:ok, d} = GraphDB.create_node(db, labels: [:N], props: %{name: "D"})

    GraphDB.create_edge(db, from: a, to: b, type: :NEXT, props: %{})
    GraphDB.create_edge(db, from: b, to: c, type: :NEXT, props: %{})
    GraphDB.create_edge(db, from: b, to: d, type: :NEXT, props: %{})

    results = GraphDB.bfs(db, start: a, max_depth: 2)
    depths = Map.new(results, fn {node, depth} -> {node, depth} end)

    assert depths[a] == 0
    assert depths[b] == 1
    assert depths[c] == 2
    assert depths[d] == 2
  end

  test "shortest path on unweighted graph", %{db: db} do
    nodes = for i <- 1..5 do
      {:ok, id} = GraphDB.create_node(db, labels: [:N], props: %{i: i})
      id
    end

    [n1, n2, n3, n4, n5] = nodes
    GraphDB.create_edge(db, from: n1, to: n2, type: :E, props: %{})
    GraphDB.create_edge(db, from: n2, to: n4, type: :E, props: %{})
    GraphDB.create_edge(db, from: n1, to: n3, type: :E, props: %{})
    GraphDB.create_edge(db, from: n3, to: n5, type: :E, props: %{})

    assert {:ok, path} = GraphDB.shortest_path(db, from: n1, to: n4)
    assert path == [n1, n2, n4]
  end
end
```

```elixir
# test/graphdb/query_test.exs
defmodule GraphDB.QueryTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, db} = GraphDB.Database.start_link()
    {:ok, alice} = GraphDB.create_node(db, labels: [:User], props: %{name: "Alice", age: 30})
    {:ok, bob}   = GraphDB.create_node(db, labels: [:User], props: %{name: "Bob",   age: 25})
    {:ok, carol} = GraphDB.create_node(db, labels: [:User], props: %{name: "Carol", age: 35})

    GraphDB.create_edge(db, from: alice, to: bob,   type: :FOLLOWS, props: %{})
    GraphDB.create_edge(db, from: alice, to: carol, type: :FOLLOWS, props: %{})

    {:ok, db: db, alice: alice, bob: bob, carol: carol}
  end

  test "MATCH (n:User) RETURN n returns all users", %{db: db} do
    results = GraphDB.query(db, "MATCH (n:User) RETURN n")
    assert length(results) == 3
  end

  test "WHERE clause filters by property", %{db: db} do
    results = GraphDB.query(db, "MATCH (n:User) WHERE n.age > 28 RETURN n.name")
    names = Enum.map(results, fn %{"n.name" => name} -> name end)
    assert Enum.sort(names) == ["Alice", "Carol"]
  end

  test "relationship traversal", %{db: db, alice: alice} do
    results = GraphDB.query(db, "MATCH (n:User)-[:FOLLOWS]->(m:User) WHERE n.name = 'Alice' RETURN m.name")
    names = Enum.map(results, fn %{"m.name" => name} -> name end)
    assert Enum.sort(names) == ["Bob", "Carol"]
  end
end
```

### Step 9: Run the tests

**Objective**: Run `--trace` to surface traversal ordering drift that a plain green suite would otherwise hide on non-deterministic adjacency iteration.


```bash
mix test test/graphdb/ --trace
```

### Step 10: Benchmark

**Objective**: Drive 1M nodes and 10M edges through Benchee so shortest-path cost is measured on real sparse topology, not toy graphs.


```elixir
# bench/graphdb_bench.exs
{:ok, db} = GraphDB.Database.start_link()

# Build a graph with 1M nodes and 10M edges
for i <- 1..1_000_000, do: GraphDB.create_node(db, labels: [:N], props: %{id: i})
for _ <- 1..10_000_000 do
  a = :rand.uniform(1_000_000)
  b = :rand.uniform(1_000_000)
  GraphDB.create_edge(db, from: a, to: b, type: :E, props: %{w: :rand.uniform(100)})
end

src = :rand.uniform(1_000_000)
tgt = :rand.uniform(1_000_000)

Benchee.run(
  %{
    "shortest path — unweighted" => fn ->
      GraphDB.shortest_path(db, from: src, to: tgt)
    end,
    "BFS depth 3 from random node" => fn ->
      GraphDB.bfs(db, start: :rand.uniform(1_000_000), max_depth: 3)
    end
  },
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: average shortest-path query under 100ms on a 1M-node, 10M-edge graph.

### Why this works

Each vertex stores its adjacency list in ETS; BFS/DFS traversal streams neighbor lists without materializing the whole graph. Query planning pushes filters into traversal to avoid enumerating the full neighborhood when only a few edges match.

---


## Main Entry Point

```elixir
def main do
  IO.puts("======== 17 build graph database ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Benchmark

```elixir
# bench/graphdb_bench.exs — Graph traversal benchmark with pattern queries
# mix run bench/graphdb_bench.exs

defmodule GraphDBBench do
  @doc "Measures graph traversal throughput and Cypher query latencies."
  def run do
    {:ok, db} = Graphdb.Database.start_link()
    
    IO.puts("=== Graph Database Benchmark (v3 standard) ===\n")

    # Build benchmark graph: 100k vertices, 1M edges (average degree 10)
    IO.puts("Building benchmark graph: 100k vertices, 1M edges...")
    build_graph(db, 100_000, 1_000_000)
    IO.puts("Graph ready.\n")

    # Benchmark phases
    measure_neighbor_lookups(db)
    measure_traversal_depths(db)
    measure_shortest_path(db)
    measure_cypher_queries(db)
    measure_graph_mutations(db)
  end

  defp build_graph(db, vertex_count, edge_count) do
    # Create vertices
    for i <- 1..vertex_count do
      Graphdb.Database.create_node(db, "node_#{i}", ["Person"], %{id: i, name: "Person_#{i}"})
    end

    # Create edges (preferential attachment: hubs have higher degree)
    for _ <- 1..edge_count do
      from = :rand.uniform(vertex_count)
      to = :rand.uniform(vertex_count)
      
      if from != to do
        Graphdb.Database.create_edge(db, "node_#{from}", "node_#{to}", "FOLLOWS", %{weight: :rand.uniform(100)})
      end
    end
  end

  defp measure_neighbor_lookups(db) do
    IO.puts("Phase 1: Neighbor lookups (adjacency list scans)")
    
    latencies = []
    {_time, latencies} = :timer.tc(fn ->
      for _ <- 1..10_000 do
        start = System.monotonic_time(:microsecond)
        node = "node_#{:rand.uniform(100_000)}"
        neighbors = Graphdb.Database.neighbors(db, node, direction: :outgoing, depth: 1)
        elapsed = System.monotonic_time(:microsecond) - start
        [elapsed | latencies]
      end
    end)

    Benchee.run(
      %{
        "1-hop neighbors (adjacency lookup)" => fn ->
          node = "node_#{:rand.uniform(100_000)}"
          Graphdb.Database.neighbors(db, node, direction: :outgoing, depth: 1)
        end,
        "degree count (no traversal)" => fn ->
          node = "node_#{:rand.uniform(100_000)}"
          Graphdb.Database.neighbors(db, node, direction: :outgoing, depth: 1) |> length()
        end
      },
      parallel: 8,
      time: 5,
      warmup: 1,
      formatters: [{Benchee.Formatters.Console, extended_statistics: true}]
    )

    compute_percentiles("Neighbor lookup latency", latencies)
  end

  defp measure_traversal_depths(db) do
    IO.puts("\nPhase 2: Multi-hop traversal (BFS/DFS with depth limiting)")
    
    Benchee.run(
      %{
        "2-hop traversal (10^2 worst case)" => fn ->
          node = "node_#{:rand.uniform(100_000)}"
          Graphdb.Database.neighbors(db, node, direction: :outgoing, depth: 2)
        end,
        "3-hop traversal (10^3 worst case)" => fn ->
          node = "node_#{:rand.uniform(100_000)}"
          Graphdb.Database.neighbors(db, node, direction: :outgoing, depth: 3)
        end,
        "4-hop traversal (10^4 worst case)" => fn ->
          node = "node_#{:rand.uniform(100_000)}"
          Graphdb.Database.neighbors(db, node, direction: :outgoing, depth: 4)
        end
      },
      parallel: 4,
      time: 5,
      warmup: 1,
      formatters: [{Benchee.Formatters.Console, extended_statistics: true}]
    )
  end

  defp measure_shortest_path(db) do
    IO.puts("\nPhase 3: Shortest path (Dijkstra's algorithm)")
    
    latencies = []
    {_time, latencies} = :timer.tc(fn ->
      for _ <- 1..100 do
        start = System.monotonic_time(:microsecond)
        from = "node_#{:rand.uniform(100_000)}"
        to = "node_#{:rand.uniform(100_000)}"
        _path = Graphdb.Database.shortest_path(db, from, to)
        elapsed = System.monotonic_time(:microsecond) - start
        [elapsed | latencies]
      end
    end)

    IO.puts("\nShortest path latency (100 samples):")
    compute_percentiles("Dijkstra query", latencies)
  end

  defp measure_cypher_queries(db) do
    IO.puts("\nPhase 4: Cypher-like query execution")
    
    Benchee.run(
      %{
        "query: MATCH (a)-[:FOLLOWS]->(b) WHERE a.id=1 RETURN b" => fn ->
          case Graphdb.Database.query(db, "MATCH (a)-[:FOLLOWS]->(b) WHERE a.id=1 RETURN b") do
            {:ok, results} -> results
            {:error, _} -> []
          end
        end,
        "query: MATCH (a)-[:FOLLOWS*2]->(b) RETURN b" => fn ->
          case Graphdb.Database.query(db, "MATCH (a)-[:FOLLOWS*2]->(b) RETURN b") do
            {:ok, results} -> results
            {:error, _} -> []
          end
        end,
        "parse Cypher query (no execution)" => fn ->
          Graphdb.Parser.parse("MATCH (a)-[:FOLLOWS]->(b) WHERE a.id=42 RETURN b")
        end
      },
      parallel: 4,
      time: 5,
      warmup: 1,
      formatters: [{Benchee.Formatters.Console, extended_statistics: true}]
    )
  end

  defp measure_graph_mutations(db) do
    IO.puts("\nPhase 5: Graph mutations (node/edge creation)")
    
    Benchee.run(
      %{
        "create node (with labels and props)" => fn ->
          node_id = "new_node_#{:rand.uniform(1_000_000)}"
          Graphdb.Database.create_node(db, node_id, ["Temp"], %{created_at: System.system_time(:millisecond)})
        end,
        "create edge (connect to random node)" => fn ->
          from = "node_#{:rand.uniform(100_000)}"
          to = "node_#{:rand.uniform(100_000)}"
          if from != to do
            Graphdb.Database.create_edge(db, from, to, "CONNECTS", %{created_at: System.system_time(:millisecond)})
          end
        end
      },
      parallel: 4,
      time: 5,
      warmup: 1,
      formatters: [{Benchee.Formatters.Console, extended_statistics: true}]
    )
  end

  defp compute_percentiles(label, latencies) when is_list(latencies) and length(latencies) > 0 do
    sorted = Enum.sort(latencies)
    p50 = Enum.at(sorted, div(length(sorted), 2)) / 1_000
    p99 = Enum.at(sorted, trunc(length(sorted) * 0.99)) / 1_000
    p999 = Enum.at(sorted, trunc(length(sorted) * 0.999)) / 1_000

    IO.puts("#{label}:")
    IO.puts("  p50:  #{Float.round(p50, 3)}ms")
    IO.puts("  p99:  #{Float.round(p99, 3)}ms")
    IO.puts("  p999: #{Float.round(p999, 3)}ms")
  end

  defp compute_percentiles(_label, _latencies), do: :ok
end

GraphDBBench.run()
```

### Benchmark targets (v3 standard) — Traversal Performance Focus

| Metric | Target | Notes |
|--------|--------|-------|
| **1-hop neighbor lookup** | 10M ops/s | O(degree) ETS bag scan; avg degree ~10 |
| **Neighbor lookup latency p50** | < 10 µs | In-memory ETS match_object |
| **Neighbor lookup latency p99** | < 100 µs | Degree > 100 or GC pause |
| **2-hop traversal (worst case 10^2)** | 100k ops/s | Expands 10→100 nodes, manageable |
| **2-hop traversal latency p99** | < 5 ms | 100 neighbors * 10 = 1k nodes checked |
| **3-hop traversal (worst case 10^3)** | 10k ops/s | Expands to 1000 nodes; memory/CPU pressure |
| **3-hop traversal latency p99** | < 50 ms | More expensive; limited by BFS queue depth |
| **Shortest path (Dijkstra)** | 1k ops/s | Single-source all-targets on 100k vertices |
| **Shortest path latency p99** | < 100 ms | Priority queue operations + relaxation |
| **Cypher query parse** | 100k ops/s | Recursive descent parser (no execution) |
| **Cypher query execute** | 1k ops/s | Parse + plan + traverse |
| **Node creation** | 100k ops/s | Insert into ETS, no index maintenance |
| **Edge creation** | 100k ops/s | Dual insert (adj_out + adj_in) |

### Traversal Complexity Analysis

For a power-law graph (social networks, web links), average degree ≈ 10:
- **1-hop**: ~10 neighbors, latency < 10 µs
- **2-hop**: ~100 neighbors (10 * 10), latency < 5 ms
- **3-hop**: ~1000 neighbors (10^3), latency < 50 ms — memory pressure starts
- **4-hop**: ~10k neighbors (10^4), latency > 100 ms — most queries should cap at 3

**Optimization strategies**:
1. **Materialized 2-hop views**: Pre-compute and cache frequent 2-hop subgraphs
2. **Bidirectional search**: Start from both ends, meet in the middle (reduces from O(d^k) to O(d^(k/2)))
3. **Index by relationship type**: Separate adjacency lists per edge type (:FOLLOWS, :FRIEND, etc.) to prune the search space

---

## Deep Dive: LSM Trees vs. B-Trees for Different Workloads

LSM (Log-Structured Merge) trees power RocksDB and LevelDB. They invert how data is organized compared to traditional B-trees:

**LSM**: Writes go to an in-memory buffer (memtable). When full, the memtable is flushed to disk as an immutable Level 0 file. Periodically, files are merged across levels (compaction), reducing the number of files to search during reads. Reads check memtable, then each level in order.

**B-tree**: Writes update the tree in-place via seeks to the correct leaf. Reads traverse from root to leaf. Requires a write-ahead log for crash safety.

LSM wins dramatically for write-heavy workloads: sequential flushes are much faster than random seeks (10–100x on rotating disks, 3–5x on SSDs). But LSM reads must check multiple levels (O(log N) files instead of O(log N) tree height), making point reads slower. For 80/20 read/write workloads, B-tree point reads dominate.

Compaction is LSM's hidden cost: periodically, all data must be rewritten to compact levels. During compaction, read latency spikes. High-performance systems (RocksDB) use rate-limiting to smooth this spike, but aggressive rate-limiting increases write latency.

A critical LSM tuning parameter is key distribution. Random writes across a large space cause many files per level, making compaction expensive. Sequential writes (e.g., time-series data) cause few files, fast compaction. Similarly, range scans benefit from compacted levels' better locality.

**Production patterns**: Time-series databases (InfluxDB, Prometheus) use LSM because writes are sequential (time order) and reads are range scans (past N hours). Document stores (MongoDB with WiredTiger) use LSM for write throughput. OLTP databases (PostgreSQL) stick with B-trees because point reads and ACID transactions are more critical than write throughput.

---

## Trade-off analysis

| Aspect | GraphDB ETS (your impl) | Neo4j | PostgreSQL recursive CTE |
|--------|------------------------|-------|--------------------------|
| Neighbor lookup | O(degree) via ETS bag | O(degree) native | O(table scan) or O(index) |
| Pattern matching | query executor walk | Cypher planner | recursive CTE |
| Shortest path | Dijkstra in Elixir | native C++ | recursive CTE + cost |
| Durability | none (in-memory) | durable | durable |
| Query language | subset Cypher | full Cypher | SQL |
| Horizontal scale | none | clustering (enterprise) | none |

Reflection: Dijkstra's algorithm requires a priority queue with O(log V) decrease-key operations. Erlang's `:gb_sets` does not support decrease-key. What data structure or modification to the algorithm allows you to implement Dijkstra without decrease-key?

---

## Common production mistakes

**1. Not maintaining the reverse adjacency table**
Without the `adj_in` table, incoming neighbor queries require a full scan of all edges. This makes `direction: :in` O(E) instead of O(degree). Maintain both `adj_out` and `adj_in` on every edge insertion and deletion.

**2. Variable-length paths without cycle detection**
`[:FOLLOWS*1..3]` traversal must track visited nodes per path, not globally. Global visited sets prevent finding different paths through the same node. Per-path visited sets allow re-visiting a node via a different path while preventing infinite cycles on a single path.

**3. Query parser errors silently returning empty results**
An unparseable query should return `{:error, {:parse_error, message, line, col}}`, not `[]`. Empty results for a bad query are indistinguishable from a valid query with no matches.

**4. Not deduplicating nodes in BFS results**
If the graph has cycles (A -> B -> A), BFS without a visited set will loop forever. The visited set must contain node IDs, not tuples — otherwise the same node at different depths is visited multiple times.

## Reflection

- If your graph were write-heavy (1000 new edges/s), would adjacency lists still win over CSR (compressed sparse row)? Analyze.
- How would you add transactional multi-edge inserts without globally locking the graph?

---

## Resources

- [openCypher specification](https://opencypher.org/resources/) — the Cypher query language standard
- [Neo4j property graph model](https://neo4j.com/docs/getting-started/graph-database/)
- Robinson, I., Webber, J. & Eifrem, E. — *Graph Databases* (O'Reilly)
- Cormen, T. et al. — *Introduction to Algorithms* — Chapter 24 (Dijkstra)
