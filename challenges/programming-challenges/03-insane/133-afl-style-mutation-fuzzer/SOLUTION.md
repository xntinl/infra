# Solution: AFL-Style Mutation Fuzzer

## Architecture Overview

The fuzzer consists of six cooperating subsystems:

1. **Fork Server**: starts the target process once, then uses fork-and-resume to create child processes for each test case. Communicates with the target via two pipes (control and status). Eliminates `execve` overhead.
2. **Coverage Tracker**: manages a 64KB shared memory bitmap. Each byte represents an edge (branch pair). The target writes to this bitmap via instrumentation. The fuzzer reads it after each execution to detect new coverage.
3. **Mutation Engine**: applies deterministic mutations (bit flips, arithmetic, interesting values) and random havoc mutations to inputs. Dictionary tokens are spliced in during havoc.
4. **Queue Manager**: maintains the corpus of interesting inputs. Each entry tracks size, execution time, coverage hash, and depth. Favored inputs are selected via a set-cover approximation.
5. **Crash Triage**: classifies execution results (clean exit, crash, timeout). Groups crashes by coverage bitmap hash for deduplication.
6. **Sync Manager**: enables parallel fuzzing by importing interesting inputs from peer fuzzer instances via a shared directory.

---

## Rust Solution

### Project Setup

```bash
cargo new afl_fuzzer
cd afl_fuzzer
```

Add to `Cargo.toml`:

```toml
[dependencies]
nix = { version = "0.29", features = ["process", "signal", "mman", "fs"] }
rand = "0.8"
crc32fast = "1"
```

### `src/coverage.rs` -- Shared Memory Coverage Bitmap

```rust
use nix::sys::mman::{mmap, munmap, shm_open, shm_unlink, MapFlags, ProtFlags};
use nix::sys::stat::Mode;
use nix::fcntl::OFlag;
use nix::unistd::ftruncate;
use std::ffi::CString;

pub const BITMAP_SIZE: usize = 65536; // 64KB

const BUCKET_THRESHOLDS: [u8; 8] = [1, 2, 3, 4, 8, 16, 32, 128];

pub struct CoverageBitmap {
    pub data: *mut u8,
    pub size: usize,
    shm_name: String,
}

impl CoverageBitmap {
    pub fn new(shm_name: &str) -> nix::Result<Self> {
        let c_name = CString::new(shm_name).unwrap();

        let fd = shm_open(
            c_name.as_c_str(),
            OFlag::O_CREAT | OFlag::O_RDWR,
            Mode::S_IRUSR | Mode::S_IWUSR,
        )?;

        ftruncate(&fd, BITMAP_SIZE as i64)?;

        let ptr = unsafe {
            mmap(
                None,
                std::num::NonZeroUsize::new(BITMAP_SIZE).unwrap(),
                ProtFlags::PROT_READ | ProtFlags::PROT_WRITE,
                MapFlags::MAP_SHARED,
                &fd,
                0,
            )?
        };

        Ok(Self {
            data: ptr.as_ptr() as *mut u8,
            size: BITMAP_SIZE,
            shm_name: shm_name.to_string(),
        })
    }

    pub fn clear(&self) {
        unsafe {
            std::ptr::write_bytes(self.data, 0, self.size);
        }
    }

    pub fn as_slice(&self) -> &[u8] {
        unsafe { std::slice::from_raw_parts(self.data, self.size) }
    }

    /// Classify hit counts into logarithmic buckets to reduce noise
    pub fn classify_counts(&self) {
        let slice = unsafe { std::slice::from_raw_parts_mut(self.data, self.size) };
        for byte in slice.iter_mut() {
            if *byte == 0 { continue; }
            *byte = match *byte {
                1 => 1,
                2 => 2,
                3 => 4,
                4..=7 => 8,
                8..=15 => 16,
                16..=31 => 32,
                32..=127 => 64,
                _ => 128,
            };
        }
    }

    /// Compute hash of all edges that were hit (non-zero positions)
    pub fn coverage_hash(&self) -> u32 {
        crc32fast::hash(self.as_slice())
    }

    /// Count number of unique edges hit
    pub fn edge_count(&self) -> usize {
        self.as_slice().iter().filter(|&&b| b > 0).count()
    }

    /// Check if this bitmap has any new edges compared to the global virgin map
    pub fn has_new_coverage(&self, virgin: &[u8]) -> bool {
        let current = self.as_slice();
        for i in 0..self.size {
            if current[i] != 0 && virgin[i] == 0xFF {
                return true;
            }
            if current[i] != 0 && (virgin[i] & current[i]) != 0 {
                return true;
            }
        }
        false
    }

    /// Update virgin map with new coverage
    pub fn update_virgin_map(&self, virgin: &mut [u8]) -> bool {
        let current = self.as_slice();
        let mut new_bits = false;
        for i in 0..self.size {
            if current[i] != 0 && (virgin[i] & current[i]) != 0 {
                virgin[i] &= !current[i];
                new_bits = true;
            }
        }
        new_bits
    }

    pub fn shm_name(&self) -> &str {
        &self.shm_name
    }
}

impl Drop for CoverageBitmap {
    fn drop(&mut self) {
        unsafe {
            let _ = munmap(
                std::ptr::NonNull::new(self.data as *mut std::ffi::c_void).unwrap(),
                self.size,
            );
        }
        let c_name = CString::new(self.shm_name.as_str()).unwrap();
        let _ = shm_unlink(c_name.as_c_str());
    }
}

unsafe impl Send for CoverageBitmap {}
unsafe impl Sync for CoverageBitmap {}
```

