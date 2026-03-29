<!-- difficulty: advanced -->
<!-- category: databases-time-series-tools -->
<!-- languages: [go, rust] -->
<!-- concepts: [a-star, pathfinding, heuristics, priority-queue, jps, grid-search, weighted-terrain] -->
<!-- estimated_time: 15-25 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [data-structures, binary-heap, graph-algorithms, coordinate-systems] -->

# Challenge 97: A* Pathfinding on a 2D Grid

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Binary heap / priority queue operations (insert, extract-min, decrease-key)
- Graph search concepts: BFS, Dijkstra's algorithm
- Coordinate systems and distance metrics (Manhattan, Euclidean, Chebyshev)
- Basic understanding of heuristic admissibility and consistency
- Go: generics, slices, `container/heap` interface
- Rust: `BinaryHeap`, `Ord` for custom types, iterators

## Learning Objectives

- **Implement** the A* search algorithm with configurable heuristics on a 2D grid
- **Analyze** how heuristic choice (Manhattan, Euclidean, Chebyshev) affects search efficiency and path optimality
- **Design** weighted terrain support where different cell types have different movement costs
- **Evaluate** the performance impact of Jump Point Search (JPS) optimization on uniform-cost grids
- **Build** path smoothing that removes unnecessary waypoints from grid-aligned paths

## The Challenge

A* is the workhorse of game AI, robotics, and route planning. It combines Dijkstra's guarantee of finding the shortest path with a heuristic that dramatically reduces the number of nodes explored. On a 1000x1000 grid, Dijkstra might explore 500,000 nodes; A* with a good heuristic explores 10,000.

Your task is to implement A* pathfinding on a 2D grid with full support for weighted terrain, multiple heuristics, diagonal movement, dynamic obstacles, and path smoothing. Then implement Jump Point Search (JPS), an optimization that exploits grid structure to skip large swaths of nodes, achieving 10-100x speedup on uniform-cost grids.

The grid is the simplest spatial structure, but the algorithm generalizes to any graph. The challenge is not just getting a path -- it is getting the optimal path efficiently, handling edge cases (unreachable goals, ties in f-score), and producing smooth paths suitable for actual movement.

Both Go and Rust implementations are required. Compare how each language handles the priority queue (Go's `container/heap` interface vs. Rust's `BinaryHeap` with `Reverse`), and how ownership affects the open/closed set management in Rust.

## Requirements

1. **Grid representation**:
   - 2D grid with configurable dimensions
   - Cell types: open (cost 1.0), wall (impassable), weighted (cost 1.0-10.0)
   - Load grids from text format: `.` = open, `#` = wall, digits `2`-`9` = weighted, `S` = start, `G` = goal
   - Coordinate system: (row, col) with (0,0) at top-left

2. **A* implementation**:
   - Open set as a min-heap priority queue keyed on f-score = g-score + h-score
   - Closed set as a visited array or hash set
   - Path reconstruction via parent pointers
   - Return: path as list of coordinates, total cost, nodes explored count
   - Handle unreachable goals (return error/None, not infinite loop)

3. **Heuristics** (all must be selectable):
   - Manhattan: `|dx| + |dy|` (admissible for 4-directional movement)
   - Euclidean: `sqrt(dx^2 + dy^2)` (admissible for any movement)
   - Chebyshev: `max(|dx|, |dy|)` (admissible for 8-directional with uniform diagonal cost)
   - Octile: `max(|dx|, |dy|) + (sqrt(2)-1) * min(|dx|, |dy|)` (admissible for 8-directional with diagonal cost sqrt(2))

4. **Movement modes**:
   - 4-directional (cardinal only)
   - 8-directional with diagonal cost sqrt(2)
   - 8-directional with diagonal cost 1.0 (Chebyshev distance)
   - Corner-cutting prevention: disallow diagonal movement past two adjacent walls

5. **Dynamic obstacles**:
   - `AddObstacle(row, col)` and `RemoveObstacle(row, col)` modify the grid
   - Re-plan path after obstacle changes (simple re-run of A*)

