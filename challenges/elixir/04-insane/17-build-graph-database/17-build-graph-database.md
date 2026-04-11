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
│       ├── parser.ex                # Cypher-like query parser (NimbleParsec or recursive descent)
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

Relational databases represent relationships as foreign keys — finding all people who follow a given person within 3 hops requires multiple self-joins that are expensive to plan and execute. Graph databases represent relationships natively as edges: traversal is a first-class operation, not a query optimization problem. A 3-hop traversal in a property graph is O(degree³) — the query engine walks the adjacency lists directly.

---

## Why this design

**Three ETS tables**: nodes (`{id, labels, props}`), edges (`{id, from, to, type, props}`), adjacency (`{from_id, to_id, edge_id, type}`). The adjacency table is the key to O(degree) neighbor lookups — `:ets.match_object/2` with the `from_id` field bound returns all outgoing edges without scanning all edges. Add a second adjacency table for incoming edges to support bidirectional traversal.

**NimbleParsec for the query parser**: Cypher is a context-free grammar with recursive patterns (`MATCH`, `WHERE`, `RETURN`). NimbleParsec handles tokenization and recursive grammar rules with combinators. A hand-rolled recursive descent parser is also valid, but NimbleParsec produces better error messages.

**Transaction diff log for rollback**: instead of MVCC (expensive for graph operations), record the inverse of every mutation: `{:delete_node, id}` for a `CREATE NODE`, `{:delete_edge, id}` for a `CREATE EDGE`. On rollback, apply inverses in reverse order. This is correct for within-transaction rollback but does not support concurrent readers seeing a consistent snapshot.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new graphdb --sup
cd graphdb
mkdir -p lib/graphdb test/graphdb bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:nimble_parsec, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Storage layer

```elixir
# lib/graphdb/storage.ex
defmodule GraphDB.Storage do
  @nodes     :graphdb_nodes      # {id, labels, props}
  @edges     :graphdb_edges      # {id, from_id, to_id, type, props}
  @adj_out   :graphdb_adj_out    # {from_id, to_id, edge_id, type}
  @adj_in    :graphdb_adj_in     # {to_id, from_id, edge_id, type}

  def init do
    :ets.new(@nodes,   [:named_table, :public, :set])
    :ets.new(@edges,   [:named_table, :public, :set])
    :ets.new(@adj_out, [:named_table, :public, :bag])
    :ets.new(@adj_in,  [:named_table, :public, :bag])
  end

  @doc "Returns all outgoing neighbors of node_id as [{to_id, edge_id, type}]."
  def outgoing(node_id) do
    # TODO: :ets.match_object(@adj_out, {node_id, :_, :_, :_})
  end

  @doc "Returns all incoming neighbors of node_id."
  def incoming(node_id) do
    # TODO: :ets.match_object(@adj_in, {node_id, :_, :_, :_})
  end

  @doc "Returns neighbors in both directions."
  def neighbors(node_id, :both), do: outgoing(node_id) ++ incoming(node_id)
  def neighbors(node_id, :out),  do: outgoing(node_id)
  def neighbors(node_id, :in),   do: incoming(node_id)
end
```

### Step 4: Traversal algorithms

```elixir
# lib/graphdb/traversal.ex
defmodule GraphDB.Traversal do
  @doc """
  BFS traversal starting from start_node_id.
  Returns [{node_id, depth}] in BFS order, up to max_depth.
  """
  def bfs(start_node_id, max_depth, direction \\ :out) do
    # TODO: standard BFS with a queue and visited set
    # HINT: use :queue.new() for the BFS queue
    # HINT: track visited to avoid cycles
  end

  @doc """
  DFS traversal starting from start_node_id.
  Returns [node_id] in DFS order, up to max_depth.
  """
  def dfs(start_node_id, max_depth, direction \\ :out) do
    # TODO
  end
end
```

```elixir
# lib/graphdb/dijkstra.ex
defmodule GraphDB.Dijkstra do
  @doc """
  Returns the shortest path from source to target as a list of node IDs.
  For weighted graphs, weight_fn is called with the edge props map.
  For unweighted graphs, use BFS instead (all weights = 1).
  """
  def shortest_path(source, target, weight_fn \\ fn _props -> 1 end) do
    # TODO: standard Dijkstra using a priority queue
    # HINT: use :gb_sets for the priority queue (ordered set from Erlang stdlib)
    # HINT: return {:ok, path} or {:error, :no_path}
  end
end
```

### Step 5: Given tests — must pass without modification

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

### Step 6: Run the tests

```bash
mix test test/graphdb/ --trace
```

### Step 7: Benchmark

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
If the graph has cycles (A → B → A), BFS without a visited set will loop forever. The visited set must contain node IDs, not tuples — otherwise the same node at different depths is visited multiple times.

---

## Resources

- [openCypher specification](https://opencypher.org/resources/) — the Cypher query language standard
- [Neo4j property graph model](https://neo4j.com/docs/getting-started/graph-database/)
- Robinson, I., Webber, J. & Eifrem, E. — *Graph Databases* (O'Reilly)
- [NimbleParsec documentation](https://hexdocs.pm/nimble_parsec/)
- Cormen, T. et al. — *Introduction to Algorithms* — Chapter 24 (Dijkstra)