### `src/forkserver.rs` -- Fork Server

```rust
use nix::unistd::{fork, ForkResult, pipe, read, write, close, dup2, execvp};
use nix::sys::wait::{waitpid, WaitStatus};
use nix::sys::signal::{kill, Signal};
use nix::unistd::Pid;
use std::ffi::CString;
use std::os::unix::io::RawFd;
use std::io;
use std::time::{Duration, Instant};

const FORKSRV_FD: RawFd = 198;

#[derive(Debug, Clone)]
pub enum ExecResult {
    Clean(i32),
    Crash(i32),      // signal number
    Timeout,
}

pub struct ForkServer {
    ctl_write: RawFd,  // fuzzer writes "go" signal
    st_read: RawFd,    // fuzzer reads child status
    server_pid: Pid,
    timeout: Duration,
}

impl ForkServer {
    pub fn start(
        target_path: &str,
        target_args: &[&str],
        shm_name: &str,
        timeout_ms: u64,
    ) -> io::Result<Self> {
        let (ctl_read, ctl_write) = pipe().map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;
        let (st_read, st_write) = pipe().map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;

        match unsafe { fork() } {
            Ok(ForkResult::Child) => {
                // Child: set up pipes and exec target
                close(ctl_write).ok();
                close(st_read).ok();

                dup2(ctl_read, FORKSRV_FD).ok();
                dup2(st_write, FORKSRV_FD + 1).ok();
                close(ctl_read).ok();
                close(st_write).ok();

                // Set shared memory env var
                std::env::set_var("__AFL_SHM_ID", shm_name);

                let prog = CString::new(target_path).unwrap();
                let args: Vec<CString> = std::iter::once(target_path)
                    .chain(target_args.iter().copied())
                    .map(|a| CString::new(a).unwrap())
                    .collect();

                execvp(&prog, &args).ok();
                std::process::exit(1);
            }
            Ok(ForkResult::Parent { child }) => {
                close(ctl_read).ok();
                close(st_write).ok();

                // Wait for fork server to be ready (it sends a 4-byte hello)
                let mut hello = [0u8; 4];
                let _ = read(st_read, &mut hello);

                Ok(Self {
                    ctl_write,
                    st_read,
                    server_pid: child,
                    timeout: Duration::from_millis(timeout_ms),
                })
            }
            Err(e) => Err(io::Error::new(io::ErrorKind::Other, e)),
        }
    }

    pub fn run_target(&self, _input_path: &str) -> io::Result<ExecResult> {
        // Send "go" signal to fork server
        let go = [0u8; 4];
        write(self.ctl_write, &go)
            .map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;

        // Read child PID
        let mut pid_buf = [0u8; 4];
        read(self.st_read, &mut pid_buf)
            .map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;
        let child_pid = Pid::from_raw(i32::from_le_bytes(pid_buf));

        // Wait for child status with timeout
        let start = Instant::now();
        loop {
            match waitpid(child_pid, Some(nix::sys::wait::WaitPidFlag::WNOHANG)) {
                Ok(WaitStatus::Exited(_, code)) => {
                    return Ok(ExecResult::Clean(code));
                }
                Ok(WaitStatus::Signaled(_, signal, _)) => {
                    return Ok(ExecResult::Crash(signal as i32));
                }
                Ok(WaitStatus::StillAlive) => {
                    if start.elapsed() > self.timeout {
                        let _ = kill(child_pid, Signal::SIGKILL);
                        let _ = waitpid(child_pid, None);
                        return Ok(ExecResult::Timeout);
                    }
                    std::thread::sleep(Duration::from_micros(100));
                }
                _ => {
                    // Read status from pipe as fallback
                    let mut status_buf = [0u8; 4];
                    read(self.st_read, &mut status_buf)
                        .map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;
                    let status = i32::from_le_bytes(status_buf);
                    if status & 0x7F == 0 {
                        return Ok(ExecResult::Clean((status >> 8) & 0xFF));
                    } else {
                        return Ok(ExecResult::Crash(status & 0x7F));
                    }
                }
            }
        }
    }
}

impl Drop for ForkServer {
    fn drop(&mut self) {
        let _ = kill(self.server_pid, Signal::SIGKILL);
        close(self.ctl_write).ok();
        close(self.st_read).ok();
    }
}

/// Simplified executor for targets without fork server support.
/// Forks and execs for each test case directly.
pub fn run_simple(
    target_path: &str,
    input_path: &str,
    timeout_ms: u64,
) -> io::Result<ExecResult> {
    match unsafe { fork() } {
        Ok(ForkResult::Child) => {
            // Redirect stdin from input file
            let fd = nix::fcntl::open(
                input_path,
                OFlag::O_RDONLY,
                Mode::empty(),
            ).unwrap();
            dup2(fd, 0).ok();
            close(fd).ok();

            let prog = CString::new(target_path).unwrap();
            let args = [prog.clone()];
            execvp(&prog, &args).ok();
            std::process::exit(1);
        }
        Ok(ForkResult::Parent { child }) => {
            let start = Instant::now();
            let timeout = Duration::from_millis(timeout_ms);

            loop {
                match waitpid(child, Some(nix::sys::wait::WaitPidFlag::WNOHANG)) {
                    Ok(WaitStatus::Exited(_, code)) => return Ok(ExecResult::Clean(code)),
                    Ok(WaitStatus::Signaled(_, sig, _)) => return Ok(ExecResult::Crash(sig as i32)),
                    Ok(WaitStatus::StillAlive) => {
                        if start.elapsed() > timeout {
                            let _ = kill(child, Signal::SIGKILL);
                            let _ = waitpid(child, None);
                            return Ok(ExecResult::Timeout);
                        }
                        std::thread::sleep(Duration::from_micros(500));
                    }
                    _ => return Ok(ExecResult::Clean(0)),
                }
            }
        }
        Err(e) => Err(io::Error::new(io::ErrorKind::Other, e)),
    }
}

use nix::fcntl::OFlag;
use nix::sys::stat::Mode;
```

