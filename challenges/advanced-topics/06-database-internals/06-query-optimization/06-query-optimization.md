<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [cost-based-optimizer, cardinality-estimation, selectivity, join-ordering, hash-join, nested-loop-join, merge-join, system-r, explain-analyze]
languages: [go, rust]
estimated_reading_time: 85 min
bloom_level: analyze
prerequisites: [sql-basics, b-tree-indexes, statistics-fundamentals]
papers: [selinger-1979-system-r, ioannidis-1996-query-optimization, leis-2015-how-good]
industry_use: [postgresql-optimizer, mysql-optimizer, spark-catalyst, calcite]
language_contrast: low
-->

# Query Optimization

> A query optimizer's job is to convert a query that specifies *what* you want into a plan that efficiently *finds* it — and the quality of that plan depends entirely on how accurately the optimizer estimates the number of rows each operation will produce.

## Mental Model

The query optimizer faces an exponential search space. A join of 10 tables has 10! / 2 ≈ 1.8 million possible join orderings, and for each ordering there are multiple join algorithms (hash join, merge join, nested loop) and multiple access paths per table (sequential scan, index scan, bitmap scan). Evaluating every option costs more than just running the query.

The System R approach (Selinger et al., 1979) made this tractable with dynamic programming. The insight: if the optimal plan for joining tables {A, B, C} uses the optimal plan for {A, B} as a subplan, then you never need to reconsider how {A, B} are joined — you just combine the best {A, B} plan with C in all possible ways. This means the DP has O(2^n) states (one per subset of tables), not O(n!) — for 10 tables, that is 1024 states instead of 1.8 million.

The accuracy of the plan depends on cardinality estimates — how many rows each operation produces. A wrong cardinality estimate is the root cause of almost all plan regressions. If the optimizer estimates 100 rows for a join result but gets 1 million, it might choose a nested-loop join (good for 100 rows, catastrophic for 1 million) instead of a hash join (good for large inputs). Cardinality estimates come from column statistics: histograms (distribution of values), MCV lists (most common values), n_distinct (number of unique values). PostgreSQL collects these via `ANALYZE`; the optimizer applies selectivity formulas to estimate how many rows survive each filter.

## Core Concepts

### Cardinality Estimation and Selectivity

The selectivity of a predicate is the fraction of rows that satisfy it. For a table with N rows:
- `WHERE age = 25`: selectivity = 1 / n_distinct(age) ≈ 0.001 for 1000 unique ages → estimated 0.001 × N rows.
- `WHERE age BETWEEN 20 AND 30`: selectivity = (30 - 20) / (max_age - min_age) ≈ 0.1 if range is 100 → 0.1 × N rows.
- `WHERE age = 25 AND city = 'NYC'`: selectivity = sel(age=25) × sel(city=NYC) assuming independence = 0.001 × 0.02 = 0.00002.

The independence assumption is the Achilles heel of column statistics. Real data has correlations: young people and students, NYC and finance workers, expensive hotels and central locations. PostgreSQL 10+ added extended statistics (`CREATE STATISTICS`) for multi-column dependencies, but it must be explicitly created.

MCVs (Most Common Values) improve selectivity estimates for skewed distributions. If 30% of rows have `country = 'US'`, a histogram would under-estimate this selectivity by using the uniform assumption. The MCV list records the exact frequency of the top N values — PostgreSQL's `statistics_target` (default 100) controls how many MCVs are stored.

### Join Algorithms: When Each Wins

**Nested Loop Join**: For each row in the outer relation, scan the inner relation for matching rows. Complexity: O(outer × inner) without an index, O(outer × log(inner)) with an index on the inner relation. Best when: the outer is small (< 100 rows) and the inner has an index on the join key. Worst case: two large tables without indexes — quadratic.

**Hash Join**: Build a hash table from the smaller relation (the "build" relation). Scan the larger relation (the "probe" relation) and look up each row in the hash table. Complexity: O(outer + inner), but requires O(build_size) memory. Best when: neither relation has an index on the join key, and the build relation fits in memory (hash join requires the hash table to fit in `work_mem`). A hash join that exceeds `work_mem` spills to disk ("batch hash join") — still O(outer + inner) but with additional I/O.

**Merge Join**: Both relations must be sorted on the join key. Then a single pass of each suffices: advance a pointer in each relation, matching rows as they align. Complexity: O(n log n) for the sort + O(outer + inner) for the merge, or O(outer + inner) if both are already sorted (index scans). Best when: both relations have indexes on the join key (making them pre-sorted), or when results need to be sorted anyway (the sort cost is paid once, not for each subsequent consumer).

### Join Ordering with Dynamic Programming

The System R optimizer uses left-deep join trees — always joining a single relation into the current result. This restricts the search space from O(2^n) arbitrary trees to O(n!) left-deep trees, but DP reduces it to O(n × 2^n) which is tractable for n ≤ 10.

The DP computes `best_plan[S]` for each subset S of relations:
- Base case: `best_plan[{R}]` = best access path for R (sequential scan or index scan).
- Inductive: `best_plan[S] = min over all R in S of cost(best_plan[S\{R}] ⋈ R)`.

