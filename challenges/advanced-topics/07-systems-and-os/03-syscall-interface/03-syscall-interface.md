<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [syscall-ABI, Linux-syscall-convention, vDSO, seccomp-bpf, capabilities, syscall-overhead, x86-64-register-ABI]
languages: [go, rust]
estimated_reading_time: 60 min
bloom_level: analyze
prerequisites: [virtual-memory-and-paging, basic-linux-cli]
papers: [Soares2010-flexsc, Tsai2016-corey]
industry_use: [Docker-seccomp, Kubernetes-seccomp, gVisor, Firecracker, Falco]
language_contrast: high
-->

# Syscall Interface

> Every abstraction in systems programming eventually bottoms out at a syscall; knowing
> the ABI, vDSO fast paths, and filtering mechanisms is what separates syscall-aware
> architecture from guesswork.

## Mental Model

A system call is a controlled transfer of execution from user space to kernel space. On
x86-64 Linux, the CPU switches from ring 3 (user) to ring 0 (kernel) by executing the
`SYSCALL` instruction, which saves the user-space instruction pointer and stack pointer
into CPU-specific registers (MSR_LSTAR, MSR_CSTAR), loads the kernel's entry point,
and disables interrupts during the transition. The total cost — save registers, switch
privilege level, execute the kernel function, restore registers — is roughly 100–300 ns
per syscall on a modern CPU with KPTI (Kernel Page Table Isolation) enabled to mitigate
Meltdown. Before KPTI, it was 50–100 ns. Spectre/Meltdown mitigations added back the
cost that Spectre removed from hardware.

This overhead is not a bug — it is the price of the protection boundary. But it matters
for high-frequency operations: a service that calls `clock_gettime(2)` a million times
per second to timestamp events is spending 100–300 ms/s on pure syscall overhead. The
kernel solves this for `clock_gettime`, `gettimeofday`, and `time` via the **vDSO**
(virtual dynamic shared object): a small piece of kernel code mapped read-only into
every process's address space. The vDSO implementation of `clock_gettime` reads the
timekeeping data from a kernel-maintained page (also mapped read-only into user space),
computes the result in user space, and never enters the kernel. The result: sub-10 ns
time queries with no privilege switch.

The second mental model is the Linux **capability model**. The root/non-root binary is
an oversimplification. Linux capabilities divide superuser privileges into 40+ distinct
capabilities: `CAP_NET_BIND_SERVICE` (bind ports < 1024), `CAP_SYS_PTRACE` (attach
debuggers), `CAP_SYS_ADMIN` (most dangerous), `CAP_NET_RAW` (raw sockets). Containers
run with a reduced capability set. `seccomp-bpf` goes further: it filters at the syscall
level, blocking not just capabilities but entire syscall numbers. Docker's default seccomp
profile blocks ~50 syscalls including `reboot`, `kexec_load`, `create_module`,
`ptrace`, and others that containers should never need.

## Core Concepts

### x86-64 Linux Syscall ABI

The Linux syscall ABI on x86-64 uses these registers:

| Register | Role |
|----------|------|
| `RAX` | Syscall number (on entry); return value (on exit) |
| `RDI` | First argument |
| `RSI` | Second argument |
| `RDX` | Third argument |
| `R10` | Fourth argument (not `RCX` — SYSCALL clobbers it) |
| `R8` | Fifth argument |
| `R9` | Sixth argument |
| `RCX` | Saved by SYSCALL (holds return address) |
| `R11` | Saved by SYSCALL (holds RFLAGS) |

Errno is returned as a negative value in RAX (e.g., `-ENOENT = -2`). The C library's
`syscall(3)` wrapper converts this to a positive errno and sets `errno` in the thread-local
variable. Go's `syscall` package returns the negative value as an `syscall.Errno` directly.

### vDSO (Virtual Dynamic Shared Object)

The vDSO is a small ELF shared object (typically 4–8 KB) that the kernel maps into every
process at a randomized address (ASLR applies). It contains optimized implementations of
a small set of syscalls that read from kernel-maintained data in a shared userspace-readable
memory page:

