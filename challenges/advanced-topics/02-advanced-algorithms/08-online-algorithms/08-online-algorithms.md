<!--
type: reference
difficulty: advanced
section: [02-advanced-algorithms]
concepts: [competitive-analysis, ski-rental-problem, k-server-problem, secretary-problem, optimal-stopping, lru-analysis]
languages: [go, rust]
estimated_reading_time: 45-75 min
bloom_level: analyze
prerequisites: [probability-basics, greedy-algorithms, basic-data-structures]
papers: [borodin-el-yaniv-1998-online-computation, sleator-tarjan-1985-amortized, mcdiarmid-1990-secretary]
industry_use: [lru-cache-varnish-redis, cdn-cache-eviction, kubernetes-autoscaler, load-balancers, trading-systems]
language_contrast: low
-->

# Online Algorithms

> An online algorithm makes irrevocable decisions without knowing future inputs — yet can still be proven to perform within a bounded factor of the optimal offline solution that knew everything in advance.

## Mental Model

An **online algorithm** sees inputs one at a time and must respond immediately, without
knowledge of future inputs. An **offline algorithm** (the omniscient adversary) sees all
inputs at once and makes optimal decisions.

The **competitive ratio** c of an online algorithm A means: for every input sequence σ,
`cost(A, σ) ≤ c × cost(OPT, σ) + constant`. This is a worst-case guarantee over all
possible input sequences — much stronger than average-case analysis.

The senior engineer's pattern recognition:

- **Cache eviction** (LRU, LFU, OPT): LRU has a proven competitive ratio of k for a
  k-page cache. This is why LRU is the default — it is not just "intuitively good" but
  *provably* within a constant factor of clairvoyant OPT.

- **Ski rental** (rent vs. buy): You must decide whether to keep renting (paying per unit)
  or buy outright (paying a fixed cost), without knowing when you'll stop. This is the
  prototype for all rent-vs-buy decisions: leasing vs. buying hardware, on-demand vs.
  reserved cloud instances. The optimal online strategy achieves competitive ratio 2 - 1/b
  where b is the buy cost.

- **Secretary problem**: Interviews n candidates in random order. After each interview,
  decide immediately: hire or reject (irreversibly). The optimal online strategy achieves
  success probability 1/e — by rejecting the first n/e candidates and then hiring the
  next candidate better than all previous ones.

- **k-server**: You have k servers on a metric space. Requests arrive at points; you must
  move a server to serve each request. Minimize total movement. The Work Function Algorithm
  achieves competitive ratio 2k-1. For k=1 this is 1 (optimal); for large k it is 2k-1.

## Core Concepts

### Competitive Analysis and Adversary Models

An adversary generates the input sequence to maximize the ratio `cost(A) / cost(OPT)`.
Two adversary types:

- **Oblivious adversary**: The adversary knows the algorithm but not its random choices.
  Competitive ratios for randomized algorithms are defined against this adversary.
- **Adaptive adversary**: The adversary sees the algorithm's choices and adapts. Against
  this adversary, randomization does not help (any randomized online algorithm can be
  derandomized to match).

For deterministic algorithms, both adversaries are equivalent. For randomized algorithms,
competitive ratios are stated against the oblivious adversary.

**Yao's minimax principle**: The expected competitive ratio of the best deterministic
algorithm on the *worst* distribution of inputs equals the competitive ratio of the best
*randomized* algorithm on the *worst* input sequence. This lets you prove lower bounds
for randomized algorithms by constructing hard distributions for deterministic ones.

### Ski Rental Problem

Renting costs $1/day. Buying costs $b$. You ski for an unknown number of days d.

- If d < b: renting is optimal; cost = d.
- If d ≥ b: buying is optimal; cost = b.

**Deterministic online strategy**: Rent for b-1 days, then buy.
- If d < b: you rented, total cost = d. OPT = d. Ratio = 1.
- If d ≥ b: you rented b-1 days then bought, total cost = 2b-1. OPT = b. Ratio = (2b-1)/b < 2.

This strategy achieves competitive ratio 2 - 1/b < 2. No deterministic algorithm can
do better than ratio 2 - 1/b.

**Randomized strategy**: Pick a random day r uniformly from [1, b] to buy. Expected
cost analysis gives competitive ratio e/(e-1) ≈ 1.58, better than deterministic 2.

