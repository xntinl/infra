<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [eBPF, BPF-ISA, BPF-verifier, BPF-maps, kprobes, tracepoints, XDP, tc-hook, perf-events, cilium-ebpf, aya]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [syscall-interface, virtual-memory-and-paging, basic-networking]
papers: [McCanne1993-BPF, Calavera2016-eBPF-tracing]
industry_use: [Cloudflare-DDoS, Meta-Katran, Cilium-CNI, Pixie-observability, Netflix-bpftrace, Facebook-Katran]
language_contrast: high
-->

# eBPF Programming

> eBPF lets you run sandboxed programs in the kernel without writing a kernel module —
> the verifier guarantees safety, the JIT makes it fast, and the hook points make it
> applicable to networking, observability, and security simultaneously.

## Mental Model

eBPF (extended Berkeley Packet Filter) is a virtual machine inside the Linux kernel. You
write a program in a restricted C dialect (or in Rust with `aya`), compile it to BPF
bytecode, and load it into the kernel. Before the program runs, the kernel's **verifier**
statically checks that it terminates (no unbounded loops in kernel eBPF — bounded loops
are allowed since Linux 5.3), does not read uninitialized memory, does not dereference
arbitrary pointers, and does not exceed stack size limits. If the verifier accepts the
program, the **JIT compiler** translates BPF bytecode to native machine code. The result
is kernel code that is provably safe (by construction) and runs at native speed.

The power of eBPF is in its **hook points**. You can attach an eBPF program to:
- **XDP (eXpress Data Path)**: the earliest point in the network receive path, before
  sk_buff allocation. Packets can be dropped, redirected, or modified at line rate.
- **tc (traffic control) ingress/egress**: after sk_buff, can modify or redirect packets.
- **kprobes/kretprobes**: any kernel function entry or return.
- **tracepoints**: stable kernel instrumentation points (e.g., `sys_enter_open`).
- **perf events**: hardware counters, software events, CPU cycle sampling.
- **LSM (Linux Security Modules)**: policy hooks in the security path.
- **cgroup**: per-cgroup network or system call policy.

**BPF maps** are the shared memory between the eBPF program (running in kernel context)
and the userspace control plane. Maps are typed data structures: hash maps, arrays, ring
buffers, LRU caches, per-CPU arrays. The eBPF program writes to a map; your Go or Rust
userspace program reads from it. This is how Cloudflare's DDoS mitigation works: an XDP
program classifies packets, increments per-source-IP counters in a BPF hash map, and
drops packets from IPs that exceed a rate threshold — all without a single syscall per
packet.

The conceptual barrier for most developers is the **verifier**. It is not a static
analyzer you can reason about informally — it is a formal abstract interpreter that tracks
the type and bounds of every register at every point in the program. A pointer derived
from a map lookup must be NULL-checked before dereferencing. A helper function argument
must have a valid type. The verifier rejects programs that are correct but that it cannot
prove correct; you must write your program to match the verifier's mental model. This is
the primary engineering cost of eBPF.

## Core Concepts

### BPF ISA

The BPF ISA has 11 64-bit registers (`r0`–`r10`, plus a frame pointer) and 512 bytes of
stack. Instructions are 8 bytes. The JIT maps BPF registers directly to x86-64 registers:
`r1`–`r5` are argument registers, `r0` is the return value, `r10` is the frame pointer.
Programs call kernel **helper functions** (`bpf_map_lookup_elem`, `bpf_get_current_pid_tgid`,
`bpf_skb_store_bytes`, etc.) via a special BPF call instruction; helpers are the only way
to interact with kernel state.

### BPF Maps

| Map Type | Use Case |
|----------|----------|
| `BPF_MAP_TYPE_HASH` | Per-flow state, source IP rate limiting |
| `BPF_MAP_TYPE_ARRAY` | Fixed-size indexed data, per-CPU stats |
| `BPF_MAP_TYPE_PERF_EVENT_ARRAY` | Streaming events to userspace (legacy) |
| `BPF_MAP_TYPE_RINGBUF` | Low-overhead streaming events (preferred, Linux 5.8+) |
| `BPF_MAP_TYPE_LRU_HASH` | Fixed-size cache with eviction |
| `BPF_MAP_TYPE_PERCPU_HASH` | Per-CPU hash maps (no lock needed) |
| `BPF_MAP_TYPE_PROG_ARRAY` | Tail call table — jump between eBPF programs |
| `BPF_MAP_TYPE_SOCK_MAP` | Socket redirection for sockmap programs |

