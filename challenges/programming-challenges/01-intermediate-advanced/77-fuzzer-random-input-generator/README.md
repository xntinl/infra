<!-- difficulty: intermediate-advanced -->
<!-- category: security-fuzzing -->
<!-- languages: [rust] -->
<!-- concepts: [fuzzing, random-generation, mutation-strategies, corpus-management, crash-detection, byte-manipulation] -->
<!-- estimated_time: 6-9 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [rust-basics, random-number-generation, process-spawning, file-io, byte-manipulation, json-basics] -->

# Challenge 77: Fuzzer Random Input Generator

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable spawning child processes and capturing exit codes in Rust (`std::process::Command`)
- Understanding of random number generation: seeded PRNGs, uniform distributions over byte ranges
- Familiarity with byte-level manipulation: slicing, inserting, deleting, and replacing bytes in `Vec<u8>`
- Basic knowledge of structured formats (JSON syntax, HTTP request structure) to generate syntactically plausible inputs
- File I/O for saving and loading corpus entries from disk
- Understanding of why programs crash: buffer overflows, integer overflows, format string bugs, off-by-one errors

## Learning Objectives

- **Implement** a random input generator that produces bytes, strings, and structured data from scratch
- **Apply** mutation strategies (bit flipping, byte insertion, deletion, replacement, boundary value injection) to transform existing inputs
- **Analyze** which mutation strategies are effective for different input formats and why
- **Design** a corpus management system that saves inputs triggering new behavior
- **Evaluate** the effectiveness of random fuzzing versus mutation-based fuzzing on a set of target programs

## The Challenge

Fuzzing is the art of feeding garbage to programs until they break. It sounds crude, but fuzzers like AFL, libFuzzer, and honggfuzz have found thousands of critical vulnerabilities in production software -- from heartbleed-class memory bugs to logic errors that static analysis misses entirely. Every major software company runs fuzzers continuously against their codebases.

Build a random input fuzzer from scratch. The fuzzer operates in two modes: **generation** (create inputs from nothing) and **mutation** (transform existing inputs). In generation mode, produce random bytes, random ASCII strings, random JSON documents, and random HTTP requests. In mutation mode, take an existing input and apply strategies: flip random bits, insert random bytes, delete byte ranges, replace bytes with boundary values (0x00, 0xFF, 0x7F, 0x80, powers of two), and splice two inputs together.

The fuzzer maintains a **corpus** -- a directory of interesting inputs. When an input causes the target program to crash (non-zero exit code, signal), save it to the corpus with metadata (timestamp, mutation strategy used, parent input if mutated). The fuzzer runs in a loop: pick an input from the corpus (or generate a fresh one), mutate it, feed it to the target, check the result, and save if interesting.

This is a dumb fuzzer -- it has no idea what the program does internally. Challenge 100 adds coverage guidance. But even dumb fuzzing finds real bugs, especially with good mutation strategies and boundary values. The key insight is that most bugs live at boundaries: the zero byte, the maximum integer, the empty string, the off-by-one length.

## Requirements

1. Implement a seeded PRNG using `rand` crate (or a minimal xorshift implementation with no dependencies) that produces uniform random `u8`, `u32`, and `usize` values within configurable ranges
2. Implement random byte generation: produce `Vec<u8>` of random length (configurable min/max) filled with random bytes
3. Implement random ASCII string generation: produce strings with configurable character sets (printable ASCII, alphanumeric, full byte range)
4. Implement random JSON generation: produce syntactically valid JSON with random nesting depth (max configurable), random key names, string/number/bool/null/array/object values. The JSON must parse successfully
5. Implement random HTTP request generation: produce valid HTTP/1.1 request lines (method, path, version), random headers (valid header names and values), and random body content
6. Implement bit-flip mutation: flip 1, 2, or 4 consecutive bits at a random position in the input
7. Implement byte-level mutations: insert random bytes at random positions, delete random byte ranges, replace random byte ranges with random values
8. Implement boundary value injection: replace random positions with values from a table of interesting values: `[0x00, 0x01, 0x7E, 0x7F, 0x80, 0xFF, 0x00FF, 0x0100, 0x7FFF, 0x8000, 0xFFFF, 0x10000, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF]` encoded as little-endian and big-endian byte sequences
9. Implement input splicing: take two corpus inputs and combine them (crossover at random offsets)
10. Implement corpus management: a directory-based corpus where each input is a file, with a metadata sidecar (JSON) recording the mutation chain, timestamp, and result
11. Implement a fuzzing loop: select input (corpus or fresh), apply random mutation (or chain of mutations), feed to target process via stdin, capture exit code and stderr, classify result (ok, crash, timeout, interesting)
12. Implement crash detection: non-zero exit code or signal termination (on Unix, detect specific signals like SIGSEGV, SIGABRT via exit status)
13. Implement deduplication: do not save crash inputs that produce identical output (stderr) to an already-saved crash
14. Write at least 3 small target programs in Rust that have intentional bugs (buffer over-read, integer overflow, panic on specific byte patterns) to test the fuzzer against
15. Print statistics: executions per second, total crashes found, corpus size, current mutation strategy

## Hints

<details>
<summary>Hint 1: PRNG without external dependencies</summary>

If you want zero dependencies, implement xorshift64:

