# Solution: TCP/IP Stack Userspace

## Architecture Overview

The stack is organized into five layers with clean interfaces between them:

1. **Device Layer** (`tun.rs`) -- reads/writes raw IP packets from a TUN device
2. **Network Layer** (`ip.rs`, `arp.rs`, `icmp.rs`) -- IPv4 parsing/construction, ARP resolution, ICMP handling
3. **Transport Layer** (`tcp/`) -- full TCP implementation with state machine, congestion control, retransmission
4. **Socket Layer** (`socket.rs`) -- application-facing API (`connect`, `send`, `recv`, `close`)
5. **Dispatch** (`stack.rs`) -- main event loop that routes packets between layers

```
Application
    | (Socket API: connect, send, recv, close)
    v
Socket Layer (TcpSocket)
    |
    v
TCP Engine (per-connection state machines)
    |   ^
    v   |
IP Layer (parse, construct, checksum, route)
    |   ^
    v   |
   ARP Cache <--> ARP Protocol
    |
    v
TUN Device (/dev/net/tun)
    |
    v
Linux Kernel (IP routing to real network)
```

## Rust Solution

### `Cargo.toml`

```toml
[package]
name = "tcp-ip-stack"
version = "0.1.0"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
log = "0.4"
env_logger = "0.11"
rand = "0.8"
byteorder = "1"

[dev-dependencies]
tokio-test = "0.4"
```

### `src/checksum.rs`

```rust
/// Internet checksum (RFC 1071) used by IP, TCP, ICMP.
pub fn internet_checksum(data: &[u8]) -> u16 {
    let mut sum = 0u32;
    let mut i = 0;
    while i + 1 < data.len() {
        sum += u32::from(u16::from_be_bytes([data[i], data[i + 1]]));
        i += 2;
    }
    if i < data.len() {
        sum += u32::from(data[i]) << 8;
    }
    fold_checksum(sum)
}

/// Incremental checksum update (RFC 1624) for modifying a single 16-bit word.
pub fn update_checksum(old_checksum: u16, old_word: u16, new_word: u16) -> u16 {
    let sum = u32::from(!old_checksum)
        + u32::from(!old_word)
        + u32::from(new_word);
    !fold_checksum(sum)
}

fn fold_checksum(mut sum: u32) -> u16 {
    while sum >> 16 != 0 {
        sum = (sum & 0xFFFF) + (sum >> 16);
    }
    !(sum as u16)
}

/// Compute TCP checksum with pseudo-header.
pub fn tcp_checksum(src_ip: [u8; 4], dst_ip: [u8; 4], tcp_segment: &[u8]) -> u16 {
    let tcp_len = tcp_segment.len() as u16;
    let mut pseudo = Vec::with_capacity(12 + tcp_segment.len());
    pseudo.extend_from_slice(&src_ip);
    pseudo.extend_from_slice(&dst_ip);
    pseudo.push(0);
    pseudo.push(6); // TCP
    pseudo.extend_from_slice(&tcp_len.to_be_bytes());
    pseudo.extend_from_slice(tcp_segment);
    internet_checksum(&pseudo)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn checksum_validates_to_zero() {
        let data = vec![0x45, 0x00, 0x00, 0x28, 0xAB, 0xCD, 0x00, 0x00,
                        0x40, 0x06, 0x00, 0x00, 0x0A, 0x00, 0x00, 0x01,
                        0x0A, 0x00, 0x00, 0x02];
        let cksum = internet_checksum(&data);
        let mut with_cksum = data.clone();
        with_cksum[10] = (cksum >> 8) as u8;
        with_cksum[11] = cksum as u8;
        assert_eq!(internet_checksum(&with_cksum), 0);
    }
}
```

### `src/ip.rs`

```rust
use crate::checksum::internet_checksum;
use std::net::Ipv4Addr;

pub const PROTO_ICMP: u8 = 1;
pub const PROTO_TCP: u8 = 6;

#[derive(Debug, Clone)]
pub struct Ipv4Packet {
    pub version: u8,
    pub ihl: u8,
    pub dscp: u8,
    pub total_length: u16,
    pub identification: u16,
    pub dont_fragment: bool,
    pub more_fragments: bool,
    pub fragment_offset: u16,
    pub ttl: u8,
    pub protocol: u8,
    pub checksum: u16,
    pub src: Ipv4Addr,
    pub dst: Ipv4Addr,
    pub payload: Vec<u8>,
}

impl Ipv4Packet {
    pub fn parse(buf: &[u8]) -> Result<Self, &'static str> {
        if buf.len() < 20 {
            return Err("packet too short for IPv4");
        }
        let version = buf[0] >> 4;
        if version != 4 {
            return Err("not IPv4");
        }
        let ihl = buf[0] & 0x0F;
        let header_len = (ihl as usize) * 4;
        let total_length = u16::from_be_bytes([buf[2], buf[3]]);

        if buf.len() < total_length as usize {
            return Err("buffer shorter than total_length");
        }

        let flags_frag = u16::from_be_bytes([buf[6], buf[7]]);

        Ok(Self {
            version,
            ihl,
            dscp: buf[1],
            total_length,
            identification: u16::from_be_bytes([buf[4], buf[5]]),
            dont_fragment: flags_frag & 0x4000 != 0,
            more_fragments: flags_frag & 0x2000 != 0,
            fragment_offset: flags_frag & 0x1FFF,
            ttl: buf[8],
            protocol: buf[9],
            checksum: u16::from_be_bytes([buf[10], buf[11]]),
            src: Ipv4Addr::new(buf[12], buf[13], buf[14], buf[15]),
            dst: Ipv4Addr::new(buf[16], buf[17], buf[18], buf[19]),
            payload: buf[header_len..total_length as usize].to_vec(),
        })
    }

    pub fn to_bytes(&self) -> Vec<u8> {
        let mut buf = vec![0u8; 20 + self.payload.len()];
        buf[0] = (4 << 4) | 5;
        buf[1] = self.dscp;
        let total = (20 + self.payload.len()) as u16;
        buf[2..4].copy_from_slice(&total.to_be_bytes());
        buf[4..6].copy_from_slice(&self.identification.to_be_bytes());

        let mut flags_frag: u16 = self.fragment_offset & 0x1FFF;
        if self.dont_fragment { flags_frag |= 0x4000; }
        if self.more_fragments { flags_frag |= 0x2000; }
        buf[6..8].copy_from_slice(&flags_frag.to_be_bytes());

        buf[8] = self.ttl;
        buf[9] = self.protocol;
        buf[12..16].copy_from_slice(&self.src.octets());
        buf[16..20].copy_from_slice(&self.dst.octets());
        buf[20..].copy_from_slice(&self.payload);

        let cksum = internet_checksum(&buf[..20]);
        buf[10..12].copy_from_slice(&cksum.to_be_bytes());

        buf
    }

    pub fn new(src: Ipv4Addr, dst: Ipv4Addr, protocol: u8, payload: Vec<u8>) -> Self {
        Self {
            version: 4,
            ihl: 5,
            dscp: 0,
            total_length: 0, // Computed in to_bytes
            identification: rand::random(),
            dont_fragment: true,
            more_fragments: false,
            fragment_offset: 0,
            ttl: 64,
            protocol,
            checksum: 0,
            src,
            dst,
            payload,
        }
    }

    pub fn verify_checksum(&self) -> bool {
        // Reconstruct header and verify
        let mut header = vec![0u8; 20];
        header[0] = (self.version << 4) | self.ihl;
        header[1] = self.dscp;
        header[2..4].copy_from_slice(&self.total_length.to_be_bytes());
        header[4..6].copy_from_slice(&self.identification.to_be_bytes());
        header[8] = self.ttl;
        header[9] = self.protocol;
        header[10..12].copy_from_slice(&self.checksum.to_be_bytes());
        header[12..16].copy_from_slice(&self.src.octets());
        header[16..20].copy_from_slice(&self.dst.octets());
        internet_checksum(&header) == 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip() {
        let pkt = Ipv4Packet::new(
            Ipv4Addr::new(10, 0, 0, 1),
            Ipv4Addr::new(10, 0, 0, 2),
            PROTO_TCP,
            vec![0xDE, 0xAD, 0xBE, 0xEF],
        );
        let bytes = pkt.to_bytes();
        let parsed = Ipv4Packet::parse(&bytes).unwrap();
        assert_eq!(parsed.src, Ipv4Addr::new(10, 0, 0, 1));
        assert_eq!(parsed.dst, Ipv4Addr::new(10, 0, 0, 2));
        assert_eq!(parsed.protocol, PROTO_TCP);
        assert_eq!(parsed.payload, vec![0xDE, 0xAD, 0xBE, 0xEF]);
        assert!(parsed.verify_checksum());
    }
}
```