### XDP (eXpress Data Path)

XDP programs attach to a network device's receive queue at the driver level (or generic
XDP which runs after the driver's receive, but before sk_buff allocation). The XDP program
returns one of:
- `XDP_PASS`: continue normal processing
- `XDP_DROP`: silently drop the packet
- `XDP_TX`: transmit back out the same interface (used for fast path reflection)
- `XDP_REDIRECT`: redirect to another device or CPU queue
- `XDP_ABORTED`: indicates a program error; drops the packet and increments a counter

Meta's Katran load balancer and Cloudflare's DDoS mitigation both use XDP for packet
processing at 10–100 Gbps line rates, which is impossible with the traditional Linux
networking stack.

### kprobes and Tracepoints

**kprobes** dynamically insert a breakpoint at any kernel function entry. The probe
replaces the first byte of the instruction with `INT3` (x86-64), traps to the kprobe
handler, which invokes your eBPF program. kprobes are unstable (kernel function names
can change between versions) and have ~200 ns overhead per probe hit.

**Tracepoints** are stable kernel instrumentation points compiled into the kernel source.
They have lower overhead than kprobes (~50 ns) and are ABI-stable across kernel versions.
Prefer tracepoints over kprobes for production tools. Examples:
`sched_switch`, `sys_enter_execve`, `net_dev_xmit`, `tcp_retransmit_skb`.

**uprobes** instrument userspace function entries — useful for tracing Go runtime events
(goroutine creation, GC) without modifying the binary.

## Implementation: Go

```go
//go:build ignore
// This file is loaded by the ebpf program loader, not compiled directly.
// The actual Go program is below this BPF source section.

// BPF C program (saved as bpf_program.c, compiled with clang):
// clang -O2 -g -target bpf -c bpf_program.c -o bpf_program.o
//
// #include <linux/bpf.h>
// #include <bpf/bpf_helpers.h>
//
// struct {
//     __uint(type, BPF_MAP_TYPE_RINGBUF);
//     __uint(max_entries, 1 << 24); // 16 MB ring buffer
// } events SEC(".maps");
//
// struct event_t {
//     __u32 pid;
//     __u32 uid;
//     char  comm[16];
// };
//
// SEC("tracepoint/syscalls/sys_enter_execve")
// int trace_execve(struct trace_event_raw_sys_enter *ctx) {
//     struct event_t ev = {};
//     ev.pid = bpf_get_current_pid_tgid() >> 32;
//     ev.uid = bpf_get_current_uid_gid() & 0xffffffff;
//     bpf_get_current_comm(&ev.comm, sizeof(ev.comm));
//     bpf_ringbuf_output(&events, &ev, sizeof(ev), 0);
//     return 0;
// }
// char LICENSE[] SEC("license") = "GPL";
```

```go
// +build linux

// Package main demonstrates loading an eBPF program with cilium/ebpf.
// Run as root or with CAP_BPF + CAP_PERFMON (Linux 5.8+).
//
// Prerequisites:
//   go get github.com/cilium/ebpf@v0.13.0
//   go generate  (runs bpf2go to compile BPF C → Go embed)
//
// In a real project, use go:generate with bpf2go to embed the compiled BPF object.
// Here we use the bytes directly to keep the example self-contained.

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

// eventT mirrors the BPF-side struct event_t.
// Must match the BPF struct layout exactly (no padding surprises on 64-bit).
type eventT struct {
	PID  uint32
	UID  uint32
	Comm [16]byte
}

func main() {
	// Remove the RLIMIT_MEMLOCK restriction — required for BPF map allocation
	// on kernels < 5.11. On 5.11+ this is a no-op but safe to call.
	if err := rlimit.RemoveMemlock(); err != nil {
		fmt.Fprintln(os.Stderr, "RemoveMemlock:", err)
		os.Exit(1)
	}

	// Load a pre-compiled BPF object file.
	// In production, use go:generate + bpf2go to embed the .o into the binary.
	// bpf2go generates type-safe Go wrappers for maps and programs.
	spec, err := ebpf.LoadCollectionSpec("bpf_program.o")
	if err != nil {
		fmt.Fprintln(os.Stderr, "LoadCollectionSpec:", err)
		os.Exit(1)
	}

	// Load the collection (maps + programs) into the kernel.
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "NewCollection:", err)
		os.Exit(1)
	}
	defer coll.Close()

	// Retrieve the ring buffer map and the tracepoint program by name.
	events := coll.Maps["events"]
	prog := coll.Programs["trace_execve"]

	// Attach the eBPF program to the tracepoint syscalls/sys_enter_execve.
	// The kernel will call trace_execve() on every execve() syscall.
	tp, err := link.Tracepoint("syscalls", "sys_enter_execve", prog, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Tracepoint:", err)
		os.Exit(1)
	}
	defer tp.Close()

	// Open a ring buffer reader. The ring buffer is the preferred event
	// transport for eBPF (Linux 5.8+): it has lower overhead than
	// perf_event_array and supports variable-length records.
	rd, err := ringbuf.NewReader(events)
	if err != nil {
		fmt.Fprintln(os.Stderr, "NewReader:", err)
		os.Exit(1)
	}
	defer rd.Close()

	// Handle Ctrl+C gracefully.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		rd.Close()
	}()

	fmt.Println("Tracing execve() syscalls... Press Ctrl+C to stop.\n")
	fmt.Printf("%-8s  %-8s  %s\n", "PID", "UID", "COMM")

	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				break
			}
			fmt.Fprintln(os.Stderr, "Read:", err)
			continue
		}

		var ev eventT
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &ev); err != nil {
			fmt.Fprintln(os.Stderr, "decode:", err)
			continue
		}

		// Convert the null-terminated comm byte array to a Go string.
		comm := string(bytes.TrimRight(ev.Comm[:], "\x00"))
		fmt.Printf("%-8d  %-8d  %s\n", ev.PID, ev.UID, comm)
	}
}
```