- `clock_gettime(CLOCK_REALTIME)`, `clock_gettime(CLOCK_MONOTONIC)`
- `gettimeofday`
- `time`
- `getcpu` (on recent kernels)

The vDSO address is placed in the ELF auxiliary vector (`AT_SYSINFO_EHDR`) at process
start. The dynamic linker resolves `clock_gettime` to the vDSO symbol, not to `glibc`'s
syscall wrapper. In Go, the runtime calls `clock_gettime` via an assembly stub that hits
the vDSO; the `time.Now()` fast path in Go 1.17+ avoids a kernel transition entirely on
Linux. The vDSO page is read-only; the kernel updates the timekeeping data using a seqlock
(the reader sees a consistent snapshot by checking the sequence counter before and after).

### seccomp-bpf

`seccomp` (secure computing mode) restricts the syscalls a process can make. `seccomp-bpf`
(Linux 3.5+) allows attaching a BPF program to filter syscalls based on syscall number
and arguments. The BPF program runs in the kernel before the syscall is dispatched and
returns one of:
- `SECCOMP_RET_ALLOW` — proceed normally
- `SECCOMP_RET_KILL_PROCESS` — terminate immediately (no signal handling)
- `SECCOMP_RET_ERRNO(e)` — return `-e` to the caller
- `SECCOMP_RET_TRACE` — notify a tracer (used by `strace`, `gdb`)

Docker and containerd load a seccomp profile (a JSON file of allowed syscalls) via
`seccomp_load` in the container's init process. gVisor uses `seccomp-bpf` to restrict
the host syscalls the sentry process can make, and intercepts guest syscalls in software.

### Linux Capability Model

Capabilities are per-thread (not per-process in the kernel). Each thread has three sets:
- **Permitted**: maximum set the thread can have
- **Effective**: currently active capabilities, checked by the kernel
- **Inheritable**: passed across `execve`

`capget`/`capset` syscalls read/write capability sets. `prctl(PR_CAP_AMBIENT, ...)` sets
ambient capabilities for non-privileged `execve` transitions. `CAP_SYS_ADMIN` is the
"kitchen sink" capability that grants ~40 different privileges — if a service needs it,
that is a design warning.

## Implementation: Go

