# 45. Build Performance Profiler

<!--
difficulty: insane
concepts: [performance-profiling, metrics-collection, time-series-analysis, recipe-introspection, parallelization-analysis]
tools: [just, bash, awk, gnuplot, date, time]
estimated_time: 2h-3h
bloom_level: evaluate
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- `gnuplot` installed (optional, for chart generation — system should degrade gracefully
  without it)
- Understanding of performance profiling concepts: wall time, bottleneck
  identification, critical path analysis

## Learning Objectives

After completing this challenge, you will be able to:

- **Design** a transparent profiling wrapper that instruments any justfile without
  requiring modifications to it
- **Evaluate** build performance data to identify bottlenecks and recommend
  parallelization opportunities based on dependency analysis

## The Challenge

Build a performance profiling system that can wrap ANY existing justfile — without
modifying it — and measure the execution time of every recipe, track performance over
time, identify bottlenecks, suggest optimization opportunities, and generate reports.
Think of it as a "profiler for just" analogous to how `perf` profiles binaries or how
build systems like Bazel provide execution timeline analysis.

The wrapping mechanism is the first hurdle. Your profiler must work with any justfile in
any directory. The user should be able to run
`just -f profiler.just profile target-dir=. recipe=build` and get a profiled execution
of the `build` recipe in the current directory's justfile. The profiler must discover
all recipes in the target justfile (via `just --list`), understand their dependency
relationships (via `just --dump` or `just --summary`), and instrument each one.

Instrumentation means capturing, for each recipe that executes during a run: the recipe
name, start timestamp (millisecond precision), end timestamp, exit code, stdout size,
and stderr size. The tricky part is that just executes dependency recipes automatically
— if `build` depends on `compile` which depends on `fetch`, running `build` triggers
all three, and your profiler must capture timing for each individually. This likely
requires parsing the execution flow or wrapping each recipe's invocation through a
timing harness.

One approach: generate a temporary "instrumented" justfile that wraps each recipe body
with timing commands while preserving the original dependency structure. Another
approach: use `just --dry-run` to discover the execution plan, then execute each recipe
individually with timing. Each approach has trade-offs — the first preserves just's
native dependency execution, while the second gives you per-recipe isolation but may
miss dependency-induced side effects.

Historical tracking is what makes this a profiler rather than a one-shot timer. Every
profiling run appends to a metrics file (JSON Lines format). Over time, this builds a
history that enables trend analysis: "the `build` recipe has gotten 40% slower over the
last 10 runs." Implement regression detection that alerts when a recipe's execution time
increases by more than a configurable threshold compared to its rolling average. This
catches gradual performance degradation that no single run would reveal.

The analysis engine examines dependency chains to find the critical path — the longest
sequential chain of recipe executions — and suggests which recipes could be
parallelized. If `test` and `lint` are both dependencies of `ci` but do not depend on
each other, the profiler should flag this as a parallelization opportunity and estimate
the time savings (the difference between sequential and parallel execution of the
siblings).

## Requirements

1. Create a profiling wrapper that works with any justfile: accept a target directory
   and recipe name, profile the execution, and store results — without modifying the
   target justfile

2. Discover all recipes and their dependencies in the target justfile using
   `just --list`, `just --dump`, or `just --summary` (use the `--unstable` flag as
   needed for detailed output)

3. Instrument individual recipe execution: capture recipe name, wall-clock start time
   (millisecond precision), wall-clock end time, exit code, and output size for each
   recipe that runs during a profiled invocation

4. Store profiling results in a metrics file (`metrics.jsonl` — one JSON object per
   recipe per run) with a run ID, timestamp, recipe name, duration in milliseconds,
   exit code, and any custom labels

5. Implement `history` recipe showing the last N runs for a specific recipe: timestamps,
   durations, and trend direction (faster/slower/stable compared to the rolling average)

6. Implement regression detection: compare the latest run's duration against the rolling
   average of the last 10 runs, and flag recipes that exceeded the average by more than
   a configurable threshold (default 25%)

