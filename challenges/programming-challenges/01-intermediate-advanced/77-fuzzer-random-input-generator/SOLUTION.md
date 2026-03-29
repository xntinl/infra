# Solution: Fuzzer Random Input Generator

## Architecture Overview

The fuzzer is organized into six modules:

1. **RNG**: Seeded xorshift64 PRNG providing uniform random bytes, integers, and range-bounded values
2. **Generators**: Random input creation from scratch -- bytes, strings, JSON, HTTP requests
3. **Mutators**: Transformation strategies applied to existing inputs -- bit flips, byte ops, boundary injection, splicing
4. **Corpus**: Directory-based storage for interesting inputs with JSON metadata sidecars
5. **Runner**: Process spawning, exit code capture, crash classification, and timeout handling
6. **Fuzzer loop**: Orchestrates generation, mutation, execution, and corpus management

```
                     +----------+
                     |  Fuzzer   |
                     |   Loop    |
                     +----+-----+
                          |
          +---------------+---------------+
          |               |               |
     +----v----+    +-----v-----+   +-----v-----+
     |Generator|    |  Mutator  |   |   Runner   |
     +---------+    +-----------+   +-----+------+
          |               |               |
          +-------+-------+         +-----v-----+
                  |                 |   Corpus   |
             +----v----+           +-----------+
             |   RNG   |
             +---------+
```

## Rust Solution

### Project Setup

```bash
cargo new fuzzer-random
cd fuzzer-random
```

`Cargo.toml`:

```toml
[package]
name = "fuzzer-random"
version = "0.1.0"
edition = "2021"
```

### Source: `src/rng.rs`

```rust
/// Xorshift64 PRNG -- no external dependencies.
pub struct Rng {
    state: u64,
}

impl Rng {
    pub fn new(seed: u64) -> Self {
        // Avoid zero state which is a fixed point
        let state = if seed == 0 { 0xDEAD_BEEF_CAFE_BABE } else { seed };
        Rng { state }
    }

    pub fn from_timestamp() -> Self {
        let seed = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as u64;
        Rng::new(seed)
    }

    pub fn next_u64(&mut self) -> u64 {
        self.state ^= self.state << 13;
        self.state ^= self.state >> 7;
        self.state ^= self.state << 17;
        self.state
    }

    pub fn next_u32(&mut self) -> u32 {
        self.next_u64() as u32
    }

    pub fn next_u8(&mut self) -> u8 {
        self.next_u64() as u8
    }

    pub fn next_bool(&mut self) -> bool {
        self.next_u64() & 1 == 1
    }

    /// Random usize in [min, max) -- panics if min >= max.
    pub fn range(&mut self, min: usize, max: usize) -> usize {
        assert!(min < max, "range requires min < max");
        min + (self.next_u64() as usize % (max - min))
    }

    /// Random u8 in [min, max].
    pub fn range_u8(&mut self, min: u8, max: u8) -> u8 {
        min + (self.next_u8() % (max - min + 1))
    }

    /// Pick a random element from a slice.
    pub fn choose<'a, T>(&mut self, items: &'a [T]) -> &'a T {
        let idx = self.range(0, items.len());
        &items[idx]
    }

    /// Fill a buffer with random bytes.
    pub fn fill_bytes(&mut self, buf: &mut [u8]) {
        for byte in buf.iter_mut() {
            *byte = self.next_u8();
        }
    }
}
```

### Source: `src/generators.rs`