Cost is estimated as: `cpu_cost × num_rows + io_cost × num_pages`. Each plan node has both an output cardinality and a cost; the optimizer selects the plan with minimum total cost for the entire query tree.

### Index Selection

The optimizer considers an index only if the index columns match the predicates in the query. An index on `(a, b)` is useful for `WHERE a = 5` (prefix match) and `WHERE a = 5 AND b = 3`, but not for `WHERE b = 3` alone (non-prefix). For range predicates (`WHERE a BETWEEN 1 AND 10`), the index is useful but only for the range scan; equality predicates have lower selectivity and are preferred as the leading column.

Bitmap index scans (PostgreSQL) are a special case: the optimizer can combine multiple single-column indexes with AND/OR operations by building a bitmap of matching page numbers, then fetching only those pages. This allows using two partial indexes where a single composite index does not exist.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
	"sort"
)

// TableStats holds statistics that the optimizer uses for cardinality estimation.
type TableStats struct {
	TableName  string
	RowCount   int64
	PageCount  int64
	Columns    map[string]*ColumnStats
}

type ColumnStats struct {
	ColumnName string
	NDistinct  float64  // number of distinct values (negative = fraction of total)
	NullFrac   float64  // fraction of rows with NULL
	AvgWidth   int      // average byte width
	MCVValues  []interface{} // most common values
	MCVFreqs   []float64     // their frequencies
	HistBounds []float64     // histogram bucket boundaries (for range predicates)
}

// selectivity estimates the fraction of rows matching a predicate.
// This mirrors PostgreSQL's clauselist_selectivity().
func selectivity(stats *TableStats, col string, op string, val float64) float64 {
	cs, ok := stats.Columns[col]
	if !ok {
		return 0.1 // unknown column: default 10%
	}

	switch op {
	case "=":
		// Check MCV list first
		for i, mcv := range cs.MCVValues {
			if v, ok := mcv.(float64); ok && v == val {
				return cs.MCVFreqs[i]
			}
		}
		// Not in MCV: uniform distribution over remaining values
		mcvFreqSum := 0.0
		for _, f := range cs.MCVFreqs {
			mcvFreqSum += f
		}
		remainingFrac := 1.0 - mcvFreqSum
		remainingDistinct := cs.NDistinct - float64(len(cs.MCVValues))
		if remainingDistinct <= 0 {
			remainingDistinct = 1
		}
		return remainingFrac / remainingDistinct

	case "<", ">":
		// Histogram-based range selectivity
		if len(cs.HistBounds) < 2 {
			return 0.1
		}
		minVal := cs.HistBounds[0]
		maxVal := cs.HistBounds[len(cs.HistBounds)-1]
		if maxVal == minVal {
			return 0.5
		}
		if op == "<" {
			sel := (val - minVal) / (maxVal - minVal)
			return math.Max(0, math.Min(1, sel))
		}
		sel := (maxVal - val) / (maxVal - minVal)
		return math.Max(0, math.Min(1, sel))

	case "between":
		// val is the low; caller would pass high separately in a real impl
		return 0.05 // simplified
	}
	return 0.1
}

// PlanNode is one node in the query execution plan tree.
type PlanNode struct {
	Type        string    // "SeqScan", "IndexScan", "HashJoin", "MergeJoin", "NestedLoop"
	TableName   string
	IndexName   string
	Predicate   string
	Children    []*PlanNode
	EstRows     float64   // estimated output rows
	EstCost     float64   // estimated total cost (in "cost units")
	ActualRows  int64     // filled in after execution
	ActualCost  float64
}

func (p *PlanNode) String() string {
	s := fmt.Sprintf("%s (rows=%.0f cost=%.2f)", p.Type, p.EstRows, p.EstCost)
	if p.TableName != "" {
		s += fmt.Sprintf(" on %s", p.TableName)
	}
	if p.Predicate != "" {
		s += fmt.Sprintf(" [%s]", p.Predicate)
	}
	return s
}

// costParams mirrors PostgreSQL's cost constants (in arbitrary units where one
// sequential page read = seq_page_cost = 1.0).
var costParams = struct {
	seqPageCost    float64 // cost of reading one page sequentially
	randPageCost   float64 // cost of reading one page randomly (index scan)
	cpuTupleCost   float64 // cost of processing one tuple
	cpuIndexCost   float64 // cost of one index comparison
	cpuHashCost    float64 // cost of one hash operation
}{
	seqPageCost:  1.0,
	randPageCost: 4.0,  // NVMe SSDs: set to 1.1; spinning disk: 4.0
	cpuTupleCost: 0.01,
	cpuIndexCost: 0.005,
	cpuHashCost:  0.0025,
}