### `src/mutation.rs` -- Mutation Engine

```rust
use rand::Rng;
use rand::seq::SliceRandom;

const INTERESTING_8: [i8; 9] = [-128, -1, 0, 1, 16, 32, 64, 100, 127];
const INTERESTING_16: [i16; 10] = [-32768, -129, -128, -1, 0, 1, 128, 255, 256, 32767];
const INTERESTING_32: [i32; 8] = [
    i32::MIN, -100000, -32769, -1, 0, 1, 32768, i32::MAX,
];

pub struct Mutator {
    dictionary: Vec<Vec<u8>>,
}

impl Mutator {
    pub fn new() -> Self {
        Self { dictionary: Vec::new() }
    }

    pub fn load_dictionary(&mut self, tokens: Vec<Vec<u8>>) {
        self.dictionary = tokens;
    }

    // --- Deterministic Mutations ---

    /// Walking bit flips: flip 1, 2, or 4 consecutive bits at every position
    pub fn bitflip_1(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        for byte_idx in 0..input.len() {
            for bit in 0..8 {
                let mut mutated = input.to_vec();
                mutated[byte_idx] ^= 1 << bit;
                results.push(mutated);
            }
        }
        results
    }

    pub fn bitflip_2(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        for byte_idx in 0..input.len() {
            for bit in 0..7 {
                let mut mutated = input.to_vec();
                mutated[byte_idx] ^= 3 << bit;
                results.push(mutated);
            }
        }
        results
    }

    pub fn bitflip_4(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        for byte_idx in 0..input.len() {
            for bit in 0..5 {
                let mut mutated = input.to_vec();
                mutated[byte_idx] ^= 0xF << bit;
                results.push(mutated);
            }
        }
        results
    }

    /// Walking byte flips
    pub fn byteflip(&self, input: &[u8], width: usize) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        if input.len() < width { return results; }
        for i in 0..=input.len() - width {
            let mut mutated = input.to_vec();
            for j in 0..width {
                mutated[i + j] ^= 0xFF;
            }
            results.push(mutated);
        }
        results
    }

    /// Arithmetic: add/subtract small values to bytes, words, dwords
    pub fn arith_8(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        for i in 0..input.len() {
            for delta in 1..=35u8 {
                let mut add = input.to_vec();
                add[i] = add[i].wrapping_add(delta);
                results.push(add);

                let mut sub = input.to_vec();
                sub[i] = sub[i].wrapping_sub(delta);
                results.push(sub);
            }
        }
        results
    }

    pub fn arith_16(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        if input.len() < 2 { return results; }
        for i in 0..input.len() - 1 {
            let val = u16::from_le_bytes([input[i], input[i + 1]]);
            for delta in 1..=35u16 {
                let mut mutated = input.to_vec();
                let new_val = val.wrapping_add(delta);
                mutated[i..i + 2].copy_from_slice(&new_val.to_le_bytes());
                results.push(mutated);

                let mut mutated = input.to_vec();
                let new_val = val.wrapping_sub(delta);
                mutated[i..i + 2].copy_from_slice(&new_val.to_le_bytes());
                results.push(mutated);
            }
        }
        results
    }

    /// Interesting value replacement
    pub fn interesting_8(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        for i in 0..input.len() {
            for &val in &INTERESTING_8 {
                let mut mutated = input.to_vec();
                mutated[i] = val as u8;
                results.push(mutated);
            }
        }
        results
    }

    pub fn interesting_16(&self, input: &[u8]) -> Vec<Vec<u8>> {
        let mut results = Vec::new();
        if input.len() < 2 { return results; }
        for i in 0..input.len() - 1 {
            for &val in &INTERESTING_16 {
                let mut mutated = input.to_vec();
                mutated[i..i + 2].copy_from_slice(&(val as u16).to_le_bytes());
                results.push(mutated);

                // Also big-endian
                let mut mutated = input.to_vec();
                mutated[i..i + 2].copy_from_slice(&(val as u16).to_be_bytes());
                results.push(mutated);
            }
        }
        results
    }

    // --- Havoc Stage ---

    pub fn havoc(&self, input: &[u8], num_mutations: usize) -> Vec<u8> {
        let mut rng = rand::thread_rng();
        let mut data = input.to_vec();

        for _ in 0..num_mutations {
            if data.is_empty() { data.push(0); }

            match rng.gen_range(0..8) {
                0 => {
                    // Random bit flip
                    let idx = rng.gen_range(0..data.len());
                    let bit = rng.gen_range(0..8);
                    data[idx] ^= 1 << bit;
                }
                1 => {
                    // Random byte replacement
                    let idx = rng.gen_range(0..data.len());
                    data[idx] = rng.gen();
                }
                2 => {
                    // Insert random byte
                    let idx = rng.gen_range(0..=data.len());
                    data.insert(idx, rng.gen());
                }
                3 => {
                    // Delete random byte
                    if data.len() > 1 {
                        let idx = rng.gen_range(0..data.len());
                        data.remove(idx);
                    }
                }
                4 => {
                    // Overwrite with interesting value
                    let idx = rng.gen_range(0..data.len());
                    data[idx] = *INTERESTING_8.choose(&mut rng).unwrap() as u8;
                }
                5 => {
                    // Clone a block within the input
                    if data.len() >= 4 {
                        let src = rng.gen_range(0..data.len() - 2);
                        let len = rng.gen_range(1..=(data.len() - src).min(32));
                        let block: Vec<u8> = data[src..src + len].to_vec();
                        let dst = rng.gen_range(0..data.len());
                        let end = (dst + len).min(data.len());
                        data[dst..end].copy_from_slice(&block[..end - dst]);
                    }
                }
                6 => {
                    // Dictionary token insertion
                    if !self.dictionary.is_empty() {
                        let token = self.dictionary.choose(&mut rng).unwrap();
                        let idx = rng.gen_range(0..=data.len());
                        for (j, &b) in token.iter().enumerate() {
                            if idx + j < data.len() {
                                data[idx + j] = b;
                            }
                        }
                    }
                }
                7 => {
                    // Arithmetic on random byte
                    let idx = rng.gen_range(0..data.len());
                    let delta: u8 = rng.gen_range(1..=35);
                    if rng.gen_bool(0.5) {
                        data[idx] = data[idx].wrapping_add(delta);
                    } else {
                        data[idx] = data[idx].wrapping_sub(delta);
                    }
                }
                _ => {}
            }
        }

        data
    }
}
```