```go
package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// --- Direct syscall: getpid ---
// Demonstrates the raw syscall path via syscall.RawSyscall.
// RawSyscall does NOT save/restore the goroutine's P — use only for
// syscalls guaranteed to return quickly (no blocking).

func rawGetpid() int {
	pid, _, _ := syscall.RawSyscall(syscall.SYS_GETPID, 0, 0, 0)
	return int(pid)
}

// --- vDSO: clock_gettime via time.Now ---
// Go's time.Now() uses the vDSO on Linux, avoiding a kernel transition.
// We benchmark it against a raw CLOCK_REALTIME syscall to measure the difference.

func benchmarkTimeNow(n int) time.Duration {
	start := time.Now()
	for i := 0; i < n; i++ {
		_ = time.Now()
	}
	return time.Since(start)
}

// clockGettimeRaw issues a raw clock_gettime syscall (bypassing the vDSO).
// This is purely for benchmarking — normally use time.Now().
func clockGettimeRaw() (int64, int64) {
	var ts syscall.Timespec
	_, _, errno := syscall.Syscall(
		syscall.SYS_CLOCK_GETTIME,
		0, // CLOCK_REALTIME
		uintptr(unsafe.Pointer(&ts)),
		0,
	)
	if errno != 0 {
		panic(errno)
	}
	return ts.Sec, int64(ts.Nsec)
}

// --- Capability inspection via capget ---
// Reads the calling thread's capability sets.
// Uses the raw syscall since the Go standard library does not expose capget.

type capHeader struct {
	version uint32
	pid     int32
}

type capData struct {
	effective   uint32
	permitted   uint32
	inheritable uint32
}

const (
	// _LINUX_CAPABILITY_VERSION_3 supports 64-bit capability sets (two 32-bit words).
	linuxCapabilityVersion3 = 0x20080522
	capNetBindService       = 1 << 10
	capSysAdmin             = 1 << 21
	capNetRaw               = 1 << 13
)

func getCapabilities() (effective, permitted uint64, err error) {
	header := capHeader{
		version: linuxCapabilityVersion3,
		pid:     0,
	}
	// Version 3 uses two capData entries (64-bit capability sets).
	var data [2]capData
	_, _, errno := syscall.RawSyscall(
		syscall.SYS_CAPGET,
		uintptr(unsafe.Pointer(&header)),
		uintptr(unsafe.Pointer(&data[0])),
		0,
	)
	if errno != 0 {
		return 0, 0, errno
	}
	effective = uint64(data[0].effective) | (uint64(data[1].effective) << 32)
	permitted = uint64(data[0].permitted) | (uint64(data[1].permitted) << 32)
	return effective, permitted, nil
}

func hasCapability(capSet uint64, cap uint32) bool {
	return capSet&(1<<cap) != 0
}

// --- seccomp-bpf: load a minimal allowlist ---
// Requires Linux 3.5+ and CAP_SYS_ADMIN, or PR_SET_NO_NEW_PRIVS first.
// This example applies a filter that allows only: read, write, exit, exit_group,
// and futex (needed by Go runtime for goroutine parking).
// WARNING: calling any other syscall after loading this filter will kill the process.

const (
	prSetNoNewPrivs   = 38
	seccompSetModeFilter = 1
	bpfStmtLoad      = 0x00
	bpfStmtJumpEQ    = 0x15
	bpfStmtRet       = 0x06
	seccompRetAllow  = 0x7fff0000
	seccompRetKill   = 0x00000000

	// Syscall numbers (x86-64 Linux).
	sysRead      = 0
	sysWrite     = 1
	sysFutex     = 202
	sysExitGroup = 231
	sysExit      = 60
)

type bpfInstruction struct {
	code uint16
	jt   uint8
	jf   uint8
	k    uint32
}

// applySeccompFilter installs a minimal seccomp-bpf allowlist.
// After this call, any syscall not in the allowlist will kill the process.
// DO NOT call this in production without a complete, tested allowlist.
func applySeccompFilter() error {
	// First, set PR_SET_NO_NEW_PRIVS so we can load seccomp without CAP_SYS_ADMIN.
	if _, _, errno := syscall.RawSyscall(
		syscall.SYS_PRCTL,
		prSetNoNewPrivs,
		1, 0,
	); errno != 0 {
		return fmt.Errorf("prctl PR_SET_NO_NEW_PRIVS: %w", errno)
	}

	// BPF program: check syscall number, allow specific ones, kill the rest.
	// BPF_LD | BPF_W | BPF_ABS loads the syscall number from the seccomp_data struct.
	// struct seccomp_data { int nr; __u32 arch; __u64 instruction_pointer; __u64 args[6]; }
	// The syscall number is at offset 0.
	filter := []bpfInstruction{
		{0x20, 0, 0, 0},                     // ld  [0]  (load syscall number)
		{0x15, 0, 1, sysRead},               // jeq read, skip-kill
		{0x06, 0, 0, seccompRetAllow},        // ret ALLOW
		{0x15, 0, 1, sysWrite},
		{0x06, 0, 0, seccompRetAllow},
		{0x15, 0, 1, sysFutex},
		{0x06, 0, 0, seccompRetAllow},
		{0x15, 0, 1, sysExitGroup},
		{0x06, 0, 0, seccompRetAllow},
		{0x15, 0, 1, sysExit},
		{0x06, 0, 0, seccompRetAllow},
		{0x06, 0, 0, seccompRetKill},         // default: kill
	}

	prog := struct {
		len    uint16
		filter *bpfInstruction
	}{
		len:    uint16(len(filter)),
		filter: &filter[0],
	}

	_, _, errno := syscall.RawSyscall(
		syscall.SYS_SECCOMP,
		seccompSetModeFilter,
		0,
		uintptr(unsafe.Pointer(&prog)),
	)
	if errno != 0 {
		return fmt.Errorf("seccomp: %w", errno)
	}
	return nil
}

func main() {
	// Lock to one OS thread so seccomp applies consistently to this goroutine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	fmt.Println("=== Syscall Interface Demo ===\n")

	// --- Raw syscall ---
	pid := rawGetpid()
	fmt.Printf("PID via raw syscall: %d (os.Getpid: %d)\n", pid, os.Getpid())

	// --- vDSO timing ---
	n := 1_000_000
	d := benchmarkTimeNow(n)
	fmt.Printf("\nvDSO time.Now(): %d calls in %v (%v/call)\n", n, d, d/time.Duration(n))

	sec, nsec := clockGettimeRaw()
	fmt.Printf("Raw clock_gettime: %d.%09d\n", sec, nsec)

	// --- Capability inspection ---
	effective, permitted, err := getCapabilities()
	if err != nil {
		fmt.Println("capget error:", err)
	} else {
		fmt.Printf("\nCapabilities:\n")
		fmt.Printf("  Effective:       0x%016x\n", effective)
		fmt.Printf("  Permitted:       0x%016x\n", permitted)
		fmt.Printf("  CAP_NET_BIND_SERVICE: %v\n", hasCapability(effective, capNetBindService))
		fmt.Printf("  CAP_SYS_ADMIN:        %v\n", hasCapability(effective, capSysAdmin))
		fmt.Printf("  CAP_NET_RAW:          %v\n", hasCapability(effective, capNetRaw))
	}

	// --- seccomp-bpf ---
	// WARNING: once loaded, this filter kills the process on any unlisted syscall.
	// Uncomment only to test seccomp behavior. After loading, only read/write/futex/exit work.
	// if err := applySeccompFilter(); err != nil {
	//     fmt.Println("seccomp error (may need root or newer kernel):", err)
	// } else {
	//     fmt.Println("seccomp filter loaded — only read/write/futex/exit are allowed")
	// }

	fmt.Println("\nDone.")
}
```

