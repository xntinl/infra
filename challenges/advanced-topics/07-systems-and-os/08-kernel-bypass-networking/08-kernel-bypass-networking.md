<!--
type: reference
difficulty: advanced
section: [07-systems-and-os]
concepts: [DPDK, AF_XDP, RDMA, SR-IOV, zero-copy, kernel-bypass, NVMe-oF, userspace-networking, poll-mode-driver]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [virtual-memory-and-paging, io-models, cpu-architecture-for-programmers, ebpf-programming]
papers: [Han2012-megapipe, Kaufmann2019-tas]
industry_use: [Cloudflare, HFT, Mellanox-RDMA, AWS-SRD, AMD-VirtIO, DPDK-OVS]
language_contrast: high
-->

# Kernel Bypass Networking

> When the Linux networking stack becomes the bottleneck — at 10 Gbps line rate or
> sub-10 µs latency requirements — kernel bypass moves packet processing entirely into
> userspace, eliminating every OS context switch on the data path.

## Mental Model

The Linux networking stack is a general-purpose design optimized for correctness,
feature richness, and kernel modularity. For each received packet it allocates an
`sk_buff` (the kernel's socket buffer descriptor), copies data from the NIC's DMA ring
into the `sk_buff`'s memory, traverses the netfilter hook chain, routes the packet, and
delivers it to a socket queue. This pipeline takes ~2–5 µs per packet on a modern kernel
and involves multiple memory copies and cache boundary crossings. At 100 Gbps (14.8M
64-byte packets/s), 5 µs per packet means one packet every 67 ns — but you have
`1000 ns / 67 ns = 14.9` ns between packets to do work. The kernel pipeline cannot keep
up: each 64-byte packet processed by the full Linux stack consumes ~800 ns of CPU time
on a single core.

Kernel bypass solves this by moving the NIC driver into userspace. The NIC's DMA rings
are mapped directly into userspace memory via `mmap`. User code polls the receive ring
without any syscall. Packets are processed in userspace without any kernel involvement.
The NIC's hardware offloads (RSS — Receive Side Scaling, checksum, segmentation) are
used where available. A single userspace thread running a poll-mode driver (PMD) on a
dedicated CPU core can sustain 10–50M packets/s.

There are three major kernel bypass mechanisms in production use:

1. **DPDK (Data Plane Development Kit)**: The C library approach. NIC vendors provide
   PMDs that bypass the kernel entirely. The CPU core runs a tight `rte_eth_rx_burst` poll
   loop. Zero copies, no syscalls, ~100 ns per packet. Used by Cloudflare, network
   equipment vendors, and high-frequency trading systems.

2. **AF_XDP**: A Linux kernel feature (5.0+) that provides a zero-copy path from the
   NIC's XDP layer to a userspace ring buffer. Less isolation than DPDK (still uses kernel
   drivers), but better integration with Linux networking, no need for DPDK's large pages
   pool, and supports in-kernel fallback. Used where DPDK's full NIC takeover is too
   aggressive.

3. **RDMA (Remote Direct Memory Access)**: A protocol + hardware feature where the NIC
   performs memory reads/writes to/from remote machine memory without involving the remote
   CPU. The NIC communicates directly with the remote machine's memory bus. Used for
   distributed storage (NVMe-oF, Lustre), high-frequency trading, and ML training
   (NCCL uses RDMA for GPU-to-GPU communication across machines).

The shared insight across all three: eliminate the OS from the data path. Packets travel
NIC → userspace memory → application, with no kernel code executing.

## Core Concepts

### DPDK Architecture

DPDK's Poll Mode Driver model:
```
CPU Core (dedicated, isolated with isolcpus)
  while (true):
    n = rte_eth_rx_burst(port, queue, mbufs, burst_size)
    for i in 0..n:
      process(mbufs[i])       // parse, classify, forward
    rte_eth_tx_burst(...)      // send processed packets
```

`mbufs` (memory buffers) come from a pre-allocated `rte_mempool` backed by huge pages.
No `malloc`, no syscalls, no locks on the fast path — just pointer manipulation in
userspace memory. DPDK's `rte_mempool` uses a lock-free ring of free mbufs, one ring
per CPU socket, backed by 2 MB huge pages to minimize TLB pressure.

Key DPDK concepts:
- **EAL (Environment Abstraction Layer)**: initializes DPDK, binds NICs, allocates huge
  page memory, and pins threads to cores.
- **PMD (Poll Mode Driver)**: vendor-supplied or generic driver that maps NIC registers
  and DMA rings into userspace.