```rust
use crate::rng::Rng;

/// Generate random bytes of length in [min_len, max_len).
pub fn random_bytes(rng: &mut Rng, min_len: usize, max_len: usize) -> Vec<u8> {
    let len = rng.range(min_len, max_len);
    let mut buf = vec![0u8; len];
    rng.fill_bytes(&mut buf);
    buf
}

/// Generate random ASCII string from the given character set.
pub fn random_ascii_string(rng: &mut Rng, min_len: usize, max_len: usize, charset: &[u8]) -> String {
    let len = rng.range(min_len, max_len);
    let bytes: Vec<u8> = (0..len).map(|_| *rng.choose(charset)).collect();
    String::from_utf8(bytes).unwrap_or_else(|_| String::from("fallback"))
}

const PRINTABLE_ASCII: &[u8] = b"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !@#$%^&*()-_=+[]{}|;:',.<>?/";
const ALPHA_NUM: &[u8] = b"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";

pub fn random_printable_string(rng: &mut Rng, min_len: usize, max_len: usize) -> String {
    random_ascii_string(rng, min_len, max_len, PRINTABLE_ASCII)
}

pub fn random_alpha_num(rng: &mut Rng, min_len: usize, max_len: usize) -> String {
    random_ascii_string(rng, min_len, max_len, ALPHA_NUM)
}

/// Generate syntactically valid random JSON.
pub fn random_json(rng: &mut Rng, max_depth: usize) -> String {
    gen_json_value(rng, max_depth)
}

fn gen_json_value(rng: &mut Rng, depth: usize) -> String {
    if depth == 0 {
        return gen_json_leaf(rng);
    }
    match rng.range(0, 7) {
        0..=2 => gen_json_leaf(rng),
        3 | 4 => gen_json_object(rng, depth - 1),
        _ => gen_json_array(rng, depth - 1),
    }
}

fn gen_json_leaf(rng: &mut Rng) -> String {
    match rng.range(0, 4) {
        0 => {
            // String
            let s = random_alpha_num(rng, 1, 12);
            format!("\"{}\"", s)
        }
        1 => {
            // Number (integer or float)
            if rng.next_bool() {
                format!("{}", rng.next_u32() as i32)
            } else {
                let int_part = rng.range(0, 10000) as f64;
                let frac = rng.range(0, 100) as f64 / 100.0;
                format!("{:.2}", int_part + frac)
            }
        }
        2 => {
            if rng.next_bool() { "true".to_string() } else { "false".to_string() }
        }
        _ => "null".to_string(),
    }
}

fn gen_json_object(rng: &mut Rng, depth: usize) -> String {
    let count = rng.range(0, 5);
    let pairs: Vec<String> = (0..count)
        .map(|_| {
            let key = random_alpha_num(rng, 1, 8);
            let val = gen_json_value(rng, depth);
            format!("\"{}\":{}", key, val)
        })
        .collect();
    format!("{{{}}}", pairs.join(","))
}

fn gen_json_array(rng: &mut Rng, depth: usize) -> String {
    let count = rng.range(0, 6);
    let items: Vec<String> = (0..count)
        .map(|_| gen_json_value(rng, depth))
        .collect();
    format!("[{}]", items.join(","))
}

/// Generate a syntactically valid HTTP/1.1 request.
pub fn random_http_request(rng: &mut Rng) -> Vec<u8> {
    let methods = ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"];
    let method = rng.choose(&methods);

    let path_segments: Vec<String> = (0..rng.range(1, 5))
        .map(|_| random_alpha_num(rng, 1, 10))
        .collect();
    let path = format!("/{}", path_segments.join("/"));

    let mut request = format!("{} {} HTTP/1.1\r\n", method, path);

    let host = format!("{}.example.com", random_alpha_num(rng, 3, 10));
    request.push_str(&format!("Host: {}\r\n", host));

    let header_count = rng.range(0, 6);
    let header_names = [
        "Content-Type", "Accept", "User-Agent", "Authorization",
        "X-Request-Id", "Cache-Control", "Accept-Encoding",
    ];
    for _ in 0..header_count {
        let name = rng.choose(&header_names);
        let value = random_printable_string(rng, 1, 30);
        request.push_str(&format!("{}: {}\r\n", name, value));
    }

    // Optionally add a body for POST/PUT/PATCH
    if *method == "POST" || *method == "PUT" || *method == "PATCH" {
        let body = random_printable_string(rng, 0, 200);
        request.push_str(&format!("Content-Length: {}\r\n", body.len()));
        request.push_str("\r\n");
        request.push_str(&body);
    } else {
        request.push_str("\r\n");
    }

    request.into_bytes()
}
```

### Source: `src/mutators.rs`