// planSeqScan estimates the cost of a sequential scan with optional filter.
func planSeqScan(stats *TableStats, predCol, predOp string, predVal float64) *PlanNode {
	sel := 1.0
	predicate := "none"
	if predCol != "" {
		sel = selectivity(stats, predCol, predOp, predVal)
		predicate = fmt.Sprintf("%s %s %.1f", predCol, predOp, predVal)
	}
	estRows := float64(stats.RowCount) * sel

	// Sequential scan: read all pages + process all tuples
	cost := float64(stats.PageCount)*costParams.seqPageCost +
		float64(stats.RowCount)*costParams.cpuTupleCost

	return &PlanNode{
		Type:      "SeqScan",
		TableName: stats.TableName,
		Predicate: predicate,
		EstRows:   math.Max(1, estRows),
		EstCost:   cost,
	}
}

// planIndexScan estimates the cost of an index scan.
func planIndexScan(stats *TableStats, indexName, predCol, predOp string, predVal float64) *PlanNode {
	sel := selectivity(stats, predCol, predOp, predVal)
	estRows := float64(stats.RowCount) * sel

	// Index scan: random I/Os for index pages + heap page fetches
	// Number of index pages visited: O(log(N)) for B-tree traversal
	indexPages := math.Log2(float64(stats.RowCount)/100+1) + 1
	heapPages := math.Min(estRows, float64(stats.PageCount)) // at most PageCount fetches

	cost := indexPages*costParams.randPageCost +
		heapPages*costParams.randPageCost +
		estRows*(costParams.cpuIndexCost+costParams.cpuTupleCost)

	return &PlanNode{
		Type:      "IndexScan",
		TableName: stats.TableName,
		IndexName: indexName,
		Predicate: fmt.Sprintf("%s %s %.1f", predCol, predOp, predVal),
		EstRows:   math.Max(1, estRows),
		EstCost:   cost,
	}
}

// planHashJoin estimates the cost of a hash join.
// The build relation (smaller) is hashed; the probe relation scans and looks up.
func planHashJoin(build, probe *PlanNode) *PlanNode {
	// Build phase: process all build rows into hash table
	buildCost := build.EstCost + build.EstRows*costParams.cpuHashCost
	// Probe phase: process all probe rows, looking up each in hash table
	probeCost := probe.EstCost + probe.EstRows*costParams.cpuHashCost

	// Join selectivity: simplified to 1.0 / max(distinct_values) — assume foreign key join
	joinSel := 0.01
	estRows := build.EstRows * probe.EstRows * joinSel

	return &PlanNode{
		Type:     "HashJoin",
		Children: []*PlanNode{build, probe},
		EstRows:  math.Max(1, estRows),
		EstCost:  buildCost + probeCost,
	}
}

// planNestedLoop estimates the cost of a nested loop join with index on inner.
func planNestedLoop(outer, inner *PlanNode, innerHasIndex bool) *PlanNode {
	var innerLoopCost float64
	if innerHasIndex {
		// With index: each outer row does one index lookup on inner
		innerLoopCost = inner.EstCost // per-row cost of index scan
	} else {
		// Without index: full scan of inner for each outer row
		innerLoopCost = inner.EstCost * outer.EstRows
	}

	totalCost := outer.EstCost + innerLoopCost
	joinSel := 0.01
	estRows := outer.EstRows * inner.EstRows * joinSel

	return &PlanNode{
		Type:     "NestedLoop",
		Children: []*PlanNode{outer, inner},
		EstRows:  math.Max(1, estRows),
		EstCost:  totalCost,
	}
}

// Optimizer uses dynamic programming to find the cheapest join order.
// Simplified: considers only left-deep plans and two join algorithms.
type Optimizer struct {
	stats map[string]*TableStats
}

// PlanJoin finds the minimum-cost plan to join a set of tables.
// Uses the System R dynamic programming approach.
func (o *Optimizer) PlanJoin(tables []string, predicates map[string]string) *PlanNode {
	// dp[bitmask] = cheapest plan for that subset of tables
	n := len(tables)
	dp := make(map[int]*PlanNode)

	// Base cases: single-table access paths
	for i, t := range tables {
		stats := o.stats[t]
		// Try both SeqScan and IndexScan if available; pick cheaper
		var bestPlan *PlanNode

		seqPlan := planSeqScan(stats, "", "", 0)
		bestPlan = seqPlan

		if pred, ok := predicates[t]; ok {
			// Simplified: predicate is "col=val"; try index scan
			indexPlan := planIndexScan(stats, t+"_idx", pred, "=", 50)
			if indexPlan.EstCost < bestPlan.EstCost {
				bestPlan = indexPlan
			}
		}
		dp[1<<i] = bestPlan
	}

	// Fill DP for subsets of size 2..n
	for size := 2; size <= n; size++ {
		// Generate all subsets of size `size`
		subsets := subsets(n, size)
		for _, mask := range subsets {
			var bestPlan *PlanNode
			// Try adding each table as the last joined relation
			for i := 0; i < n; i++ {
				if mask&(1<<i) == 0 {
					continue
				}
				subMask := mask ^ (1 << i)
				if dp[subMask] == nil {
					continue
				}
				left := dp[subMask]
				right := dp[1<<i]

				// Try hash join
				hj := planHashJoin(right, left) // build on smaller (right)
				if right.EstRows > left.EstRows {
					hj = planHashJoin(left, right)
				}

				// Try nested loop (assume index on inner if inner is a single-table plan)
				innerHasIndex := right.Type == "IndexScan"
				nl := planNestedLoop(left, right, innerHasIndex)

				// Pick cheaper
				var candidate *PlanNode
				if hj.EstCost < nl.EstCost {
					candidate = hj
				} else {
					candidate = nl
				}

				if bestPlan == nil || candidate.EstCost < bestPlan.EstCost {
					bestPlan = candidate
				}
			}
			if bestPlan != nil {
				dp[mask] = bestPlan
			}
		}
	}

	// Result: plan for the full set of tables
	fullMask := (1 << n) - 1
	return dp[fullMask]
}