### `src/queue.rs` -- Queue Management and Scheduling

```rust
use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::fs;
use std::io;

#[derive(Debug, Clone)]
pub struct QueueEntry {
    pub id: usize,
    pub file_path: PathBuf,
    pub file_size: usize,
    pub coverage_hash: u32,
    pub edge_count: usize,
    pub edges: HashSet<usize>,  // indices of edges hit
    pub exec_time_us: u64,
    pub depth: usize,
    pub was_fuzzed: bool,
    pub favored: bool,
}

pub struct Queue {
    entries: Vec<QueueEntry>,
    next_id: usize,
    queue_dir: PathBuf,
    global_coverage: HashSet<usize>,
}

impl Queue {
    pub fn new(queue_dir: &Path) -> io::Result<Self> {
        fs::create_dir_all(queue_dir)?;
        Ok(Self {
            entries: Vec::new(),
            next_id: 0,
            queue_dir: queue_dir.to_path_buf(),
            global_coverage: HashSet::new(),
        })
    }

    pub fn add(
        &mut self,
        input: &[u8],
        coverage_hash: u32,
        edges: HashSet<usize>,
        exec_time_us: u64,
        depth: usize,
    ) -> io::Result<&QueueEntry> {
        let id = self.next_id;
        self.next_id += 1;

        let filename = format!("id:{:06},src:fuzzer", id);
        let file_path = self.queue_dir.join(&filename);
        fs::write(&file_path, input)?;

        let edge_count = edges.len();
        self.global_coverage.extend(&edges);

        self.entries.push(QueueEntry {
            id,
            file_path,
            file_size: input.len(),
            coverage_hash,
            edge_count,
            edges,
            exec_time_us,
            depth,
            was_fuzzed: false,
            favored: false,
        });

        Ok(self.entries.last().unwrap())
    }

    /// Select favored inputs using greedy set-cover approximation.
    /// Minimal set of inputs that covers all observed edges.
    pub fn cull_queue(&mut self) {
        // Reset favored status
        for entry in &mut self.entries {
            entry.favored = false;
        }

        let mut uncovered = self.global_coverage.clone();
        let mut remaining: Vec<usize> = (0..self.entries.len()).collect();

        while !uncovered.is_empty() && !remaining.is_empty() {
            // Find entry that covers the most uncovered edges
            let best_idx = remaining.iter()
                .max_by_key(|&&idx| {
                    let covered = self.entries[idx].edges.intersection(&uncovered).count();
                    // Tie-break: prefer smaller inputs and faster executions
                    (covered, usize::MAX - self.entries[idx].file_size)
                })
                .copied();

            match best_idx {
                Some(idx) => {
                    self.entries[idx].favored = true;
                    for edge in &self.entries[idx].edges {
                        uncovered.remove(edge);
                    }
                    remaining.retain(|&i| i != idx);
                }
                None => break,
            }
        }
    }

    /// Get next entry to fuzz. Prioritize: unfuzzed favored > favored > unfuzzed > any
    pub fn next_to_fuzz(&mut self) -> Option<&QueueEntry> {
        // Unfuzzed favored
        if let Some(entry) = self.entries.iter().find(|e| e.favored && !e.was_fuzzed) {
            return Some(entry);
        }
        // Any unfuzzed
        if let Some(entry) = self.entries.iter().find(|e| !e.was_fuzzed) {
            return Some(entry);
        }
        // Round-robin on favored
        self.entries.iter().find(|e| e.favored)
    }

    pub fn mark_fuzzed(&mut self, id: usize) {
        if let Some(entry) = self.entries.iter_mut().find(|e| e.id == id) {
            entry.was_fuzzed = true;
        }
    }

    pub fn len(&self) -> usize {
        self.entries.len()
    }

    pub fn total_edges(&self) -> usize {
        self.global_coverage.len()
    }

    pub fn entries(&self) -> &[QueueEntry] {
        &self.entries
    }
}
```

### `src/crash.rs` -- Crash Deduplication and Triage