### Go-specific considerations

`cilium/ebpf` is the canonical Go library for eBPF. It provides:
- `ebpf.CollectionSpec` — parsed BPF ELF object (maps, programs, BTF)
- `ebpf.Collection` — loaded kernel objects
- `link` package — attaching programs to hook points (tracepoints, kprobes, XDP, cgroups)
- `ringbuf`, `perf` packages — reading events from the kernel
- `bpf2go` code generator — compiles BPF C to Go-embedded bytes at build time

The `bpf2go` workflow is strongly recommended for production: `go generate` invokes `clang`
to compile the BPF C program, then generates Go code with type-safe accessors for every
map and program. The result is a single self-contained binary with the BPF bytecode
embedded, no runtime compilation required. Cilium, Pixie, and most production eBPF tools
in Go use this pattern.

For XDP programs, use `link.AttachXDP` with the interface index. For network programs
requiring sk_buff access, use `link.AttachTCIngress`/`AttachTCEgress`.

## Implementation: Rust

```rust
// eBPF with Aya — Rust framework for eBPF programs
//
// Aya consists of two parts:
//   1. The eBPF kernel program (aya-bpf crate), compiled with cargo-bpf or xtask
//   2. The userspace loader (aya crate), which loads and manages the kernel program
//
// Project structure:
//   my-ebpf/
//     Cargo.toml (workspace)
//     my-ebpf-ebpf/   -- kernel-side code (no_std, target: bpfel-unknown-none)
//       src/main.rs
//     my-ebpf/        -- userspace loader
//       src/main.rs
//
// This file shows the USERSPACE LOADER.
// The kernel-side program (my-ebpf-ebpf/src/main.rs) is shown in the comment below.

// ===== Kernel-side (my-ebpf-ebpf/src/main.rs) =====
//
// #![no_std]
// #![no_main]
// use aya_bpf::{
//     macros::{map, tracepoint},
//     maps::RingBuf,
//     programs::TracePointContext,
//     BpfContext,
// };
// use aya_log_ebpf::info;
//
// #[map]
// static mut EVENTS: RingBuf = RingBuf::with_byte_size(1 << 24, 0);
//
// #[repr(C)]
// pub struct ExecveEvent {
//     pub pid: u32,
//     pub uid: u32,
//     pub comm: [u8; 16],
// }
//
// #[tracepoint(name = "trace_execve", category = "syscalls")]
// pub fn trace_execve(ctx: TracePointContext) -> i64 {
//     let pid = ctx.pid();
//     let uid = ctx.uid();
//     let comm = ctx.command().unwrap_or([0u8; 16]);
//     unsafe {
//         if let Some(mut entry) = EVENTS.reserve::<ExecveEvent>(0) {
//             let ev = entry.as_mut_ptr();
//             (*ev).pid = pid;
//             (*ev).uid = uid;
//             (*ev).comm = comm;
//             entry.submit(0);
//         }
//     }
//     0
// }
//
// #[panic_handler]
// fn panic(_info: &core::panic::PanicInfo) -> ! { loop {} }

// ===== Userspace loader (my-ebpf/src/main.rs) =====
// Add to Cargo.toml:
//   [dependencies]
//   aya = { version = "0.12", features = ["async_tokio"] }
//   aya-log = "0.2"
//   tokio = { version = "1", features = ["full"] }
//   log = "0.4"
//   env_logger = "0.10"
//   bytes = "1"

use aya::{
    include_bytes_aligned,
    maps::RingBuf,
    programs::TracePoint,
    Bpf,
};
use aya_log::BpfLogger;
use bytes::BytesMut;
use log::{info, warn};
use std::convert::TryFrom;
use tokio::signal;

#[repr(C)]
#[derive(Debug)]
struct ExecveEvent {
    pid: u32,
    uid: u32,
    comm: [u8; 16],
}

// include_bytes_aligned! embeds the compiled BPF ELF object at compile time.
// The path is relative to the workspace root; adjust as needed.
// static BPF_BYTES: &[u8] = include_bytes_aligned!(
//     "../../target/bpfel-unknown-none/release/my-ebpf-ebpf"
// );

#[tokio::main]
async fn main() -> Result<(), anyhow::Error> {
    env_logger::init();

    // In a real project, load from the embedded bytes above.
    // Here we load from a file for clarity.
    let bpf_path = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "target/bpfel-unknown-none/release/my-ebpf-ebpf".to_string());

    let mut bpf = Bpf::load_file(&bpf_path)?;

    // Optional: forward aya_log_ebpf messages from BPF programs to the Rust logger.
    if let Err(e) = BpfLogger::init(&mut bpf) {
        warn!("BPF logger init failed (kernel may not support it): {e}");
    }

    // Retrieve the tracepoint program from the loaded collection.
    let program: &mut TracePoint = bpf.program_mut("trace_execve").unwrap().try_into()?;

    // Load the program into the kernel (runs the verifier).
    program.load()?;

    // Attach to the tracepoint: syscalls/sys_enter_execve.
    program.attach("syscalls", "sys_enter_execve")?;
    info!("Attached to syscalls/sys_enter_execve");

    // Open the ring buffer map for reading.
    let mut ring_buf = RingBuf::try_from(bpf.map_mut("EVENTS").unwrap())?;

    println!("Tracing execve() syscalls. Press Ctrl+C to stop.");
    println!("{:<8}  {:<8}  {}", "PID", "UID", "COMM");

    // Poll the ring buffer for events.
    // In async mode, aya integrates with tokio's epoll reactor.
    loop {
        tokio::select! {
            _ = signal::ctrl_c() => {
                info!("Received Ctrl+C, exiting");
                break;
            }
            _ = tokio::time::sleep(std::time::Duration::from_millis(1)) => {
                while let Some(item) = ring_buf.next() {
                    if item.len() < std::mem::size_of::<ExecveEvent>() {
                        warn!("Short event: {} bytes", item.len());
                        continue;
                    }
                    // Safety: we trust the BPF program to write a correctly-sized ExecveEvent.
                    let ev: ExecveEvent = unsafe {
                        std::ptr::read_unaligned(item.as_ptr() as *const ExecveEvent)
                    };
                    let comm_end = ev.comm.iter().position(|&b| b == 0).unwrap_or(16);
                    let comm = std::str::from_utf8(&ev.comm[..comm_end]).unwrap_or("<invalid>");
                    println!("{:<8}  {:<8}  {}", ev.pid, ev.uid, comm);
                }
            }
        }
    }

    Ok(())
}
```