### `src/arp.rs`

```rust
use std::collections::HashMap;
use std::net::Ipv4Addr;
use std::time::{Duration, Instant};

const ARP_CACHE_TIMEOUT: Duration = Duration::from_secs(300);

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct MacAddr([u8; 6]);

impl MacAddr {
    pub const BROADCAST: Self = Self([0xFF; 6]);
    pub const ZERO: Self = Self([0; 6]);

    pub fn new(bytes: [u8; 6]) -> Self { Self(bytes) }
    pub fn as_bytes(&self) -> &[u8; 6] { &self.0 }
}

impl std::fmt::Display for MacAddr {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{:02x}:{:02x}:{:02x}:{:02x}:{:02x}:{:02x}",
            self.0[0], self.0[1], self.0[2], self.0[3], self.0[4], self.0[5])
    }
}

#[derive(Debug, Clone)]
pub struct ArpPacket {
    pub hardware_type: u16,
    pub protocol_type: u16,
    pub hw_len: u8,
    pub proto_len: u8,
    pub operation: u16, // 1 = request, 2 = reply
    pub sender_hw: MacAddr,
    pub sender_ip: Ipv4Addr,
    pub target_hw: MacAddr,
    pub target_ip: Ipv4Addr,
}

pub const ARP_REQUEST: u16 = 1;
pub const ARP_REPLY: u16 = 2;

impl ArpPacket {
    pub fn parse(buf: &[u8]) -> Result<Self, &'static str> {
        if buf.len() < 28 {
            return Err("ARP packet too short");
        }
        Ok(Self {
            hardware_type: u16::from_be_bytes([buf[0], buf[1]]),
            protocol_type: u16::from_be_bytes([buf[2], buf[3]]),
            hw_len: buf[4],
            proto_len: buf[5],
            operation: u16::from_be_bytes([buf[6], buf[7]]),
            sender_hw: MacAddr::new([buf[8], buf[9], buf[10], buf[11], buf[12], buf[13]]),
            sender_ip: Ipv4Addr::new(buf[14], buf[15], buf[16], buf[17]),
            target_hw: MacAddr::new([buf[18], buf[19], buf[20], buf[21], buf[22], buf[23]]),
            target_ip: Ipv4Addr::new(buf[24], buf[25], buf[26], buf[27]),
        })
    }

    pub fn to_bytes(&self) -> Vec<u8> {
        let mut buf = vec![0u8; 28];
        buf[0..2].copy_from_slice(&self.hardware_type.to_be_bytes());
        buf[2..4].copy_from_slice(&self.protocol_type.to_be_bytes());
        buf[4] = self.hw_len;
        buf[5] = self.proto_len;
        buf[6..8].copy_from_slice(&self.operation.to_be_bytes());
        buf[8..14].copy_from_slice(self.sender_hw.as_bytes());
        buf[14..18].copy_from_slice(&self.sender_ip.octets());
        buf[18..24].copy_from_slice(self.target_hw.as_bytes());
        buf[24..28].copy_from_slice(&self.target_ip.octets());
        buf
    }

    pub fn new_request(sender_hw: MacAddr, sender_ip: Ipv4Addr, target_ip: Ipv4Addr) -> Self {
        Self {
            hardware_type: 1,     // Ethernet
            protocol_type: 0x0800, // IPv4
            hw_len: 6,
            proto_len: 4,
            operation: ARP_REQUEST,
            sender_hw,
            sender_ip,
            target_hw: MacAddr::ZERO,
            target_ip,
        }
    }

    pub fn new_reply(sender_hw: MacAddr, sender_ip: Ipv4Addr, target_hw: MacAddr, target_ip: Ipv4Addr) -> Self {
        Self {
            hardware_type: 1,
            protocol_type: 0x0800,
            hw_len: 6,
            proto_len: 4,
            operation: ARP_REPLY,
            sender_hw,
            sender_ip,
            target_hw,
            target_ip,
        }
    }
}

struct ArpEntry {
    mac: MacAddr,
    expires: Instant,
}

pub struct ArpCache {
    entries: HashMap<Ipv4Addr, ArpEntry>,
    pending: HashMap<Ipv4Addr, Vec<Vec<u8>>>, // Packets waiting for resolution
}

impl ArpCache {
    pub fn new() -> Self {
        Self {
            entries: HashMap::new(),
            pending: HashMap::new(),
        }
    }

    pub fn lookup(&self, ip: &Ipv4Addr) -> Option<MacAddr> {
        self.entries.get(ip).and_then(|entry| {
            if entry.expires > Instant::now() {
                Some(entry.mac)
            } else {
                None
            }
        })
    }

    pub fn insert(&mut self, ip: Ipv4Addr, mac: MacAddr) {
        self.entries.insert(ip, ArpEntry {
            mac,
            expires: Instant::now() + ARP_CACHE_TIMEOUT,
        });
    }

    pub fn queue_packet(&mut self, ip: Ipv4Addr, packet: Vec<u8>) {
        self.pending.entry(ip).or_default().push(packet);
    }

    pub fn drain_pending(&mut self, ip: &Ipv4Addr) -> Vec<Vec<u8>> {
        self.pending.remove(ip).unwrap_or_default()
    }

    pub fn evict_expired(&mut self) {
        let now = Instant::now();
        self.entries.retain(|_, entry| entry.expires > now);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn arp_round_trip() {
        let pkt = ArpPacket::new_request(
            MacAddr::new([0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF]),
            Ipv4Addr::new(10, 0, 0, 1),
            Ipv4Addr::new(10, 0, 0, 2),
        );
        let bytes = pkt.to_bytes();
        let parsed = ArpPacket::parse(&bytes).unwrap();
        assert_eq!(parsed.operation, ARP_REQUEST);
        assert_eq!(parsed.sender_ip, Ipv4Addr::new(10, 0, 0, 1));
        assert_eq!(parsed.target_ip, Ipv4Addr::new(10, 0, 0, 2));
    }

    #[test]
    fn cache_insert_and_lookup() {
        let mut cache = ArpCache::new();
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        let mac = MacAddr::new([1, 2, 3, 4, 5, 6]);
        cache.insert(ip, mac);
        assert_eq!(cache.lookup(&ip), Some(mac));
    }

    #[test]
    fn cache_pending_drain() {
        let mut cache = ArpCache::new();
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        cache.queue_packet(ip, vec![1, 2, 3]);
        cache.queue_packet(ip, vec![4, 5, 6]);
        let pending = cache.drain_pending(&ip);
        assert_eq!(pending.len(), 2);
        assert!(cache.drain_pending(&ip).is_empty());
    }
}
```

### `src/icmp.rs`

