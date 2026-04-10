# 17. Build a Graph Database with Cypher-Like Query Language

**Difficulty**: Insane

## Prerequisites

- Mastered: ETS, GenServer, binary parsing, recursive algorithms (BFS, DFS, Dijkstra)
- Mastered: Parser combinators or lexer/parser construction in Elixir
- Familiarity with: Property graphs, openCypher specification, Dijkstra's algorithm, Neo4j data model

## Problem Statement

Build a property graph database in Elixir with its own query language parser that supports
a meaningful subset of Cypher. The database must store nodes and directed edges with
arbitrary properties, support graph traversal queries, and execute shortest-path algorithms
efficiently on large graphs:

1. Nodes have a unique integer ID, one or more labels (e.g., `:User`, `:Post`), and a map
   of string-keyed properties with values of type string, integer, float, or boolean.
2. Edges are directed, have a unique ID, a source node ID, a target node ID, a relationship
   type (e.g., `FOLLOWS`, `LIKES`), and a map of properties.
3. The adjacency structure must be stored in ETS for O(1) lookup of both outgoing and
   incoming neighbors of any node.
4. Implement BFS and DFS traversal with configurable maximum depth, start node, and
   optional direction filter (`:out`, `:in`, `:both`).
5. Implement shortest path using Dijkstra's algorithm for weighted graphs (edge weight from
   a specified property) and unweighted BFS for unweighted graphs.
6. Implement a query parser for the following Cypher-like patterns:
   - `MATCH (n:User) RETURN n`
   - `MATCH (n:User)-[:FOLLOWS]->(m:User) RETURN n, m`
   - `MATCH (n:User) WHERE n.age > 25 RETURN n.name`
   - `MATCH (n:User)-[:FOLLOWS*1..3]->(m:User) RETURN m` (variable-length path)
7. Implement transactions: `CREATE`, `MERGE`, and `DELETE` operations within a transaction
   are atomic; `ROLLBACK` reverts the graph to the pre-transaction state.
8. Benchmark: a graph with 1 million nodes and 10 million edges, shortest path between
   two randomly selected nodes must complete in under 100ms on average.

## Acceptance Criteria

- [ ] `GraphDB.create_node(db, labels: [:User], props: %{name: "Alice", age: 30})`
      returns `{:ok, node_id}`.
- [ ] `GraphDB.create_edge(db, from: alice_id, to: bob_id, type: :FOLLOWS, props: %{since: 2022})`
      returns `{:ok, edge_id}`.
- [ ] `GraphDB.neighbors(db, node_id, direction: :out)` returns all nodes reachable via
      outgoing edges in O(degree) time.
- [ ] `GraphDB.bfs(db, start: node_id, max_depth: 3)` returns all nodes reachable within
      3 hops with their depth, in BFS order.
- [ ] `GraphDB.shortest_path(db, from: a, to: b)` returns the list of node IDs on the
      shortest unweighted path, or `{:error, :no_path}` if disconnected.
- [ ] `GraphDB.query(db, "MATCH (n:User) WHERE n.age > 25 RETURN n.name")` returns a list
      of maps; unknown labels or properties return empty results, not errors.
- [ ] `MATCH (n:User)-[:FOLLOWS]->(m:User)` correctly traverses only `:FOLLOWS` edges and
      filters both endpoints to label `:User`.
- [ ] Variable-length path `[:FOLLOWS*1..3]` returns all reachable nodes via 1 to 3
      consecutive `:FOLLOWS` edges.
- [ ] A transaction that creates 3 nodes and 2 edges and is then rolled back leaves the
      graph unchanged.
- [ ] Benchmark: average shortest-path query time on a 1M-node, 10M-edge graph is under
      100ms; documented with graph diameter and edge density.

## What You Will Learn

- Property graph storage: the dual adjacency list representation for O(1) in and out neighbor access
- Parser combinator construction: building a Cypher-like parser with `NimbleParsec` or a hand-rolled recursive descent parser
- The BFS shortest-path guarantee and why Dijkstra is necessary only when edge weights are non-uniform
- Pattern matching on graph paths: how to bind variables and filter by label and property in a single traversal
- Variable-length path expansion and how to avoid cycles with a visited set
- The transaction log approach for graph rollback: recording inverse operations (delete for each create)

## Hints

This exercise is intentionally sparse. Research:

- Store the graph in three ETS tables: `nodes` (`{id, labels, props}`), `edges` (`{id, from, to, type, props}`), `adjacency` (`{from_id, to_id, edge_id, type}`) — query the third for neighbor lookups
- For the query parser, `NimbleParsec` handles tokenization and recursive grammar rules cleanly; define atoms for keywords and rules for patterns
- Variable-length paths: implement as iterative BFS up to `max_depth`, accumulating paths; prune cycles by tracking visited node IDs per path
- Dijkstra in Elixir: use a priority queue implemented as a sorted list or `:gb_sets` (balanced tree from Erlang stdlib)
- Transactions: maintain a diff log as a GenServer accumulator `[{:create_node, id} | {:create_edge, id} | ...]`; on rollback, apply inverses in reverse order

## Reference Material

- openCypher specification: https://opencypher.org/resources/
- Neo4j property graph model: https://neo4j.com/docs/getting-started/graph-database/
- "Graph Databases" — Robinson, Webber & Eifrem (O'Reilly)
- NimbleParsec documentation: https://hexdocs.pm/nimble_parsec/
- Dijkstra's algorithm: Cormen et al., "Introduction to Algorithms", Chapter 24

## Difficulty Rating

★★★★★★

## Estimated Time

55–75 hours