### Rust-specific considerations

Aya's key advantage over C-based eBPF toolchains is that both the kernel-side and
userspace code are written in Rust, with no C toolchain required at runtime. The
`cargo-bpf` tool (from the `aya` project) compiles kernel-side BPF code to BPF
bytecode using LLVM's BPF target, embedded in the userspace binary via
`include_bytes_aligned!`. This enables a single `cargo build` to produce a complete
eBPF program without managing separate clang invocations.

The `no_std`, `no_main` kernel-side Aya code runs in BPF context — no heap allocation,
no panics, no standard library. The `aya-bpf` crate provides safe wrappers for BPF map
operations, helper functions, and program context types. The BPF verifier's constraints
are reflected in Aya's type system: `RingBuf::reserve` returns an `Option`, forcing the
programmer to handle the NULL case that the verifier requires.

For production eBPF tools in Rust, consider `redbpf` (alternative to aya, older but
more mature) and `libbpf-rs` (Rust bindings to the C libbpf library, stable and widely
tested but requires C toolchain).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Primary library | `cilium/ebpf` | `aya` (Rust-native) or `libbpf-rs` |
| BPF kernel code | C with clang, embedded via `bpf2go` | Rust with `aya-bpf` crate, `no_std` |
| Code generation | `bpf2go` (generates type-safe Go accessors) | `include_bytes_aligned!` + manual accessors |
| Map API | Type-safe via generated code | Typed wrappers in `aya::maps` |
| Async event loop | `ringbuf.Reader` (blocking) or `perf.Reader` | `tokio::select!` with aya async support |
| BTF support | Yes (via libbpf-go internals in cilium/ebpf) | Yes (via aya's BTF support) |
| Production maturity | High (Cilium, Pixie, Falco use cilium/ebpf) | Growing (Aya 0.x, not yet 1.0) |
| No-C-toolchain | No (bpf2go requires clang) | Yes (aya compiles BPF with Rust/LLVM) |
| XDP support | Yes (link.AttachXDP) | Yes (aya XDP program type) |
| CO-RE (compile once run everywhere) | Yes | Yes (aya supports BTF-based CO-RE) |

## Production War Stories

**Cloudflare's DDoS mitigation (XDP)**: Cloudflare processes 100+ Gbps of traffic at
the edge. Their DDoS mitigation layer uses XDP programs that, on each incoming packet:
read the source IP from the packet header, look it up in a BPF hash map of blocked CIDRs,
and return `XDP_DROP` if blocked. The entire operation — packet receive, map lookup,
decision — happens before the Linux networking stack allocates a single `sk_buff`. This
is why Cloudflare can absorb a 1 Tbps attack without the kernel's TCP stack seeing a
single packet from the attack traffic. The XDP program is ~200 BPF instructions and runs
in under 1 µs per packet.

**Meta's Katran load balancer**: Meta's L4 load balancer (open-sourced as Katran) uses
XDP for encapsulation-based load balancing. Each packet is XDP-processed: the destination
backend is selected from a consistent-hash table stored in a BPF array, the packet is
encapsulated in an outer IP header, and `XDP_TX` transmits it. Katran processes 10M
packets per second on a single core. The previous iptables-based solution required 10
cores for the same throughput.

**Pixie's userspace tracing**: Pixie (acquired by New Relic) traces Go HTTP and gRPC
services without code instrumentation. It attaches uprobes to Go runtime functions
(`runtime.casgstatus`, `net/http.(*conn).serve`) to capture goroutine lifecycle events,
and to TLS implementation entry points to capture plaintext before encryption. The
captured data streams via BPF ring buffers to the Pixie edge collector. Teams can query
live traffic data in SQL without deploying anything to the application.

**Netflix's bpftrace deployment**: Netflix runs bpftrace (a high-level eBPF scripting
language) in production for on-demand performance analysis. When a service shows latency
anomalies, SREs attach bpftrace scripts to the running process to measure block device
latency histograms, TCP retransmit rates per connection, or scheduler run queue depth —
without restarting the service. The eBPF verifier guarantees these scripts cannot corrupt
kernel state.