```rust
use crate::checksum::internet_checksum;

pub const ICMP_ECHO_REQUEST: u8 = 8;
pub const ICMP_ECHO_REPLY: u8 = 0;
pub const ICMP_DEST_UNREACHABLE: u8 = 3;
pub const ICMP_TIME_EXCEEDED: u8 = 11;

#[derive(Debug, Clone)]
pub struct IcmpPacket {
    pub icmp_type: u8,
    pub code: u8,
    pub checksum: u16,
    pub rest: [u8; 4], // Identifier + Sequence for echo, varies for others
    pub payload: Vec<u8>,
}

impl IcmpPacket {
    pub fn parse(buf: &[u8]) -> Result<Self, &'static str> {
        if buf.len() < 8 {
            return Err("ICMP packet too short");
        }
        Ok(Self {
            icmp_type: buf[0],
            code: buf[1],
            checksum: u16::from_be_bytes([buf[2], buf[3]]),
            rest: [buf[4], buf[5], buf[6], buf[7]],
            payload: buf[8..].to_vec(),
        })
    }

    pub fn to_bytes(&self) -> Vec<u8> {
        let mut buf = vec![0u8; 8 + self.payload.len()];
        buf[0] = self.icmp_type;
        buf[1] = self.code;
        buf[4..8].copy_from_slice(&self.rest);
        buf[8..].copy_from_slice(&self.payload);

        let cksum = internet_checksum(&buf);
        buf[2..4].copy_from_slice(&cksum.to_be_bytes());
        buf
    }

    pub fn echo_reply(request: &IcmpPacket) -> Self {
        Self {
            icmp_type: ICMP_ECHO_REPLY,
            code: 0,
            checksum: 0,
            rest: request.rest,
            payload: request.payload.clone(),
        }
    }

    pub fn dest_unreachable(code: u8, original_packet: &[u8]) -> Self {
        // Include IP header + first 8 bytes of original datagram
        let payload_len = original_packet.len().min(28);
        Self {
            icmp_type: ICMP_DEST_UNREACHABLE,
            code,
            checksum: 0,
            rest: [0; 4],
            payload: original_packet[..payload_len].to_vec(),
        }
    }

    pub fn identifier(&self) -> u16 {
        u16::from_be_bytes([self.rest[0], self.rest[1]])
    }

    pub fn sequence(&self) -> u16 {
        u16::from_be_bytes([self.rest[2], self.rest[3]])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn echo_reply_preserves_id_and_seq() {
        let request = IcmpPacket {
            icmp_type: ICMP_ECHO_REQUEST,
            code: 0,
            checksum: 0,
            rest: [0x00, 0x01, 0x00, 0x0A], // id=1, seq=10
            payload: b"ping data".to_vec(),
        };
        let reply = IcmpPacket::echo_reply(&request);
        assert_eq!(reply.icmp_type, ICMP_ECHO_REPLY);
        assert_eq!(reply.identifier(), 1);
        assert_eq!(reply.sequence(), 10);
        assert_eq!(reply.payload, b"ping data");
    }

    #[test]
    fn checksum_valid() {
        let pkt = IcmpPacket {
            icmp_type: ICMP_ECHO_REPLY,
            code: 0,
            checksum: 0,
            rest: [0, 1, 0, 1],
            payload: b"test".to_vec(),
        };
        let bytes = pkt.to_bytes();
        assert_eq!(internet_checksum(&bytes), 0, "ICMP checksum should validate");
    }
}
```

### `src/tcp/segment.rs`

```rust
use crate::checksum::tcp_checksum;

pub const FIN: u8 = 0x01;
pub const SYN: u8 = 0x02;
pub const RST: u8 = 0x04;
pub const PSH: u8 = 0x08;
pub const ACK: u8 = 0x10;

#[derive(Debug, Clone)]
pub struct TcpSegment {
    pub src_port: u16,
    pub dst_port: u16,
    pub seq: u32,
    pub ack: u32,
    pub data_offset: u8,
    pub flags: u8,
    pub window: u16,
    pub checksum: u16,
    pub urgent: u16,
    pub options: Vec<u8>,
    pub payload: Vec<u8>,
}

/// TCP option for MSS.
pub const TCP_OPT_MSS: u8 = 2;
pub const TCP_OPT_MSS_LEN: u8 = 4;

impl TcpSegment {
    pub fn parse(buf: &[u8]) -> Result<Self, &'static str> {
        if buf.len() < 20 {
            return Err("segment too short");
        }
        let data_offset = buf[12] >> 4;
        let header_len = (data_offset as usize) * 4;
        if buf.len() < header_len {
            return Err("buffer shorter than data offset");
        }

        let options = if header_len > 20 {
            buf[20..header_len].to_vec()
        } else {
            vec![]
        };

        Ok(Self {
            src_port: u16::from_be_bytes([buf[0], buf[1]]),
            dst_port: u16::from_be_bytes([buf[2], buf[3]]),
            seq: u32::from_be_bytes([buf[4], buf[5], buf[6], buf[7]]),
            ack: u32::from_be_bytes([buf[8], buf[9], buf[10], buf[11]]),
            data_offset,
            flags: buf[13] & 0x3F,
            window: u16::from_be_bytes([buf[14], buf[15]]),
            checksum: u16::from_be_bytes([buf[16], buf[17]]),
            urgent: u16::from_be_bytes([buf[18], buf[19]]),
            options,
            payload: buf[header_len..].to_vec(),
        })
    }

    pub fn to_bytes(&self, src_ip: [u8; 4], dst_ip: [u8; 4]) -> Vec<u8> {
        let options_len = self.options.len();
        let padding = (4 - (options_len % 4)) % 4;
        let header_len = 20 + options_len + padding;
        let data_offset = (header_len / 4) as u8;

        let mut buf = vec![0u8; header_len + self.payload.len()];
        buf[0..2].copy_from_slice(&self.src_port.to_be_bytes());
        buf[2..4].copy_from_slice(&self.dst_port.to_be_bytes());
        buf[4..8].copy_from_slice(&self.seq.to_be_bytes());
        buf[8..12].copy_from_slice(&self.ack.to_be_bytes());
        buf[12] = data_offset << 4;
        buf[13] = self.flags;
        buf[14..16].copy_from_slice(&self.window.to_be_bytes());
        buf[18..20].copy_from_slice(&self.urgent.to_be_bytes());

        if !self.options.is_empty() {
            buf[20..20 + options_len].copy_from_slice(&self.options);
        }

        buf[header_len..].copy_from_slice(&self.payload);

        let cksum = tcp_checksum(src_ip, dst_ip, &buf);
        buf[16..18].copy_from_slice(&cksum.to_be_bytes());

        buf
    }

    pub fn has_flag(&self, flag: u8) -> bool {
        self.flags & flag != 0
    }

    /// Extract MSS from options if present.
    pub fn mss(&self) -> Option<u16> {
        let mut i = 0;
        while i < self.options.len() {
            match self.options[i] {
                0 => break,            // End of options
                1 => { i += 1; }       // NOP
                TCP_OPT_MSS if i + 3 < self.options.len() => {
                    return Some(u16::from_be_bytes([self.options[i + 2], self.options[i + 3]]));
                }
                kind => {
                    if i + 1 >= self.options.len() { break; }
                    let len = self.options[i + 1] as usize;
                    if len < 2 { break; }
                    i += len;
                    continue;
                }
            }
            i += 1;
        }
        None
    }

    /// Build MSS option bytes.
    pub fn mss_option(mss: u16) -> Vec<u8> {
        vec![TCP_OPT_MSS, TCP_OPT_MSS_LEN, (mss >> 8) as u8, mss as u8]
    }

    pub fn build(
        src_port: u16, dst_port: u16,
        seq: u32, ack: u32,
        flags: u8, window: u16,
        options: Vec<u8>, payload: Vec<u8>,
    ) -> Self {
        Self {
            src_port, dst_port, seq, ack,
            data_offset: 5, // Updated in to_bytes
            flags, window,
            checksum: 0, urgent: 0,
            options, payload,
        }
    }
}

/// Sequence number comparison (wrapping-safe).
pub fn seq_lt(a: u32, b: u32) -> bool {
    (a.wrapping_sub(b) as i32) < 0
}

pub fn seq_lte(a: u32, b: u32) -> bool {
    a == b || seq_lt(a, b)
}

pub fn seq_gt(a: u32, b: u32) -> bool {
    seq_lt(b, a)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn segment_round_trip() {
        let seg = TcpSegment::build(8080, 80, 1000, 2000, SYN | ACK, 65535, vec![], vec![]);
        let bytes = seg.to_bytes([10, 0, 0, 1], [10, 0, 0, 2]);
        let parsed = TcpSegment::parse(&bytes).unwrap();
        assert_eq!(parsed.src_port, 8080);
        assert_eq!(parsed.dst_port, 80);
        assert!(parsed.has_flag(SYN));
        assert!(parsed.has_flag(ACK));
    }

    #[test]
    fn mss_option_parsing() {
        let options = TcpSegment::mss_option(1460);
        let seg = TcpSegment::build(80, 80, 0, 0, SYN, 65535, options, vec![]);
        assert_eq!(seg.mss(), Some(1460));
    }

    #[test]
    fn sequence_wraparound() {
        assert!(seq_lt(u32::MAX - 5, 5));
        assert!(!seq_lt(5, u32::MAX - 5));
    }
}
```

