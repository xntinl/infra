# 30. Build a Profiler and Performance Analyzer

**Difficulty**: Insane

---

## Prerequisites

- Erlang `:dbg`, `:trace`, and `:erlang.trace/3` tracing infrastructure
- Elixir process inspection: `Process.info/2`, `:erlang.process_info/2`
- Flame graph format specifications (Brendan Gregg collapsed stacks)
- Speedscope JSON format for interactive visualization
- Call graph data structures and tree aggregation algorithms
- OTP code_server and module loading internals
- Understanding of BEAM garbage collection mechanics

---

## Problem Statement

Build a runtime profiler for Elixir/OTP applications that can attach to a running node and collect performance data without requiring a restart or code modification. The profiler must:

1. Collect stacktrace samples from all or selected processes at a configurable sampling frequency
2. Automatically instrument selected modules by wrapping their exported functions using the Erlang tracing infrastructure
3. Build a call graph from collected traces, attributing time and allocation to individual functions
4. Generate flame graph output compatible with Brendan Gregg's format and the Speedscope interactive viewer
5. Attribute memory allocations to the functions that caused them, identifying which code paths drive the most heap growth
6. Identify functions that generate garbage collection pressure by correlating GC events with the active call stack
7. Attach to a remote production node over the Erlang distribution protocol without requiring a restart

---

## Acceptance Criteria

- [ ] Sampling profiler: uses `:erlang.trace/3` or a timer-based approach to collect stack samples from all running processes at a configurable rate (default 100 Hz); samples are aggregated across the collection window; overhead must be below 5% CPU on the target node
- [ ] Instrumentation: `Profiler.instrument(MyModule)` wraps all exported functions of `MyModule` using `:dbg` trace patterns; entry and exit times are recorded; the original module behavior is unaffected; instrumentation is removable at runtime
- [ ] Call graph: aggregated samples are organized into a tree of `{module, function, arity}` nodes; each node records total time (inclusive), self time (exclusive), call count, and average call duration; the tree can be queried by depth and filtered by module
- [ ] Flame graphs: `Profiler.export_flamegraph(path)` writes a file in Brendan Gregg's collapsed stack format (`a;b;c 42`) that can be fed to `flamegraph.pl` or Speedscope; the output correctly aggregates identical stack paths
- [ ] Memory attribution: `Profiler.memory_profile(duration)` samples `Process.info(pid, :memory)` and correlates spikes with the current call stack of the allocating process; reports top N functions by total bytes allocated during the window
- [ ] GC pressure: uses `:erlang.trace(:all, true, [:garbage_collection])` to intercept GC events; correlates each GC event with the call stack of the process at the moment of collection; reports top N functions by GC count and total words collected
- [ ] Live attach: `Profiler.connect(node_name, cookie)` connects to a running Erlang/Elixir node over the distribution protocol; all profiling operations are performed on the remote node; no restart, code injection, or configuration change is required on the target

---

## What You Will Learn

- Erlang tracing infrastructure: `:dbg`, `:erlang.trace`, match specifications
- Sampling vs. instrumentation profiling trade-offs
- Flame graph format and how stack aggregation works
- BEAM process memory model: heap, stack, binary heap, and GC triggers
- Erlang distribution protocol for remote node attachment
- Statistical sampling and how to account for sampling bias
- How to add runtime instrumentation without affecting correctness

---

## Hints

- Read the `:dbg` and `:erlang.trace/3` documentation in depth — the match specification language is powerful but complex
- Research the flame graph format by reading Brendan Gregg's original blog post and `flamegraph.pl` source
- Study how `:recon_trace` from the `recon` library implements safe production tracing with rate limiting
- Think about how to correlate GC events (which fire asynchronously) with the call stack that caused the allocation
- Investigate how module hot-swapping works in BEAM — your instrumentation wrapper must handle concurrent calls during the swap
- Look into the `:observer` source code for examples of reading process and memory info from a remote node

---

## Reference Material

- "The Erlang Runtime System" (ERTS) — open-source book (adoptingerlang.org)
- Brendan Gregg, "Flame Graphs" (brendangregg.com)
- Speedscope flame graph viewer documentation (github.com/jlfwong/speedscope)
- `recon` library documentation — Fred Hebert (hex.pm/packages/recon)
- "Erlang in Anger" — Fred Hebert (free PDF, erlanginanger.com)
- `:dbg` and `:erlang.trace` Erlang/OTP documentation

---

## Difficulty Rating ★★★★★★

Correlating GC events with call stacks, wrapping modules without affecting correctness, and attaching to production nodes safely requires mastery of BEAM internals that very few engineers possess.

---

## Estimated Time

60–100 hours