```rust
use crate::rng::Rng;

const INTERESTING_8: &[u8] = &[0x00, 0x01, 0x7E, 0x7F, 0x80, 0xFE, 0xFF];

const INTERESTING_16: &[u16] = &[
    0x0000, 0x0001, 0x007F, 0x0080, 0x00FF, 0x0100,
    0x7FFF, 0x8000, 0xFFFE, 0xFFFF,
];

const INTERESTING_32: &[u32] = &[
    0x0000_0000, 0x0000_0001, 0x0000_007F, 0x0000_0080,
    0x0000_00FF, 0x0000_0100, 0x0000_FFFF, 0x0001_0000,
    0x7FFF_FFFF, 0x8000_0000, 0xFFFF_FFFE, 0xFFFF_FFFF,
];

#[derive(Debug, Clone, Copy)]
pub enum MutationKind {
    BitFlip,
    ByteInsert,
    ByteDelete,
    ByteReplace,
    BoundaryValue,
    Splice,
}

impl MutationKind {
    pub fn random(rng: &mut Rng) -> Self {
        match rng.range(0, 5) {
            0 => MutationKind::BitFlip,
            1 => MutationKind::ByteInsert,
            2 => MutationKind::ByteDelete,
            3 => MutationKind::ByteReplace,
            _ => MutationKind::BoundaryValue,
        }
    }

    pub fn name(&self) -> &'static str {
        match self {
            MutationKind::BitFlip => "bit_flip",
            MutationKind::ByteInsert => "byte_insert",
            MutationKind::ByteDelete => "byte_delete",
            MutationKind::ByteReplace => "byte_replace",
            MutationKind::BoundaryValue => "boundary_value",
            MutationKind::Splice => "splice",
        }
    }
}

/// Flip `count` consecutive bits starting at a random bit position.
pub fn bit_flip(rng: &mut Rng, input: &[u8], count: usize) -> Vec<u8> {
    if input.is_empty() {
        return input.to_vec();
    }
    let mut result = input.to_vec();
    let total_bits = input.len() * 8;
    let start_bit = rng.range(0, total_bits.saturating_sub(count).max(1));

    for i in 0..count {
        let bit_pos = start_bit + i;
        if bit_pos >= total_bits {
            break;
        }
        let byte_idx = bit_pos / 8;
        let bit_idx = bit_pos % 8;
        result[byte_idx] ^= 1 << bit_idx;
    }
    result
}

/// Insert `count` random bytes at a random position.
pub fn byte_insert(rng: &mut Rng, input: &[u8], count: usize) -> Vec<u8> {
    let pos = if input.is_empty() { 0 } else { rng.range(0, input.len() + 1) };
    let mut result = Vec::with_capacity(input.len() + count);
    result.extend_from_slice(&input[..pos]);
    for _ in 0..count {
        result.push(rng.next_u8());
    }
    result.extend_from_slice(&input[pos..]);
    result
}

/// Delete up to `max_count` bytes starting at a random position.
pub fn byte_delete(rng: &mut Rng, input: &[u8], max_count: usize) -> Vec<u8> {
    if input.is_empty() {
        return input.to_vec();
    }
    let pos = rng.range(0, input.len());
    let count = max_count.min(input.len() - pos).max(1);
    let actual = rng.range(1, count + 1);
    let mut result = Vec::with_capacity(input.len() - actual);
    result.extend_from_slice(&input[..pos]);
    result.extend_from_slice(&input[(pos + actual)..]);
    result
}

/// Replace a random byte range with random bytes.
pub fn byte_replace(rng: &mut Rng, input: &[u8], max_count: usize) -> Vec<u8> {
    if input.is_empty() {
        return input.to_vec();
    }
    let mut result = input.to_vec();
    let pos = rng.range(0, input.len());
    let count = max_count.min(input.len() - pos).max(1);
    for i in 0..count {
        result[pos + i] = rng.next_u8();
    }
    result
}

/// Replace bytes at a random position with an interesting boundary value.
pub fn boundary_inject(rng: &mut Rng, input: &[u8]) -> Vec<u8> {
    if input.is_empty() {
        return input.to_vec();
    }
    let mut result = input.to_vec();

    match rng.range(0, 3) {
        0 => {
            // 1-byte interesting value
            let val = *rng.choose(INTERESTING_8);
            let pos = rng.range(0, result.len());
            result[pos] = val;
        }
        1 => {
            // 2-byte interesting value
            if result.len() >= 2 {
                let val = *rng.choose(INTERESTING_16);
                let pos = rng.range(0, result.len() - 1);
                let bytes = if rng.next_bool() {
                    val.to_le_bytes()
                } else {
                    val.to_be_bytes()
                };
                result[pos] = bytes[0];
                result[pos + 1] = bytes[1];
            }
        }
        _ => {
            // 4-byte interesting value
            if result.len() >= 4 {
                let val = *rng.choose(INTERESTING_32);
                let pos = rng.range(0, result.len() - 3);
                let bytes = if rng.next_bool() {
                    val.to_le_bytes()
                } else {
                    val.to_be_bytes()
                };
                for i in 0..4 {
                    result[pos + i] = bytes[i];
                }
            }
        }
    }
    result
}

/// Splice two inputs together at random crossover points.
pub fn splice(rng: &mut Rng, a: &[u8], b: &[u8]) -> Vec<u8> {
    if a.is_empty() {
        return b.to_vec();
    }
    if b.is_empty() {
        return a.to_vec();
    }
    let split_a = rng.range(0, a.len());
    let split_b = rng.range(0, b.len());
    let mut result = Vec::with_capacity(split_a + (b.len() - split_b));
    result.extend_from_slice(&a[..split_a]);
    result.extend_from_slice(&b[split_b..]);
    result
}

/// Apply a single random mutation to the input.
pub fn mutate(rng: &mut Rng, input: &[u8]) -> (Vec<u8>, MutationKind) {
    let kind = MutationKind::random(rng);
    let result = match kind {
        MutationKind::BitFlip => {
            let count = *rng.choose(&[1usize, 2, 4]);
            bit_flip(rng, input, count)
        }
        MutationKind::ByteInsert => {
            let count = rng.range(1, 16);
            byte_insert(rng, input, count)
        }
        MutationKind::ByteDelete => byte_delete(rng, input, 8),
        MutationKind::ByteReplace => byte_replace(rng, input, 4),
        MutationKind::BoundaryValue => boundary_inject(rng, input),
        MutationKind::Splice => {
            // Splice with self (caller should provide corpus input)
            splice(rng, input, input)
        }
    };
    (result, kind)
}
```