### `src/tcp/connection.rs`

```rust
use super::segment::*;
use super::congestion::CongestionControl;
use super::rtt::RttEstimator;
use std::collections::VecDeque;
use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TcpState {
    Closed, Listen, SynSent, SynReceived, Established,
    FinWait1, FinWait2, CloseWait, Closing, LastAck, TimeWait,
}

struct UnackedSegment {
    seq: u32,
    len: u32, // payload length + SYN/FIN consumption
    data: Vec<u8>,
    flags: u8,
    sent_at: Instant,
    retransmitted: bool,
}

pub struct TcpConnection {
    pub state: TcpState,
    pub local_port: u16,
    pub remote_port: u16,

    // Send sequence space
    snd_una: u32,
    snd_nxt: u32,
    snd_wnd: u32,
    iss: u32,

    // Receive sequence space
    rcv_nxt: u32,
    rcv_wnd: u32,
    irs: u32,

    // MSS
    mss: u16,

    // Buffers
    send_buf: VecDeque<u8>,
    recv_buf: VecDeque<u8>,

    // Retransmission
    unacked: Vec<UnackedSegment>,
    rtt: RttEstimator,
    congestion: CongestionControl,

    // Duplicate ACK counter (for fast retransmit)
    dup_ack_count: u32,
    last_ack_received: u32,

    // TIME_WAIT
    time_wait_start: Option<Instant>,
}

impl TcpConnection {
    pub fn new(local_port: u16) -> Self {
        let iss = generate_isn();
        Self {
            state: TcpState::Closed,
            local_port,
            remote_port: 0,
            snd_una: iss,
            snd_nxt: iss,
            snd_wnd: 0,
            iss,
            rcv_nxt: 0,
            rcv_wnd: 65535,
            irs: 0,
            mss: 536, // Default MSS
            send_buf: VecDeque::new(),
            recv_buf: VecDeque::new(),
            unacked: Vec::new(),
            rtt: RttEstimator::new(),
            congestion: CongestionControl::new(),
            dup_ack_count: 0,
            last_ack_received: iss,
            time_wait_start: None,
        }
    }

    pub fn connect(&mut self, remote_port: u16) -> Option<TcpSegment> {
        if self.state != TcpState::Closed { return None; }
        self.remote_port = remote_port;
        self.state = TcpState::SynSent;
        self.snd_nxt = self.iss.wrapping_add(1);

        let options = TcpSegment::mss_option(1460);
        Some(TcpSegment::build(
            self.local_port, self.remote_port,
            self.iss, 0, SYN, self.rcv_wnd as u16,
            options, vec![],
        ))
    }

    pub fn listen(&mut self) {
        self.state = TcpState::Listen;
    }

    pub fn on_segment(&mut self, seg: &TcpSegment) -> Vec<TcpSegment> {
        let mut out = Vec::new();
        match self.state {
            TcpState::Closed => {
                if !seg.has_flag(RST) {
                    out.push(self.build_rst(seg));
                }
            }
            TcpState::Listen => {
                if seg.has_flag(SYN) {
                    self.remote_port = seg.src_port;
                    self.irs = seg.seq;
                    self.rcv_nxt = seg.seq.wrapping_add(1);
                    self.snd_wnd = seg.window as u32;
                    if let Some(mss) = seg.mss() { self.mss = mss; }

                    let options = TcpSegment::mss_option(1460);
                    out.push(TcpSegment::build(
                        self.local_port, self.remote_port,
                        self.iss, self.rcv_nxt,
                        SYN | ACK, self.rcv_wnd as u16,
                        options, vec![],
                    ));
                    self.snd_nxt = self.iss.wrapping_add(1);
                    self.state = TcpState::SynReceived;
                }
            }
            TcpState::SynSent => {
                if seg.has_flag(SYN) && seg.has_flag(ACK) {
                    self.irs = seg.seq;
                    self.rcv_nxt = seg.seq.wrapping_add(1);
                    self.snd_una = seg.ack;
                    self.snd_wnd = seg.window as u32;
                    if let Some(mss) = seg.mss() { self.mss = mss; }
                    self.congestion.set_mss(self.mss);
                    self.state = TcpState::Established;

                    out.push(TcpSegment::build(
                        self.local_port, self.remote_port,
                        self.snd_nxt, self.rcv_nxt,
                        ACK, self.rcv_wnd as u16,
                        vec![], vec![],
                    ));
                }
            }
            TcpState::SynReceived => {
                if seg.has_flag(ACK) && seg.ack == self.snd_nxt {
                    self.snd_una = seg.ack;
                    self.congestion.set_mss(self.mss);
                    self.state = TcpState::Established;
                }
            }
            TcpState::Established => {
                if seg.has_flag(RST) { self.state = TcpState::Closed; return out; }
                if seg.has_flag(ACK) { self.process_ack(seg, &mut out); }
                if !seg.payload.is_empty() { self.process_data(seg, &mut out); }
                if seg.has_flag(FIN) {
                    self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                    self.state = TcpState::CloseWait;
                    out.push(self.build_ack());
                }
            }
            TcpState::FinWait1 => {
                if seg.has_flag(ACK) { self.process_ack(seg, &mut out); }
                if seg.has_flag(FIN) {
                    self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                    if seg.has_flag(ACK) && seg.ack == self.snd_nxt {
                        self.state = TcpState::TimeWait;
                        self.time_wait_start = Some(Instant::now());
                    } else {
                        self.state = TcpState::Closing;
                    }
                    out.push(self.build_ack());
                } else if seg.has_flag(ACK) && seg.ack == self.snd_nxt {
                    self.state = TcpState::FinWait2;
                }
            }
            TcpState::FinWait2 => {
                if seg.has_flag(FIN) {
                    self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                    self.state = TcpState::TimeWait;
                    self.time_wait_start = Some(Instant::now());
                    out.push(self.build_ack());
                }
            }
            TcpState::CloseWait => {
                if seg.has_flag(ACK) { self.process_ack(seg, &mut out); }
            }
            TcpState::Closing => {
                if seg.has_flag(ACK) {
                    self.state = TcpState::TimeWait;
                    self.time_wait_start = Some(Instant::now());
                }
            }
            TcpState::LastAck => {
                if seg.has_flag(ACK) { self.state = TcpState::Closed; }
            }
            TcpState::TimeWait => {
                if seg.has_flag(FIN) {
                    out.push(self.build_ack());
                    self.time_wait_start = Some(Instant::now());
                }
            }
        }
        out
    }

    pub fn close(&mut self) -> Option<TcpSegment> {
        match self.state {
            TcpState::Established => {
                self.state = TcpState::FinWait1;
                let fin = TcpSegment::build(
                    self.local_port, self.remote_port,
                    self.snd_nxt, self.rcv_nxt,
                    FIN | ACK, self.rcv_wnd as u16, vec![], vec![],
                );
                self.snd_nxt = self.snd_nxt.wrapping_add(1);
                Some(fin)
            }
            TcpState::CloseWait => {
                self.state = TcpState::LastAck;
                let fin = TcpSegment::build(
                    self.local_port, self.remote_port,
                    self.snd_nxt, self.rcv_nxt,
                    FIN | ACK, self.rcv_wnd as u16, vec![], vec![],
                );
                self.snd_nxt = self.snd_nxt.wrapping_add(1);
                Some(fin)
            }
            _ => None,
        }
    }

    pub fn send(&mut self, data: &[u8]) -> Vec<TcpSegment> {
        if self.state != TcpState::Established { return vec![]; }
        self.send_buf.extend(data);
        self.flush_send()
    }

    pub fn recv(&mut self, max: usize) -> Vec<u8> {
        let n = max.min(self.recv_buf.len());
        self.recv_buf.drain(..n).collect()
    }

    pub fn retransmit_expired(&mut self) -> Vec<TcpSegment> {
        let rto = self.rtt.rto();
        let now = Instant::now();
        let mut retransmits = Vec::new();

        for unacked in &mut self.unacked {
            if now.duration_since(unacked.sent_at) >= rto {
                // RTO timeout: back to slow start
                self.congestion.on_timeout();
                retransmits.push(TcpSegment::build(
                    self.local_port, self.remote_port,
                    unacked.seq, self.rcv_nxt,
                    unacked.flags | ACK, self.rcv_wnd as u16,
                    vec![], unacked.data.clone(),
                ));
                unacked.sent_at = now;
                unacked.retransmitted = true;
            }
        }
        retransmits
    }

    pub fn check_time_wait(&mut self) -> bool {
        if let Some(start) = self.time_wait_start {
            if start.elapsed() >= Duration::from_secs(120) { // 2*MSL
                self.state = TcpState::Closed;
                return true;
            }
        }
        false
    }

    fn process_ack(&mut self, seg: &TcpSegment, out: &mut Vec<TcpSegment>) {
        let ack = seg.ack;
        self.snd_wnd = seg.window as u32;

        if ack == self.last_ack_received && seq_lt(ack, self.snd_nxt) {
            self.dup_ack_count += 1;

            // Fast retransmit on 3 duplicate ACKs
            if self.dup_ack_count == 3 {
                self.congestion.on_fast_retransmit();
                if let Some(unacked) = self.unacked.first() {
                    out.push(TcpSegment::build(
                        self.local_port, self.remote_port,
                        unacked.seq, self.rcv_nxt,
                        unacked.flags | ACK, self.rcv_wnd as u16,
                        vec![], unacked.data.clone(),
                    ));
                }
            } else if self.dup_ack_count > 3 {
                self.congestion.on_dup_ack();
            }
        } else if seq_gt(ack, self.snd_una) {
            // New ACK
            let bytes_acked = ack.wrapping_sub(self.snd_una);

            // RTT measurement (Karn's algorithm: only measure non-retransmitted)
            if let Some(unacked) = self.unacked.first() {
                if !unacked.retransmitted && seq_lte(unacked.seq.wrapping_add(unacked.len), ack) {
                    let rtt = unacked.sent_at.elapsed();
                    self.rtt.update(rtt);
                }
            }

            self.snd_una = ack;
            self.last_ack_received = ack;
            self.dup_ack_count = 0;

            self.unacked.retain(|u| seq_gt(u.seq.wrapping_add(u.len), ack));

            if self.dup_ack_count >= 3 {
                self.congestion.on_new_ack_after_recovery();
            } else {
                self.congestion.on_ack(bytes_acked as usize);
            }

            // Try sending more data
            let new_segments = self.flush_send();
            out.extend(new_segments);
        }
    }

    fn process_data(&mut self, seg: &TcpSegment, out: &mut Vec<TcpSegment>) {
        if seg.seq == self.rcv_nxt {
            self.recv_buf.extend(&seg.payload);
            self.rcv_nxt = self.rcv_nxt.wrapping_add(seg.payload.len() as u32);
            out.push(self.build_ack());
        } else {
            // Out of order: send duplicate ACK
            out.push(self.build_ack());
        }
    }

    fn flush_send(&mut self) -> Vec<TcpSegment> {
        let mut segments = Vec::new();
        let mss = self.mss as usize;

        while !self.send_buf.is_empty() {
            let in_flight = self.snd_nxt.wrapping_sub(self.snd_una) as usize;
            let cwnd = self.congestion.window();
            let rwnd = self.snd_wnd as usize;
            let effective_window = cwnd.min(rwnd);

            if in_flight >= effective_window { break; }
            let available = effective_window - in_flight;
            let send_size = mss.min(self.send_buf.len()).min(available);
            if send_size == 0 { break; }

            let data: Vec<u8> = self.send_buf.drain(..send_size).collect();
            let seg = TcpSegment::build(
                self.local_port, self.remote_port,
                self.snd_nxt, self.rcv_nxt,
                ACK | PSH, self.rcv_wnd as u16,
                vec![], data.clone(),
            );

            self.unacked.push(UnackedSegment {
                seq: self.snd_nxt,
                len: data.len() as u32,
                data,
                flags: PSH,
                sent_at: Instant::now(),
                retransmitted: false,
            });

            self.snd_nxt = self.snd_nxt.wrapping_add(send_size as u32);
            segments.push(seg);
        }
        segments
    }

    fn build_ack(&self) -> TcpSegment {
        TcpSegment::build(
            self.local_port, self.remote_port,
            self.snd_nxt, self.rcv_nxt,
            ACK, self.rcv_wnd as u16, vec![], vec![],
        )
    }

    fn build_rst(&self, seg: &TcpSegment) -> TcpSegment {
        TcpSegment::build(
            self.local_port, seg.src_port,
            seg.ack, 0, RST, 0, vec![], vec![],
        )
    }
}

fn generate_isn() -> u32 {
    use std::time::SystemTime;
    let t = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();
    ((t.as_micros() & 0xFFFF_FFFF) as u32).wrapping_mul(250_000)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn handshake_and_data() {
        let mut server = TcpConnection::new(80);
        let mut client = TcpConnection::new(54321);
        server.listen();

        let syn = client.connect(80).unwrap();
        let resp = server.on_segment(&syn);
        let syn_ack = &resp[0];
        let resp = client.on_segment(syn_ack);
        server.on_segment(&resp[0]);

        assert_eq!(client.state, TcpState::Established);
        assert_eq!(server.state, TcpState::Established);

        let segs = client.send(b"hello");
        for s in &segs { server.on_segment(s); }
        assert_eq!(server.recv(10), b"hello");
    }
}
```