**Production application**: The decision between on-demand cloud instances ($0.10/hr) vs.
reserved instances ($500/yr) is exactly ski rental. If you run for more than 500 hours
(b), buying the reservation is optimal. The online strategy: run on-demand for b-1 hours,
then buy the reservation if still running.

### Cache Eviction: LRU Analysis

A cache of size k. Requests arrive for pages. On a miss, some page must be evicted.
LRU evicts the least recently used page.

**Competitive ratio of LRU**: For cache size k, LRU is k-competitive. Proof sketch:
partition any access sequence into phases where each phase sees k+1 distinct pages. LRU
causes at most k misses per phase (it evicts the LRU page; it can be wrong at most k
times before it "resets"). OPT causes at least 1 miss per phase (it must miss the page
that was furthest in the future). Ratio: k/1 = k.

**OPT (Bélády's algorithm)**: Evict the page whose next access is furthest in the future.
OPT is optimal but requires knowing the future. LRU approximates it.

**CLOCK / Second Chance**: An approximation of LRU that uses a circular buffer with
"reference bits" instead of tracking exact recency. Used in Linux's page replacement.
Competitive ratio same as LRU asymptotically but with lower overhead.

### Secretary Problem (Optimal Stopping)

N candidates arrive in random order. After each interview, make an irrevocable hire/reject
decision. Goal: hire the best candidate.

**Optimal strategy**: Reject the first r-1 candidates (but note the best seen so far).
Hire the first candidate better than all previous ones. Optimal r = n/e ≈ 0.368n.

Success probability: Σ_{i=r}^{n} (best among first i is in first r-1) × (1/(i)) = r/n × Σ_{i=r}^{n} 1/(i-1)
≈ (r/n) × ln(n/r). Maximized at r = n/e, giving probability ≈ 1/e ≈ 0.368.

**Competitive ratio perspective**: The online algorithm finds the best candidate with
probability 1/e. The offline OPT always finds the best (ratio: 1). So the competitive
ratio for the secretary problem (where the "cost" is the rank of the hired candidate
vs. the best available) is e.

**Extensions in production**:
- When you don't know n in advance: use a time-based threshold (reject the first T seconds).
- Multiple secretary problem: hiring k candidates, or multiple choices.
- The "prophet inequality": if you know the distribution of candidate values, you can achieve
  a 1/2-competitive algorithm (Krengel-Sucheston theorem).

### k-Server Problem

k servers on a metric space. Requests arrive at points; a server must be moved to serve
each request. Minimize total movement.

**Work function algorithm (WFA)**: At each request r, move the server that minimizes
`w(S) + dist(s, r)` where `w(S)` is the "work function" — the minimum cost to serve
all past requests given that the current server configuration is S. WFA achieves
competitive ratio 2k-1 (the k-server conjecture: optimal ratio is k, unproven for k > 2).

**Special cases**: For the line (1D metric), the Double Coverage algorithm is k-competitive.
For uniform metric spaces, any lazy algorithm (only move a server to the requested point)
achieves competitive ratio k with LRU-style eviction.

**Production relevance**: CDN server placement (where to forward a request) is a k-server
instance on a network metric. Load balancers use online algorithms with similar structure.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
	"math/rand"
)

// ─── Ski Rental: Deterministic and Randomized ────────────────────────────────

// SkiRentalDeterministic returns the total cost of the deterministic online strategy.
// Rent b-1 days, then buy. buyCost = b.
func SkiRentalDeterministic(actualDays, buyCost int) int {
	if actualDays < buyCost {
		return actualDays // rented the whole time
	}
	// Rented b-1 days, then bought
	return (buyCost - 1) + buyCost // = 2b-1
}

// SkiRentalRandomized picks a random day [1,b] to buy; returns expected cost.
// In expectation: competitive ratio = e/(e-1) ≈ 1.58.
func SkiRentalRandomizedExpected(actualDays, buyCost int) float64 {
	if buyCost == 0 { return 0 }
	// Expected cost of buying on day r uniform in [1, b]
	total := 0.0
	for r := 1; r <= buyCost; r++ {
		if actualDays < r {
			// Quit before buying: paid actualDays in rent
			total += float64(actualDays)
		} else {
			// Bought on day r: paid r-1 rent + b buy cost
			total += float64(r-1) + float64(buyCost)
		}
	}
	return total / float64(buyCost)
}