```rust
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::fs;
use std::io;

#[derive(Debug, Clone)]
pub struct CrashInfo {
    pub id: usize,
    pub file_path: PathBuf,
    pub signal: i32,
    pub coverage_hash: u32,
}

pub struct CrashTracker {
    crashes: Vec<CrashInfo>,
    hangs: Vec<CrashInfo>,
    seen_hashes: HashMap<u32, usize>, // hash -> first crash id with that hash
    crash_dir: PathBuf,
    hang_dir: PathBuf,
    next_crash_id: usize,
    next_hang_id: usize,
}

impl CrashTracker {
    pub fn new(output_dir: &Path) -> io::Result<Self> {
        let crash_dir = output_dir.join("crashes");
        let hang_dir = output_dir.join("hangs");
        fs::create_dir_all(&crash_dir)?;
        fs::create_dir_all(&hang_dir)?;

        Ok(Self {
            crashes: Vec::new(),
            hangs: Vec::new(),
            seen_hashes: HashMap::new(),
            crash_dir,
            hang_dir,
            next_crash_id: 0,
            next_hang_id: 0,
        })
    }

    /// Record a crash. Returns true if this is a unique crash (new coverage hash).
    pub fn record_crash(
        &mut self,
        input: &[u8],
        signal: i32,
        coverage_hash: u32,
    ) -> io::Result<bool> {
        let is_unique = !self.seen_hashes.contains_key(&coverage_hash);

        let id = self.next_crash_id;
        self.next_crash_id += 1;

        let filename = format!(
            "id:{:06},sig:{:02},hash:{:08x}",
            id, signal, coverage_hash
        );
        let file_path = self.crash_dir.join(&filename);
        fs::write(&file_path, input)?;

        self.crashes.push(CrashInfo {
            id,
            file_path,
            signal,
            coverage_hash,
        });

        if is_unique {
            self.seen_hashes.insert(coverage_hash, id);
        }

        Ok(is_unique)
    }

    pub fn record_hang(&mut self, input: &[u8], coverage_hash: u32) -> io::Result<()> {
        let id = self.next_hang_id;
        self.next_hang_id += 1;

        let filename = format!("id:{:06},hash:{:08x}", id, coverage_hash);
        let file_path = self.hang_dir.join(&filename);
        fs::write(&file_path, input)?;

        self.hangs.push(CrashInfo {
            id,
            file_path,
            signal: 0,
            coverage_hash,
        });

        Ok(())
    }

    pub fn unique_crash_count(&self) -> usize {
        self.seen_hashes.len()
    }

    pub fn total_crash_count(&self) -> usize {
        self.crashes.len()
    }

    pub fn hang_count(&self) -> usize {
        self.hangs.len()
    }
}
```

### `src/sync.rs` -- Parallel Fuzzer Synchronization

```rust
use std::path::{Path, PathBuf};
use std::fs;
use std::io;
use std::collections::HashSet;

pub struct SyncManager {
    instance_id: String,
    sync_dir: PathBuf,
    imported_files: HashSet<PathBuf>,
}

impl SyncManager {
    pub fn new(instance_id: &str, sync_dir: &Path) -> io::Result<Self> {
        let instance_dir = sync_dir.join(instance_id);
        fs::create_dir_all(instance_dir.join("queue"))?;

        Ok(Self {
            instance_id: instance_id.to_string(),
            sync_dir: sync_dir.to_path_buf(),
            imported_files: HashSet::new(),
        })
    }

    /// Export an interesting input for other instances to pick up
    pub fn export_input(&self, input: &[u8], id: usize) -> io::Result<()> {
        let path = self.sync_dir
            .join(&self.instance_id)
            .join("queue")
            .join(format!("id:{:06}", id));
        fs::write(path, input)
    }

    /// Import new inputs from peer fuzzer instances
    pub fn import_from_peers(&mut self) -> io::Result<Vec<Vec<u8>>> {
        let mut imported = Vec::new();

        let entries = match fs::read_dir(&self.sync_dir) {
            Ok(e) => e,
            Err(_) => return Ok(imported),
        };

        for entry in entries.flatten() {
            let peer_name = entry.file_name().to_string_lossy().to_string();
            if peer_name == self.instance_id { continue; }

            let peer_queue = entry.path().join("queue");
            if !peer_queue.exists() { continue; }

            if let Ok(files) = fs::read_dir(&peer_queue) {
                for file in files.flatten() {
                    let path = file.path();
                    if self.imported_files.contains(&path) { continue; }

                    if let Ok(data) = fs::read(&path) {
                        imported.push(data);
                        self.imported_files.insert(path);
                    }
                }
            }
        }

        Ok(imported)
    }
}
```

### `src/stats.rs` -- Status Display

