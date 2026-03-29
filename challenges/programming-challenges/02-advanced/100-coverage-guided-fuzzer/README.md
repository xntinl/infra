<!-- difficulty: advanced -->
<!-- category: security-fuzzing -->
<!-- languages: [rust] -->
<!-- concepts: [coverage-guided-fuzzing, instrumentation, branch-coverage, corpus-prioritization, energy-scheduling, input-minimization] -->
<!-- estimated_time: 15-22 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [rust-basics, process-spawning, shared-memory, byte-mutation, hash-maps, basic-statistics, bitwise-operations] -->

# Challenge 100: Coverage-Guided Fuzzer

## Languages

Rust (stable, latest edition)

## Prerequisites

- Experience with process spawning and inter-process communication in Rust
- Understanding of code coverage concepts: basic blocks, edges, branches
- Familiarity with shared memory (or memory-mapped files) for IPC
- Byte-level mutation strategies from Challenge 77 (bit flipping, boundary injection, etc.)
- Hash maps and sets for tracking coverage state
- Basic understanding of evolutionary algorithms: fitness functions, selection pressure

## Learning Objectives

- **Implement** a coverage-guided fuzzer that uses branch coverage feedback to prioritize mutations
- **Analyze** how edge coverage tracking differs from line coverage and why it matters for finding bugs
- **Design** an energy scheduling algorithm that allocates more mutation cycles to inputs that discover new coverage
- **Evaluate** the effectiveness of coverage guidance versus random fuzzing on programs with deep code paths
- **Implement** test case minimization to reduce crash-triggering inputs to their essential bytes

## The Challenge

Random fuzzing (Challenge 77) finds shallow bugs but struggles with deep code paths. If a bug requires the input to pass three specific comparisons, random mutation has a probability of ~2^-24 of hitting all three simultaneously. Coverage-guided fuzzing solves this by observing which parts of the program each input exercises. When a mutation reaches new code, the fuzzer saves that input and mutates it further, progressively exploring deeper into the program.

This is the core insight behind AFL, libFuzzer, and honggfuzz -- the most effective fuzzers ever built. AFL discovered thousands of vulnerabilities in real software by combining coverage tracking with evolutionary mutation. Your fuzzer will implement the same principle.

Build a fuzzer that instruments target functions to report **edge coverage** -- which branches were taken during execution. The fuzzer maintains a global coverage bitmap. When an input produces a new bit in the bitmap (new edge), it is added to the corpus with high priority. Inputs that don't find new coverage are discarded. The energy scheduler allocates more mutations to inputs that have recently found new coverage, focusing effort on the most promising areas of the input space.

The coverage bitmap is a fixed-size array (e.g., 64KB) indexed by edge hashes. Each entry counts how many times that edge was hit (bucketed into powers of two: 1, 2, 4, 8, 16, 32, 64, 128+). A "new bit" means either a new edge was hit or an existing edge was hit a different number of times (new bucket). This hitcount bucketing lets the fuzzer distinguish between "this loop ran once" and "this loop ran 100 times."

The implementation uses in-process fuzzing: the target is a Rust function compiled into the fuzzer binary, and coverage is tracked via manual instrumentation (inserted counters at branch points). This avoids the complexity of external process instrumentation while teaching the same coverage concepts.

## Requirements

1. Define a coverage bitmap as a `[u8; 65536]` array, shared between the fuzzer and the instrumented target function
2. Implement edge tracking: at each branch point in the target, compute an edge hash `(prev_location XOR current_location) % BITMAP_SIZE` and increment the corresponding bitmap entry. Update `prev_location = current_location >> 1` after each edge
3. Implement hitcount bucketing: map raw hit counts to buckets `[1, 2, 3, 4-7, 8-15, 16-31, 32-127, 128+]` for comparison purposes. Two inputs have the same coverage signature only if all bucketed counts match
4. Implement a corpus queue: each entry stores the input bytes, the set of new coverage bits it discovered, its energy level, and mutation history
5. Implement corpus scoring: an input's priority is proportional to the number of new coverage bits it introduced. Inputs that found many new bits get more mutations
6. Implement energy scheduling: each input has an energy level (number of mutations to apply before moving to the next input). New-coverage inputs start with high energy. Inputs that haven't produced new coverage in N mutations have their energy reduced
7. Implement at least 5 mutation strategies: bit flip, byte flip, arithmetic (add/subtract small values to multi-byte integers), interesting values (boundary constants), and random havoc (chain of random mutations)
8. Implement corpus minimization: periodically remove corpus entries whose coverage is fully subsumed by other entries (all bits they contributed are also hit by other inputs)
9. Implement input minimization: given a crash-triggering input, iteratively reduce it by removing chunks, replacing chunks with zeros, and testing if the crash still reproduces. Use binary search on chunk sizes for efficiency
10. Implement at least 3 target functions with intentional bugs at varying depths: one reachable in ~2 mutations, one requiring ~4-6 specific byte values, and one behind a checksum-like guard that random fuzzing cannot reach but coverage guidance can
11. Track and report: total executions, executions per second, corpus size, coverage percentage (bits set / total bits), new coverage events over time, crashes found
12. Implement a coverage stability check: run the same input twice and verify the coverage bitmap is identical (detect nondeterministic targets)
13. Store crash-triggering inputs with their minimized versions and coverage bitmaps