// ─── LRU Cache ────────────────────────────────────────────────────────────────

type LRUCache struct {
	cap    int
	cache  map[int]*lruNode
	head   *lruNode // most recently used
	tail   *lruNode // least recently used
	hits   int
	misses int
}

type lruNode struct{ key, val int; prev, next *lruNode }

func NewLRUCache(cap int) *LRUCache {
	h := &lruNode{}
	t := &lruNode{}
	h.next = t; t.prev = h
	return &LRUCache{cap: cap, cache: make(map[int]*lruNode), head: h, tail: t}
}

func (c *LRUCache) Get(key int) (int, bool) {
	if node, ok := c.cache[key]; ok {
		c.moveToFront(node)
		c.hits++
		return node.val, true
	}
	c.misses++
	return 0, false
}

func (c *LRUCache) Put(key, val int) {
	if node, ok := c.cache[key]; ok {
		node.val = val
		c.moveToFront(node)
		return
	}
	node := &lruNode{key: key, val: val}
	c.cache[key] = node
	c.addToFront(node)
	if len(c.cache) > c.cap {
		evicted := c.tail.prev
		c.remove(evicted)
		delete(c.cache, evicted.key)
	}
}

func (c *LRUCache) addToFront(node *lruNode) {
	node.next = c.head.next; node.prev = c.head
	c.head.next.prev = node; c.head.next = node
}

func (c *LRUCache) remove(node *lruNode) {
	node.prev.next = node.next; node.next.prev = node.prev
}

func (c *LRUCache) moveToFront(node *lruNode) { c.remove(node); c.addToFront(node) }

func (c *LRUCache) HitRate() float64 {
	total := c.hits + c.misses
	if total == 0 { return 0 }
	return float64(c.hits) / float64(total)
}

// ─── Secretary Problem ────────────────────────────────────────────────────────

// SecretaryOptimal implements the optimal 1/e stopping rule.
// candidates: slice of values (interview order). Returns hired candidate value.
func SecretaryOptimal(candidates []int) int {
	n := len(candidates)
	if n == 0 { return -1 }
	r := int(math.Ceil(float64(n) / math.E))

	// Phase 1: observe first r candidates, record best
	bestObserved := math.MinInt64
	for i := 0; i < r; i++ {
		if candidates[i] > bestObserved { bestObserved = candidates[i] }
	}

	// Phase 2: hire the first candidate better than all observed
	for i := r; i < n; i++ {
		if candidates[i] > bestObserved { return candidates[i] }
	}
	// Fallback: hire last candidate
	return candidates[n-1]
}

