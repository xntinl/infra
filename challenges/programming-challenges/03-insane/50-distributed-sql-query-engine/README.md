# 50. Distributed SQL Query Engine

<!--
difficulty: insane
category: networking-and-protocols
languages: [go, rust]
concepts: [sql-parsing, query-planning, distributed-execution, hash-join, shuffle-exchange, aggregation, tcp-protocol, partitioning]
estimated_time: 40-60 hours
bloom_level: create
prerequisites: [sql-fundamentals, tcp-networking, binary-protocols, concurrency, data-structures, distributed-systems-basics]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- SQL query semantics: SELECT, WHERE, JOIN, GROUP BY, ORDER BY, aggregate functions
- TCP socket programming and binary protocol design
- Concurrent programming (goroutines/channels in Go, threads/channels in Rust)
- Tree data structures (for AST and query plan representation)
- Hash tables and sorting algorithms
- Basic understanding of distributed systems: partitioning, shuffling, coordinator-worker patterns

## Learning Objectives

- **Create** an SQL parser that transforms query strings into abstract syntax trees
- **Design** a query planner that converts logical plans into distributed physical execution plans with exchange operators
- **Architect** a coordinator-worker system where plan fragments execute in parallel across multiple nodes
- **Evaluate** the trade-offs between hash join and sort-merge join for different data distributions and memory constraints
- **Build** a two-phase aggregation pipeline (partial aggregation at workers, final aggregation at coordinator)

## The Challenge

Every database you have ever used -- PostgreSQL, MySQL, Snowflake, BigQuery -- has a query engine at its core. This engine takes an SQL string, parses it into a tree, optimizes it into an execution plan, and runs that plan against data. In a distributed database, the plan is split into fragments that execute in parallel on different nodes, with data shuffled between them.

Build a distributed SQL query execution engine. The system has one coordinator node and N worker nodes. Tables are hash-partitioned across workers by a partition key. The coordinator parses SQL, plans the query, splits the plan into fragments, sends fragments to workers over TCP, collects partial results, and assembles the final answer.

No SQL libraries, no database engines, no ORMs. Parse the SQL yourself. Plan the query yourself. Execute the joins, aggregations, and sorts yourself. Shuffle data between workers yourself. The only networking primitive is TCP.

You will implement this in both Go and Rust. The coordinator and workers in each implementation must be protocol-compatible: a Go coordinator can talk to Rust workers and vice versa.

## Requirements

1. **SQL parser**: parse a subset of SQL: `SELECT columns FROM table [WHERE conditions] [JOIN table ON condition] [GROUP BY columns] [ORDER BY columns [ASC|DESC]] [LIMIT n]`. Support column references, string/integer/float literals, comparison operators (`=`, `!=`, `<`, `>`, `<=`, `>=`), logical operators (`AND`, `OR`), and aggregate functions (`COUNT`, `SUM`, `AVG`, `MIN`, `MAX`)
2. **Abstract Syntax Tree**: represent parsed queries as a typed AST with nodes for SELECT, FROM, WHERE, JOIN, GROUP BY, ORDER BY, and expressions
3. **Logical plan**: convert the AST into a logical query plan tree with operators: Scan, Filter, Project, Join, Aggregate, Sort, Limit
4. **Physical plan**: convert the logical plan into a physical plan that accounts for data distribution. Insert Exchange operators where data must be shuffled (e.g., before a join on a non-partition key, before a global sort)
5. **Exchange operators**: implement HashExchange (repartition data by a hash key) and GatherExchange (collect all data to the coordinator). Serialize rows, send them over TCP to the target node, and deserialize on arrival
6. **Hash join**: implement in-memory hash join. The build side is loaded into a hash table keyed by the join column. The probe side scans and looks up matches. Choose the smaller table as the build side
7. **Sort-merge join**: implement sort-merge join as an alternative when both sides are already sorted on the join key or when data does not fit in memory for hash join
8. **Parallel aggregation**: implement two-phase aggregation. Workers compute partial aggregates locally (partial SUM, partial COUNT). The coordinator combines partial results into final aggregates (final SUM = sum of partial SUMs, final AVG = total SUM / total COUNT)
9. **Coordinator node**: listens on TCP, accepts SQL queries from clients, parses and plans them, splits the plan into fragments, sends fragments to workers, collects results, and returns the final result set
10. **Worker nodes**: connect to the coordinator on startup, receive plan fragments over TCP, execute them against local data partitions, and return results. Workers can also exchange data with other workers during shuffles
11. **Data model**: tables are defined with a schema (column names and types: INT, FLOAT, VARCHAR). Data is loaded from CSV files, hash-partitioned across workers by a designated partition key
12. **Network protocol**: binary, length-prefixed frames over TCP. Frame types: PLAN_FRAGMENT, ROW_BATCH, EXCHANGE_DATA, RESULT, ERROR, HEARTBEAT. Row batches are columnar: column types followed by column data for a batch of rows
13. **Result formatting**: the coordinator prints results as a formatted table with column headers and aligned values

## Acceptance Criteria

- [ ] SQL parser handles SELECT, WHERE, JOIN, GROUP BY, ORDER BY, LIMIT, and aggregate functions
- [ ] Parser produces a correct AST for nested expressions with operator precedence
- [ ] Logical plan correctly represents the query semantics
- [ ] Physical plan inserts Exchange operators at the correct points
- [ ] Hash join produces correct results for equi-joins
- [ ] Sort-merge join produces correct results and handles duplicates
- [ ] Two-phase aggregation produces correct SUM, COUNT, AVG, MIN, MAX
- [ ] Coordinator distributes plan fragments to workers and collects results
- [ ] Workers execute plan fragments against local data partitions
- [ ] Hash exchange correctly repartitions data across workers
- [ ] A query joining two tables partitioned on different keys produces correct results
- [ ] ORDER BY returns globally sorted results
- [ ] Go and Rust implementations use the same wire protocol (cross-compatible)
- [ ] System handles 3+ worker nodes concurrently

## Resources

- [CMU 15-445: Query Execution](https://15445.courses.cs.cmu.edu/) -- Andy Pavlo's database course, lectures on query execution and optimization
- [How Query Engines Work (Andy Grove)](https://howqueryengineswork.com/) -- free online book building a query engine from scratch in Kotlin
- [Apache Calcite](https://calcite.apache.org/docs/) -- SQL parser and query optimizer framework, study the logical/physical plan separation
- [Volcano/Iterator Model (Graefe)](https://paperhub.s3.amazonaws.com/dace52a42c07f7f8348b08dc2b186061.pdf) -- the foundational paper on iterator-based query execution
- [Morsel-Driven Parallelism (Leis et al.)](https://db.in.tum.de/~leis/papers/morsels.pdf) -- parallel query execution without exchange operators
- [Pratt Parser (Matklad)](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html) -- practical guide to building expression parsers with precedence