- **Mempool**: NUMA-aware, huge-page-backed, lock-free pool of fixed-size mbufs.
- **mbuf**: a descriptor for one packet, with a pointer to the packet data, length,
  offload flags, and metadata.
- **RSS (Receive Side Scaling)**: NIC hardware distributes packets across multiple
  receive queues based on a hash of the 5-tuple, enabling multi-core packet processing
  without locks.

### AF_XDP (Address Family XDP)

AF_XDP is a socket type that bypasses the TCP/IP stack for packets matched by an XDP
program. The setup:

1. Create a `UMEM` (userspace memory region) backed by a large `mmap`'d buffer.
2. Create an `AF_XDP` socket and bind it to a NIC queue.
3. Write `XDP_REDIRECT` rules in a BPF program that direct certain packets to the AF_XDP
   socket.
4. Consume packets from the `RX ring` and produce packets on the `TX ring`.

AF_XDP rings are implemented as shared memory between the kernel and userspace using
`mmap`. Packet data lives in the UMEM; the rings just hold offsets into the UMEM. A
packet reception requires no memory copy — the kernel DMA-writes into the UMEM directly.
AF_XDP achieves ~3–5M packets/s per core, compared to DPDK's 10–50M — slower because
AF_XDP still uses kernel XDP hooks and requires some interaction with the kernel driver.

AF_XDP zero-copy mode (`XDP_ZEROCOPY`) requires NIC driver support (Intel ixgbe, i40e,
mlx5 support it). Without zero-copy, there is still a copy from the NIC DMA buffer to
the UMEM, but the TCP/IP stack is still bypassed.

### RDMA

RDMA allows a remote NIC to directly read from or write to a host's memory without
involving the host CPU. The operations:
- **RDMA Write**: write data from local memory to remote memory. Remote CPU is not
  interrupted.
- **RDMA Read**: read data from remote memory into local memory. Remote CPU is not
  involved.
- **Send/Receive**: traditional message-passing with CPU involvement at both ends.

RDMA requires `verbs` API (libibverbs, or rdma-core on Linux). Work Requests are posted
to a Queue Pair (QP); the NIC processes them and posts Work Completions to a Completion
Queue (CQ). The application polls the CQ for completions.

AWS's "Scalable Reliable Datagram" (SRD) protocol (used by EFA — Elastic Fabric
Adapter) provides RDMA-like semantics over Ethernet at scale. ML training on AWS uses
EFA for NCCL all-reduce operations.

### SR-IOV (Single Root I/O Virtualization)

SR-IOV is a PCIe hardware feature that allows a physical NIC to present multiple virtual
functions (VFs) to the OS. Each VF looks like an independent NIC with its own DMA engine
and interrupt lines. Kubernetes uses SR-IOV to give pods direct, isolated access to
physical NIC bandwidth without going through the host's vSwitch.

## Implementation: Go