// subsets generates all bitmasks for subsets of {0..n-1} of a given size.
func subsets(n, size int) []int {
	var result []int
	var gen func(start, curr, remaining int)
	gen = func(start, curr, remaining int) {
		if remaining == 0 {
			result = append(result, curr)
			return
		}
		for i := start; i < n; i++ {
			gen(i+1, curr|(1<<i), remaining-1)
		}
	}
	gen(0, 0, size)
	return result
}

// explainPlan prints a human-readable EXPLAIN output (like PostgreSQL's).
func explainPlan(plan *PlanNode, indent int) {
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "   "
	}
	if indent > 0 {
		prefix = prefix[:len(prefix)-1] + "-> "
	}
	fmt.Printf("%s%s\n", prefix, plan.String())
	for _, child := range plan.Children {
		explainPlan(child, indent+1)
	}
}

func main() {
	// Simulate an orders/customers/products schema
	opts := &Optimizer{
		stats: map[string]*TableStats{
			"orders": {
				TableName: "orders",
				RowCount:  1_000_000,
				PageCount: 20_000,
				Columns: map[string]*ColumnStats{
					"customer_id": {NDistinct: 50000, HistBounds: []float64{1, 100000}},
					"total":       {NDistinct: 10000, HistBounds: []float64{0, 10000},
						MCVValues: []interface{}{float64(99)}, MCVFreqs: []float64{0.05}},
				},
			},
			"customers": {
				TableName: "customers",
				RowCount:  100_000,
				PageCount: 2_000,
				Columns: map[string]*ColumnStats{
					"id":      {NDistinct: 100000},
					"country": {NDistinct: 50, MCVValues: []interface{}{float64(1)}, MCVFreqs: []float64{0.3}},
				},
			},
			"products": {
				TableName: "products",
				RowCount:  10_000,
				PageCount: 200,
				Columns: map[string]*ColumnStats{
					"id":       {NDistinct: 10000},
					"category": {NDistinct: 20, HistBounds: []float64{1, 20}},
				},
			},
		},
	}

	fmt.Println("=== Query Optimizer Demo ===")
	fmt.Println("Query: SELECT * FROM orders JOIN customers ON orders.customer_id = customers.id")
	fmt.Println("       JOIN products ON orders.product_id = products.id")
	fmt.Println("       WHERE customers.country = 'US'")
	fmt.Println()

	tables := []string{"orders", "customers", "products"}
	predicates := map[string]string{
		"customers": "country",
	}

	plan := opts.PlanJoin(tables, predicates)

	fmt.Println("Optimal plan:")
	explainPlan(plan, 0)

	// Compare: sequential scan vs index scan selectivity
	fmt.Println("\n=== Selectivity Estimation ===")
	ordersStats := opts.stats["orders"]

	// Equality predicate on a skewed column (total=99 is an MCV with 5% frequency)
	selEq := selectivity(ordersStats, "total", "=", 99)
	fmt.Printf("SELECT * FROM orders WHERE total = 99: selectivity = %.4f (MCV hit)\n", selEq)

	// Range predicate
	selRange := selectivity(ordersStats, "total", "<", 1000)
	fmt.Printf("SELECT * FROM orders WHERE total < 1000: selectivity = %.4f\n", selRange)

	// Access path comparison for orders with total=99
	fmt.Println("\n=== Access Path: SeqScan vs IndexScan (total=99) ===")
	seq := planSeqScan(ordersStats, "total", "=", 99)
	idx := planIndexScan(ordersStats, "orders_total_idx", "total", "=", 99)
	fmt.Printf("SeqScan:   rows=%.0f cost=%.2f\n", seq.EstRows, seq.EstCost)
	fmt.Printf("IndexScan: rows=%.0f cost=%.2f\n", idx.EstRows, idx.EstCost)

	if idx.EstCost < seq.EstCost {
		fmt.Println("Optimizer chooses: IndexScan")
	} else {
		fmt.Println("Optimizer chooses: SeqScan (index scan not worth it at this selectivity)")
	}

	// Show join algorithm selection for different table sizes
	fmt.Println("\n=== Join Algorithm Selection ===")
	smallTable := &PlanNode{Type: "SeqScan", TableName: "small", EstRows: 100, EstCost: 50}
	largeTable := &PlanNode{Type: "SeqScan", TableName: "large", EstRows: 1_000_000, EstCost: 20_000}
	candidates := []*PlanNode{
		planHashJoin(smallTable, largeTable),
		planNestedLoop(smallTable, largeTable, false),
		planNestedLoop(smallTable, largeTable, true),
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].EstCost < candidates[j].EstCost
	})
	for _, c := range candidates {
		innerHasIdx := ""
		if c.Type == "NestedLoop" && len(c.Children) > 1 && c.Children[1].Type == "IndexScan" {
			innerHasIdx = " (inner indexed)"
		}
		fmt.Printf("%s%s: cost=%.2f\n", c.Type, innerHasIdx, c.EstCost)
	}
}
```

### Go-specific considerations

The DP table `map[int]*PlanNode` uses integers as bitmasks representing subsets of relations. For small query sizes (n ≤ 20), this is practical. For larger queries, PostgreSQL uses a hash map keyed by `Relids` (bitset of relation IDs). The Go `map[int]` provides O(1) amortized access, but the hash function for small integers is trivial, making this effectively O(1).

The cost model constants (`seqPageCost`, `randPageCost`) are calibrated for specific hardware. PostgreSQL recommends setting `random_page_cost = 1.1` for SSDs (vs the default 4.0 for spinning disks) to allow the optimizer to prefer index scans over sequential scans. This single parameter change can dramatically improve performance for OLTP workloads that are currently doing unnecessary sequential scans.

## Implementation: Rust

```rust
use std::collections::HashMap;

