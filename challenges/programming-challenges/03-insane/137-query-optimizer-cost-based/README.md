# 137. Cost-Based Query Optimizer

<!--
difficulty: insane
category: database-internals
languages: [rust]
concepts: [query-optimization, cost-estimation, plan-enumeration, dynamic-programming, join-ordering, statistics, cardinality-estimation, physical-operators]
estimated_time: 30-50 hours
bloom_level: create
prerequisites: [rust-ownership, sql-basics, tree-data-structures, dynamic-programming, hash-maps, iterators, basic-statistics]
-->

## Languages

- Rust (stable)

## Prerequisites

- SQL semantics: SELECT, FROM, WHERE, JOIN, GROUP BY
- Tree data structures for representing query plans
- Dynamic programming (memoization of optimal substructures)
- Basic statistics: histograms, selectivity estimation, cardinality
- Rust trait objects for polymorphic operator trees

## Learning Objectives

- **Create** a cost-based query optimizer that transforms SQL into an efficient physical execution plan
- **Implement** the System R dynamic programming algorithm for optimal join ordering
- **Design** a statistics framework with table cardinality, column histograms, and selectivity estimation
- **Evaluate** physical operator alternatives (hash join vs sort-merge join, sequential scan vs index scan) using cost models
- **Analyze** how cardinality estimation errors compound across multi-join queries

## The Challenge

The query optimizer is the brain of every relational database. It takes a declarative SQL query ("what I want") and produces an imperative execution plan ("how to get it"). For a query joining N tables, there are O(N!) possible join orderings, each with multiple physical operator choices. A 10-table join has 3.6 million orderings before considering operator variants. The optimizer must find a near-optimal plan in milliseconds.

The System R optimizer (IBM, 1979) solved this with dynamic programming. The key insight: the optimal plan for joining tables {A, B, C, D} can be built from the optimal plans for all subsets. If the best plan for {A, B} has cost 100 and the best plan for {C, D} has cost 50, then {A,B} JOIN {C,D} is a candidate for the full plan. The DP explores all 2^N subsets and their join combinations, caching the best plan per subset.

Cost estimation requires statistics. The optimizer needs to know: how many rows does table T have? What fraction of rows satisfy predicate `x > 10`? How many distinct values does column C have? Histograms approximate the value distribution, enabling selectivity estimates like "30% of rows have age between 20 and 30." Cardinality estimates feed into cost formulas: a hash join costs O(build_side) + O(probe_side), while a sort-merge join costs O(N log N + M log M).

Build a cost-based query optimizer. Parse SQL into a logical plan, estimate cardinalities, enumerate physical plans, and select the cheapest.

## Requirements

1. Parse a subset of SQL into a logical plan tree. Support: `SELECT columns FROM table [JOIN table ON condition]* [WHERE predicates] [GROUP BY columns]`. Logical operators: Scan, Filter, Project, Join (inner), Aggregate
2. Implement table statistics: row count, column statistics (min, max, distinct count, null fraction), and equi-width histograms with configurable bucket count. Provide an API to register statistics for tables
3. Implement selectivity estimation for predicates: equality (`=`) uses `1/distinct_count`, range (`>`, `<`, `BETWEEN`) uses histogram bucket fractions, AND multiplies selectivities (independence assumption), OR uses inclusion-exclusion
4. Implement cardinality estimation for each logical operator: Scan outputs table row count, Filter multiplies input cardinality by selectivity, Join estimates output as `left_card * right_card * join_selectivity`
5. Implement at least three physical operators: **SequentialScan** (reads all rows), **HashJoin** (builds hash table on smaller side, probes with larger), **SortMergeJoin** (sorts both sides, merges). Each operator has a cost formula based on input cardinalities
6. Implement the System R dynamic programming algorithm for join ordering: enumerate all subsets of the FROM clause tables, compute the optimal plan for each subset by trying all partitions into two non-empty subsets, and cache the cheapest plan per subset
7. For each join in the DP, consider all physical join operators and pick the one with the lowest estimated cost
8. Produce a final physical plan tree that can be printed showing operator type, estimated rows, and estimated cost at each node
9. Support at least 6-table joins in under one second of optimization time

## Acceptance Criteria

- [ ] SQL parsing produces correct logical plans for single-table queries, two-table joins, and multi-table joins with WHERE clauses
- [ ] Selectivity estimation for equality predicates returns approximately `1/distinct_count`
- [ ] Histogram-based range selectivity returns accurate fractions for uniform and skewed distributions
- [ ] Cardinality estimates propagate correctly through Filter, Join, and Aggregate operators
- [ ] The DP optimizer produces a valid join tree for N tables (tested with N = 2, 4, 6)
- [ ] The optimizer chooses HashJoin for highly asymmetric table sizes and SortMergeJoin when inputs are pre-sorted or similar in size
- [ ] The physical plan for a star schema query (one large fact table, several small dimension tables) places small tables on the build side of hash joins
- [ ] Optimization for 6 tables completes in under 1 second; for 10 tables in under 10 seconds
- [ ] Plan output shows operator tree with estimated cardinality and cost at each node

## Research Resources

- [CMU 15-445: Query Planning & Optimization (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/14-optimization.pdf) -- lecture slides covering cost-based optimization, System R, and plan enumeration
- [CMU 15-445: Query Planning II](https://15445.courses.cs.cmu.edu/fall2024/slides/15-optimization2.pdf) -- join ordering, cardinality estimation, and histogram-based statistics
- [Access Path Selection in a Relational Database Management System (Selinger et al., 1979)](https://courses.cs.duke.edu/compsci516/cps216/spring03/papers/selinger-etal-1979.pdf) -- the original System R optimizer paper
- [Andy Pavlo's Query Optimization Lectures (YouTube)](https://www.youtube.com/playlist?list=PLSE8ODhjZXjbj8BMuIrRcacnQh20hmY9g) -- CMU 15-445 lectures 14-15
- [How Query Optimizers Work (Benjamin Nevarez)](http://www.benjaminnevarez.com/wp-content/uploads/2010/08/InsideTheQueryOptimizer.pdf) -- accessible introduction to SQL Server's optimizer
- [Database Internals by Alex Petrov, Ch. 15-16](https://www.databass.dev/) -- query processing and optimization