```go
// +build linux

// AF_XDP example in Go using the cilium/ebpf + xdp libraries.
// For a production AF_XDP implementation, use:
//   github.com/asavie/xdp  (pure Go AF_XDP bindings)
//   github.com/tsg/gopacket (packet parsing)
//
// This example shows the AF_XDP socket setup and packet reception loop.
// It requires root and a compatible NIC.
//
// go get github.com/asavie/xdp@latest
// go get github.com/vishvananda/netlink@latest

package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// umem represents the AF_XDP userspace memory region.
// Packets are DMA'd directly into this memory by the NIC (with zero-copy NIC support).
type umem struct {
	data []byte
	fd   int // UMEM file descriptor
}

// xdpSocket wraps an AF_XDP socket.
type xdpSocket struct {
	fd   int
	umem *umem
}

// xdpRing is the shared ring descriptor between kernel and userspace.
// The kernel and userspace share this ring via mmap.
type xdpRing struct {
	producer *uint32 // kernel increments this on packet arrival
	consumer *uint32 // userspace increments this after consuming
	flags    *uint32
	desc     unsafe.Pointer // array of packet descriptors
	size     uint32
}

const (
	frameSize  = 2048         // bytes per UMEM frame
	frameCount = 1024         // number of frames in UMEM
	umemSize   = frameSize * frameCount
)

// rawAfXdpSetup demonstrates the system calls needed to set up AF_XDP.
// A complete implementation requires handling the fill ring, completion ring,
// TX ring, and RX ring — this shows the conceptual sequence.
func rawAfXdpSetup(ifindex int, queueID int) error {
	// 1. Create UMEM: mmap a large anonymous region for packet data.
	umemMem, err := syscall.Mmap(
		-1, 0, umemSize,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANONYMOUS|syscall.MAP_PRIVATE,
	)
	if err != nil {
		return fmt.Errorf("mmap UMEM: %w", err)
	}
	fmt.Printf("UMEM: %d bytes at %p\n", len(umemMem), &umemMem[0])

	// 2. Create AF_XDP socket.
	sockFd, err := unix.Socket(unix.AF_XDP, unix.SOCK_RAW, 0)
	if err != nil {
		syscall.Munmap(umemMem)
		return fmt.Errorf("AF_XDP socket: %w", err)
	}
	fmt.Printf("AF_XDP socket: fd=%d\n", sockFd)

	// 3. Register UMEM with the socket via XDP_UMEM_REG setsockopt.
	// This tells the kernel to use our mmap'd memory for packet data.
	type xdpUmemReg struct {
		addr     uint64
		len      uint64
		chunkSize uint32
		headroom uint32
		flags    uint32
	}
	reg := xdpUmemReg{
		addr:     uint64(uintptr(unsafe.Pointer(&umemMem[0]))),
		len:      uint64(len(umemMem)),
		chunkSize: frameSize,
		headroom:  0,
	}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_SETSOCKOPT,
		uintptr(sockFd),
		unix.SOL_XDP,
		unix.XDP_UMEM_REG,
		uintptr(unsafe.Pointer(&reg)),
		unsafe.Sizeof(reg),
		0,
	)
	if errno != 0 {
		unix.Close(sockFd)
		syscall.Munmap(umemMem)
		return fmt.Errorf("XDP_UMEM_REG: %w", errno)
	}
	fmt.Println("UMEM registered with AF_XDP socket")

	// 4. Set up the fill ring (we provide empty frames to the kernel).
	// The kernel fills these frames with incoming packets via DMA.
	fillRingSize := uint32(frameCount)
	_, _, errno = syscall.Syscall6(
		syscall.SYS_SETSOCKOPT,
		uintptr(sockFd),
		unix.SOL_XDP,
		unix.XDP_UMEM_FILL_RING,
		uintptr(unsafe.Pointer(&fillRingSize)),
		4,
		0,
	)
	if errno != 0 {
		unix.Close(sockFd)
		syscall.Munmap(umemMem)
		return fmt.Errorf("XDP_UMEM_FILL_RING: %w", errno)
	}
	fmt.Printf("Fill ring created: %d slots\n", fillRingSize)

	// 5. Bind the socket to a specific NIC queue.
	// After binding, packets matching our XDP BPF program (XDP_REDIRECT) arrive here.
	type sockaddrXDP struct {
		family   uint16
		flags    uint16
		ifindex  uint32
		queueID  uint32
		sharedUmemFD uint32
	}
	addr := sockaddrXDP{
		family:  unix.AF_XDP,
		flags:   0, // XDP_ZEROCOPY if supported
		ifindex: uint32(ifindex),
		queueID: uint32(queueID),
	}
	_, _, errno = syscall.Syscall(
		syscall.SYS_BIND,
		uintptr(sockFd),
		uintptr(unsafe.Pointer(&addr)),
		unsafe.Sizeof(addr),
	)
	if errno != 0 {
		unix.Close(sockFd)
		syscall.Munmap(umemMem)
		return fmt.Errorf("bind AF_XDP: %w", errno)
	}
	fmt.Printf("AF_XDP bound to ifindex=%d queue=%d\n", ifindex, queueID)

	unix.Close(sockFd)
	syscall.Munmap(umemMem)
	return nil
}

// parseEthernetHeader parses a raw Ethernet frame header.
// Used in the packet processing loop to identify packet type.
func parseEthernetHeader(frame []byte) (dstMAC, srcMAC net.HardwareAddr, etherType uint16) {
	if len(frame) < 14 {
		return nil, nil, 0
	}
	dstMAC = net.HardwareAddr(frame[0:6])
	srcMAC = net.HardwareAddr(frame[6:12])
	etherType = binary.BigEndian.Uint16(frame[12:14])
	return
}

// packetStats tracks per-type packet counts for reporting.
type packetStats struct {
	total   uint64
	ipv4    uint64
	ipv6    uint64
	arp     uint64
	other   uint64
	dropped uint64
}

func (s *packetStats) process(frame []byte) {
	s.total++
	_, _, etherType := parseEthernetHeader(frame)
	switch etherType {
	case 0x0800:
		s.ipv4++
	case 0x86DD:
		s.ipv6++
	case 0x0806:
		s.arp++
	default:
		s.other++
	}
}

func main() {
	fmt.Println("=== Kernel Bypass Networking Demo ===\n")

	// Demonstrate the AF_XDP setup on loopback (lo, ifindex 1).
	// This will fail without the AF_XDP XDP program attached, but shows the API.
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		fmt.Println("Cannot find loopback interface:", err)
		// Fall through to show what we can demonstrate.
	} else {
		fmt.Printf("Interface: %s (index=%d)\n", iface.Name, iface.Index)
		if err := rawAfXdpSetup(iface.Index, 0); err != nil {
			fmt.Println("AF_XDP setup (requires root + NIC support):", err)
		}
	}

	// Demonstrate packet parsing (the hot path in a bypass stack).
	fmt.Println("\n--- Packet Parsing (hot path) ---")
	// Synthesize an ARP frame.
	arpFrame := []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst: broadcast
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, // src: fake MAC
		0x08, 0x06,                           // ethertype: ARP
		0x00, 0x01, 0x08, 0x00, 0x06, 0x04, 0x00, 0x01, // ARP header
	}
	stats := &packetStats{}
	for i := 0; i < 1_000_000; i++ {
		stats.process(arpFrame)
	}
	fmt.Printf("Processed %d packets: IPv4=%d IPv6=%d ARP=%d Other=%d\n",
		stats.total, stats.ipv4, stats.ipv6, stats.arp, stats.other)

	// Set up signal handler for clean shutdown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("\nDone (or press Ctrl+C).")
	select {
	case <-sig:
	default:
	}
}
```