### Go-specific considerations

Go distinguishes `syscall.Syscall` from `syscall.RawSyscall`. `RawSyscall` does not
notify the Go scheduler that the current goroutine is in a syscall — the scheduler assumes
it returns quickly. `Syscall` sets the goroutine state to `_Gsyscall` before entering the
kernel, which tells the scheduler to run other goroutines on the same P. For blocking
syscalls (reads, waits), always use `syscall.Syscall` or the `golang.org/x/sys/unix`
equivalents, or the scheduler will appear to stall.

The `golang.org/x/sys/unix` package is preferred over `syscall` for new code: it exposes
Linux-specific syscalls (`SYS_SECCOMP`, `SYS_CAPGET`, `SYS_CLONE3`) that the standard
`syscall` package does not wrap, and it maintains up-to-date constant tables.

`syscall.SyscallN` (Go 1.21+) supports syscalls with up to 9 arguments, eliminating the
need for the `Syscall6` variant for most cases.

## Implementation: Rust

```rust
// Add to Cargo.toml:
// libc = "0.2"
//
// On Linux x86-64 only. The inline assembly requires nightly for full stability
// but the libc-based path works on stable.

use libc::{
    c_int, c_long, prctl, syscall, SYS_capget, SYS_clock_gettime, SYS_getpid,
    SYS_prctl, SYS_seccomp, PR_SET_NO_NEW_PRIVS, CLOCK_MONOTONIC,
};
use std::time::Instant;

// --- Raw syscall via inline assembly ---
// This is the lowest-level path: no libc, no wrapper, direct SYSCALL instruction.
// In production Rust, use libc::syscall() instead — it handles errno correctly.
#[cfg(target_arch = "x86_64")]
unsafe fn raw_getpid_asm() -> i64 {
    let result: i64;
    std::arch::asm!(
        "syscall",
        in("rax") libc::SYS_getpid,
        // All other syscall registers are clobbered; mark them.
        out("rax") result,
        out("rcx") _,
        out("r11") _,
        options(nostack),
    );
    result
}

// --- clock_gettime via libc (hits the vDSO on Linux) ---
fn clock_gettime_monotonic() -> (i64, i64) {
    let mut ts = libc::timespec { tv_sec: 0, tv_nsec: 0 };
    unsafe {
        libc::clock_gettime(CLOCK_MONOTONIC, &mut ts);
    }
    (ts.tv_sec, ts.tv_nsec)
}

fn bench_clock_gettime(n: usize) -> std::time::Duration {
    let start = Instant::now();
    for _ in 0..n {
        let _ = clock_gettime_monotonic();
    }
    start.elapsed()
}

// --- Capability inspection ---
#[repr(C)]
struct CapHeader {
    version: u32,
    pid: i32,
}

#[repr(C)]
#[derive(Debug, Copy, Clone)]
struct CapData {
    effective: u32,
    permitted: u32,
    inheritable: u32,
}

const LINUX_CAPABILITY_VERSION_3: u32 = 0x20080522;

fn get_capabilities() -> Result<(u64, u64), String> {
    let mut header = CapHeader {
        version: LINUX_CAPABILITY_VERSION_3,
        pid: 0,
    };
    let mut data = [CapData {
        effective: 0,
        permitted: 0,
        inheritable: 0,
    }; 2];

    let rc = unsafe {
        syscall(
            SYS_capget,
            &mut header as *mut CapHeader,
            data.as_mut_ptr(),
        )
    };
    if rc != 0 {
        return Err(format!("capget failed: errno {rc}"));
    }
    let effective = (data[0].effective as u64) | ((data[1].effective as u64) << 32);
    let permitted = (data[0].permitted as u64) | ((data[1].permitted as u64) << 32);
    Ok((effective, permitted))
}

fn has_capability(cap_set: u64, cap: u32) -> bool {
    cap_set & (1 << cap) != 0
}

// --- seccomp-bpf ---
// Represents a BPF instruction. On Linux, seccomp uses classic BPF (cBPF),
// not eBPF. Each instruction is 8 bytes: opcode, jt, jf, k.
#[repr(C)]
struct SockFilter {
    code: u16,
    jt: u8,
    jf: u8,
    k: u32,
}

#[repr(C)]
struct SockFprog {
    len: u16,
    _pad: [u16; 3],
    filter: *const SockFilter,
}

fn apply_minimal_seccomp() -> Result<(), String> {
    // Step 1: PR_SET_NO_NEW_PRIVS — allows loading seccomp without CAP_SYS_ADMIN.
    let rc = unsafe { prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) };
    if rc != 0 {
        return Err(format!("PR_SET_NO_NEW_PRIVS failed: {rc}"));
    }

    // BPF filter: load syscall number from seccomp_data (offset 0), allow a
    // minimal set, kill on anything else.
    // Syscall numbers: read=0, write=1, futex=202, exit_group=231, exit=60
    let filter = [
        SockFilter { code: 0x20, jt: 0, jf: 0, k: 0 },           // ld [0] (nr)
        SockFilter { code: 0x15, jt: 0, jf: 1, k: 0 },           // jeq read
        SockFilter { code: 0x06, jt: 0, jf: 0, k: 0x7fff0000 },  // ret ALLOW
        SockFilter { code: 0x15, jt: 0, jf: 1, k: 1 },           // jeq write
        SockFilter { code: 0x06, jt: 0, jf: 0, k: 0x7fff0000 },
        SockFilter { code: 0x15, jt: 0, jf: 1, k: 202 },         // jeq futex
        SockFilter { code: 0x06, jt: 0, jf: 0, k: 0x7fff0000 },
        SockFilter { code: 0x15, jt: 0, jf: 1, k: 231 },         // jeq exit_group
        SockFilter { code: 0x06, jt: 0, jf: 0, k: 0x7fff0000 },
        SockFilter { code: 0x15, jt: 0, jf: 1, k: 60 },          // jeq exit
        SockFilter { code: 0x06, jt: 0, jf: 0, k: 0x7fff0000 },
        SockFilter { code: 0x06, jt: 0, jf: 0, k: 0x00000000 },  // ret KILL
    ];

    let prog = SockFprog {
        len: filter.len() as u16,
        _pad: [0; 3],
        filter: filter.as_ptr(),
    };

    const SECCOMP_SET_MODE_FILTER: u64 = 1;
    let rc = unsafe {
        syscall(
            SYS_seccomp,
            SECCOMP_SET_MODE_FILTER,
            0u64,
            &prog as *const SockFprog as u64,
        )
    };
    if rc != 0 {
        return Err(format!("seccomp failed: {rc}"));
    }
    Ok(())
}

fn main() {
    println!("=== Syscall Interface (Rust) ===\n");

    // --- Raw getpid via inline assembly ---
    #[cfg(target_arch = "x86_64")]
    {
        let asm_pid = unsafe { raw_getpid_asm() };
        let libc_pid = unsafe { libc::getpid() };
        println!("PID via asm!:  {asm_pid}");
        println!("PID via libc:  {libc_pid}");
    }

    // --- vDSO benchmark ---
    let n = 1_000_000_usize;
    let d = bench_clock_gettime(n);
    println!(
        "\nvDSO clock_gettime: {n} calls in {:?} ({:?}/call)",
        d,
        d / n as u32,
    );

    // --- Capabilities ---
    match get_capabilities() {
        Ok((effective, permitted)) => {
            println!("\nCapabilities:");
            println!("  Effective: 0x{effective:016x}");
            println!("  Permitted: 0x{permitted:016x}");
            println!("  CAP_NET_BIND_SERVICE (10): {}", has_capability(effective, 10));
            println!("  CAP_SYS_ADMIN (21):        {}", has_capability(effective, 21));
            println!("  CAP_NET_RAW (13):           {}", has_capability(effective, 13));
        }
        Err(e) => println!("capget error: {e}"),
    }

    // --- seccomp ---
    // Uncomment to test — after loading, any syscall outside read/write/futex/exit kills the process.
    // match apply_minimal_seccomp() {
    //     Ok(()) => println!("\nSeccomp filter loaded"),
    //     Err(e) => println!("\nSeccomp error: {e}"),
    // }

    println!("\nDone.");
}
```