6. **Path smoothing**:
   - Line-of-sight check between two points (Bresenham's line algorithm or similar)
   - Remove redundant waypoints: if point A has line-of-sight to point C, remove point B between them
   - Greedy smoothing pass: iterate from start, find farthest visible point, repeat

7. **Jump Point Search (JPS)**:
   - Implement JPS for uniform-cost grids (all open cells have cost 1.0)
   - Forced neighbor detection for cardinal and diagonal moves
   - Jump function that scans in a direction until finding a jump point or hitting a wall
   - Fall back to standard A* when the grid has weighted terrain

8. **Visualization**:
   - ASCII grid output showing path (`*`), explored nodes (`~`), walls (`#`), start (`S`), goal (`G`)
   - Optional: PPM image output (path in red, explored in light blue, walls in dark gray)

## Hints

1. For the priority queue in Go, implement `container/heap.Interface` with a struct that holds `(f_score, node_index)`. In Rust, use `BinaryHeap<Reverse<(OrderedFloat<f64>, usize)>>` since Rust's `BinaryHeap` is a max-heap and `f64` does not implement `Ord`. The `ordered-float` crate or a manual wrapper solves the `Ord` issue.

2. Use a flat `[]float64` / `Vec<f64>` for g-scores indexed by `row * width + col`. This avoids hash map overhead and exploits cache locality. Initialize all values to `f64::INFINITY` / `math.MaxFloat64`. The closed set can be a `[]bool` / `Vec<bool>` with the same indexing.

3. For JPS, the jump function for cardinal directions is straightforward: scan cells in the direction until you hit a wall (return None) or find a forced neighbor (return that cell). For diagonal directions, at each step check both cardinal components -- if either cardinal jump finds a jump point, the current cell is a jump point. This recursive structure is the core of JPS.

4. Path smoothing with line-of-sight is a post-processing step. Walk from the start: find the farthest waypoint visible from the current position, mark it as the next waypoint, repeat from there. Use Bresenham's algorithm to check if any cell along the line is a wall. The smoothed path will have far fewer waypoints but the same traversal cost on open grids.

5. For corner-cutting prevention: when moving diagonally from (r,c) to (r+dr, c+dc), check that both (r+dr, c) and (r, c+dc) are passable. If either is a wall, the diagonal move is blocked. This prevents paths that squeeze through two diagonally-adjacent walls.

## Acceptance Criteria

- [ ] A* finds the shortest path on a simple grid with no obstacles
- [ ] Walls are correctly avoided -- path routes around obstacles
- [ ] Weighted terrain produces cost-optimal paths (prefers cheaper cells)
- [ ] All four heuristics produce optimal paths (admissibility test)
- [ ] 4-directional and 8-directional movement modes produce correct paths
- [ ] Diagonal movement respects corner-cutting rules
- [ ] Unreachable goals return an error/None rather than hanging
- [ ] Path smoothing reduces waypoint count without crossing walls
- [ ] JPS produces the same optimal path as standard A* on uniform grids
- [ ] JPS explores significantly fewer nodes than standard A* (measure and report)
- [ ] Dynamic obstacle addition and removal work correctly
- [ ] ASCII visualization shows path, explored nodes, walls, start, and goal
- [ ] Performance: A* handles 1000x1000 grids in under 1 second
- [ ] Both Go and Rust implementations produce identical paths for the same input
- [ ] All tests pass (`go test ./...` and `cargo test`)

## Research Resources

- [A* Search Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/A*_search_algorithm) -- canonical pseudocode and proofs
- [Introduction to A* (Red Blob Games)](https://www.redblobgames.com/pathfinding/a-star/introduction.html) -- the best interactive visual tutorial
- [Jump Point Search (Harabor & Grastien, 2011)](https://ojs.aaai.org/index.php/AAAI/article/view/7994) -- original JPS paper
- [JPS Explained (zerowidth.com)](https://zerowidth.com/2013/a-visual-explanation-of-jump-point-search.html) -- visual JPS walkthrough
- [Amit's A* Pages](http://theory.stanford.edu/~amitp/GameProgramming/) -- comprehensive game pathfinding reference
- [Bresenham's Line Algorithm](https://en.wikipedia.org/wiki/Bresenham%27s_line_algorithm) -- for line-of-sight checks