### Go-specific considerations

Go's standard library does not provide AF_XDP or DPDK bindings. Options:

1. **`asavie/xdp`**: A pure Go AF_XDP implementation with a clean API for creating UMEM,
   setting up XDP programs, and receiving/transmitting packets. Best choice for Go services
   that need kernel bypass without DPDK.

2. **DPDK via CGo**: Wrap `rte_eth_rx_burst` and `rte_eth_tx_burst` in a CGo interface.
   The overhead of CGo calls (~100 ns per call) is significant compared to the ~10 ns
   per-packet budget at 100 Gbps; batch calls of 64 packets per CGo boundary are typical.

3. **AF_XDP via `cilium/ebpf` + raw syscalls**: `cilium/ebpf` provides the XDP attachment;
   the AF_XDP socket management is done with `golang.org/x/sys/unix`.

For RDMA in Go, the `go-rdma/rdma` project provides verbs bindings via CGo. For most
Go services, the practical path to RDMA is not the Go application layer but rather using
RDMA-capable infrastructure (e.g., AWS EFA, InfiniBand) at the network level while keeping
the application in standard TCP/IP.

## Implementation: Rust

```rust
// AF_XDP implementation in Rust using xsk-rs (AF_XDP bindings).
// DPDK bindings via dpdk-sys are also available but require the DPDK library.
//
// Add to Cargo.toml:
//   xsk-rs = "0.4"           -- AF_XDP Rust bindings
//   libc = "0.2"
//   pnet = "0.34"            -- packet parsing
//
// Run as root: sudo cargo run

use std::net::Ipv4Addr;
use std::time::{Duration, Instant};

// --- AF_XDP setup using xsk-rs ---
// xsk-rs provides safe Rust bindings for AF_XDP sockets.
// It handles UMEM creation, ring setup, and the send/receive loops.

// Note: This is a conceptual outline. Real xsk-rs usage requires
// binding to a specific network interface and queue.

#[cfg(target_os = "linux")]
mod afxdp {
    use std::num::NonZeroU32;

    // Represents a zero-copy AF_XDP packet processor.
    // In production, use xsk_rs::Socket and xsk_rs::Umem directly.
    pub struct AfXdpReceiver {
        interface: String,
        queue_id: u32,
        frame_count: u32,
        frame_size: u32,
    }

    impl AfXdpReceiver {
        pub fn new(interface: &str, queue_id: u32) -> Self {
            Self {
                interface: interface.to_string(),
                queue_id,
                frame_count: 1024,
                frame_size: 2048,
            }
        }

        pub fn umem_size(&self) -> usize {
            (self.frame_count * self.frame_size) as usize
        }

        // In a real implementation, this would:
        // 1. xsk_umem__create() — register UMEM with the kernel
        // 2. xsk_socket__create() — create and bind the AF_XDP socket
        // 3. Populate the fill ring with empty frames
        pub fn setup_description(&self) -> String {
            format!(
                "AF_XDP: interface={}, queue={}, UMEM={}KB ({} frames x {} bytes)",
                self.interface,
                self.queue_id,
                self.umem_size() / 1024,
                self.frame_count,
                self.frame_size,
            )
        }
    }
}

// --- Packet parsing: the per-packet hot path ---
// In a kernel bypass stack, this runs millions of times per second.
// Every allocation, branch, and cache miss counts.

#[repr(C, packed)]
struct EthernetHeader {
    dst_mac: [u8; 6],
    src_mac: [u8; 6],
    ether_type: u16,
}

#[repr(C, packed)]
struct Ipv4Header {
    version_ihl: u8,
    tos: u8,
    total_len: u16,
    id: u16,
    flags_frag: u16,
    ttl: u8,
    protocol: u8,
    checksum: u16,
    src: u32,
    dst: u32,
}

#[inline(always)]
fn parse_ethernet(frame: &[u8]) -> Option<(u16, &[u8])> {
    if frame.len() < 14 {
        return None;
    }
    // Use from_be_bytes to handle endianness correctly without runtime branches.
    let ether_type = u16::from_be_bytes([frame[12], frame[13]]);
    Some((ether_type, &frame[14..]))
}

#[inline(always)]
fn parse_ipv4(payload: &[u8]) -> Option<(Ipv4Addr, Ipv4Addr, u8)> {
    if payload.len() < 20 {
        return None;
    }
    let ihl = (payload[0] & 0x0f) as usize * 4;
    if payload.len() < ihl {
        return None;
    }
    let src = Ipv4Addr::new(payload[12], payload[13], payload[14], payload[15]);
    let dst = Ipv4Addr::new(payload[16], payload[17], payload[18], payload[19]);
    let proto = payload[9];
    Some((src, dst, proto))
}

// PacketStats tracks per-type counts without any heap allocation.
// This is the correct pattern for high-throughput packet counters.
struct PacketStats {
    total: u64,
    ipv4: u64,
    ipv6: u64,
    arp: u64,
    other: u64,
}

impl PacketStats {
    const fn new() -> Self {
        Self { total: 0, ipv4: 0, ipv6: 0, arp: 0, other: 0 }
    }

    #[inline(always)]
    fn process(&mut self, frame: &[u8]) {
        self.total += 1;
        if let Some((etype, _payload)) = parse_ethernet(frame) {
            match etype {
                0x0800 => self.ipv4 += 1,
                0x86DD => self.ipv6 += 1,
                0x0806 => self.arp += 1,
                _ => self.other += 1,
            }
        }
    }
}

// --- RDMA concept: demonstrates the queue pair (QP) model ---
// In a real RDMA program, you would use rdma-core (libibverbs) via FFI.
// Here we show the conceptual work request pattern.

struct WorkRequest {
    id: u64,
    opcode: WrOpcode,
    local_addr: u64,
    length: u32,
    remote_addr: u64,  // for RDMA ops
    remote_key: u32,   // memory region key for remote access
}

#[derive(Debug)]
enum WrOpcode {
    Send,
    Recv,
    RdmaWrite, // write to remote memory without involving remote CPU
    RdmaRead,  // read from remote memory without involving remote CPU
}

fn rdma_conceptual_demo() {
    // A RDMA Write work request: we ask the NIC to copy local_buffer → remote_addr.
    // The remote CPU is NOT interrupted. The NIC handles the transfer end-to-end.
    let wr = WorkRequest {
        id: 42,
        opcode: WrOpcode::RdmaWrite,
        local_addr: 0x1000,  // local buffer address (from registered MR)
        length: 4096,
        remote_addr: 0x2000, // remote buffer address (from exchanged MR info)
        remote_key: 0xdeadbeef,
    };
    println!(
        "RDMA WR: opcode={:?} local=0x{:x} len={} remote=0x{:x} rkey=0x{:x}",
        wr.opcode, wr.local_addr, wr.length, wr.remote_addr, wr.remote_key
    );
    // In real libibverbs code:
    //   ibv_post_send(qp, &wr, &bad_wr)  -- submit to NIC hardware queue
    //   ibv_poll_cq(cq, 1, &wc)          -- poll completion queue
    //   assert!(wc.status == IBV_WC_SUCCESS)
}

fn main() {
    println!("=== Kernel Bypass Networking (Rust) ===\n");

    // AF_XDP receiver description.
    #[cfg(target_os = "linux")]
    {
        use afxdp::AfXdpReceiver;
        let receiver = AfXdpReceiver::new("eth0", 0);
        println!("{}", receiver.setup_description());
        println!("(Actual socket bind requires root + xsk-rs + loaded XDP program)\n");
    }

    // Hot path packet parsing benchmark.
    println!("=== Packet Parsing Throughput ===");
    let mut stats = PacketStats::new();

    // Synthesize a realistic IPv4 frame.
    let ipv4_frame: Vec<u8> = vec![
        // Ethernet header
        0x00, 0x1c, 0xbf, 0x0a, 0x2b, 0x3c, // dst MAC
        0x52, 0x54, 0x00, 0xab, 0xcd, 0xef, // src MAC
        0x08, 0x00,                           // EtherType: IPv4
        // IPv4 header
        0x45, 0x00, 0x00, 0x28,               // version+IHL, TOS, total len
        0x00, 0x01, 0x00, 0x00, 0x40, 0x06,  // id, flags, TTL=64, proto=TCP
        0x00, 0x00,                           // checksum (0 for demo)
        192, 168, 1, 1,                       // src IP
        192, 168, 1, 2,                       // dst IP
    ];

    let n = 10_000_000u64;
    let start = Instant::now();
    for _ in 0..n {
        stats.process(&ipv4_frame);
    }
    let elapsed = start.elapsed();

    println!(
        "Parsed {} packets in {:?} ({:.0} Mpps)",
        n,
        elapsed,
        n as f64 / elapsed.as_secs_f64() / 1_000_000.0
    );
    println!(
        "Stats: total={} ipv4={} ipv6={} arp={} other={}",
        stats.total, stats.ipv4, stats.ipv6, stats.arp, stats.other
    );

    // RDMA conceptual demo.
    println!("\n=== RDMA Work Request Model ===");
    rdma_conceptual_demo();

    // NUMA-aware DPDK concept.
    println!("\n=== DPDK / Kernel Bypass Architecture Summary ===");
    println!("DPDK poll mode driver flow:");
    println!("  CPU core (isolated) → rte_eth_rx_burst → process mbuf → rte_eth_tx_burst");
    println!("  No syscalls, no interrupts, no kernel TCP/IP stack.");
    println!("  NIC DMA → huge-page mempool → userspace thread.");
    println!("  Throughput: 10–50 Mpps per core at 100 Gbps.");

    println!("\nAF_XDP flow (lighter-weight bypass):");
    println!("  XDP BPF program → XDP_REDIRECT → AF_XDP socket → UMEM → userspace");
    println!("  Zero-copy with compatible NICs (Intel ixgbe/i40e/mlx5).");
    println!("  Throughput: 3–10 Mpps per core at 10–25 Gbps.");

    println!("\nDone.");
}
```