```rust
struct Rng {
    state: u64,
}

impl Rng {
    fn next_u64(&mut self) -> u64 {
        self.state ^= self.state << 13;
        self.state ^= self.state >> 7;
        self.state ^= self.state << 17;
        self.state
    }

    fn next_u8(&mut self) -> u8 {
        self.next_u64() as u8
    }

    fn range(&mut self, min: usize, max: usize) -> usize {
        min + (self.next_u64() as usize % (max - min))
    }
}
```

Seed with the current timestamp or a user-provided value for reproducibility.

</details>

<details>
<summary>Hint 2: Boundary values as byte sequences</summary>

The interesting values list contains both 1-byte, 2-byte, and 4-byte boundaries. When injecting, pick one randomly and write it at a random offset. For multi-byte values, write in both little-endian and big-endian:

```rust
const INTERESTING_8: &[u8] = &[0, 1, 0x7F, 0x80, 0xFF];
const INTERESTING_16: &[u16] = &[0, 0x80, 0xFF, 0x100, 0x7FFF, 0x8000, 0xFFFF];
const INTERESTING_32: &[u32] = &[0, 0x80, 0xFFFF, 0x10000, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF];
```

These values sit on boundaries that trigger off-by-one, sign extension, and truncation bugs.

</details>

<details>
<summary>Hint 3: JSON generation with depth control</summary>

Use recursion with a depth counter. At each level, randomly choose a JSON type. For objects and arrays, recurse. Decrease remaining depth at each level. When depth reaches 0, only emit leaf types (string, number, bool, null):

```rust
fn gen_json_value(rng: &mut Rng, max_depth: usize) -> String {
    if max_depth == 0 {
        return gen_json_leaf(rng);
    }
    match rng.range(0, 6) {
        0..=1 => gen_json_leaf(rng),          // string, number, bool, null
        2 => gen_json_object(rng, max_depth - 1),
        3 => gen_json_array(rng, max_depth - 1),
        _ => gen_json_leaf(rng),
    }
}
```

</details>

<details>
<summary>Hint 4: Crash classification via exit status</summary>

On Unix, `std::process::ExitStatus` encodes signal information. A segfault is signal 11 (SIGSEGV), abort is signal 6 (SIGABRT):

```rust
use std::os::unix::process::ExitStatusExt;

let status = child.wait()?;
if let Some(signal) = status.signal() {
    // Killed by signal (crash)
    match signal {
        11 => CrashKind::Segfault,
        6 => CrashKind::Abort,
        _ => CrashKind::Signal(signal),
    }
} else if !status.success() {
    CrashKind::NonZeroExit(status.code().unwrap_or(-1))
}
```

</details>

## Acceptance Criteria

- [ ] Random byte generator produces vectors of configurable length filled with uniformly distributed bytes
- [ ] Random ASCII string generator produces valid UTF-8 strings with characters from the configured set
- [ ] Random JSON generator produces syntactically valid JSON that parses without error (test with `serde_json` or manual validation)
- [ ] Random HTTP request generator produces parseable HTTP/1.1 requests with valid method, path, headers, and body
- [ ] Bit-flip mutation changes exactly the specified number of bits and leaves everything else intact
- [ ] Byte insertion increases input length by the inserted amount
- [ ] Byte deletion decreases input length by the deleted amount
- [ ] Boundary value injection replaces bytes at the target position with the correct encoding of the interesting value
- [ ] Splicing combines two inputs into one that contains subsequences of both
- [ ] Corpus directory is created and populated with crash-triggering inputs as files
- [ ] Metadata sidecar files record mutation strategy, parent input hash, and timestamp
- [ ] Fuzzer correctly detects crashes (non-zero exit, signals) in the provided target programs
- [ ] Fuzzer finds at least one crash in each of the 3 intentional-bug targets within 60 seconds
- [ ] Duplicate crashes (identical stderr output) are not saved multiple times
- [ ] Statistics (exec/s, crashes, corpus size) are printed to stderr during fuzzing
- [ ] Seeding the PRNG with the same value and replaying the same corpus produces identical mutation sequences
- [ ] All tests pass with `cargo test`

## Research Resources

- [AFL Technical Whitepaper (Michal Zalewski)](https://lcamtuf.coredump.cx/afl/technical_details.txt) -- the design document for American Fuzzy Lop, the most influential fuzzer. Explains mutation strategies, corpus management, and the insight behind coverage-guided fuzzing
- [The Fuzzing Book (Andreas Zeller et al.)](https://www.fuzzingbook.org/) -- comprehensive online textbook covering random generation, mutation, grammar-based fuzzing, and coverage guidance with executable examples
- [libFuzzer documentation](https://llvm.org/docs/LibFuzzer.html) -- LLVM's in-process fuzzer; study its mutation strategies and corpus management approach
- [Fuzzing: Art, Science, and Engineering (Manes et al., 2019)](https://arxiv.org/abs/1812.00140) -- survey paper categorizing fuzzing techniques and their effectiveness
- [Rust `rand` crate documentation](https://docs.rs/rand/latest/rand/) -- random number generation in Rust, including distributions and seeding
- [Property-based testing with `proptest`](https://docs.rs/proptest/latest/proptest/) -- related technique that generates structured random inputs using strategies; useful for understanding generation approaches