## Complexity Analysis

| Operation | Latency | Notes |
|-----------|---------|-------|
| XDP packet drop | < 1 µs | Before sk_buff allocation |
| XDP with BPF map lookup | ~1–2 µs | Hash map lookup at line rate |
| tc ingress BPF | ~2–5 µs | After sk_buff; full packet access |
| kprobe BPF program | ~200–500 ns overhead | Per probe hit; replaces INT3 |
| tracepoint BPF program | ~50–200 ns overhead | Lighter than kprobe |
| BPF ringbuf write | ~30–100 ns | Lock-free if single-producer |
| BPF hash map lookup | ~50–200 ns | Similar to a userspace hash map |
| BPF verifier on load | 1–100 ms | Depends on program complexity |
| JIT compilation (once) | 1–10 ms | One-time cost per program load |

## Common Pitfalls

1. **Ignoring BTF (BPF Type Format) portability.** BPF programs that access kernel
   struct fields directly (e.g., `task_struct->pid`) are not portable across kernel
   versions because struct layouts change. BTF-based CO-RE (Compile Once, Run Everywhere)
   uses kernel BTF metadata to relocate struct field accesses at load time. Always use
   CO-RE for production programs.

2. **Forgetting the NULL check after map lookup.** `bpf_map_lookup_elem` can return
   NULL if the key is not in the map. The verifier will reject programs that dereference
   the return value without a NULL check — but the verifier error messages are often
   cryptic about why.