### Rust-specific considerations

The `dpdk-sys` crate provides raw FFI bindings to the DPDK C library. For production
use, wrapping the raw bindings in a safe Rust API is non-trivial: DPDK's mbuf pool uses
raw pointers extensively, and lifetime management for mbufs requires careful design.
The `capsule` project (Rust DPDK framework) provides a higher-level, idiomatic API.

For AF_XDP, `xsk-rs` is the most complete pure Rust implementation. It provides safe
wrappers for UMEM creation, socket binding, and ring management. The `#[repr(C)]`
attribute on socket address structures is critical: AF_XDP socket addresses must match
the kernel's C struct layout exactly.

For RDMA, Rust's `rdma-core` crate provides bindings to libibverbs. The
`async-rdma` crate provides an async API for RDMA operations built on tokio. Both are
niche and less mature than the C verbs API, but the Rust ownership model maps well to
RDMA's concept of registered memory regions (MRs) — the region's registration lifetime
maps naturally to Rust's lifetime system.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| AF_XDP library | `asavie/xdp` (pure Go) | `xsk-rs` |
| DPDK | CGo wrapper (`capsule` / custom) | `dpdk-sys` + `capsule` |
| RDMA | CGo libibverbs wrappers | `rdma-core` or `async-rdma` |
| Zero-copy safety | Manual (slices into UMEM) | Lifetime tracking for UMEM frames |
| Polling loop style | `for { rx(...) }` goroutine | `loop { rx_burst(...) }` thread |
| NUMA-aware huge pages | CGo or raw `mmap(MAP_HUGETLB)` | `libc::mmap(MAP_HUGETLB)` |
| Packet parsing | Manual byte slicing | `pnet` crate or manual |
| Production maturity | `asavie/xdp` is production-ready | `xsk-rs` production-ready; `capsule` experimental |
| GC interaction | GC cannot move pinned UMEM frames | No GC; memory is stable |
| DPDK huge page pool | Requires CAP_SYS_ADMIN + hugepages | Same requirements |