#[derive(Debug, Clone)]
struct ColumnStats {
    n_distinct:   f64,
    null_frac:    f64,
    hist_bounds:  Vec<f64>,
    mcv_values:   Vec<f64>,
    mcv_freqs:    Vec<f64>,
}

#[derive(Debug, Clone)]
struct TableStats {
    name:      String,
    row_count: i64,
    page_count: i64,
    columns:   HashMap<String, ColumnStats>,
}

fn selectivity(stats: &TableStats, col: &str, op: &str, val: f64) -> f64 {
    let cs = match stats.columns.get(col) {
        Some(c) => c,
        None => return 0.1,
    };
    match op {
        "=" => {
            for (i, &v) in cs.mcv_values.iter().enumerate() {
                if (v - val).abs() < f64::EPSILON {
                    return cs.mcv_freqs[i];
                }
            }
            let mcv_sum: f64 = cs.mcv_freqs.iter().sum();
            let remaining = (1.0 - mcv_sum)
                / (cs.n_distinct - cs.mcv_values.len() as f64).max(1.0);
            remaining
        }
        "<" => {
            if cs.hist_bounds.len() < 2 { return 0.1; }
            let min = cs.hist_bounds[0];
            let max = cs.hist_bounds[cs.hist_bounds.len()-1];
            if max == min { return 0.5; }
            ((val - min) / (max - min)).clamp(0.0, 1.0)
        }
        _ => 0.1,
    }
}

#[derive(Debug, Clone)]
struct PlanNode {
    node_type:  String,
    table_name: String,
    index_name: String,
    predicate:  String,
    children:   Vec<PlanNode>,
    est_rows:   f64,
    est_cost:   f64,
}

impl PlanNode {
    fn explain(&self, indent: usize) {
        let prefix = if indent == 0 {
            String::new()
        } else {
            "   ".repeat(indent - 1) + "-> "
        };
        let table = if !self.table_name.is_empty() { format!(" on {}", self.table_name) } else { String::new() };
        let pred = if !self.predicate.is_empty() { format!(" [{}]", self.predicate) } else { String::new() };
        println!("{}{}{}{} (rows={:.0} cost={:.2})",
            prefix, self.node_type, table, pred, self.est_rows, self.est_cost);
        for child in &self.children {
            child.explain(indent + 1);
        }
    }
}

const SEQ_PAGE_COST:  f64 = 1.0;
const RAND_PAGE_COST: f64 = 1.1; // SSD setting
const CPU_TUPLE_COST: f64 = 0.01;
const CPU_INDEX_COST: f64 = 0.005;
const CPU_HASH_COST:  f64 = 0.0025;

fn plan_seq_scan(stats: &TableStats, pred_col: Option<(&str, &str, f64)>) -> PlanNode {
    let (sel, predicate) = match pred_col {
        Some((col, op, val)) => (
            selectivity(stats, col, op, val),
            format!("{} {} {}", col, op, val),
        ),
        None => (1.0, "none".to_string()),
    };
    let est_rows = (stats.row_count as f64 * sel).max(1.0);
    let cost = stats.page_count as f64 * SEQ_PAGE_COST
        + stats.row_count as f64 * CPU_TUPLE_COST;
    PlanNode {
        node_type: "SeqScan".to_string(),
        table_name: stats.name.clone(),
        index_name: String::new(),
        predicate,
        children: vec![],
        est_rows,
        est_cost: cost,
    }
}

fn plan_index_scan(stats: &TableStats, index_name: &str, pred_col: &str, op: &str, val: f64) -> PlanNode {
    let sel = selectivity(stats, pred_col, op, val);
    let est_rows = (stats.row_count as f64 * sel).max(1.0);
    let index_pages = (stats.row_count as f64 / 100.0 + 1.0).log2() + 1.0;
    let heap_pages = est_rows.min(stats.page_count as f64);
    let cost = (index_pages + heap_pages) * RAND_PAGE_COST
        + est_rows * (CPU_INDEX_COST + CPU_TUPLE_COST);
    PlanNode {
        node_type: "IndexScan".to_string(),
        table_name: stats.name.clone(),
        index_name: index_name.to_string(),
        predicate: format!("{} {} {}", pred_col, op, val),
        children: vec![],
        est_rows,
        est_cost: cost,
    }
}