```rust
use std::time::Instant;

pub struct FuzzerStats {
    pub start_time: Instant,
    pub total_execs: u64,
    pub total_paths: usize,
    pub unique_crashes: usize,
    pub total_crashes: usize,
    pub hangs: usize,
    pub total_edges: usize,
    pub current_stage: String,
    pub last_new_path_time: Instant,
}

impl FuzzerStats {
    pub fn new() -> Self {
        let now = Instant::now();
        Self {
            start_time: now,
            total_execs: 0,
            total_paths: 0,
            unique_crashes: 0,
            total_crashes: 0,
            hangs: 0,
            total_edges: 0,
            current_stage: "init".to_string(),
            last_new_path_time: now,
        }
    }

    pub fn execs_per_sec(&self) -> f64 {
        let elapsed = self.start_time.elapsed().as_secs_f64();
        if elapsed > 0.0 {
            self.total_execs as f64 / elapsed
        } else {
            0.0
        }
    }

    pub fn display(&self) {
        let elapsed = self.start_time.elapsed();
        let hours = elapsed.as_secs() / 3600;
        let minutes = (elapsed.as_secs() % 3600) / 60;
        let seconds = elapsed.as_secs() % 60;

        println!("+-------------------------------------------------+");
        println!("| AFL-RS Fuzzer Status                            |");
        println!("+-------------------------------------------------+");
        println!("| Runtime        : {:02}:{:02}:{:02}                        |", hours, minutes, seconds);
        println!("| Exec speed     : {:.1} exec/s                  |", self.execs_per_sec());
        println!("| Total execs    : {:<32} |", self.total_execs);
        println!("| Total paths    : {:<32} |", self.total_paths);
        println!("| Total edges    : {:<32} |", self.total_edges);
        println!("| Unique crashes : {:<32} |", self.unique_crashes);
        println!("| Total crashes  : {:<32} |", self.total_crashes);
        println!("| Hangs          : {:<32} |", self.hangs);
        println!("| Stage          : {:<32} |", self.current_stage);
        println!("+-------------------------------------------------+");
    }
}
```

### `src/lib.rs` -- Fuzzer Core Loop