A critical Go limitation: the GC may scan and move heap objects. UMEM frames used by
AF_XDP or DPDK must not be moved by the GC, which means they must be allocated outside
the Go heap (via `mmap` or CGo). In Rust, there is no GC and `Box`/`Vec` allocations
are stable in memory. This makes Rust significantly more ergonomic for kernel bypass
programming: you do not need to fight the runtime to prevent it from moving your packet
buffers.

## Production War Stories

**Cloudflare's kernel bypass DDoS mitigation**: Cloudflare's gen-6 DDoS mitigation
stack combines XDP (for filtering in kernel context) with AF_XDP (for more complex
processing in userspace). The XDP program handles the fast path: drop packets from
known-bad sources, redirect everything else to AF_XDP sockets. The AF_XDP path handles
rate limiting, connection tracking, and challenge generation — work that requires more
than 1000 BPF instructions. The combined stack processes ~20M packets/s per server,
sustaining absorption of 1+ Tbps DDoS attacks.

**High-frequency trading and DPDK**: A major HFT firm replaced their kernel network
stack with DPDK for their market data feed processing. The kernel UDP receive path added
~15 µs of jitter (due to soft IRQ processing, socket buffer locking, and system call
overhead). DPDK reduced the jitter to ~200 ns by polling the NIC ring directly from a
dedicated CPU core. The firm reported that at their tick sizes, the 15 µs improvement
was worth $2–5M/year in reduced adverse selection.