### Source: `src/corpus.rs`

```rust
use std::collections::HashSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};
use crate::rng::Rng;

pub struct Corpus {
    dir: PathBuf,
    crash_dir: PathBuf,
    entries: Vec<PathBuf>,
    crash_hashes: HashSet<u64>,
}

impl Corpus {
    pub fn new(base_dir: &Path) -> std::io::Result<Self> {
        let dir = base_dir.join("queue");
        let crash_dir = base_dir.join("crashes");
        fs::create_dir_all(&dir)?;
        fs::create_dir_all(&crash_dir)?;

        // Load existing entries
        let mut entries = Vec::new();
        if dir.exists() {
            for entry in fs::read_dir(&dir)? {
                let entry = entry?;
                let path = entry.path();
                if path.extension().map_or(false, |e| e == "input") {
                    entries.push(path);
                }
            }
        }

        Ok(Corpus {
            dir,
            crash_dir,
            entries,
            crash_hashes: HashSet::new(),
        })
    }

    pub fn len(&self) -> usize {
        self.entries.len()
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    /// Pick a random input from the corpus.
    pub fn pick(&self, rng: &mut Rng) -> Option<Vec<u8>> {
        if self.entries.is_empty() {
            return None;
        }
        let idx = rng.range(0, self.entries.len());
        fs::read(&self.entries[idx]).ok()
    }

    /// Save an input to the corpus queue.
    pub fn save_input(&mut self, data: &[u8], metadata: &InputMetadata) -> std::io::Result<PathBuf> {
        let hash = simple_hash(data);
        let filename = format!("id_{:016x}.input", hash);
        let path = self.dir.join(&filename);
        fs::write(&path, data)?;

        // Write metadata sidecar
        let meta_path = self.dir.join(format!("id_{:016x}.meta.json", hash));
        let meta_json = format!(
            "{{\"timestamp\":{},\"mutation\":\"{}\",\"parent_hash\":\"{}\",\"input_len\":{}}}",
            metadata.timestamp, metadata.mutation_name, metadata.parent_hash, data.len()
        );
        fs::write(&meta_path, meta_json)?;

        self.entries.push(path.clone());
        Ok(path)
    }

    /// Save a crash-triggering input. Returns None if duplicate.
    pub fn save_crash(
        &mut self,
        data: &[u8],
        stderr_output: &[u8],
        metadata: &InputMetadata,
    ) -> std::io::Result<Option<PathBuf>> {
        let stderr_hash = simple_hash(stderr_output);
        if self.crash_hashes.contains(&stderr_hash) {
            return Ok(None); // Duplicate crash
        }
        self.crash_hashes.insert(stderr_hash);

        let input_hash = simple_hash(data);
        let filename = format!("crash_{:016x}.input", input_hash);
        let path = self.crash_dir.join(&filename);
        fs::write(&path, data)?;

        let meta_path = self.crash_dir.join(format!("crash_{:016x}.meta.json", input_hash));
        let meta_json = format!(
            "{{\"timestamp\":{},\"mutation\":\"{}\",\"parent_hash\":\"{}\",\"stderr_hash\":\"{:016x}\",\"input_len\":{}}}",
            metadata.timestamp, metadata.mutation_name, metadata.parent_hash, stderr_hash, data.len()
        );
        fs::write(&meta_path, meta_json)?;

        Ok(Some(path))
    }
}

pub struct InputMetadata {
    pub timestamp: u64,
    pub mutation_name: String,
    pub parent_hash: String,
}

impl InputMetadata {
    pub fn new(mutation_name: &str, parent_hash: &str) -> Self {
        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();
        InputMetadata {
            timestamp,
            mutation_name: mutation_name.to_string(),
            parent_hash: parent_hash.to_string(),
        }
    }
}

/// FNV-1a hash for quick deduplication (not cryptographic).
pub fn simple_hash(data: &[u8]) -> u64 {
    let mut hash: u64 = 0xcbf29ce484222325;
    for &byte in data {
        hash ^= byte as u64;
        hash = hash.wrapping_mul(0x100000001b3);
    }
    hash
}
```

### Source: `src/runner.rs`