### Rust-specific considerations

The `nix` crate provides type-safe wrappers for `prctl`, `capget`, and `seccomp` on
Linux. The `seccomp` crate (crates.io) provides a higher-level API for building seccomp
filters programmatically and converting them to BPF bytecode, equivalent to libseccomp
in C. For production use, prefer these over raw `libc::syscall` — the type safety catches
argument ordering bugs that are silent in raw syscall calls.

Inline `asm!` in Rust (stable since Rust 1.59) gives access to the CPU's `SYSCALL`
instruction directly, which is useful in `no_std` environments where `libc` is unavailable.
The `syscalls` crate provides a safe wrapper around `asm!`-based syscalls for all Linux
architectures.

For eBPF-based syscall filtering (the successor to seccomp-bpf), see the `aya` framework
in the eBPF topic of this section.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Syscall wrapper | `syscall` package + `golang.org/x/sys/unix` | `libc` crate + `nix` crate |
| Raw syscall | `syscall.RawSyscall` | `libc::syscall` or `asm!` |
| vDSO `time.Now()` | Yes (Go runtime uses vDSO on Linux) | Yes (via libc `clock_gettime`) |
| Blocking syscall handling | `syscall.Syscall` yields goroutine to scheduler | Manual; no scheduler |
| seccomp | Raw syscall or `golang.org/x/sys/unix.Seccomp` | `seccomp` crate or raw `libc::syscall` |
| Capabilities | Raw `SYS_CAPGET`/`SYS_CAPSET` | `caps` crate or raw `libc::syscall` |
| syscall overhead | Go adds goroutine state management overhead | Zero overhead beyond syscall itself |
| No-std syscalls | Not applicable (Go requires a runtime) | `syscalls` crate with `asm!` |