**NCCL and RDMA for ML training**: NVIDIA's NCCL (Collective Communications Library)
uses RDMA (via RoCE or InfiniBand) for GPU-to-GPU all-reduce operations during
distributed ML training. Without RDMA, copying a 10 GB gradient tensor to another host
takes ~800 ms over 100 Gbps Ethernet (including CPU involvement). With RDMA, the NIC
copies directly from one GPU's memory to another GPU's memory via GPUDirect RDMA,
achieving ~300 ms. At 1000-GPU training jobs running 24/7, this is the difference
between 3-week and 7-week training runs.

**AWS and SRD (Scalable Reliable Datagram)**: AWS's Elastic Fabric Adapter (EFA) uses
SRD, a custom protocol with RDMA-like semantics over Ethernet. EFA allows AWS ML
training instances (P4, P5) to communicate at 400 Gbps with microsecond latency.
SRD differs from standard RDMA by using multipath routing and handling congestion
explicitly, making it more suitable for large-scale EC2 deployments than traditional
InfiniBand.

## Complexity Analysis

| Operation | Latency | Throughput |
|-----------|---------|------------|
| Linux kernel TCP recv | ~5–20 µs | ~2M pps/core (40 Gbps) |
| epoll-based recv (bypassing TCP) | ~2–5 µs | ~4M pps/core |
| AF_XDP (generic mode) | ~1–2 µs | ~5M pps/core |
| AF_XDP (zero-copy mode) | ~500 ns | ~10M pps/core |
| DPDK (Intel ixgbe, 10G) | ~100–200 ns | ~15M pps/core |
| DPDK (Intel i40e, 25G) | ~80–150 ns | ~20M pps/core |
| DPDK (Mellanox mlx5, 100G) | ~50–100 ns | ~50M pps/core |
| RDMA Write (loopback) | ~1–2 µs | ~20 GB/s |
| RDMA Write (remote, InfiniBand) | ~2–5 µs | ~12 GB/s (100 Gbps) |
| NVMe-oF over RDMA (4KB read) | ~10–30 µs | 1M+ IOPS |
| NVMe local (4KB read) | ~50–200 µs | 1–2M IOPS |

The key metric for kernel bypass selection: if your application processes fewer than
1M packets/s, the Linux kernel stack is fine. Between 1M and 10M pps, AF_XDP is the
right tradeoff. Above 10M pps or below 1 µs latency requirements, DPDK is necessary.

## Common Pitfalls

1. **Running DPDK with THP disabled.** DPDK's mempool requires huge pages to minimize
   TLB pressure during packet processing. Without huge pages, the packet buffer pool
   spans thousands of 4 KB pages, causing TLB thrashing and dropping performance to
   kernel levels. Pre-allocate huge pages before DPDK init:
   `echo 1024 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages`.

2. **Not isolating DPDK poll cores with `isolcpus`.** The kernel scheduler may preempt
   a DPDK PMD thread to run another task. A single preemption on a 10 Gbps NIC causes
   ~15,000 packets to queue in the NIC ring during the ~150 µs context switch. Add
   `isolcpus=2,3,4,5` to the kernel command line and use `rte_eal_init` with core affinity
   to prevent preemption.