### `src/tcp/congestion.rs`

```rust
/// TCP Reno congestion control.
pub struct CongestionControl {
    cwnd: usize,      // congestion window in bytes
    ssthresh: usize,  // slow start threshold
    mss: usize,       // maximum segment size
    in_recovery: bool,
}

impl CongestionControl {
    pub fn new() -> Self {
        Self {
            cwnd: 1460,     // Start at 1 MSS
            ssthresh: 65535,
            mss: 1460,
            in_recovery: false,
        }
    }

    pub fn set_mss(&mut self, mss: u16) {
        self.mss = mss as usize;
        if self.cwnd < self.mss {
            self.cwnd = self.mss;
        }
    }

    pub fn window(&self) -> usize {
        self.cwnd
    }

    /// Called when a new ACK arrives (not a duplicate).
    pub fn on_ack(&mut self, bytes_acked: usize) {
        if self.cwnd < self.ssthresh {
            // Slow start: increase cwnd by MSS for each ACK
            self.cwnd += self.mss;
            log::trace!("slow start: cwnd={} ssthresh={}", self.cwnd, self.ssthresh);
        } else {
            // Congestion avoidance: increase cwnd by MSS * MSS / cwnd per ACK
            let increment = (self.mss * self.mss) / self.cwnd;
            self.cwnd += increment.max(1);
            log::trace!("congestion avoidance: cwnd={}", self.cwnd);
        }
        self.in_recovery = false;
    }

    /// Called on triple duplicate ACK (fast retransmit).
    pub fn on_fast_retransmit(&mut self) {
        self.ssthresh = (self.cwnd / 2).max(2 * self.mss);
        self.cwnd = self.ssthresh + 3 * self.mss;
        self.in_recovery = true;
        log::debug!("fast retransmit: cwnd={} ssthresh={}", self.cwnd, self.ssthresh);
    }

    /// Called for each additional duplicate ACK during fast recovery.
    pub fn on_dup_ack(&mut self) {
        if self.in_recovery {
            self.cwnd += self.mss;
        }
    }

    /// Called when new ACK arrives after fast recovery.
    pub fn on_new_ack_after_recovery(&mut self) {
        self.cwnd = self.ssthresh;
        self.in_recovery = false;
        log::debug!("exiting fast recovery: cwnd={}", self.cwnd);
    }

    /// Called on RTO timeout: back to slow start.
    pub fn on_timeout(&mut self) {
        self.ssthresh = (self.cwnd / 2).max(2 * self.mss);
        self.cwnd = self.mss;
        self.in_recovery = false;
        log::debug!("timeout: cwnd={} ssthresh={}", self.cwnd, self.ssthresh);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn slow_start_doubles() {
        let mut cc = CongestionControl::new();
        let initial = cc.window();
        cc.on_ack(1460); // ACK 1 segment
        assert_eq!(cc.window(), initial + 1460);
    }

    #[test]
    fn congestion_avoidance_linear() {
        let mut cc = CongestionControl::new();
        cc.ssthresh = 1460; // Force into CA immediately
        cc.cwnd = 1460;
        let before = cc.window();
        cc.on_ack(1460);
        assert!(cc.window() > before);
        assert!(cc.window() < before + 1460); // Less than doubling
    }

    #[test]
    fn fast_retransmit_halves_window() {
        let mut cc = CongestionControl::new();
        cc.cwnd = 10 * 1460;
        cc.on_fast_retransmit();
        assert_eq!(cc.ssthresh, 5 * 1460);
        assert_eq!(cc.cwnd, 5 * 1460 + 3 * 1460);
    }

    #[test]
    fn timeout_resets_to_one_mss() {
        let mut cc = CongestionControl::new();
        cc.cwnd = 10 * 1460;
        cc.on_timeout();
        assert_eq!(cc.cwnd, 1460);
        assert_eq!(cc.ssthresh, 5 * 1460);
    }
}
```

