<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Query Planner and Executor

The query planner bridges the gap between the declarative SQL statement ("what data I want") and the imperative execution plan ("how to get it"). Your task is to build a query planner and volcano-model executor in Go that takes a parsed SQL AST and produces an optimized physical execution plan as a tree of iterator-based operators. You will implement table scans, index scans, nested loop joins, hash joins, filtering, projection, sorting, grouping with aggregation, and a simple cost-based optimizer that chooses between execution strategies based on table statistics. This is where your database engine starts to feel like a real database.

## Requirements

1. Define the iterator (volcano) model interface: `type Operator interface { Init() error; Next() (*Tuple, error); Close() error }` where `Tuple` is a row of typed values. Every physical operator implements this interface, and the executor simply calls `Next()` on the root operator in a loop until it returns nil. Define `Schema` as an ordered list of column definitions (name, type) attached to each operator's output.

2. Implement `SeqScanOperator` that reads all tuples from a table's heap file (simulated as a slice of pages or an in-memory table store for now). Implement `IndexScanOperator` that uses a B+Tree index to look up tuples by key, supporting both point lookups and range scans. Both operators must conform to the `Operator` interface and produce tuples lazily (one at a time via `Next()`).

3. Implement `FilterOperator` that wraps a child operator and evaluates a boolean expression on each tuple, only yielding tuples that pass the predicate. Implement an expression evaluator that handles: comparison operators, arithmetic, AND/OR/NOT, NULL handling (three-valued logic: NULL compared to anything is NULL, which is falsy), LIKE with % and _ wildcards, BETWEEN, IN, and IS NULL/IS NOT NULL.

4. Implement `ProjectionOperator` that takes a child operator and a list of output expressions, computing new tuples with only the requested columns/expressions. Implement `SortOperator` that materializes all input tuples, sorts them by the ORDER BY columns (with ASC/DESC and NULL handling), and then yields them in order. Implement `LimitOperator` and `OffsetOperator` for LIMIT/OFFSET.

5. Implement two join operators: `NestedLoopJoinOperator` (for small tables or when no better option exists) that iterates the outer relation and for each tuple scans the inner relation, evaluating the ON condition; and `HashJoinOperator` (for equi-joins) that builds a hash table from the smaller (build) side on the join key, then probes it with each tuple from the larger (probe) side. Support INNER, LEFT, and RIGHT joins.

6. Implement `GroupByOperator` that partitions input tuples by the GROUP BY expressions (using a hash map), computes aggregate functions (COUNT, SUM, AVG, MIN, MAX) for each group, and yields one tuple per group. Handle COUNT(*) (count all rows including NULLs) vs COUNT(column) (count non-NULL values). Implement HAVING as a filter applied after aggregation.

7. Implement a simple cost-based query planner that takes the AST and produces a physical plan. The planner must: resolve table and column names against a catalog (in-memory schema registry), choose between SeqScan and IndexScan based on whether a usable index exists for the WHERE predicate, choose between NestedLoopJoin and HashJoin based on estimated table sizes, and push down filter predicates as close to the scan operators as possible (predicate pushdown optimization).

8. Write tests covering: full end-to-end query execution (SQL string -> lexer -> parser -> planner -> executor -> result tuples) for SELECT, INSERT, UPDATE, DELETE. Test complex queries with JOINs, GROUP BY with HAVING, ORDER BY with LIMIT, subqueries in WHERE, NULL handling in all operators, predicate pushdown verification (inspect the plan to confirm filters are below joins), and a benchmark comparing hash join vs. nested loop join performance on tables with 10,000+ rows.

## Hints

- The volcano model is pull-based: the consumer calls `Next()` on the producer. This naturally handles pipelining (no unnecessary materialization) except for blocking operators like Sort and HashJoin's build phase.
- For the hash join build phase, use a `map[uint64][]*Tuple` keyed by a hash of the join column values. For multi-column joins, hash all join columns together.
- Three-valued logic with NULLs: `NULL AND FALSE = FALSE`, `NULL AND TRUE = NULL`, `NULL OR TRUE = TRUE`, `NULL OR FALSE = NULL`. This affects filter evaluation.
- For predicate pushdown, walk the WHERE expression tree and split conjuncts (AND-separated conditions). Each conjunct that references only one table can be pushed down to that table's scan operator.
- The catalog is a simple `map[string]*TableDef` with schema information and row count estimates for cost estimation.
- For LIKE pattern matching, convert `%` to `.*` and `_` to `.` and use `regexp.MatchString`, or implement a manual matcher for better performance.

## Success Criteria

1. A SELECT with WHERE, ORDER BY, and LIMIT returns the correct sorted subset of rows from a test table with 1,000 rows.
2. A two-table INNER JOIN with a WHERE clause returns correct results for both nested loop and hash join strategies.
3. GROUP BY with COUNT, SUM, and AVG produces correct aggregate values, including correct NULL handling.
4. LEFT JOIN correctly includes unmatched rows from the left table with NULL-filled right columns.
5. The planner selects IndexScan when an index exists on the filtered column and SeqScan otherwise.
6. Predicate pushdown is verifiable by inspecting the physical plan tree: filter conditions appear directly on scan operators rather than above joins.
7. End-to-end tests covering 20+ distinct SQL queries all produce expected results.

## Research Resources

- [CMU 15-445 Query Processing](https://15445.courses.cs.cmu.edu/fall2024/slides/12-queryexecution1.pdf)
- [Volcano - An Extensible and Parallel Query Evaluation System](https://paperhub.s3.amazonaws.com/dace52a42c07f7f8348b08dc2b186061.pdf)
- [How Query Engines Work (Andy Grove)](https://howqueryengineswork.com/)
- [Hash Join Algorithm Explained](https://en.wikipedia.org/wiki/Hash_join)
- [Three-Valued Logic in SQL](https://modern-sql.com/concept/three-valued-logic)
- [Query Optimization Overview (Stanford)](https://web.stanford.edu/class/cs245/slides/)