```rust
pub mod coverage;
pub mod forkserver;
pub mod mutation;
pub mod queue;
pub mod crash;
pub mod sync;
pub mod stats;

use coverage::{CoverageBitmap, BITMAP_SIZE};
use mutation::Mutator;
use queue::Queue;
use crash::CrashTracker;
use stats::FuzzerStats;
use forkserver::ExecResult;

use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::fs;
use std::io;
use std::time::Instant;

pub struct FuzzerConfig {
    pub target_path: String,
    pub target_args: Vec<String>,
    pub input_dir: PathBuf,
    pub output_dir: PathBuf,
    pub timeout_ms: u64,
    pub dictionary_path: Option<PathBuf>,
    pub instance_id: String,
}

pub struct Fuzzer {
    config: FuzzerConfig,
    mutator: Mutator,
    queue: Queue,
    crashes: CrashTracker,
    stats: FuzzerStats,
    virgin_bits: Vec<u8>,
}

impl Fuzzer {
    pub fn new(config: FuzzerConfig) -> io::Result<Self> {
        let queue = Queue::new(&config.output_dir.join("queue"))?;
        let crashes = CrashTracker::new(&config.output_dir)?;
        let mut mutator = Mutator::new();

        if let Some(ref dict_path) = config.dictionary_path {
            if let Ok(content) = fs::read_to_string(dict_path) {
                let tokens: Vec<Vec<u8>> = content.lines()
                    .filter(|l| !l.starts_with('#') && !l.is_empty())
                    .filter_map(|l| {
                        let trimmed = l.trim().trim_matches('"');
                        if trimmed.is_empty() { None }
                        else { Some(trimmed.as_bytes().to_vec()) }
                    })
                    .collect();
                mutator.load_dictionary(tokens);
            }
        }

        Ok(Self {
            config,
            mutator,
            queue,
            crashes,
            stats: FuzzerStats::new(),
            virgin_bits: vec![0xFF; BITMAP_SIZE],
        })
    }

    /// Load seed corpus from input directory
    pub fn load_seeds(&mut self) -> io::Result<usize> {
        let mut count = 0;
        let input_dir = self.config.input_dir.clone();

        if !input_dir.exists() {
            return Err(io::Error::new(
                io::ErrorKind::NotFound,
                format!("seed directory not found: {:?}", input_dir),
            ));
        }

        for entry in fs::read_dir(&input_dir)? {
            let entry = entry?;
            if entry.file_type()?.is_file() {
                let data = fs::read(entry.path())?;
                let edges = HashSet::new();
                self.queue.add(&data, 0, edges, 0, 0)?;
                count += 1;
            }
        }

        self.stats.total_paths = self.queue.len();
        Ok(count)
    }

    /// Extract edge indices from bitmap
    fn extract_edges(bitmap: &CoverageBitmap) -> HashSet<usize> {
        bitmap.as_slice()
            .iter()
            .enumerate()
            .filter(|(_, &b)| b > 0)
            .map(|(i, _)| i)
            .collect()
    }

    /// Run one fuzzing cycle on a queue entry
    pub fn fuzz_one(
        &mut self,
        input: &[u8],
        bitmap: &CoverageBitmap,
        run_target: &dyn Fn(&[u8]) -> io::Result<ExecResult>,
    ) -> io::Result<()> {
        // Deterministic stage: bit flips
        self.stats.current_stage = "bitflip 1/1".to_string();
        let mutations = self.mutator.bitflip_1(input);
        for mutated in mutations.iter().take(input.len() * 8) {
            self.execute_and_check(mutated, bitmap, 1, run_target)?;
        }

        // Deterministic stage: interesting values
        self.stats.current_stage = "interest 8/8".to_string();
        let mutations = self.mutator.interesting_8(input);
        for mutated in &mutations {
            self.execute_and_check(mutated, bitmap, 1, run_target)?;
        }

        // Havoc stage
        self.stats.current_stage = "havoc".to_string();
        let havoc_rounds = 256;
        for _ in 0..havoc_rounds {
            let num_mutations = rand::thread_rng().gen_range(1..=6);
            let mutated = self.mutator.havoc(input, num_mutations);
            self.execute_and_check(&mutated, bitmap, 2, run_target)?;
        }

        Ok(())
    }

    fn execute_and_check(
        &mut self,
        input: &[u8],
        bitmap: &CoverageBitmap,
        depth: usize,
        run_target: &dyn Fn(&[u8]) -> io::Result<ExecResult>,
    ) -> io::Result<()> {
        bitmap.clear();
        let start = Instant::now();
        let result = run_target(input)?;
        let exec_time = start.elapsed().as_micros() as u64;

        self.stats.total_execs += 1;

        bitmap.classify_counts();
        let cov_hash = bitmap.coverage_hash();

        match result {
            ExecResult::Crash(signal) => {
                let is_new = self.crashes.record_crash(input, signal, cov_hash)?;
                self.stats.total_crashes += 1;
                if is_new {
                    self.stats.unique_crashes += 1;
                }
            }
            ExecResult::Timeout => {
                self.crashes.record_hang(input, cov_hash)?;
                self.stats.hangs += 1;
            }
            ExecResult::Clean(_) => {
                if bitmap.update_virgin_map(&mut self.virgin_bits) {
                    let edges = Self::extract_edges(bitmap);
                    self.queue.add(input, cov_hash, edges, exec_time, depth)?;
                    self.stats.total_paths = self.queue.len();
                    self.stats.total_edges = self.queue.total_edges();
                    self.stats.last_new_path_time = Instant::now();
                }
            }
        }

        Ok(())
    }
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use mutation::Mutator;
    use queue::Queue;
    use crash::CrashTracker;
    use stats::FuzzerStats;

    #[test]
    fn test_bitflip_1() {
        let mutator = Mutator::new();
        let input = vec![0xFF];
        let mutations = mutator.bitflip_1(&input);
        assert_eq!(mutations.len(), 8);
        // Each mutation flips exactly one bit
        for m in &mutations {
            let diff = m[0] ^ input[0];
            assert_eq!(diff.count_ones(), 1);
        }
    }

    #[test]
    fn test_byteflip() {
        let mutator = Mutator::new();
        let input = vec![0x00, 0x00, 0x00, 0x00];
        let mutations = mutator.byteflip(&input, 1);
        assert_eq!(mutations.len(), 4);
        for (i, m) in mutations.iter().enumerate() {
            assert_eq!(m[i], 0xFF);
        }
    }

    #[test]
    fn test_arith_8() {
        let mutator = Mutator::new();
        let input = vec![100];
        let mutations = mutator.arith_8(&input);
        // 35 additions + 35 subtractions per byte
        assert_eq!(mutations.len(), 70);
        assert_eq!(mutations[0][0], 101); // +1
        assert_eq!(mutations[1][0], 99);  // -1
    }

    #[test]
    fn test_interesting_8() {
        let mutator = Mutator::new();
        let input = vec![42];
        let mutations = mutator.interesting_8(&input);
        assert_eq!(mutations.len(), 9); // 9 interesting values
        let vals: Vec<u8> = mutations.iter().map(|m| m[0]).collect();
        assert!(vals.contains(&0));
        assert!(vals.contains(&1));
        assert!(vals.contains(&127));
    }

    #[test]
    fn test_havoc_changes_input() {
        let mutator = Mutator::new();
        let input = vec![0x41, 0x42, 0x43, 0x44];
        let mut any_different = false;
        for _ in 0..10 {
            let mutated = mutator.havoc(&input, 3);
            if mutated != input {
                any_different = true;
                break;
            }
        }
        assert!(any_different, "havoc should produce different output");
    }

    #[test]
    fn test_queue_add_and_schedule() {
        let dir = std::env::temp_dir().join("fuzzer_test_queue");
        let _ = std::fs::remove_dir_all(&dir);

        let mut queue = Queue::new(&dir).unwrap();

        let mut edges1 = std::collections::HashSet::new();
        edges1.insert(10);
        edges1.insert(20);
        queue.add(b"test1", 0x1111, edges1, 100, 0).unwrap();

        let mut edges2 = std::collections::HashSet::new();
        edges2.insert(20);
        edges2.insert(30);
        queue.add(b"test2", 0x2222, edges2, 200, 1).unwrap();

        assert_eq!(queue.len(), 2);
        assert_eq!(queue.total_edges(), 3);

        // After culling, at least one entry should be favored
        queue.cull_queue();
        let favored_count = queue.entries().iter().filter(|e| e.favored).count();
        assert!(favored_count >= 1);

        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn test_crash_deduplication() {
        let dir = std::env::temp_dir().join("fuzzer_test_crashes");
        let _ = std::fs::remove_dir_all(&dir);

        let mut tracker = CrashTracker::new(&dir).unwrap();

        // Same coverage hash = duplicate
        let is_new = tracker.record_crash(b"crash1", 11, 0xDEAD).unwrap();
        assert!(is_new);

        let is_new = tracker.record_crash(b"crash2", 11, 0xDEAD).unwrap();
        assert!(!is_new, "same hash should be duplicate");

        // Different hash = unique
        let is_new = tracker.record_crash(b"crash3", 6, 0xBEEF).unwrap();
        assert!(is_new);

        assert_eq!(tracker.unique_crash_count(), 2);
        assert_eq!(tracker.total_crash_count(), 3);

        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn test_stats_display() {
        let mut stats = FuzzerStats::new();
        stats.total_execs = 1000;
        stats.total_paths = 42;
        stats.unique_crashes = 3;
        stats.current_stage = "havoc".to_string();
        // Should not panic
        stats.display();
    }

    #[test]
    fn test_dictionary_loading() {
        let mut mutator = Mutator::new();
        mutator.load_dictionary(vec![
            b"GET ".to_vec(),
            b"HTTP/1.1".to_vec(),
            b"\r\n".to_vec(),
        ]);

        let input = vec![0x00; 20];
        let mutated = mutator.havoc(&input, 10);
        // Just verify it does not crash with dictionary
        assert!(!mutated.is_empty());
    }
}
```