## Production War Stories

**gVisor and syscall interception**: gVisor (Google's container sandbox) intercepts all
guest syscalls in user space using a kernel-mode stub (`ptrace` or KVM-based). Every
syscall from a sandboxed process is intercepted, checked, and emulated by the sentry
process. This adds 2–20 µs per syscall vs. native execution. For services that call
`epoll_wait` in a tight loop (network-intensive services), the overhead is negligible.
For services that call `stat()` or `open()` thousands of times per request (metadata-heavy
workloads), the overhead can be 5–10x latency increase. gVisor's Kubernetes integration
(RuntimeClass `gvisor`) is now used for untrusted workloads at Google.

**Docker seccomp and false positives**: Docker's default seccomp profile blocks `ptrace`.
A service that calls `ptrace` (perhaps via a JVM's JIT compiler's self-profiling path, or
a Go profiler calling `perf_event_open`) will fail with EPERM under the default Docker
profile. The solution is a custom seccomp profile or `--security-opt seccomp=unconfined`.
The latter disables all filtering — prefer auditing the required syscalls and whitelisting
only those.

**Firecracker's minimal syscall surface**: Firecracker (AWS Lambda's microVM hypervisor)
loads a seccomp filter that allows only ~50 syscalls — the minimal set needed to manage
KVM, virtio devices, and signal handling. The filter is generated programmatically from
Firecracker's source code. Any vulnerability that requires an unlisted syscall cannot be
exploited, even if the attacker achieves code execution in the Firecracker process.

