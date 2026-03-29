# 133. AFL-Style Mutation Fuzzer

<!--
difficulty: insane
category: security-testing
languages: [rust]
concepts: [fork-server, shared-memory-bitmap, mutation-strategies, coverage-guided-fuzzing, crash-deduplication, queue-scheduling, parallel-fuzzing]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [unix-process-model, shared-memory, signal-handling, binary-mutation, code-coverage-concepts, file-io]
-->

## Languages

- Rust (1.75+ stable)

## Prerequisites

- Unix process model: fork, exec, waitpid, signals
- POSIX shared memory and memory-mapped files
- Code coverage instrumentation concepts (edge coverage)
- Binary file manipulation (byte-level mutations)
- Process signal handling (SIGSEGV, SIGABRT, SIGALRM)

## Learning Objectives

By the end of this challenge you will be able to **create** a coverage-guided mutation fuzzer inspired by AFL that discovers crashes in target programs through intelligent input mutation, coverage feedback, and crash deduplication.

## The Challenge

Build a mutation-based fuzzer that uses coverage feedback to guide input generation toward unexplored program paths. The fuzzer spawns target programs via a fork server for speed, tracks edge coverage through a shared memory bitmap, applies deterministic and random mutations to inputs, and schedules queue entries to maximize coverage growth. Crashes are deduplicated by their coverage signature.

This is not a random input generator or a simple byte flipper. You are building the core engine behind tools like AFL and LibFuzzer: a feedback loop where coverage data from each execution guides which inputs to keep, which to mutate further, and which mutations are most productive. The fork server eliminates the cost of re-exec'ing the target on every input. The coverage bitmap captures which branches the target took, and new coverage means a new queue entry worth exploring.

## Requirements

- [ ] Fork server: spawn target once, then fork+resume for each test case (avoid exec overhead). Communicate via pipes (control and status)
- [ ] Shared memory bitmap: 64KB bitmap tracking edge coverage (branch source XOR'd with branch destination). Target writes to bitmap via `__AFL_SHM_ID` environment variable
- [ ] Deterministic mutations: walking bit flips (1, 2, 4 bits), walking byte flips (1, 2, 4 bytes), arithmetic (add/sub small integers to bytes/words/dwords), interesting value replacement (0, 1, -1, MAX_INT, etc.)
- [ ] Havoc stage: random combinations of mutations (splice, insert, delete, overwrite random bytes, dictionary token insertion)
- [ ] Queue management: each test case is a queue entry with metadata (file size, execution time, coverage bitmap hash, depth, was-fuzzed flag)
- [ ] Favored inputs: select a minimal set of queue entries that covers all observed edges (set-cover approximation). Favored inputs get more fuzzing time
- [ ] Crash deduplication: group crashes by their coverage bitmap hash. Unique coverage path = unique crash
- [ ] Timeout detection: configurable per-execution timeout, kill hung processes via SIGALRM
- [ ] Crash/hang classification: separate output directories for crashes, hangs, and queue
- [ ] Status screen: display stats (executions/sec, total paths, unique crashes, coverage percentage, current stage) updated periodically
- [ ] Parallel fuzzing: multiple fuzzer instances sync inputs via a shared directory. Each instance writes interesting inputs to its own queue; others periodically import from peers
- [ ] Seed corpus: accept a directory of initial inputs as starting seeds
- [ ] Dictionary support: optional dictionary file with tokens to splice into mutations

## Hints

1. The fork server protocol: the fuzzer starts the target once. The target blocks at a known point (just before `main` logic). For each test case, the fuzzer writes a "go" signal on the control pipe; the target forks, the child processes the input, and the parent reports the child's exit status on the status pipe. This avoids the ~1ms overhead of `execve` per run, which matters when you need 10K+ execs/sec.

2. Coverage is tracked as edges, not basic blocks. Use `prev_location XOR cur_location` as the index into the shared bitmap, where locations are random compile-time IDs. Classify hit counts into buckets (1, 2, 3, 4-7, 8-15, 16-31, 32-127, 128+) to detect loop iteration changes without bitmap noise.

3. For crash deduplication, hash only the edges that were hit (positions where bitmap > 0), not the full 64KB. Two crashes with identical edge sets are likely the same bug. Store the hash with each crash to avoid re-analysis.

## Acceptance Criteria

- [ ] Fork server successfully spawns and reuses a target process across multiple inputs
- [ ] Shared memory bitmap correctly captures edge coverage from an instrumented target
- [ ] Deterministic mutation stages systematically explore byte-level variations
- [ ] Havoc stage generates diverse random mutations
- [ ] Queue scheduling prioritizes favored inputs that contribute unique coverage
- [ ] Crash deduplication correctly groups crashes by coverage signature
- [ ] Timeouts are detected and classified separately from crashes
- [ ] Multiple fuzzer instances sync inputs through the shared directory
- [ ] Fuzzer discovers injected bugs in a provided test harness
- [ ] `cargo test` passes with unit and integration tests

## Research Resources

- [AFL Technical Whitepaper (Michal Zalewski)](https://lcamtuf.coredump.cx/afl/technical_details.txt) -- the authoritative description of AFL internals
- [AFL source code](https://github.com/google/AFL) -- reference implementation in C
- [The Fuzzing Book (Zeller et al.)](https://www.fuzzingbook.org/) -- mutation-based fuzzing, coverage-guided fuzzing chapters
- [LibFuzzer documentation](https://llvm.org/docs/LibFuzzer.html) -- in-process coverage-guided fuzzing, alternative architecture
- [AFL fork server explanation (lcamtuf blog)](https://lcamtuf.blogspot.com/2014/10/fuzzing-binaries-without-execve.html) -- fork server design rationale
- [Rust `nix` crate](https://docs.rs/nix/latest/nix/) -- safe Rust wrappers for fork, shmget, waitpid, signals
- [Rust `libc` crate](https://docs.rs/libc/latest/libc/) -- raw POSIX bindings for shared memory and process control