// SimulateSecretary runs many trials to estimate empirical success probability.
func SimulateSecretary(n, trials int) float64 {
	successes := 0
	cands := make([]int, n)
	for t := 0; t < trials; t++ {
		for i := range cands { cands[i] = i }
		rand.Shuffle(n, func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
		best := n - 1 // index of the best candidate
		hired := -1
		for i, v := range cands { if v == n-1 { best = i } }
		_ = best
		hired = SecretaryOptimal(cands)
		if hired == n-1 { successes++ } // hired the best
	}
	return float64(successes) / float64(trials)
}

// ─── Competitive Ratio Analysis ───────────────────────────────────────────────

// ComputeCompetitiveRatio simulates LRU vs OPT (Bélády) on a page access sequence.
func ComputeCompetitiveRatio(pages []int, cacheSize int) (lruMisses, optMisses int) {
	// LRU simulation
	lru := NewLRUCache(cacheSize)
	for _, p := range pages {
		if _, ok := lru.Get(p); !ok {
			lru.Put(p, p)
			lruMisses++
		}
	}

	// Bélády's OPT: evict the page whose next use is furthest in the future
	optCache := make(map[int]bool)
	optList := []int{}

	for i, p := range pages {
		if optCache[p] { continue }
		optMisses++
		if len(optList) < cacheSize {
			optCache[p] = true
			optList = append(optList, p)
			continue
		}
		// Find page in cache with furthest next use
		worstPage, worstIdx := -1, i
		for _, cp := range optList {
			nextUse := len(pages) // never used again = infinity
			for j := i + 1; j < len(pages); j++ {
				if pages[j] == cp { nextUse = j; break }
			}
			if nextUse > worstIdx { worstIdx = nextUse; worstPage = cp }
		}
		if worstPage != -1 {
			delete(optCache, worstPage)
			for k, v := range optList { if v == worstPage { optList[k] = p; break } }
		}
		optCache[p] = true
	}
	return
}

func main() {
	// Ski rental
	for _, days := range []int{3, 7, 10} {
		det := SkiRentalDeterministic(days, 7)
		randExp := SkiRentalRandomizedExpected(days, 7)
		opt := days; if opt > 7 { opt = 7 }
		fmt.Printf("Days=%d OPT=%d Det=%d Rand=%.2f Ratio=%.2f\n",
			days, opt, det, randExp, float64(det)/float64(opt))
	}

	// LRU cache
	cache := NewLRUCache(3)
	pages := []int{1, 2, 3, 4, 1, 2, 5, 1, 2, 3, 4, 5}
	for _, p := range pages { cache.Put(p, p); cache.Get(p) }
	fmt.Printf("LRU hit rate: %.2f%%\n", cache.HitRate()*100)

	// Secretary problem
	prob := SimulateSecretary(100, 10000)
	fmt.Printf("Secretary success probability: %.3f (theory: %.3f)\n", prob, 1/math.E)

	// Competitive ratio
	lru, opt := ComputeCompetitiveRatio([]int{1,2,3,4,1,2,5,1,2,3,4,5}, 3)
	fmt.Printf("LRU misses: %d, OPT misses: %d, ratio: %.2f\n",
		lru, opt, float64(lru)/float64(opt))
}
```

### Go-specific considerations

- **LRU cache with `map` + doubly-linked list**: This is the canonical Go implementation
  (also the LeetCode "LRU Cache" pattern). The `sync.Mutex` wrapper for concurrent use
  is trivial to add. For high-concurrency LRU, use a sharded LRU (partition keyspace into
  N shards, each with its own lock) or a concurrent cache crate.
- **Secretary problem with `rand.Shuffle`**: Go's `rand.Shuffle` uses Fisher-Yates, which
  is O(n) and produces a uniformly random permutation. For reproducible simulation, seed
  with `rand.New(rand.NewSource(42))`.
- **Bélády's OPT is O(n²)**: The implementation shown has O(n²) time due to the "find
  next use" scan. For large sequences, precompute `next_use[i][page]` with a hash map.

## Implementation: Rust

```rust
use std::collections::{HashMap, VecDeque};
use std::f64::consts::E;

// ─── Ski Rental ───────────────────────────────────────────────────────────────

fn ski_rental_deterministic(actual_days: u64, buy_cost: u64) -> u64 {
    if actual_days < buy_cost { actual_days } else { 2 * buy_cost - 1 }
}

fn ski_rental_randomized_expected(actual_days: u64, buy_cost: u64) -> f64 {
    if buy_cost == 0 { return 0.0; }
    let total: f64 = (1..=buy_cost).map(|r| {
        if actual_days < r {
            actual_days as f64
        } else {
            (r - 1 + buy_cost) as f64
        }
    }).sum();
    total / buy_cost as f64
}

// ─── LRU Cache ────────────────────────────────────────────────────────────────
// Uses a HashMap + VecDeque approximation for clarity.
// Production: use `lru` crate (indexmap-based O(1) LRU).

struct LruCache {
    cap: usize,
    map: HashMap<i64, i64>,
    order: VecDeque<i64>, // front = most recent
    hits: usize,
    misses: usize,
}

impl LruCache {
    fn new(cap: usize) -> Self {
        LruCache { cap, map: HashMap::new(), order: VecDeque::new(), hits: 0, misses: 0 }
    }

    fn get(&mut self, key: i64) -> Option<i64> {
        if let Some(&val) = self.map.get(&key) {
            self.hits += 1;
            // Move to front (O(n) — use doubly-linked list for O(1) in production)
            self.order.retain(|&k| k != key);
            self.order.push_front(key);
            Some(val)
        } else {
            self.misses += 1;
            None
        }
    }

    fn put(&mut self, key: i64, val: i64) {
        if self.map.contains_key(&key) {
            self.order.retain(|&k| k != key);
        } else if self.map.len() >= self.cap {
            if let Some(evicted) = self.order.pop_back() {
                self.map.remove(&evicted);
            }
        }
        self.map.insert(key, val);
        self.order.push_front(key);
    }

    fn hit_rate(&self) -> f64 {
        let total = self.hits + self.misses;
        if total == 0 { 0.0 } else { self.hits as f64 / total as f64 }
    }
}

// ─── Secretary Problem ────────────────────────────────────────────────────────

fn secretary_optimal(candidates: &[i64]) -> i64 {
    let n = candidates.len();
    if n == 0 { return -1; }
    let r = (n as f64 / E).ceil() as usize;

    let best_observed = candidates[..r].iter().copied().max().unwrap_or(i64::MIN);

    candidates[r..].iter().copied()
        .find(|&v| v > best_observed)
        .unwrap_or(*candidates.last().unwrap())
}

fn simulate_secretary(n: usize, trials: usize) -> f64 {
    use rand::seq::SliceRandom;
    let mut rng = rand::thread_rng();
    let mut candidates: Vec<i64> = (0..n as i64).collect();
    let best = (n - 1) as i64;
    let successes: usize = (0..trials).filter(|_| {
        candidates.shuffle(&mut rng);
        secretary_optimal(&candidates) == best
    }).count();
    successes as f64 / trials as f64
}

// ─── Competitive Ratio Comparison ────────────────────────────────────────────

fn compute_lru_misses(pages: &[i64], cache_size: usize) -> usize {
    let mut lru = LruCache::new(cache_size);
    let mut misses = 0;
    for &p in pages {
        if lru.get(p).is_none() {
            lru.put(p, p);
            misses += 1;
        }
    }
    misses
}

fn compute_opt_misses(pages: &[i64], cache_size: usize) -> usize {
    let mut cache: Vec<i64> = Vec::with_capacity(cache_size);
    let mut misses = 0;

    for (i, &p) in pages.iter().enumerate() {
        if cache.contains(&p) { continue; }
        misses += 1;
        if cache.len() < cache_size { cache.push(p); continue; }

        // Bélády: evict page with furthest next use
        let evict_idx = cache.iter().enumerate().max_by_key(|&(_, &cp)| {
            pages[i+1..].iter().position(|&q| q == cp).unwrap_or(usize::MAX)
        }).map(|(idx, _)| idx).unwrap();

        cache[evict_idx] = p;
    }
    misses
}

fn main() {
    // Ski rental
    for days in [3u64, 7, 10] {
        let det = ski_rental_deterministic(days, 7);
        let rnd = ski_rental_randomized_expected(days, 7);
        let opt = days.min(7);
        println!("Days={} OPT={} Det={} Rand={:.2} Ratio={:.2}",
            days, opt, det, rnd, det as f64 / opt as f64);
    }

    // LRU
    let mut cache = LruCache::new(3);
    for &p in &[1i64,2,3,4,1,2,5,1,2,3,4,5] { cache.put(p, p); cache.get(p); }
    println!("LRU hit rate: {:.1}%", cache.hit_rate() * 100.0);

    // Secretary
    let prob = simulate_secretary(100, 10_000);
    println!("Secretary success: {:.3} (theory: {:.3})", prob, 1.0/E);

    // Competitive ratio
    let pages = [1i64,2,3,4,1,2,5,1,2,3,4,5];
    let lru = compute_lru_misses(&pages, 3);
    let opt = compute_opt_misses(&pages, 3);
    println!("LRU={} OPT={} ratio={:.2}", lru, opt, lru as f64 / opt as f64);
}
```

### Rust-specific considerations

- **LRU cache in production**: The shown `VecDeque`-based LRU has O(n) `retain` for moves.
  Use the `lru` crate (`crates.io`) which is indexmap-based and gives true O(1) get/put.
  Alternatively, the `linked_hash_map` crate provides a `HashMap` with insertion-order
  iteration, enabling efficient LRU.
- **`rand::seq::SliceRandom`**: The `shuffle` method on slices requires importing this
  trait from the `rand` crate. It uses Fisher-Yates internally.
- **Secretary simulation ownership**: The `candidates.shuffle(&mut rng)` mutates in-place,
  which is efficient. The closure captures `candidates` by mutable reference — this works
  because the closure is called sequentially in `filter`.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| LRU production implementation | `github.com/hashicorp/golang-lru` — widely used in Go ecosystem | `lru` crate — O(1) operations, thread-safe variant available |
| Secretary problem simulation | `rand.Shuffle` in stdlib | `SliceRandom::shuffle` from `rand` crate |
| Bélády OPT oracle | Hand-roll; no stdlib support | Same |
| Concurrent LRU cache | `sync.Mutex` + LRU; or `ristretto` (Cloudflare's Go cache) | `arc-cache` crate for adaptive replacement cache |
| `HashMap` performance | Go's built-in map is a hash table with chaining; acceptable | `HashMap` uses Robin Hood hashing; generally faster |
| Doubly-linked list | `container/list` in stdlib for LRU | No stdlib doubly-linked list; index-based or `linked_hash_map` crate |

## Production War Stories

**Varnish Cache / Nginx proxy cache**: Both use variants of the CLOCK algorithm (approximation
of LRU) for cache eviction. Varnish's "LRU ban" system uses a generational sweeper that is
competitive-ratio-aware: it tracks the ratio of hits to misses and adjusts eviction
aggressiveness accordingly.

**Redis sorted set eviction**: Redis's maxmemory policy supports LRU and LFU eviction. The
LRU implementation uses a sampled LRU (pick 5 random keys, evict the LRU among them) rather
than exact LRU. This trades a small loss in hit rate for O(1) eviction without a full LRU
list — an engineering tradeoff that accepts a slightly worse competitive ratio for operational
simplicity.

**Kubernetes Horizontal Pod Autoscaler (HPA)**: The HPA's scale-up/scale-down decision is an
online algorithm: it sees current CPU utilization but not future load. The cooling period
(3-minute default) is a form of the "ski rental" solution — wait for enough evidence before
committing to a scaling decision.

**Financial trading systems (optimal stopping)**: The "optimal stopping" theory of the
secretary problem appears directly in algorithmic trading: "should I execute this trade now
or wait for a better price?" The prophet inequality (1/2-competitive with known value
distributions) is the theoretical basis for optimal execution algorithms.

**Akamai CDN routing**: Akamai's adaptive routing selects which edge server to serve a
request from, based on real-time performance measurements. This is a k-server-like problem
on the internet graph. The algorithm is competitive-ratio-aware: it uses the measured
"cost" (latency × bandwidth) to maintain a near-optimal server assignment as conditions
change.

## Complexity Analysis

| Problem | Online Algorithm | Competitive Ratio | Lower Bound | Notes |
|---------|-----------------|-------------------|-------------|-------|
| Paging (cache) | LRU | k (cache size) | k | Tight; LRU is k-competitive |
| Paging (cache) | CLOCK | k | k | Same ratio, lower overhead |
| Ski rental | Rent b-1, buy | 2 - 1/b | 2 - 1/b | Tight; no det. algorithm beats this |
| Ski rental | Randomized | e/(e-1) ≈ 1.58 | e/(e-1) | Tight against oblivious adversary |
| Secretary | Reject n/e, then hire | 1/e success prob. | 1/e | Tight |
| k-server | Work Function Algo | 2k-1 | k (conjecture) | Open problem for k > 2 |
| Load balancing (identical machines) | Greedy LPT | 4/3 - 1/(3m) | — | For m machines |

**The k-server conjecture**: The optimal competitive ratio for the k-server problem is
conjectured to be k. It has been proven for k=1 (trivial), k=2 (elegant proof), and special
metric spaces, but remains open in general. The Work Function Algorithm achieves 2k-1.

## Common Pitfalls

1. **Applying offline analysis to online settings**: "LRU is O(1) per operation" is true,
   but saying "LRU achieves the optimal hit rate" is false — it is only k-competitive, not
   optimal. OPT requires future knowledge. Conflating the two leads to incorrect performance
   predictions in production.

2. **Secretary problem without shuffling**: The 1/e guarantee assumes candidates arrive in
   uniformly random order. If candidates arrive in sorted order (e.g., sorted by interview
   time, which correlates with quality), the strategy fails. Production systems must either
   shuffle the input or use a different stopping strategy.

3. **Ski rental ignoring switching costs**: The ski rental analysis assumes the cost is
   exactly `b` to buy. In practice, "buying" (e.g., switching to reserved instances) has
   commitment overhead, migration costs, and uncertainty about future usage. Amortize these
   into the effective `b` before applying the strategy.

4. **LRU cache competitive ratio confusion**: The k-competitive ratio of LRU means that
   across all possible access sequences, LRU does at most k× worse than OPT. It does NOT
   mean that LRU performs k× worse on typical workloads — in practice, LRU often approaches
   the OPT hit rate for workloads with temporal locality.

5. **Not accounting for tied candidates in secretary problem**: When multiple candidates have
   the same value (quality scores are quantized), the standard secretary algorithm needs
   modification. The "hire best so far" criterion should be "> strictly greater than all
   previous" not "≥" — ties break the 1/e guarantee.

## Exercises

**Exercise 1 — Verification** (30 min):
Simulate the secretary problem for n ∈ {10, 100, 1000, 10000} candidates, with 10,000
trials each. Verify that the empirical success probability converges to 1/e as n grows.
Plot the probability vs. n. Also plot the success probability as a function of the
threshold r/n (not just the optimal r = n/e) to understand the shape of the stopping
rule's performance landscape.

**Exercise 2 — Extension** (2–4 h):
Implement LRU, LFU (Least Frequently Used), and CLOCK cache policies with the same
interface. Run them on three workload distributions: (a) uniform random, (b) Zipf
distribution (power law — realistic for web caches), (c) sequential scan (worst case
for LRU). Measure hit rates for each policy × workload combination for cache sizes
{4, 8, 16, 32}. Which policy wins on each workload and why?

**Exercise 3 — From Scratch** (4–8 h):
Implement the Work Function Algorithm for the k-server problem on the line metric
(integers on a number line). Verify the 2k-1 competitive ratio empirically against the
offline OPT on 100 random request sequences of length 1000. Compare against the simpler
"move nearest server" greedy algorithm. For which request patterns does the greedy fail
badly?

**Exercise 4 — Production Scenario** (8–15 h):
Build an adaptive CDN cache configuration service. Input: a stream of URL access logs
(URL, size, access_time). Goal: continuously decide which URLs to cache on a server
with capacity C bytes, maximizing hit rate. Implement three strategies: (a) LRU by access
time, (b) LFU by frequency, (c) Size-aware LRU (Bélády-approximate: evict the largest
item with the lowest expected reuse). Expose as a service that accepts a config JSON
(capacity, strategy) and a streaming API for access log events. Benchmark hit rates on
real web access log datasets (Common Crawl or Wikimedia logs). Document which strategy
wins at which cache sizes and why the competitive-ratio theory predicts this.

## Further Reading

### Foundational Papers
- Sleator, D. D., & Tarjan, R. E. (1985). "Amortized efficiency of list update and
  paging rules." *Communications of the ACM*, 28(2), 202–208. The original competitive
  analysis of LRU.
- Manasse, M. S., McGeoch, L. A., & Sleator, D. D. (1988). "Competitive algorithms for
  server problems." *STOC 1988*. The k-server problem formulation.
- Flajolet, P., & Sedgewick, R. (2009). *Analytic Combinatorics*. Chapter 12 covers
  optimal stopping theory rigorously.

### Books
- *Online Computation and Competitive Analysis* — Borodin & El-Yaniv. The standard textbook.
  Chapter 1 (paging, ski rental), Chapter 7 (k-server), Chapter 10 (randomization).
- *Optimal Stopping and Applications* — Ferguson. The statistical decision theory perspective
  on secretary problems. Available free online.

### Production Code to Read
- **Varnish cache** (`varnish-cache/bin/varnishd/cache/cache_lru.c`): Production LRU
  implementation with the CLOCK approximation and ban lists.
- **Redis maxmemory eviction** (`src/evict.c`): The sampled LRU implementation — `evictionPoolPopulate`
  and the `server.maxmemory_policy` handling. Compare with the theoretical k-competitive LRU.
- **Hashicorp Golang LRU** (`github.com/hashicorp/golang-lru`): Well-tested Go LRU with
  variants: 2Q (approximates LFU), ARC (adaptive replacement cache).

### Conference Talks
- "LRU vs. ARC: Cache Algorithms in Practice" — Systems and Storage Conference (USENIX
  FAST) 2014. Empirical comparison on real storage workloads.
- "Online Algorithms in Infrastructure" — SREcon Americas 2019. How ski rental, k-server,
  and prophet inequality appear in production infrastructure decisions.
- "The Mathematics of Optimal Stopping" — 3Blue1Brown YouTube series on the secretary
  problem. Accessible visual introduction.