**vDSO and container time issues**: Containers running with read-only root filesystems
and restricted mounts sometimes prevent the vDSO from being mapped correctly. Symptoms:
`clock_gettime` becomes a full syscall, adding 100–200 ns per call. A service doing
10M time queries per second suddenly adds 1–2 seconds of pure overhead. Diagnosis:
`strace -e clock_gettime ./service` — if vDSO is working, `clock_gettime` will not
appear in strace output (it never enters the kernel).

## Complexity Analysis

| Operation | Latency | Notes |
|-----------|---------|-------|
| `syscall` (KPTI off) | ~50–100 ns | Pre-Meltdown mitigation |
| `syscall` (KPTI on) | ~100–300 ns | KPTI adds TLB flush on transition |
| vDSO `clock_gettime` | ~5–10 ns | User-space seqlock read |
| `time.Now()` (Go, Linux) | ~8–15 ns | vDSO path via runtime |
| `syscall.Syscall` overhead (Go) | +20–40 ns | Goroutine state save/restore |
| `gVisor` syscall intercept | +2–20 µs | ptrace or KVM-based |
| seccomp-bpf filter evaluation | +50–200 ns | BPF program execution in kernel |
| `capget` syscall | ~200 ns | Standard syscall, no vDSO |

## Common Pitfalls

1. **Calling blocking syscalls from goroutines without `syscall.Syscall`.** If you use
   `syscall.RawSyscall` for a syscall that can block (e.g., a custom `ioctl` that waits),
   the Go scheduler will not preempt the goroutine, starving other goroutines on the same P.

2. **Loading seccomp filters that block the Go runtime's required syscalls.** The Go
   runtime uses `futex`, `clone`, `sigaltstack`, `mmap`, `madvise`, and others. A seccomp
   filter that blocks these will crash the process at runtime, not at filter load time.
   Test filters with `strace -f -e trace=all` to enumerate every syscall the runtime makes
   during startup and under load.

3. **Assuming capabilities are process-wide.** Linux capabilities are per-thread. Forking
   a thread and calling `capset` in the child does not affect the parent. In Go, goroutines
   can migrate between OS threads, so calling `capset` without `runtime.LockOSThread()` may
   change the capabilities of a different goroutine.