## Hints

1. The edge hash `(prev_location XOR current_location)` is AFL's key insight. It captures
   *which transition* occurred, not just which locations were visited. A -> B -> C produces
   different edge hashes than A -> C -> B, even though the same locations are visited. The
   right-shift on `prev_location` (`prev_location >> 1`) prevents `A -> B` and `B -> A` from
   producing the same hash.

2. For in-process fuzzing, use a thread-local coverage bitmap. Before each execution, clear the
   bitmap. After execution, compare against the global coverage map to detect new bits. The
   target function is called directly (no process spawn), so coverage is fast -- 100,000+
   execs/sec is achievable.

3. Energy scheduling follows a simple rule: inputs that recently found new coverage get 2x-4x
   more mutations than baseline. After a full cycle through the corpus without new coverage,
   enter a "havoc" phase with increased mutation intensity. This balances exploration (trying
   new mutations) with exploitation (focusing on productive inputs).

4. Input minimization is essentially delta debugging: try removing the first half of the input.
   If the crash still reproduces, keep the shorter version. Otherwise, try removing the second
   half. Then try quarters, eighths, etc. Also try replacing byte ranges with zeros (which
   often preserves crashes while simplifying the input).

## Acceptance Criteria

- [ ] Coverage bitmap is 65536 bytes and correctly tracks edge hits during target execution
- [ ] Edge hash uses `prev_location XOR current_location` and updates `prev_location` after each edge
- [ ] Hitcount bucketing maps raw counts to the 8 specified buckets
- [ ] New coverage detection: an input is flagged as interesting only when it produces a new bit (new edge or new bucket) in the global coverage map
- [ ] Corpus entries store their input, coverage contribution, and energy level
- [ ] Energy scheduling allocates 2x+ mutations to inputs that found new coverage vs. baseline
- [ ] All 5 mutation strategies produce valid mutations (bit flip, byte flip, arithmetic, interesting values, havoc)
- [ ] Corpus minimization removes entries whose coverage is fully subsumed by other entries
- [ ] Input minimization reduces a crash-triggering input by at least 50% on average
- [ ] Shallow target bug (depth ~2) is found within 1 second
- [ ] Medium target bug (depth ~5) is found within 30 seconds with coverage guidance
- [ ] Coverage percentage increases over time (printed in stats)
- [ ] Coverage stability check detects nondeterministic targets
- [ ] Crash inputs are saved with their minimized versions
- [ ] The fuzzer achieves at least 10,000 execs/sec in in-process mode
- [ ] All tests pass with `cargo test`

## Research Resources

- [AFL Technical Whitepaper (Michal Zalewski)](https://lcamtuf.coredump.cx/afl/technical_details.txt) -- the seminal document explaining coverage-guided fuzzing. Read sections on edge coverage, hitcount bucketing, and the evolutionary algorithm
- [The Fuzzing Book: Coverage-based Fuzzing](https://www.fuzzingbook.org/html/GreyboxFuzzer.html) -- interactive chapter with executable code showing how coverage guidance works
- [libFuzzer: A Library for Coverage-Guided Fuzz Testing](https://llvm.org/docs/LibFuzzer.html) -- LLVM's in-process fuzzer documentation; study the mutation strategies and corpus management
- [AFL++: Combining Incremental Steps of Fuzzing Research](https://github.com/AFLplusplus/AFLplusplus) -- modern fork of AFL with power schedules, CMPLOG, and MOpt mutation scheduling
- [Fuzz Testing: A Comprehensive Survey (Li et al., 2020)](https://arxiv.org/abs/1812.00140) -- survey categorizing greybox fuzzing techniques
- [REDQUEEN: Fuzzing with Input-to-State Correspondence](https://www.syssec.ruhr-uni-bochum.de/media/emma/veroeffentlichungen/2019/02/12/NDSS19-Redqueen.pdf) -- technique for automatically solving magic byte comparisons that block coverage
- [MOpt: Optimized Mutation Scheduling (Lyu et al., 2019)](https://www.usenix.org/conference/usenixsecurity19/presentation/lyu) -- applying particle swarm optimization to select mutation strategies