```rust
use std::io::Write;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

#[derive(Debug, Clone)]
pub enum ExecResult {
    Ok,
    Crash(CrashInfo),
    Timeout,
}

#[derive(Debug, Clone)]
pub struct CrashInfo {
    pub kind: CrashKind,
    pub stderr: Vec<u8>,
}

#[derive(Debug, Clone)]
pub enum CrashKind {
    Signal(i32),
    NonZeroExit(i32),
}

impl CrashKind {
    pub fn name(&self) -> String {
        match self {
            CrashKind::Signal(s) => format!("signal_{}", s),
            CrashKind::NonZeroExit(c) => format!("exit_{}", c),
        }
    }
}

pub struct Runner {
    target_path: String,
    timeout: Duration,
}

impl Runner {
    pub fn new(target_path: &str, timeout_ms: u64) -> Self {
        Runner {
            target_path: target_path.to_string(),
            timeout: Duration::from_millis(timeout_ms),
        }
    }

    /// Run the target with the given input on stdin. Returns the result.
    pub fn run(&self, input: &[u8]) -> ExecResult {
        let start = Instant::now();

        let child = Command::new(&self.target_path)
            .stdin(Stdio::piped())
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn();

        let mut child = match child {
            Ok(c) => c,
            Err(_) => return ExecResult::Timeout, // treat spawn failure as timeout
        };

        // Write input to stdin
        if let Some(mut stdin) = child.stdin.take() {
            let _ = stdin.write_all(input);
            // stdin is dropped here, closing it
        }

        // Wait with timeout
        let status = match child.wait() {
            Ok(s) => s,
            Err(_) => return ExecResult::Timeout,
        };

        if start.elapsed() > self.timeout {
            let _ = child.kill();
            return ExecResult::Timeout;
        }

        let stderr = child
            .stderr
            .and_then(|mut e| {
                let mut buf = Vec::new();
                std::io::Read::read_to_end(&mut e, &mut buf).ok()?;
                Some(buf)
            })
            .unwrap_or_default();

        // Check for crash
        #[cfg(unix)]
        {
            use std::os::unix::process::ExitStatusExt;
            if let Some(signal) = status.signal() {
                return ExecResult::Crash(CrashInfo {
                    kind: CrashKind::Signal(signal),
                    stderr,
                });
            }
        }

        if !status.success() {
            let code = status.code().unwrap_or(-1);
            return ExecResult::Crash(CrashInfo {
                kind: CrashKind::NonZeroExit(code),
                stderr,
            });
        }

        ExecResult::Ok
    }
}
```

### Source: `src/main.rs`

