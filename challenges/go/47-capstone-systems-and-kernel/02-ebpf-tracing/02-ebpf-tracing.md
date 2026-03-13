<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h
-->

# eBPF Tracing Tool

## The Challenge

Build a comprehensive eBPF-based tracing and observability tool in Go that can attach probes to kernel functions, tracepoints, and user-space functions to collect system-level telemetry without modifying the observed programs. eBPF (extended Berkeley Packet Filter) is a revolutionary Linux kernel technology that allows running sandboxed programs in kernel space, enabling zero-overhead observability, networking, and security features. Your tool must compile eBPF programs (written in restricted C), load them into the kernel, attach them to probe points, read events from ring buffers and perf event arrays, and display the collected data in real time. You will implement function call tracing, latency histograms, and network packet inspection.

## Requirements

1. Implement an eBPF program loader that reads compiled eBPF object files (ELF format), extracts program sections and map definitions, loads programs into the kernel via the `bpf()` system call, and creates eBPF maps (hash maps, arrays, ring buffers) for kernel-to-userspace communication.
2. Implement kprobe attachment: attach eBPF programs to kernel function entry and return points (kprobe/kretprobe) to trace function calls; demonstrate by tracing `sys_openat` to log every file open operation system-wide, capturing the filename, PID, and timestamp.
3. Implement tracepoint attachment: attach eBPF programs to kernel tracepoints (e.g., `sched:sched_process_exec`) to trace process execution, capturing the binary path, PID, PPID, and command-line arguments.
4. Build a latency histogram tool: attach kprobe/kretprobe pairs to a function (e.g., `vfs_read`), measure the time between entry and return using `bpf_ktime_get_ns()`, and aggregate latencies into an eBPF histogram map (log2 buckets) that is read and displayed by the userspace Go program.
5. Implement a ring buffer reader: use the `BPF_MAP_TYPE_RINGBUF` map type for efficient kernel-to-userspace event streaming, reading events in batches and processing them in Go with proper struct deserialization using `encoding/binary`.
6. Build a network packet filter: attach an eBPF program to a network interface using `BPF_PROG_TYPE_XDP` (eXpress Data Path) that counts packets by protocol (TCP, UDP, ICMP) and source IP, with the counters readable from userspace via an eBPF hash map.
7. Implement uprobe attachment: attach eBPF programs to user-space function entry points in a target binary (e.g., tracing `main.handleRequest` in a Go HTTP server) by parsing the target's ELF symbol table to find the function offset.
8. Provide a CLI interface with subcommands: `trace openat` (file opens), `trace exec` (process executions), `histogram vfs_read` (read latency), `netcount <interface>` (packet counts), and `uprobe <binary> <symbol>` (user-space function tracing).

## Hints

- Use the `cilium/ebpf` Go library for loading and managing eBPF programs and maps -- it provides a pure-Go ELF loader and map abstractions.
- Write eBPF C programs (compiled with `clang -target bpf`) and embed the compiled `.o` files using `//go:embed` or use `bpf2go` to generate Go bindings.
- eBPF programs have restrictions: no unbounded loops (verifier must prove termination), limited stack size (512 bytes), and only kernel-approved helper functions.
- For kprobes, use `bpf_probe_read_kernel()` to safely read kernel memory and `bpf_get_current_pid_tgid()` for the current PID.
- Ring buffer events should include a fixed-size header (event type, size) followed by event-specific data; deserialize in Go using `binary.Read` with a matching struct.
- XDP programs return verdicts: `XDP_PASS` (allow), `XDP_DROP` (discard), `XDP_TX` (bounce back). For counting, always return `XDP_PASS`.
- For uprobes, the target binary must not be stripped; use `debug/elf` to parse the symbol table.
- This exercise requires root privileges and a Linux kernel 5.7+ for ring buffer support.

## Success Criteria

1. The `trace openat` subcommand correctly logs every file open on the system, including PID and filename, verified by opening a file in another terminal.
2. The `trace exec` subcommand correctly logs every process execution with the binary path and arguments.
3. The latency histogram for `vfs_read` shows a plausible distribution with sub-microsecond reads (cache hits) and millisecond reads (disk I/O).
4. The ring buffer reader processes events without dropping any under a load of 10,000 events/sec.
5. The XDP packet counter correctly counts TCP, UDP, and ICMP packets on a network interface during a `ping` and `curl` test.
6. Uprobe tracing on a Go HTTP server correctly logs every call to the traced function with the PID and timestamp.
7. All eBPF programs pass the kernel verifier and load without errors.
8. The tool handles Ctrl+C gracefully, detaching probes and cleaning up maps on exit.

## Research Resources

- "BPF Performance Tools" (Gregg, 2019) -- comprehensive eBPF reference
- cilium/ebpf Go library -- https://github.com/cilium/ebpf
- BPF and XDP reference guide -- https://docs.cilium.io/en/latest/bpf/
- Linux kernel BPF documentation -- https://www.kernel.org/doc/html/latest/bpf/
- bpftrace -- https://github.com/bpftrace/bpftrace -- inspiration for tracing CLI design
- Brendan Gregg's BPF page -- https://www.brendangregg.com/ebpf.html