fn plan_hash_join(build: PlanNode, probe: PlanNode) -> PlanNode {
    let (build, probe) = if build.est_rows <= probe.est_rows { (build, probe) } else { (probe, build) };
    let cost = build.est_cost + build.est_rows * CPU_HASH_COST
        + probe.est_cost + probe.est_rows * CPU_HASH_COST;
    let est_rows = (build.est_rows * probe.est_rows * 0.01).max(1.0);
    PlanNode {
        node_type: "HashJoin".to_string(),
        table_name: String::new(),
        index_name: String::new(),
        predicate: String::new(),
        children: vec![build, probe],
        est_rows,
        est_cost: cost,
    }
}

fn plan_nested_loop(outer: PlanNode, inner: PlanNode) -> PlanNode {
    let cost = outer.est_cost + inner.est_cost * outer.est_rows;
    let est_rows = (outer.est_rows * inner.est_rows * 0.01).max(1.0);
    PlanNode {
        node_type: "NestedLoop".to_string(),
        table_name: String::new(),
        index_name: String::new(),
        predicate: String::new(),
        children: vec![outer, inner],
        est_rows,
        est_cost: cost,
    }
}

struct Optimizer {
    stats: HashMap<String, TableStats>,
}

impl Optimizer {
    fn plan_join(&self, tables: &[&str]) -> Option<PlanNode> {
        let n = tables.len();
        let mut dp: HashMap<usize, PlanNode> = HashMap::new();

        // Base case: single table plans
        for (i, &t) in tables.iter().enumerate() {
            let stats = self.stats.get(t)?;
            let seq = plan_seq_scan(stats, None);
            let idx = plan_index_scan(stats, &format!("{}_id_idx", t), "id", "=", 1.0);
            let best = if idx.est_cost < seq.est_cost { idx } else { seq };
            dp.insert(1 << i, best);
        }

        // Fill DP for subsets
        for size in 2..=n {
            for mask in 0..(1usize << n) {
                if mask.count_ones() as usize != size { continue; }
                let mut best: Option<PlanNode> = None;

                for i in 0..n {
                    if mask & (1 << i) == 0 { continue; }
                    let sub_mask = mask ^ (1 << i);
                    let left = match dp.get(&sub_mask) {
                        Some(p) => p.clone(),
                        None => continue,
                    };
                    let right = match dp.get(&(1 << i)) {
                        Some(p) => p.clone(),
                        None => continue,
                    };

                    let hj = plan_hash_join(left.clone(), right.clone());
                    let nl = plan_nested_loop(left, right);
                    let candidate = if hj.est_cost < nl.est_cost { hj } else { nl };

                    if best.as_ref().map_or(true, |b| candidate.est_cost < b.est_cost) {
                        best = Some(candidate);
                    }
                }
                if let Some(p) = best { dp.insert(mask, p); }
            }
        }

        let full_mask = (1 << n) - 1;
        dp.remove(&full_mask)
    }
}