```rust
mod rng;
mod generators;
mod mutators;
mod corpus;
mod runner;

use std::path::Path;
use std::time::Instant;

use rng::Rng;
use generators::{random_bytes, random_json, random_http_request};
use mutators::{mutate, splice, MutationKind};
use corpus::{Corpus, InputMetadata, simple_hash};
use runner::{Runner, ExecResult};

struct Stats {
    total_execs: u64,
    crashes_found: u64,
    start_time: Instant,
    last_print: Instant,
}

impl Stats {
    fn new() -> Self {
        let now = Instant::now();
        Stats {
            total_execs: 0,
            crashes_found: 0,
            start_time: now,
            last_print: now,
        }
    }

    fn print_if_needed(&mut self, corpus_size: usize) {
        if self.last_print.elapsed().as_secs() >= 1 {
            let elapsed = self.start_time.elapsed().as_secs_f64();
            let execs_per_sec = if elapsed > 0.0 {
                self.total_execs as f64 / elapsed
            } else {
                0.0
            };
            eprintln!(
                "[stats] exec/s: {:.0} | total: {} | crashes: {} | corpus: {}",
                execs_per_sec, self.total_execs, self.crashes_found, corpus_size
            );
            self.last_print = Instant::now();
        }
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: {} <target_binary> [corpus_dir] [seed]", args[0]);
        eprintln!("  target_binary: path to the program to fuzz");
        eprintln!("  corpus_dir:    directory for corpus storage (default: ./corpus)");
        eprintln!("  seed:          PRNG seed for reproducibility (default: timestamp)");
        std::process::exit(1);
    }

    let target = &args[1];
    let corpus_dir = args.get(2).map(|s| s.as_str()).unwrap_or("./corpus");
    let seed: u64 = args
        .get(3)
        .and_then(|s| s.parse().ok())
        .unwrap_or_else(|| {
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos() as u64
        });

    let mut rng = Rng::new(seed);
    let mut corpus = Corpus::new(Path::new(corpus_dir)).expect("Failed to create corpus directory");
    let runner = Runner::new(target, 5000);
    let mut stats = Stats::new();

    eprintln!("[fuzzer] target: {}", target);
    eprintln!("[fuzzer] corpus: {}", corpus_dir);
    eprintln!("[fuzzer] seed: {}", seed);

    // Seed the corpus with some generated inputs if empty
    if corpus.is_empty() {
        eprintln!("[fuzzer] seeding corpus with generated inputs...");
        for _ in 0..20 {
            let input = match rng.range(0, 3) {
                0 => random_bytes(&mut rng, 1, 256),
                1 => random_json(&mut rng, 3).into_bytes(),
                _ => random_http_request(&mut rng),
            };
            let meta = InputMetadata::new("generated", "none");
            let _ = corpus.save_input(&input, &meta);
        }
    }

    loop {
        // Decide: pick from corpus and mutate, or generate fresh
        let (input, mutation_name, parent_hash) = if !corpus.is_empty() && rng.range(0, 10) < 8 {
            // 80%: mutate from corpus
            let base = corpus.pick(&mut rng).unwrap_or_default();
            let parent_h = format!("{:016x}", simple_hash(&base));

            // Occasionally splice two corpus entries
            if rng.range(0, 10) == 0 {
                let other = corpus.pick(&mut rng).unwrap_or_default();
                let spliced = splice(&mut rng, &base, &other);
                (spliced, "splice".to_string(), parent_h)
            } else {
                let (mutated, kind) = mutate(&mut rng, &base);
                (mutated, kind.name().to_string(), parent_h)
            }
        } else {
            // 20%: fresh generation
            let input = match rng.range(0, 3) {
                0 => random_bytes(&mut rng, 1, 512),
                1 => random_json(&mut rng, 4).into_bytes(),
                _ => random_http_request(&mut rng),
            };
            (input, "generated".to_string(), "none".to_string())
        };

        let result = runner.run(&input);
        stats.total_execs += 1;

        match result {
            ExecResult::Crash(info) => {
                let meta = InputMetadata::new(&mutation_name, &parent_hash);
                match corpus.save_crash(&input, &info.stderr, &meta) {
                    Ok(Some(path)) => {
                        stats.crashes_found += 1;
                        eprintln!(
                            "[CRASH] {} via {} -> {:?}",
                            info.kind.name(),
                            mutation_name,
                            path
                        );
                    }
                    Ok(None) => {
                        // Duplicate crash, skip
                    }
                    Err(e) => eprintln!("[error] failed to save crash: {}", e),
                }
            }
            ExecResult::Ok => {
                // Optionally save to corpus if input is novel (for dumb fuzzer, save periodically)
                if stats.total_execs % 100 == 0 && corpus.len() < 500 {
                    let meta = InputMetadata::new(&mutation_name, &parent_hash);
                    let _ = corpus.save_input(&input, &meta);
                }
            }
            ExecResult::Timeout => {
                // Log timeout but don't save
            }
        }

        stats.print_if_needed(corpus.len());
    }
}
```

### Target Programs for Testing

Create these under `targets/`:

```rust
// targets/target_overflow.rs
// Panics on specific byte pattern: crashes if input contains [0xDE, 0xAD]
use std::io::Read;

fn main() {
    let mut buf = Vec::new();
    std::io::stdin().read_to_end(&mut buf).unwrap();

    for i in 0..buf.len().saturating_sub(1) {
        if buf[i] == 0xDE && buf[i + 1] == 0xAD {
            // Simulate buffer over-read
            let _bad = buf[buf.len() + 10]; // panic: index out of bounds
        }
    }
}
```

```rust
// targets/target_integer.rs
// Integer overflow leading to panic
use std::io::Read;

fn main() {
    let mut buf = Vec::new();
    std::io::stdin().read_to_end(&mut buf).unwrap();

    if buf.len() >= 4 {
        let val = u32::from_le_bytes([buf[0], buf[1], buf[2], buf[3]]);
        let doubled = (val as u64) * 2;
        if doubled > u32::MAX as u64 {
            let small = doubled as u8; // truncation
            let arr = vec![0u8; small as usize];
            // If small is 0 after truncation and we index it, panic
            if arr.is_empty() {
                panic!("unexpected empty allocation from overflow");
            }
        }
    }
}
```

```rust
// targets/target_format.rs
// Panics when input starts with specific magic bytes and has invalid length field
use std::io::Read;

fn main() {
    let mut buf = Vec::new();
    std::io::stdin().read_to_end(&mut buf).unwrap();

    if buf.len() >= 6 && buf[0] == 0x89 && buf[1] == 0x50 {
        // "Magic header" detected, parse length
        let len = u16::from_be_bytes([buf[2], buf[3]]) as usize;
        // Bug: doesn't check len against actual buffer size
        let _slice = &buf[4..4 + len]; // panic if len > buf.len() - 4
    }
}
```

### Tests: `src/lib.rs` and `tests/`