### `src/tcp/rtt.rs`

```rust
use std::time::Duration;

/// RTT estimator using Jacobson/Karels algorithm (RFC 6298).
pub struct RttEstimator {
    srtt: Option<Duration>,  // smoothed RTT
    rttvar: Duration,        // RTT variation
    rto: Duration,           // retransmission timeout
    min_rto: Duration,
    max_rto: Duration,
}

impl RttEstimator {
    pub fn new() -> Self {
        Self {
            srtt: None,
            rttvar: Duration::from_millis(0),
            rto: Duration::from_secs(1), // Initial RTO = 1 second (RFC 6298)
            min_rto: Duration::from_millis(200),
            max_rto: Duration::from_secs(60),
        }
    }

    /// Update RTT estimate with a new measurement.
    /// Implements Jacobson/Karels algorithm per RFC 6298 Section 2.
    pub fn update(&mut self, rtt: Duration) {
        match self.srtt {
            None => {
                // First measurement
                self.srtt = Some(rtt);
                self.rttvar = rtt / 2;
                self.rto = rtt + Duration::max(self.min_rto, self.rttvar * 4);
            }
            Some(srtt) => {
                // RTTVAR = (1 - beta) * RTTVAR + beta * |SRTT - R'|
                // SRTT = (1 - alpha) * SRTT + alpha * R'
                // where alpha = 1/8, beta = 1/4
                let diff = if rtt > srtt {
                    rtt - srtt
                } else {
                    srtt - rtt
                };

                // rttvar = 3/4 * rttvar + 1/4 * diff
                self.rttvar = (self.rttvar * 3 + diff) / 4;

                // srtt = 7/8 * srtt + 1/8 * rtt
                let new_srtt = (srtt * 7 + rtt) / 8;
                self.srtt = Some(new_srtt);

                // RTO = SRTT + max(G, 4*RTTVAR) where G = clock granularity
                self.rto = new_srtt + Duration::max(self.min_rto, self.rttvar * 4);
            }
        }

        // Clamp RTO
        self.rto = self.rto.max(self.min_rto).min(self.max_rto);
        log::trace!("RTT update: srtt={:?} rttvar={:?} rto={:?}", self.srtt, self.rttvar, self.rto);
    }

    /// Get the current RTO value.
    pub fn rto(&self) -> Duration {
        self.rto
    }

    /// Double RTO for exponential backoff (RFC 6298 Section 5.5).
    pub fn backoff(&mut self) {
        self.rto = (self.rto * 2).min(self.max_rto);
    }

    /// Get smoothed RTT if available.
    pub fn srtt(&self) -> Option<Duration> {
        self.srtt
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn first_measurement_sets_srtt() {
        let mut est = RttEstimator::new();
        est.update(Duration::from_millis(100));
        assert!(est.srtt().is_some());
        let srtt = est.srtt().unwrap();
        assert_eq!(srtt, Duration::from_millis(100));
    }

    #[test]
    fn subsequent_measurements_smooth() {
        let mut est = RttEstimator::new();
        est.update(Duration::from_millis(100));
        est.update(Duration::from_millis(200));
        let srtt = est.srtt().unwrap();
        // 7/8 * 100 + 1/8 * 200 = 87.5 + 25 = 112.5
        assert!(srtt > Duration::from_millis(100));
        assert!(srtt < Duration::from_millis(200));
    }

    #[test]
    fn backoff_doubles() {
        let mut est = RttEstimator::new();
        est.update(Duration::from_millis(100));
        let rto1 = est.rto();
        est.backoff();
        let rto2 = est.rto();
        assert!(rto2 >= rto1 * 2 - Duration::from_millis(1));
    }

    #[test]
    fn rto_clamped() {
        let mut est = RttEstimator::new();
        est.update(Duration::from_millis(1));
        assert!(est.rto() >= Duration::from_millis(200)); // min_rto
    }
}
```

### `src/tcp/mod.rs`

```rust
pub mod segment;
pub mod connection;
pub mod congestion;
pub mod rtt;
```

### `src/stack.rs`