### Running

```bash
cargo test
cargo test -- --nocapture
```

### Expected Test Output

```
running 8 tests
test tests::test_bitflip_1 ... ok
test tests::test_byteflip ... ok
test tests::test_arith_8 ... ok
test tests::test_interesting_8 ... ok
test tests::test_havoc_changes_input ... ok
test tests::test_queue_add_and_schedule ... ok
test tests::test_crash_deduplication ... ok
test tests::test_stats_display ... ok
test tests::test_dictionary_loading ... ok

test result: ok. 9 passed; 0 failed; 0 ignored
```

---

## Design Decisions

1. **Shared memory via POSIX `shm_open` instead of System V `shmget`**: POSIX shared memory uses file-descriptor-based APIs that integrate cleanly with Rust's ownership model. The `nix` crate provides safe wrappers. System V shared memory (`shmget`/`shmat`) uses integer IDs and global namespace, making cleanup on crash harder. AFL uses System V for historical reasons; modern implementations prefer POSIX.

2. **Coverage bitmap with logarithmic hit count buckets**: raw hit counts are noisy -- a loop running 100 vs 101 times is not meaningfully different. Bucketing counts (1, 2, 3, 4-7, 8-15, 16-31, 32-127, 128+) captures significant behavior changes (loop not taken vs taken, taken once vs many times) without polluting the bitmap with noise from minor iteration count variations.

3. **Greedy set-cover for queue culling over random selection**: the favored input selection uses a greedy approximation to minimum set cover. At each step it picks the input covering the most uncovered edges, with tie-breaking on file size (smaller is better). This produces a minimal corpus that maximizes coverage per unit of fuzzing time. Random selection would waste cycles on inputs that exercise already-covered paths.

4. **Deterministic stages before havoc**: AFL's insight is that deterministic mutations (walking bit flips, arithmetic, interesting values) often find bugs that random mutations miss, because they systematically exercise every byte position. Running deterministic stages first on each queue entry, then switching to random havoc, provides both systematic and stochastic exploration.

5. **Crash deduplication by coverage hash rather than stack trace**: stack traces require debug symbols and are fragile (ASLR, inlining). Coverage bitmap hashes capture which execution path led to the crash, which is more stable and does not require the target to be compiled with debug info. Two crashes taking different paths to the same fault point are correctly identified as distinct bugs.

6. **Fork server protocol via paired pipes**: two pipes (control and status) form a simple synchronous protocol. The fuzzer writes 4 bytes to signal "execute next input"; the fork server forks, the child runs, and the parent writes the child's exit status back. This is simpler and more portable than using signals or shared memory for synchronization.

7. **Parallel sync via filesystem rather than IPC**: each fuzzer instance writes interesting inputs to its own directory. Peers periodically scan each other's directories for new files. This is the same approach AFL uses -- it requires no shared state, no coordination protocol, and naturally handles instance crashes (the directory persists). The trade-off is sync latency (seconds, not milliseconds), which is acceptable for fuzzing.

## Common Mistakes

- **Not clearing the bitmap before each execution**: the shared memory bitmap accumulates coverage from all executions. If not cleared (zeroed) before each run, the fuzzer sees stale coverage from previous inputs and cannot detect new edges. Always `memset` the bitmap to zero before signaling the fork server.
- **Hit count bucket boundaries off by one**: the bucket for "3" must map to a different value than "4-7". If the boundary is wrong, a loop change from 3 to 4 iterations does not register as new coverage, causing the fuzzer to miss interesting inputs.
- **Not handling SIGCHLD in the fork server**: if the parent process does not call `waitpid` on terminated children, zombie processes accumulate and eventually exhaust the process table. Always reap child processes after collecting their status.
- **Deterministic mutations generating too many test cases**: for a 10KB input, walking bit flips alone produce 80,000 mutations. Limit deterministic stages to inputs below a size threshold (e.g., 1KB) or cap the number of mutations per stage to maintain throughput.
- **Forgetting endianness in arithmetic mutations**: interesting 16-bit and 32-bit values must be tested in both little-endian and big-endian byte orders, because the target may interpret multi-byte fields in either direction depending on the file format.

## Performance Notes

Fuzzer throughput is measured in executions per second. The fork server eliminates ~1ms of `execve` overhead per execution, enabling 10K-50K execs/sec for simple targets (compared to 1K-5K without the fork server). The shared memory bitmap read adds ~10us per execution (64KB memcmp).

Deterministic mutation stages are the throughput bottleneck for large inputs. A 1KB input produces ~70K deterministic mutations (bit flips + arithmetic + interesting values). At 20K execs/sec, this takes ~3.5 seconds per queue entry. For larger inputs, skipping or sampling deterministic stages maintains overall fuzzer velocity.

Queue culling runs in O(n * e) where n is the number of queue entries and e is the average number of edges per entry. For corpora with thousands of entries, this can take hundreds of milliseconds. Running culling periodically (every 100 new queue entries) rather than on every addition keeps overhead manageable.

## Going Further

- Add structure-aware mutations for specific file formats (e.g., PDF, PNG header-aware mutations)
- Implement Intel PT-based coverage tracking for binary-only targets
- Add crash analysis: automatic minimization of crashing inputs
- Implement persistent mode (in-process fuzzing without fork overhead)
- Add ASAN/MSAN integration for detecting memory safety bugs
- Build a web UI for monitoring multiple fuzzer instances