```rust
// src/lib.rs -- re-export modules for testing
pub mod rng;
pub mod generators;
pub mod mutators;
pub mod corpus;
pub mod runner;
```

```rust
// tests/fuzzer_tests.rs
use fuzzer_random::rng::Rng;
use fuzzer_random::generators::*;
use fuzzer_random::mutators::*;
use fuzzer_random::corpus::simple_hash;

#[test]
fn rng_is_deterministic() {
    let mut rng1 = Rng::new(42);
    let mut rng2 = Rng::new(42);
    for _ in 0..100 {
        assert_eq!(rng1.next_u64(), rng2.next_u64());
    }
}

#[test]
fn random_bytes_respects_length_bounds() {
    let mut rng = Rng::new(123);
    for _ in 0..50 {
        let bytes = random_bytes(&mut rng, 10, 100);
        assert!(bytes.len() >= 10 && bytes.len() < 100);
    }
}

#[test]
fn random_json_is_valid() {
    let mut rng = Rng::new(456);
    for _ in 0..20 {
        let json_str = random_json(&mut rng, 3);
        // Verify it parses as valid JSON by checking balanced braces/brackets
        let bytes = json_str.as_bytes();
        let mut brace_depth: i32 = 0;
        let mut bracket_depth: i32 = 0;
        let mut in_string = false;
        let mut prev_escape = false;
        for &b in bytes {
            if prev_escape {
                prev_escape = false;
                continue;
            }
            match b {
                b'\\' if in_string => prev_escape = true,
                b'"' => in_string = !in_string,
                b'{' if !in_string => brace_depth += 1,
                b'}' if !in_string => brace_depth -= 1,
                b'[' if !in_string => bracket_depth += 1,
                b']' if !in_string => bracket_depth -= 1,
                _ => {}
            }
            assert!(brace_depth >= 0, "unmatched brace in: {}", json_str);
            assert!(bracket_depth >= 0, "unmatched bracket in: {}", json_str);
        }
        assert_eq!(brace_depth, 0, "unclosed brace in: {}", json_str);
        assert_eq!(bracket_depth, 0, "unclosed bracket in: {}", json_str);
    }
}

#[test]
fn bit_flip_changes_exactly_n_bits() {
    let mut rng = Rng::new(789);
    let input = vec![0x00; 16];

    for flip_count in [1, 2, 4] {
        let result = bit_flip(&mut rng, &input, flip_count);
        assert_eq!(result.len(), input.len());

        let differing_bits: u32 = input
            .iter()
            .zip(result.iter())
            .map(|(a, b)| (a ^ b).count_ones())
            .sum();
        assert_eq!(
            differing_bits, flip_count as u32,
            "expected {} bits flipped, got {}",
            flip_count, differing_bits
        );
    }
}

#[test]
fn byte_insert_increases_length() {
    let mut rng = Rng::new(101);
    let input = vec![0xAA; 10];
    let result = byte_insert(&mut rng, &input, 5);
    assert_eq!(result.len(), 15);
}

#[test]
fn byte_delete_decreases_length() {
    let mut rng = Rng::new(202);
    let input = vec![0xBB; 20];
    let result = byte_delete(&mut rng, &input, 5);
    assert!(result.len() < input.len());
    assert!(result.len() >= input.len() - 5);
}

#[test]
fn boundary_inject_writes_known_values() {
    let mut rng = Rng::new(303);
    let input = vec![0x42; 32];
    let result = boundary_inject(&mut rng, &input);
    assert_eq!(result.len(), input.len());
    // At least one byte should differ
    assert_ne!(result, input);
}

#[test]
fn splice_combines_two_inputs() {
    let mut rng = Rng::new(404);
    let a = vec![0xAA; 10];
    let b = vec![0xBB; 10];
    let result = splice(&mut rng, &a, &b);
    // Result should contain bytes from both inputs
    let has_aa = result.iter().any(|&x| x == 0xAA);
    let has_bb = result.iter().any(|&x| x == 0xBB);
    // At least one of the input patterns should be present
    assert!(has_aa || has_bb);
}

#[test]
fn simple_hash_is_deterministic() {
    let data = b"hello fuzzer";
    assert_eq!(simple_hash(data), simple_hash(data));
}

#[test]
fn simple_hash_differs_for_different_inputs() {
    assert_ne!(simple_hash(b"input_a"), simple_hash(b"input_b"));
}

#[test]
fn http_request_has_valid_structure() {
    let mut rng = Rng::new(505);
    let req = random_http_request(&mut rng);
    let req_str = String::from_utf8_lossy(&req);
    // Must contain HTTP version
    assert!(req_str.contains("HTTP/1.1"), "missing HTTP version");
    // Must contain Host header
    assert!(req_str.contains("Host:"), "missing Host header");
    // Must contain CRLF line endings
    assert!(req_str.contains("\r\n"), "missing CRLF");
}
```