7. Implement critical path analysis: given a recipe's dependency tree, identify the
   longest sequential execution chain and report its total time as the theoretical
   minimum build time

8. Identify parallelization opportunities: find sibling dependencies (recipes that share
   a parent dependency but do not depend on each other) and estimate potential time
   savings from parallel execution

9. Generate a summary report (terminal-friendly table) after each profiling run: recipe
   names sorted by duration, individual and cumulative durations, percentage of total
   time, and any regression warnings

10. Generate a timeline chart: text-based Gantt-like timeline if gnuplot is unavailable,
    or a PNG chart via gnuplot if available, showing recipe execution on a time axis

11. Implement `compare` recipe that takes two run IDs and shows a side-by-side
    comparison of recipe timings, highlighting significant differences (>10% change)

12. Implement `clean` recipe that prunes metrics older than a configurable retention
    period (default 30 days) from the metrics file to prevent unbounded growth

## Hints

- `just --summary --justfile path/to/justfile` lists all recipes;
  `just --dump --justfile path/to/justfile` outputs the parsed justfile which includes
  dependency information — parse the dependency lines to build the dependency graph

- `/usr/bin/time -v` (Linux) or `gtime -v` (macOS with GNU time) provides detailed
  execution statistics beyond just wall time — but `date +%s%N` before and after is
  simpler and more portable for millisecond-precision timing

- For JSON Lines format, each line is a self-contained JSON object —
  `jq -s` can slurp the entire file for analysis, and appending is as simple as
  `echo '{"recipe":"build","duration_ms":1234}' >> metrics.jsonl`

- Critical path analysis is essentially finding the longest path in a DAG — walk each
  path from root to leaf, sum durations along the way, and the maximum is the critical
  path

- `awk` is excellent for computing rolling averages from the metrics file without
  loading the entire history into memory — filter by recipe name, take the last N
  entries, and compute the mean

## Success Criteria

1. `just -f profiler.just profile target=./myproject recipe=build` profiles the build
   recipe and all its dependencies in the target project's justfile without errors

2. After profiling, `metrics.jsonl` contains one entry per recipe per run with correct
   timing data in milliseconds

3. `just -f profiler.just history recipe=build last=10` shows the last 10 profiled runs
   of `build` with durations and trend indicators (arrow up/down/flat)

4. A recipe that is artificially slowed (e.g., by adding `sleep 5`) triggers a
   regression warning on the next profiling run showing the percentage increase

5. Critical path analysis correctly identifies the longest dependency chain and reports
   its total duration as the theoretical minimum

6. Parallelization suggestions correctly identify sibling recipes that could run
   concurrently and estimate the time savings with specific numbers

7. The post-run summary table shows all recipe timings sorted by duration, their
   percentage contribution to total execution time, and cumulative percentage

8. The profiler works on a justfile it has never seen before, discovering recipes and
   dependencies automatically without any configuration

## Research Resources

- [Just Manual - Dump and Summary](https://just.systems/man/en/chapter_57.html)
  -- introspecting justfile structure programmatically for recipe discovery

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- writing the complex instrumentation and analysis logic

- [Gnuplot Documentation](http://www.gnuplot.info/documentation.html)
  -- generating performance charts and Gantt-like timelines from metrics data

- [JSON Lines Format](https://jsonlines.org/)
  -- append-friendly structured logging format for metrics storage

- [Critical Path Method - Wikipedia](https://en.wikipedia.org/wiki/Critical_path_method)
  -- theoretical foundation for identifying bottleneck chains in dependency graphs

- [Just Manual - Justfile Function](https://just.systems/man/en/chapter_43.html)
  -- referencing paths relative to the profiler justfile for metrics storage

## What's Next

Proceed to exercise 46, where you will automate the critical first minutes of
production incident response.

## Summary

- **Non-invasive profiling** -- instrumenting any justfile without modification through external wrapping and recipe discovery
- **Performance tracking** -- building a time-series history of recipe execution times with regression detection
- **Optimization analysis** -- identifying critical paths and parallelization opportunities in recipe dependency graphs