```rust
use crate::arp::{ArpCache, ArpPacket, MacAddr, ARP_REQUEST, ARP_REPLY};
use crate::icmp::{IcmpPacket, ICMP_ECHO_REQUEST};
use crate::ip::{Ipv4Packet, PROTO_ICMP, PROTO_TCP};
use crate::tcp::connection::TcpConnection;
use crate::tcp::segment::TcpSegment;

use std::collections::HashMap;
use std::net::Ipv4Addr;

/// Four-tuple identifying a TCP connection.
#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub struct ConnectionKey {
    pub local_addr: Ipv4Addr,
    pub local_port: u16,
    pub remote_addr: Ipv4Addr,
    pub remote_port: u16,
}

/// The userspace TCP/IP stack.
pub struct NetworkStack {
    pub local_ip: Ipv4Addr,
    pub local_mac: MacAddr,
    pub gateway_ip: Ipv4Addr,
    pub arp_cache: ArpCache,
    pub connections: HashMap<ConnectionKey, TcpConnection>,
    pub listeners: HashMap<u16, TcpConnection>,
    outbound: Vec<Vec<u8>>,
}

impl NetworkStack {
    pub fn new(local_ip: Ipv4Addr, local_mac: MacAddr, gateway_ip: Ipv4Addr) -> Self {
        Self {
            local_ip,
            local_mac,
            gateway_ip,
            arp_cache: ArpCache::new(),
            connections: HashMap::new(),
            listeners: HashMap::new(),
            outbound: Vec::new(),
        }
    }

    /// Process an incoming raw IP packet from the TUN device.
    pub fn process_packet(&mut self, buf: &[u8]) {
        let ip_pkt = match Ipv4Packet::parse(buf) {
            Ok(pkt) => pkt,
            Err(e) => { log::warn!("invalid IP packet: {}", e); return; }
        };

        if ip_pkt.dst != self.local_ip {
            return; // Not for us
        }

        match ip_pkt.protocol {
            PROTO_ICMP => self.handle_icmp(&ip_pkt),
            PROTO_TCP => self.handle_tcp(&ip_pkt),
            _ => log::trace!("ignoring protocol {}", ip_pkt.protocol),
        }
    }

    fn handle_icmp(&mut self, ip_pkt: &Ipv4Packet) {
        let icmp = match IcmpPacket::parse(&ip_pkt.payload) {
            Ok(pkt) => pkt,
            Err(e) => { log::warn!("invalid ICMP: {}", e); return; }
        };

        if icmp.icmp_type == ICMP_ECHO_REQUEST {
            let reply = IcmpPacket::echo_reply(&icmp);
            let response = Ipv4Packet::new(self.local_ip, ip_pkt.src, PROTO_ICMP, reply.to_bytes());
            self.outbound.push(response.to_bytes());
            log::info!("ICMP echo reply -> {}", ip_pkt.src);
        }
    }

    fn handle_tcp(&mut self, ip_pkt: &Ipv4Packet) {
        let seg = match TcpSegment::parse(&ip_pkt.payload) {
            Ok(s) => s,
            Err(e) => { log::warn!("invalid TCP segment: {}", e); return; }
        };

        let key = ConnectionKey {
            local_addr: self.local_ip,
            local_port: seg.dst_port,
            remote_addr: ip_pkt.src,
            remote_port: seg.src_port,
        };

        // Check existing connection
        if let Some(conn) = self.connections.get_mut(&key) {
            let responses = conn.on_segment(&seg);
            for resp in responses {
                self.send_tcp_segment(&resp, ip_pkt.src);
            }
            return;
        }

        // Check listeners for new connections
        if let Some(listener) = self.listeners.get_mut(&seg.dst_port) {
            let mut conn = TcpConnection::new(seg.dst_port);
            conn.listen();
            let responses = conn.on_segment(&seg);
            for resp in &responses {
                self.send_tcp_segment(resp, ip_pkt.src);
            }
            self.connections.insert(key, conn);
        }
    }

    fn send_tcp_segment(&mut self, seg: &TcpSegment, dst: Ipv4Addr) {
        let tcp_bytes = seg.to_bytes(self.local_ip.octets(), dst.octets());
        let ip_pkt = Ipv4Packet::new(self.local_ip, dst, PROTO_TCP, tcp_bytes);
        self.outbound.push(ip_pkt.to_bytes());
    }

    /// Take all outbound packets to write to the TUN device.
    pub fn drain_outbound(&mut self) -> Vec<Vec<u8>> {
        std::mem::take(&mut self.outbound)
    }

    /// Register a listener on a port.
    pub fn listen(&mut self, port: u16) {
        let mut conn = TcpConnection::new(port);
        conn.listen();
        self.listeners.insert(port, conn);
        log::info!("listening on port {}", port);
    }

    /// Initiate an outbound connection.
    pub fn connect(&mut self, remote_addr: Ipv4Addr, remote_port: u16, local_port: u16) {
        let mut conn = TcpConnection::new(local_port);
        if let Some(syn) = conn.connect(remote_port) {
            self.send_tcp_segment(&syn, remote_addr);
            let key = ConnectionKey {
                local_addr: self.local_ip,
                local_port,
                remote_addr,
                remote_port,
            };
            self.connections.insert(key, conn);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn icmp_echo_reply() {
        let mut stack = NetworkStack::new(
            Ipv4Addr::new(10, 0, 0, 1),
            MacAddr::new([0xAA; 6]),
            Ipv4Addr::new(10, 0, 0, 254),
        );

        let icmp_req = IcmpPacket {
            icmp_type: ICMP_ECHO_REQUEST,
            code: 0, checksum: 0,
            rest: [0, 1, 0, 1],
            payload: b"ping".to_vec(),
        };
        let ip_pkt = Ipv4Packet::new(
            Ipv4Addr::new(10, 0, 0, 2),
            Ipv4Addr::new(10, 0, 0, 1),
            PROTO_ICMP,
            icmp_req.to_bytes(),
        );

        stack.process_packet(&ip_pkt.to_bytes());
        let outbound = stack.drain_outbound();
        assert_eq!(outbound.len(), 1, "should produce one ICMP reply");
    }
}
```

### `src/main.rs`

```rust
mod arp;
mod checksum;
mod icmp;
mod ip;
mod stack;
mod tcp;

use crate::arp::MacAddr;
use crate::stack::NetworkStack;
use std::net::Ipv4Addr;

fn main() {
    env_logger::init();

    let local_ip = Ipv4Addr::new(10, 0, 0, 2);
    let gateway = Ipv4Addr::new(10, 0, 0, 1);
    let local_mac = MacAddr::new([0x02, 0x42, 0xAC, 0x11, 0x00, 0x02]);

    log::info!("TCP/IP stack starting: ip={}, gateway={}", local_ip, gateway);

    #[cfg(target_os = "linux")]
    {
        run_with_tun(local_ip, gateway, local_mac);
    }

    #[cfg(not(target_os = "linux"))]
    {
        log::info!("TUN not available on this platform. Running demo.");
        run_demo(local_ip, gateway, local_mac);
    }
}

#[cfg(target_os = "linux")]
fn run_with_tun(local_ip: Ipv4Addr, gateway: Ipv4Addr, local_mac: MacAddr) {
    use std::fs::OpenOptions;
    use std::io::{Read, Write};

    // Open TUN device
    let mut tun = match OpenOptions::new().read(true).write(true).open("/dev/net/tun") {
        Ok(f) => f,
        Err(e) => {
            log::error!("cannot open /dev/net/tun: {} (run as root)", e);
            return;
        }
    };

    let mut stack = NetworkStack::new(local_ip, local_mac, gateway);
    stack.listen(80);

    let mut buf = vec![0u8; 65535];
    loop {
        match tun.read(&mut buf) {
            Ok(n) if n > 0 => {
                stack.process_packet(&buf[..n]);
                for packet in stack.drain_outbound() {
                    let _ = tun.write(&packet);
                }
            }
            Ok(_) => {}
            Err(e) => {
                log::error!("TUN read error: {}", e);
                break;
            }
        }
    }
}

fn run_demo(local_ip: Ipv4Addr, gateway: Ipv4Addr, local_mac: MacAddr) {
    use crate::tcp::connection::TcpConnection;
    use crate::tcp::segment::*;

    let mut stack = NetworkStack::new(local_ip, local_mac, gateway);
    stack.listen(80);

    // Simulate a client connecting
    let mut client = TcpConnection::new(54321);
    let syn = client.connect(80).unwrap();
    log::info!("client SYN -> server");

    let syn_bytes = syn.to_bytes(
        Ipv4Addr::new(10, 0, 0, 3).octets(),
        local_ip.octets(),
    );
    let ip_pkt = crate::ip::Ipv4Packet::new(
        Ipv4Addr::new(10, 0, 0, 3),
        local_ip,
        crate::ip::PROTO_TCP,
        syn_bytes,
    );
    stack.process_packet(&ip_pkt.to_bytes());

    let outbound = stack.drain_outbound();
    log::info!("stack produced {} outbound packets", outbound.len());

    if let Some(pkt_bytes) = outbound.first() {
        let ip = crate::ip::Ipv4Packet::parse(pkt_bytes).unwrap();
        let seg = TcpSegment::parse(&ip.payload).unwrap();
        log::info!("server response: flags={:#04x} seq={} ack={}", seg.flags, seg.seq, seg.ack);

        let responses = client.on_segment(&seg);
        log::info!("client state: {:?}", client.state);
        if !responses.is_empty() {
            log::info!("client sends ACK");
        }
    }

    log::info!("demo complete");
}
```