fn main() {
    let mut stats = HashMap::new();
    stats.insert("orders".to_string(), TableStats {
        name: "orders".to_string(),
        row_count: 1_000_000,
        page_count: 20_000,
        columns: {
            let mut m = HashMap::new();
            m.insert("total".to_string(), ColumnStats {
                n_distinct: 10_000.0,
                null_frac: 0.0,
                hist_bounds: vec![0.0, 10_000.0],
                mcv_values: vec![99.0],
                mcv_freqs:  vec![0.05],
            });
            m
        },
    });
    stats.insert("customers".to_string(), TableStats {
        name: "customers".to_string(),
        row_count: 100_000,
        page_count: 2_000,
        columns: {
            let mut m = HashMap::new();
            m.insert("id".to_string(), ColumnStats {
                n_distinct: 100_000.0,
                null_frac: 0.0,
                hist_bounds: vec![1.0, 100_000.0],
                mcv_values: vec![],
                mcv_freqs: vec![],
            });
            m
        },
    });
    stats.insert("products".to_string(), TableStats {
        name: "products".to_string(),
        row_count: 10_000,
        page_count: 200,
        columns: {
            let mut m = HashMap::new();
            m.insert("id".to_string(), ColumnStats {
                n_distinct: 10_000.0,
                null_frac: 0.0,
                hist_bounds: vec![1.0, 10_000.0],
                mcv_values: vec![],
                mcv_freqs: vec![],
            });
            m
        },
    });

    let optimizer = Optimizer { stats };

    println!("=== Rust Query Optimizer Demo ===");
    if let Some(plan) = optimizer.plan_join(&["orders", "customers", "products"]) {
        println!("Optimal join plan:");
        plan.explain(0);
    }

    // Access path comparison
    let orders = optimizer.stats.get("orders").unwrap();
    let seq = plan_seq_scan(orders, Some(("total", "=", 99.0)));
    let idx = plan_index_scan(orders, "orders_total_idx", "total", "=", 99.0);
    println!("\nAccess path comparison (total = 99, selectivity={:.4}):",
        selectivity(orders, "total", "=", 99.0));
    println!("  SeqScan:   rows={:.0} cost={:.2}", seq.est_rows, seq.est_cost);
    println!("  IndexScan: rows={:.0} cost={:.2}", idx.est_rows, idx.est_cost);
    println!("  Winner: {}", if idx.est_cost < seq.est_cost { "IndexScan" } else { "SeqScan" });
}
```

### Rust-specific considerations

`HashMap<usize, PlanNode>` for the DP table uses `usize` bitmasks as keys — efficient for small n. `PlanNode` is cloned during DP construction because the same subtree may be referenced in multiple candidate plans. This is acceptable here (cloning is O(depth)) but production optimizers use reference-counted plan nodes or arena allocation to avoid cloning.

`clamp(0.0, 1.0)` on the selectivity calculation prevents returning negative or > 1.0 values when the predicate value is outside the histogram bounds — a subtle source of incorrect cardinality estimates in production optimizers.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| DP table | `map[int]*PlanNode` — pointer keys, GC manages nodes | `HashMap<usize, PlanNode>` — value keys, explicit clone |
| Cost comparison | `float64` comparison | `f64` comparison — identical semantics |
| Bitmask subset generation | Recursive closure (clean) | Two-level loop with `count_ones()` check |
| Statistics structures | `map[string]*ColumnStats` | `HashMap<String, ColumnStats>` |
| Plan node tree | Pointer-linked `*PlanNode` | Owned `PlanNode` with `Vec<PlanNode>` children |

## Production War Stories

**PostgreSQL cardinality misestimation and the n+1 problem**: A production Rails application had a query joining 6 tables with a complex WHERE clause. PostgreSQL estimated 10 rows for a key subquery but got 80,000 — off by 8,000x. The root cause: correlated columns (city and zip_code — in the US, a zip code uniquely determines a city). The optimizer applied the independence assumption: P(city='Boston' AND zip='02101') = P(city='Boston') × P(zip='02101') ≈ 0.001 × 0.001 = 0.000001, but the actual selectivity was 0.001 (since zip uniquely determines city). The fix: `CREATE STATISTICS ON (city, zip_code) FROM addresses;` to teach the optimizer about the dependency.

**MySQL optimizer's inability to reorder joins with subqueries (pre-8.0)**: MySQL's optimizer (prior to version 8.0) could not merge subqueries into the main query plan — a subquery was always executed as a dependent subquery (a nested loop with the subquery executed once per outer row). A query like `SELECT * FROM orders WHERE customer_id IN (SELECT id FROM customers WHERE country = 'US')` would execute the subquery for every row in orders. PostgreSQL's optimizer flattens this into a semi-join. Rewriting MySQL queries to use JOINs instead of IN (SELECT ...) subqueries was a common performance fix that is less necessary in MySQL 8.0+ but still appears in legacy codebases.

**Spark Catalyst's adaptive query execution**: Apache Spark 3.0 introduced Adaptive Query Execution (AQE): the optimizer re-optimizes the plan at runtime based on actual row counts from completed stages. This directly addresses the cardinality estimation problem: if stage 1 produces 1 million rows instead of the estimated 100,000, AQE re-plans stage 2 to use a different join strategy. The trade-off: AQE adds runtime overhead for the re-planning step and cannot be used for stages that are part of a dependency cycle. It is most effective for queries with multiple shuffles (where each shuffle's output size can be measured before planning the next).

## Complexity Analysis

| Component | Complexity | Notes |
|-----------|------------|-------|
| DP join ordering (n tables) | O(2^n × n) plans evaluated | n ≤ 10 in most DBs; threshold for heuristics |
| Cardinality estimation | O(predicates × columns) | Per-predicate selectivity calculation |
| Selectivity with MCV | O(MCV_count) | Linear scan of MCV list |
| Selectivity with histogram | O(log(buckets)) | Binary search in bucket bounds |
| Hash join (in-memory) | O(build + probe) | O(M) memory for build-side hash table |
| Nested loop with index | O(outer × log(inner)) | O(log N) per outer row for index lookup |
| Merge join | O(n log n) | Sort both sides then O(n) merge |

The exponential join ordering DP (O(2^n × n)) becomes impractical for n > 15-20 tables. PostgreSQL switches to a genetic algorithm (GEQO) when the number of relations exceeds `geqo_threshold` (default 12). Spark and Calcite use similar heuristics. The lesson: for OLTP queries with ≤ 8 joins, DP is fine. For data warehouse queries joining 20+ tables, the optimizer must use heuristics.

## Common Pitfalls

**Pitfall 1: Stale statistics causing wrong plan selection**

The optimizer uses statistics collected by the last `ANALYZE` run. If a table's data distribution changes significantly after `ANALYZE` (batch import, data migration, seasonal variation), the optimizer works with stale statistics and may choose wrong plans. Monitor `pg_stat_user_tables.last_analyze` — tables that have not been analyzed in > 24 hours and have high `n_dead_tup` or `n_mod_since_analyze` need re-analysis. For tables with predictable data skew (time-series tables where recent data is accessed most), consider sampling strategies that weight recent data more heavily.

**Pitfall 2: Incorrect row estimates from column correlation (independence assumption)**

When two columns have a statistical dependency (zip_code → city, product_category → price_range), the optimizer applies the independence assumption and underestimates the combined selectivity. The symptom: EXPLAIN shows `rows=10` but EXPLAIN ANALYZE shows `actual rows=10000`. The fix is explicit multi-column statistics (`CREATE STATISTICS`). Extended statistics require knowing which column pairs are correlated — tools like `pg_stats_extended` can help identify them.

**Pitfall 3: Nested loop join chosen for large inner table due to wrong selectivity**

When the optimizer underestimates the outer table's row count (due to stale statistics), it may choose a nested loop join: "100 rows × index scan on inner = cheap." If the outer actually produces 100,000 rows, the nested loop does 100,000 index scans — catastrophically slow. The fix: update statistics (`ANALYZE`), or use query hints (`enable_nestloop = off` in PostgreSQL) as a temporary measure while investigating the root cause.

**Pitfall 4: Index not used due to implicit type cast**

`WHERE id = '42'` (string '42' compared to integer id) triggers an implicit cast. In PostgreSQL, `id::text = '42'` is not index-compatible — the index is on `id` (integer), but the comparison is on `id::text`. The optimizer correctly avoids the index. The symptom: a query with an index on `id` does a sequential scan. The fix: ensure the literal type matches the column type: `WHERE id = 42` (not `'42'`). ORMs sometimes generate this bug when they map integer IDs as strings.

**Pitfall 5: Over-indexing causing optimizer confusion**

Counterintuitively, too many indexes can hurt performance. With 10 indexes on a table, the optimizer considers all of them as access paths, increasing planning time. More importantly, the optimizer may choose a bitmap index scan combining 3 partial indexes when a single composite index would be more efficient. The rule: add indexes to solve measured performance problems, not preemptively. Each index has a storage cost (typically 10-30% of the table size), a write overhead (every insert/update/delete must update the index), and an optimizer evaluation overhead.

## Exercises

**Exercise 1** (30 min): Run `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` on a multi-table join in your PostgreSQL instance. Identify the join algorithm used for each join (HashJoin, MergeJoin, NestedLoop), the estimated vs actual rows, and the buffer hit/miss counts. Find a node where estimated rows differ from actual rows by more than 10x. Use `pg_stats` to understand why.

**Exercise 2** (2-4h): Extend the Go optimizer to support merge join: add `planMergeJoin(outer, inner PlanNode, bothIndexed bool) PlanNode` that estimates merge join cost (sort cost if not indexed + merge scan cost). Verify that the optimizer selects merge join when both relations have indexes on the join key.

**Exercise 3** (4-8h): Implement cardinality estimation with histogram buckets using equi-depth histograms (each bucket contains the same number of rows, but different value ranges). Compare estimation accuracy to equi-width histograms (equal value ranges) using a dataset with a skewed distribution (Zipfian). Measure mean absolute percentage error across 1000 random range queries.

**Exercise 4** (8-15h): Implement an adaptive query execution component in Rust: after each plan node executes, compare actual vs estimated row counts. If the ratio exceeds 10x, trigger a re-plan of the remaining unexecuted portion of the plan tree. Use a mock execution engine that returns row counts from a pre-loaded statistics table. Benchmark planning overhead vs improvement in plan quality across 100 queries.

## Further Reading

### Foundational Papers
- Selinger, P. et al. (1979). "Access Path Selection in a Relational Database Management System." *SIGMOD*, 23–34. The System R optimizer; the foundation of all modern cost-based optimizers.
- Leis, V. et al. (2015). "How Good Are Query Optimizers, Really?" *PVLDB*, 9(3), 204–215. Empirical study showing cardinality estimation is the dominant source of bad plans.
- Ioannidis, Y. (1996). "Query Optimization." *ACM Computing Surveys*, 28(1), 121–123. Comprehensive survey.

### Books
- Ramakrishnan, R. & Gehrke, J. (2002). *Database Management Systems* (3rd ed.). Chapters 12-15 cover query optimization in detail.
- Hellerstein, J. et al. (2007). "Architecture of a Database System." *Foundations and Trends in Databases*. Section 4 covers query optimization in production systems.

### Production Code to Read
- `postgres/src/backend/optimizer/path/` — access path generation
- `postgres/src/backend/optimizer/plan/planner.c` — top-level plan generation
- `apache/calcite/core/src/main/java/org/apache/calcite/plan/` — Calcite's rule-based + cost-based optimizer

### Talks
- Neumann, T. (VLDB 2009): "Query Simplification: Graceful Degradation for Join-Order Optimization" — handling large numbers of joins
- Larson, P.A. (SIGMOD 2016): "Cardinality Estimation Done Right: Index-Based Join Sampling" — sampling-based cardinality estimation
