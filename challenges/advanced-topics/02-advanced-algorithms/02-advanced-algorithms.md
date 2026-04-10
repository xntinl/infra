# Advanced Algorithms — Reference Overview

## Why This Section Matters

Algorithmic literacy separates engineers who can solve a problem from those who can
solve it at scale. The algorithms in this section are not academic curiosities: they
are the internal machinery of PostgreSQL query planners, Linux kernel schedulers,
Google's infrastructure tooling, DNA sequencing pipelines, and real-time GIS systems.

The gap between "knowing an algorithm exists" and "being able to apply it under
production constraints" is large. This section bridges that gap. Each subtopic starts
from the mental model — the pattern-recognition instinct that tells a senior engineer
"this is a bipartite matching problem" — and builds toward idiomatic, production-ready
implementations in both Go and Rust.

You will find here the algorithms that separate competitive programmers from systems
engineers: not just correctness, but cache behavior, amortized complexity, approximation
ratios when exact solutions are too expensive, and the theory that explains why your
dynamic array is fast even though individual inserts are slow.

## Subtopics

| # | Topic | Core Algorithms | Real-World Context | Difficulty |
|---|-------|----------------|-------------------|------------|
| 01 | [Advanced Dynamic Programming](./01-dynamic-programming-advanced/01-dynamic-programming-advanced.md) | Rerooting, bitmask DP, Knuth's opt., divide-and-conquer opt., aliens trick | Routing, combinatorial optimization, interval scheduling | Advanced |
| 02 | [Advanced Graph Algorithms](./02-graph-algorithms-advanced/02-graph-algorithms-advanced.md) | Tarjan SCC, Hopcroft-Karp, Gomory-Hu tree, HLD | Build systems, scheduling, network flow, tree queries | Advanced |
| 03 | [String Algorithms](./03-string-algorithms/03-string-algorithms.md) | Suffix array (SA-IS), LCP/Kasai, Aho-Corasick, Z-algorithm, suffix automaton | Search engines, DNA analysis, log parsing | Advanced |
| 04 | [Computational Geometry](./04-computational-geometry/04-computational-geometry.md) | Convex hull, sweep line, point-in-polygon, Delaunay triangulation | Collision detection, GPS, GIS, rendering | Advanced |
| 05 | [Randomized Algorithms](./05-randomized-algorithms/05-randomized-algorithms.md) | Treap, randomized QuickSort, skip list, reservoir sampling, Miller-Rabin | Databases, cryptography, streaming analytics | Advanced |
| 06 | [Approximation Algorithms](./06-approximation-algorithms/06-approximation-algorithms.md) | Vertex cover 2-approx, Christofides TSP, greedy set cover, FPTAS knapsack | NP-hard logistics, resource allocation, network design | Advanced |
| 07 | [Amortized Analysis](./07-amortized-analysis/07-amortized-analysis.md) | Potential method, accounting method, dynamic arrays, union-find, Fibonacci heap | Every data structure runtime proof, why "it's fast in practice" | Advanced |
| 08 | [Online Algorithms](./08-online-algorithms/08-online-algorithms.md) | Competitive analysis, ski rental, k-server, secretary problem | Cache eviction, load balancing, real-time scheduling | Advanced |

## Dependency Map

```
                    ┌─────────────────────────┐
                    │  Prerequisites           │
                    │  - Graph traversal (BFS/DFS)
                    │  - Basic DP              │
                    │  - Asymptotic notation   │
                    │  - Hash tables / trees   │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                   ▼
     ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
     │ 07 Amortized │   │ 01 Adv. DP   │   │ 02 Adv. Graph│
     │ Analysis     │   │              │   │              │
     └──────┬───────┘   └──────┬───────┘   └──────┬───────┘
            │                  │                   │
            │           ┌──────┴───────┐    ┌──────┴───────┐
            │           │ 06 Approx.   │    │ 03 Strings    │
            │           │ Algorithms   │    │              │
            │           └──────────────┘    └──────────────┘
            │
     ┌──────┴───────┐   ┌──────────────┐   ┌──────────────┐
     │ 05 Randomized│   │ 04 Comp. Geo.│   │ 08 Online    │
     │ Algorithms   │   │              │   │ Algorithms   │
     └──────────────┘   └──────────────┘   └──────────────┘
```

**Recommended reading order for newcomers to this material:**
1. Start with **07 — Amortized Analysis** if your intuition about "why dynamic arrays are O(1) amortized" is fuzzy. It underpins everything.
2. **05 — Randomized Algorithms** for probabilistic intuition (skip lists, treaps).
3. **01 — Advanced DP** and **02 — Advanced Graphs** can be read in parallel.
4. **03 — Strings**, **04 — Geometry**, **06 — Approximation**, **08 — Online** are independent.

## Time Investment

| Topic | Reading | Exercises (all 4) | Total |
|-------|---------|-------------------|-------|
| 01 — Advanced DP | 60–90 min | 20–35 h | ~37 h |
| 02 — Advanced Graphs | 60–90 min | 20–35 h | ~37 h |
| 03 — String Algorithms | 60–90 min | 20–35 h | ~37 h |
| 04 — Computational Geometry | 45–75 min | 18–30 h | ~32 h |
| 05 — Randomized Algorithms | 45–75 min | 18–30 h | ~32 h |
| 06 — Approximation Algorithms | 45–75 min | 18–30 h | ~32 h |
| 07 — Amortized Analysis | 45–60 min | 15–25 h | ~27 h |
| 08 — Online Algorithms | 45–75 min | 18–30 h | ~32 h |
| **Section total** | **~7–10 h** | **~147–250 h** | **~256 h** |

Exercises are optional but the "Production Scenario" (Exercise 4) in each subtopic is
where the real learning happens. Prioritize those if time is limited.

## Prerequisites

Before entering this section you should be comfortable with:

- **Graph traversal**: BFS, DFS, Dijkstra, topological sort
- **Basic dynamic programming**: 1D and 2D table DP, memoization vs tabulation
- **Asymptotic notation**: O(·), Ω(·), Θ(·) — and what they hide (constants, cache effects)
- **Data structures**: heaps, hash tables, balanced BSTs (AVL or red-black), segment trees
- **Probability basics**: expected value, linearity of expectation, random variables
- **Go or Rust**: idiomatic concurrency, error handling, standard library proficiency

If any of these feel shaky, the `01-fundamentals/` section of this repository covers them.