3. **Ring buffer backpressure.** If the userspace consumer does not drain the ring buffer
   fast enough, `bpf_ringbuf_reserve` returns NULL and events are dropped. The ring buffer
   does not block the BPF program. Add a `ringbuf_drop` counter (via a BPF counter map) to
   detect this in production.

4. **Using kprobes for stable interfaces.** kprobe targets are kernel function names,
   which change between versions. A kprobe on `tcp_sendmsg_locked` may fail to attach on
   a kernel that renamed the function. Use tracepoints (`tcp_send_reset`, `tcp_retransmit_skb`)
   for stable interfaces, or BTF-based kfuncs for verified stability.

5. **Exceeding the BPF instruction limit.** Classic BPF had a 4096-instruction limit;
   eBPF on Linux 5.3+ allows up to 1 million instructions with bounded loops. But the
   verifier's complexity (number of states to check) grows superlinearly with program size.
   Programs that inline large helper functions hit the complexity limit before the
   instruction limit. Use BPF tail calls (`bpf_tail_call`) to split large programs.

## Exercises

**Exercise 1** (30 min): Use `bpftrace` to write a one-liner that traces all `open()`
syscalls and prints the filename and PID. Observe the output for 60 seconds on a running
system. Then write the equivalent in `cilium/ebpf` Go code using a tracepoint.

**Exercise 2** (2–4 h): Implement a per-process syscall counter using a BPF hash map
keyed by PID. The BPF program attaches to `sys_enter` (the raw tracepoint) and increments
a counter. The userspace program reads the map every second and prints the top 10
syscall-heavy PIDs. Implement this in Go with `cilium/ebpf`.

**Exercise 3** (4–8 h): Implement a minimal XDP firewall: load a BPF hash map with
blocked source IP addresses. The XDP program checks each incoming packet's source IP
against the map and returns `XDP_DROP` for blocked IPs, `XDP_PASS` otherwise. The
userspace controller updates the map via CLI commands. Test with `tcpreplay` or `hping3`.

**Exercise 4** (8–15 h): Implement a latency histogram for `read()` and `write()` syscalls
using kretprobes. Track entry time in a per-PID BPF hash map (on `sys_enter_read`), compute
latency on return (on `sys_exit_read`), and store in a BPF histogram map (a power-of-two
bucket array). The userspace program renders the histogram every second. Implement in Rust
with `aya`.

## Further Reading

### Foundational Papers

- McCanne, S., Jacobson, V. (1993). "The BSD Packet Filter: A New Architecture for
  User-level Packet Capture." USENIX 1993. The original BPF paper — explains the virtual
  machine design and why it outperformed the then-current approaches.
- Starovoitov, A., Borkmann, D. (2014). "Demonstrating the power of eBPF." Linux Plumbers
  Conference 2014. The eBPF extension proposal; explains the register model expansion
  and verifier rationale.

### Books

- Gregg, B. "BPF Performance Tools" (Addison-Wesley, 2019). 880 pages of production BPF
  usage patterns — the definitive reference for tracing and observability with BPF.
- Calavera, D., Fontana, L. "Linux Observability with BPF" (O'Reilly, 2019). Covers
  program types, maps, and real-world use cases with C and Python examples.

### Production Code to Read

- Cilium's datapath: `pkg/datapath/linux/` — production XDP + tc BPF programs for Kubernetes
  networking. Among the most complex eBPF programs in production.
- Katran: `katran/lib/bpf/` — Meta's XDP load balancer BPF code.
- `cilium/ebpf` examples: `examples/` directory — minimal, well-commented examples for
  each program type.
- Aya examples: `examples/` in the aya repo — Rust-native eBPF for each program type.

### Conference Talks

- "eBPF in Production: Adventures in the BPF Wilderness" — eBPF Summit 2021. Cloudflare
  engineers describe their XDP DDoS mitigation architecture.
- "Making eBPF Programming Easier" — Linux Plumbers 2022. Design of the libbpf CO-RE
  mechanism and why it matters for portability.
- "Building a Production-Grade eBPF Load Balancer" — KubeCon 2020. Katran architecture,
  XDP performance numbers, and consistent hashing in BPF.
- "Aya: Compile Your eBPF Programs in Rust" — eBPF Summit 2021. Introduction to the
  Aya framework and why a Rust-native toolchain matters.