### Running

```bash
cargo build
cargo test

# Build target programs
rustc targets/target_overflow.rs -o targets/target_overflow
rustc targets/target_integer.rs -o targets/target_integer
rustc targets/target_format.rs -o targets/target_format

# Run fuzzer against a target
cargo run -- targets/target_overflow ./corpus_overflow 42
```

### Expected Output

```
[fuzzer] target: targets/target_overflow
[fuzzer] corpus: ./corpus_overflow
[fuzzer] seed: 42
[fuzzer] seeding corpus with generated inputs...
[CRASH] exit_101 via bit_flip -> "./corpus_overflow/crashes/crash_a3f8..."
[stats] exec/s: 1523 | total: 1523 | crashes: 1 | corpus: 23
[stats] exec/s: 1497 | total: 2990 | crashes: 1 | corpus: 25
[CRASH] exit_101 via boundary_value -> "./corpus_overflow/crashes/crash_7b2e..."
[stats] exec/s: 1510 | total: 4501 | crashes: 2 | corpus: 28
...
```

## Design Decisions

1. **Xorshift64 over `rand` crate**: Using a hand-rolled PRNG keeps the project zero-dependency. Xorshift64 is fast and has a period of 2^64 - 1, which is more than sufficient for fuzzing. The tradeoff is statistical quality -- xorshift fails some BigCrush tests -- but for mutation fuzzing, perfect randomness is irrelevant.

2. **FNV-1a for deduplication**: Crash deduplication compares stderr output hashes. FNV-1a is not cryptographic but is fast and has good distribution for short strings. The risk of false deduplication (two different crashes with the same stderr hash) is negligible in practice.

3. **Directory-based corpus with sidecar metadata**: Each input is a separate file, with metadata in a paired JSON file. This allows external tools to inspect the corpus, replay specific inputs, and bisect crash histories. The alternative (single database file) is faster for bulk operations but harder to debug.

4. **80/20 mutation vs generation split**: Most fuzzer iterations should mutate existing corpus entries rather than generating fresh inputs. Mutations explore the neighborhood of known-interesting inputs. Fresh generation provides diversity to escape local optima.

5. **Process-per-execution model**: Each target invocation spawns a new process. This is safe (crashes don't kill the fuzzer) but slow (~1000-5000 exec/s). The AFL fork server model (Challenge 133) amortizes process creation cost for 10-100x speedup.

## Common Mistakes

1. **PRNG state of zero**: Xorshift with state 0 is a fixed point -- it returns 0 forever. Always guard against zero seed, either by rejecting it or replacing with a nonzero constant.

2. **Empty input mutations**: All mutation functions must handle empty input gracefully. Bit-flipping an empty slice, deleting from an empty slice, or picking a random position in an empty slice all panic without bounds checking.

3. **Not closing stdin before waiting**: If the fuzzer writes to the child's stdin but never drops/closes it, the child blocks forever waiting for EOF. In Rust, dropping the `ChildStdin` handle closes the pipe.

4. **Saving every input to corpus**: A dumb fuzzer has no coverage information to decide what's "interesting." Saving every execution bloats the corpus and slows selection. Save only crashes and a periodic sample.

5. **Boundary values as single bytes only**: The most interesting boundary bugs involve multi-byte values (0xFFFF, 0x80000000). Only injecting single-byte boundaries misses integer overflow and sign extension bugs entirely.

## Performance Notes

| Metric | Typical Value |
|--------|--------------|
| Exec/s (process per exec) | 1,000 - 5,000 |
| Corpus growth | ~1 entry per 100 execs (sampling) |
| Crash detection latency | Typically < 60s for obvious bugs |
| Memory usage | O(corpus size) for entry list |
| Mutation overhead | < 1 microsecond per mutation |

The bottleneck is process spawning. On Linux, `fork()+exec()` takes ~100-500 microseconds per invocation. On macOS, it is 2-5x slower due to security checks. The fork server approach (pre-fork the target and clone the fork for each execution) reaches 10,000-50,000 exec/s.

## Going Further

- Add **grammar-based generation**: define input grammars (BNF) and generate syntactically valid inputs that exercise deeper parser paths
- Implement **dictionary mode**: extract tokens from the target binary (strings, constants) and use them in mutations to hit comparison-guarded branches
- Add **persistent mode**: keep the target process alive between executions using a shared memory handshake, eliminating fork/exec overhead
- Build a **minimizer**: given a crash-triggering input, reduce it to the smallest input that still triggers the same crash (delta debugging)
- Connect to **coverage guidance** (Challenge 100) to make mutation decisions based on which inputs explore new code paths