3. **Forgetting to pin AF_XDP UMEM frames before DMA.** The kernel must know the physical
   addresses of UMEM frames for DMA. On systems where IOMMU is enabled, the UMEM must
   be pinned (not swappable) and registered with the IOMMU. Failures here show up as
   silent packet drops, not errors.

4. **Using RDMA without memory registration (MR) semantics.** RDMA operations require
   both local and remote buffers to be registered as Memory Regions. The MR registration
   pins the physical pages and gives the NIC direct DMA access. Using unregistered memory
   in an RDMA verb causes SIGSEGV or hardware errors. In Rust, the lifetime of an MR
   must outlive all RDMA operations that reference it.

5. **Ignoring RSS (Receive Side Scaling) queue count.** AF_XDP and DPDK both work per-
   queue. If your NIC has 4 RSS queues and you only process queue 0, you are missing
   3/4 of the traffic. The XDP BPF program must either process all queues or redirect
   all traffic to a single queue before AF_XDP picks it up.

## Exercises

**Exercise 1** (30 min): Install `dpdk` and run `testpmd` on a virtual machine with a
TAP device. Observe the `Forward statistics` output. Calculate the per-core packet rate
and compare against Linux `iperf` UDP performance on the same VM.

**Exercise 2** (2–4 h): Implement an AF_XDP packet counter in Go using `asavie/xdp`.
Attach a passthrough XDP program (`XDP_PASS`) to a loopback or virtual interface, set
up an AF_XDP socket, and count packets by EtherType in the receive loop. Send test
traffic with `hping3` or `tcpreplay` and verify the counts.

**Exercise 3** (4–8 h): Implement a minimal AF_XDP-based rate limiter in Rust using
`xsk-rs`. The XDP BPF program redirects all UDP traffic to the AF_XDP socket. The
userspace Rust program tracks packet rate per source IP (using a hash map), drops
packets above a threshold by not re-injecting them into the TX ring, and passes
compliant traffic. Test with `iperf3 -u`.

**Exercise 4** (8–15 h): Build an AF_XDP load balancer: the XDP program selects a
backend from a consistent-hash BPF map, and AF_XDP userspace performs the actual
DNAT (destination NAT) by rewriting the packet's destination IP and recalculating
the IP checksum, then transmitting via the AF_XDP TX ring. Implement in Rust with
`xsk-rs` and `aya` for the BPF map management. Benchmark throughput against a
standard Linux `iptables` DNAT rule.

## Further Reading

### Foundational Papers

- Han, S. et al. (2012). "MegaPipe: A New Programming Interface for Scalable Network
  I/O." OSDI 2012. Motivates kernel bypass for web servers and shows epoll's limitations.
- Kaufmann, A. et al. (2019). "TAS: TCP Acceleration as an OS Service." EuroSys 2019.
  TCP offload to a kernel-bypass service; relevant RDMA comparison data.
- Rodrigues, R. et al. (2004). "Rosetta: Fast Kernel Bypass Networking." SOSP 2003 WIP.
  Early description of the kernel bypass approach.

### Books

- Levin, J. "MacOS and iOS Internals" (2nd ed.) — Chapter on IOKit for macOS equivalent
  of DPDK (different architecture, same concepts).
- Kerrisk, M. "The Linux Programming Interface" — Chapter 61 (Sockets: Advanced Topics)
  for `sendmsg`/`recvmsg` with control messages, zerocopy semantics.
- DPDK documentation: https://doc.dpdk.org/guides/ — the authoritative reference for
  DPDK EAL, mempool, and PMD APIs.

### Production Code to Read

- `asavie/xdp` — Go AF_XDP library with clean examples.
- `xsk-rs/examples/` — Rust AF_XDP examples with UMEM setup and ring management.
- Cloudflare's `l4drop` — open-source XDP + AF_XDP DDoS drop tool.
- Meta's Katran — open-source DPDK-based L4 load balancer.
- `DPDK/examples/l2fwd/` — the canonical "learning switch" DPDK example.

### Conference Talks

- "AF_XDP: A New Zero-Copy Socket Interface for Linux" — Linux Plumbers 2018. Original
  AF_XDP design from Björn Töpel and Magnus Karlsson (Intel).
- "DPDK Summit 2019: Cloudflare's DDoS Mitigation Stack" — Cloudflare's hybrid
  XDP + DPDK architecture for absorbing Tbps attacks.
- "RDMA and GPU Direct" — SC19. How ML frameworks use RDMA for distributed training.
- "Building a 100 Gbps Load Balancer with AF_XDP" — Linux Foundation 2022. Engineering
  walkthrough of moving from DPDK to AF_XDP for better operational simplicity.