4. **Forgetting KPTI costs.** Benchmarks run before 2018 (pre-Meltdown mitigations) or
   on AMD CPUs (which use ASID-based isolation and avoid the full TLB flush) show syscall
   overhead 2–3x lower than on patched Intel CPUs. Extrapolating old benchmarks to modern
   Intel hardware understates syscall cost.

5. **Using `syscall.Syscall` vs `syscall.Syscall6` for the wrong number of arguments.**
   The Go `syscall` package provides `Syscall` (up to 3 args), `Syscall6` (up to 6 args),
   and since 1.21 `SyscallN` (variadic). Passing the wrong number silently passes garbage
   in the unused registers on some kernel versions.

## Exercises

**Exercise 1** (30 min): Benchmark `time.Now()` vs a raw `clock_gettime(CLOCK_REALTIME)`
syscall in Go. Measure the difference between the vDSO path and the syscall path.
Confirm the vDSO is being used by running under `strace -e trace=clock_gettime` and
verifying `time.Now()` does not appear.

**Exercise 2** (2–4 h): Write a Go or Rust program that reads `/proc/self/status` to
display the process's capability sets (CapPrm, CapEff, CapBnd lines), then calls
`capget` directly and compares the results. Test inside a Docker container (with and
without `--cap-add SYS_ADMIN`) and explain the difference.

**Exercise 3** (4–8 h): Implement a minimal container sandbox in Go using seccomp-bpf:
build a seccomp profile from the output of `strace -e trace=all` on a simple target
program, then apply that profile programmatically with `SYS_SECCOMP`. Verify that the
sandbox blocks an unexpected syscall (e.g., `mkdir`) without killing allowed ones.

**Exercise 4** (8–15 h): Write a syscall auditing daemon using `SECCOMP_RET_TRACE` + a
`ptrace`-based tracer in Go. The daemon forks a child, loads a seccomp filter with
`SECCOMP_RET_TRACE` for selected syscalls, then monitors the child's syscall activity
in real time. Log syscall number, arguments, and latency. This is the core mechanism
behind Falco and similar runtime security tools.

## Further Reading

### Foundational Papers

- Soares, L., Stumm, M. (2010). "FlexSC: Flexible System Call Scheduling with
  Exception-Less System Calls." OSDI 2010. Proposes batching syscalls to amortize
  the kernel transition cost; directly motivates io_uring's design.
- Tsai, C. et al. (2016). "A Study of Modern Linux API Usage and Compatibility:
  What to Support When You're Supporting." EuroSys 2016. Empirical analysis of which
  syscalls real applications use (spoiler: a small subset dominates).

### Books

- Kerrisk, M. "The Linux Programming Interface" — Chapters 20–22 (Signals), Chapter 38
  (Writing Secure Privileged Programs), and Chapter 47 (Seccomp) are the reference.
- Love, R. "Linux System Programming" (2nd ed.) — Chapter 4 (Advanced File I/O) and
  Chapter 11 (Signals and Concurrency) cover the scheduler/syscall interaction.

### Production Code to Read

- Docker's default seccomp profile: `profiles/seccomp/default.json` in the Moby repo.
  Contains a well-curated allowlist with explanations for each blocked syscall.
- Go runtime's syscall entry: `src/runtime/sys_linux_amd64.s` — `·syscall`, `·rawsyscall`,
  and the `·time_now` vDSO call.
- Firecracker's seccomp filter: `src/seccompiler/src/` in the Firecracker repo.

### Conference Talks

- "The Definitive Guide to Linux System Calls" — LinuxCon 2016. Covers the full ABI,
  vDSO mechanism, and vsyscall legacy.
- "Seccomp in the Real World" — Linux Security Summit 2018. Case studies of seccomp
  deployment in Chrome, Docker, and OpenSSH.
- "Adventures in Capability Land" — FOSDEM 2022. Fine-grained privilege reduction in
  production services using Linux capabilities.