## Build and Run

```bash
# Build
cargo build --release

# Run demo (no root needed)
RUST_LOG=info cargo run

# Run with TUN (Linux, root)
sudo ip tuntap add dev tun0 mode tun
sudo ip addr add 10.0.0.1/24 dev tun0
sudo ip link set tun0 up
sudo RUST_LOG=info cargo run

# Test from another terminal
ping 10.0.0.2    # ICMP echo
curl 10.0.0.2    # TCP connection to port 80

# Run tests
cargo test -- --nocapture

# Run specific layer tests
cargo test checksum -- --nocapture
cargo test ip::tests -- --nocapture
cargo test arp::tests -- --nocapture
cargo test icmp::tests -- --nocapture
cargo test tcp::segment::tests -- --nocapture
cargo test tcp::connection::tests -- --nocapture
cargo test tcp::congestion::tests -- --nocapture
cargo test tcp::rtt::tests -- --nocapture
cargo test stack::tests -- --nocapture
```

## Expected Output

Demo mode:
```
[INFO  tcp_ip_stack] TCP/IP stack starting: ip=10.0.0.2, gateway=10.0.0.1
[INFO  tcp_ip_stack] TUN not available on this platform. Running demo.
[INFO  tcp_ip_stack::stack] listening on port 80
[INFO  tcp_ip_stack] client SYN -> server
[INFO  tcp_ip_stack] stack produced 1 outbound packets
[INFO  tcp_ip_stack] server response: flags=0x12 seq=875000000 ack=3750000001
[INFO  tcp_ip_stack] client state: Established
[INFO  tcp_ip_stack] client sends ACK
[INFO  tcp_ip_stack] demo complete
```

Test output:
```
running 15 tests
test checksum::tests::checksum_validates_to_zero ... ok
test ip::tests::round_trip ... ok
test arp::tests::arp_round_trip ... ok
test arp::tests::cache_insert_and_lookup ... ok
test arp::tests::cache_pending_drain ... ok
test icmp::tests::echo_reply_preserves_id_and_seq ... ok
test icmp::tests::checksum_valid ... ok
test tcp::segment::tests::segment_round_trip ... ok
test tcp::segment::tests::mss_option_parsing ... ok
test tcp::segment::tests::sequence_wraparound ... ok
test tcp::connection::tests::handshake_and_data ... ok
test tcp::congestion::tests::slow_start_doubles ... ok
test tcp::congestion::tests::fast_retransmit_halves_window ... ok
test tcp::congestion::tests::timeout_resets_to_one_mss ... ok
test tcp::rtt::tests::first_measurement_sets_srtt ... ok
test tcp::rtt::tests::backoff_doubles ... ok
test stack::tests::icmp_echo_reply ... ok
test result: ok. 17 passed; 0 failed; 0 ignored
```

## Design Decisions

**Why `smoltcp`-style layered architecture without trait objects.** Each layer is a concrete type that takes parsed input and produces output bytes. No `dyn Dispatch` or vtable indirection. The stack is a synchronous event processor: call `process_packet` with incoming bytes, then `drain_outbound` to get responses. This makes the entire stack testable without async runtime or real I/O.

**Why `CongestionControl` is separate from `TcpConnection`.** The congestion control algorithm (Reno, Cubic, BBR) is conceptually independent from the TCP state machine. Separating it allows testing congestion behavior in isolation and swapping algorithms without modifying connection logic.

**Why Jacobson/Karels for RTT estimation.** This is the algorithm specified in RFC 6298 and used by every production TCP stack. The exponentially weighted moving average naturally adapts to changing network conditions. The alpha (1/8) and beta (1/4) constants are chosen to provide smooth estimates without overreacting to jitter.

**Why Karn's algorithm excludes retransmitted segments from RTT measurement.** A retransmitted segment's ACK is ambiguous: the ACK might be for the original transmission or the retransmission. Including ambiguous samples corrupts the RTT estimate, potentially making it too small and triggering spurious retransmissions. Karn's algorithm simply skips any segment that was retransmitted.

**Why the ARP cache queues packets pending resolution.** When the stack needs to send a packet to an IP address not in the ARP cache, it cannot send immediately. The packet is queued, an ARP request is sent, and when the reply arrives, all queued packets are transmitted. Dropping the packet instead would require the transport layer to detect the loss and retransmit, adding unnecessary latency.

## Common Mistakes

1. **Using regular comparison operators on sequence numbers.** Sequence numbers are modular 32-bit. `a < b` produces wrong results after wraparound. Use wrapping subtraction with signed interpretation.

2. **Forgetting that SYN and FIN consume one sequence number each.** After sending SYN with `seq=N`, the next data byte has `seq=N+1`. Forgetting this makes every subsequent ACK off by one.

3. **Computing TCP checksum without the pseudo-header.** The TCP checksum includes a pseudo-header with source/destination IP addresses. Omitting it produces a checksum that your own code accepts but every other TCP stack rejects.

4. **Incrementing `cwnd` by MSS on every ACK during congestion avoidance.** Congestion avoidance should increase `cwnd` by approximately MSS per RTT, not per ACK. The correct increment per ACK is `MSS * MSS / cwnd`, which sums to approximately MSS over a window's worth of ACKs.

5. **Measuring RTT on retransmitted segments.** Karn's algorithm: never update the RTT estimate from an ACK that could be acknowledging a retransmission. The ACK is ambiguous -- it might be for the original or the retransmit.

6. **Not implementing fast recovery correctly.** After fast retransmit (3 dup ACKs), `cwnd` should be set to `ssthresh + 3*MSS`, not just `ssthresh`. Each additional dup ACK during recovery inflates `cwnd` by one MSS. When new data is ACKed, `cwnd` deflates back to `ssthresh`.

## Performance Notes

- Packet parsing is zero-allocation for the header: all fields are extracted from the byte slice directly. Only the payload is copied to a `Vec<u8>`. For higher performance, use borrowed slices with lifetimes.
- The ARP cache uses `HashMap` with O(1) lookup. Eviction runs on a timer, scanning all entries. For large caches, use an LRU map with O(1) eviction.
- Congestion control arithmetic uses integer math exclusively (no floating point). Division by `cwnd` for the congestion avoidance increment is the only division per ACK.
- The connection table (`HashMap<ConnectionKey, TcpConnection>`) supports O(1) lookup per incoming packet. Production stacks use lock-free hash tables for multi-core scaling.
- TUN device I/O is the bottleneck: each `read`/`write` syscall processes one packet. For higher throughput, batch reads with `readv` or use `AF_XDP` for zero-copy packet processing.

## Going Further

- Implement TCP timestamps (RFC 7323) for more accurate RTT measurement and PAWS (protection against wrapped sequence numbers)
- Add IPv6 support with ICMPv6 neighbor discovery replacing ARP
- Implement SACK (Selective Acknowledgment, RFC 2018) for better loss recovery with multiple dropped segments
- Replace Reno congestion control with Cubic (Linux default) or BBR (Google's congestion control)
- Build a DNS resolver on top of your UDP support to resolve hostnames
- Add a `wget`-like tool that uses your stack to download files over HTTP
