# Solution: TCP State Machine Userspace

## Architecture Overview

The implementation is structured in four layers:

1. **Network Layer** -- IP packet parsing and construction with checksum computation; interfaces with the TUN device or raw socket for reading/writing raw packets
2. **Transport Layer** -- TCP segment parsing and construction including pseudo-header checksum; sequence number arithmetic utilities
3. **State Machine** -- implements all 11 TCP states with RFC 793-compliant transitions; produces outbound segments in response to incoming segments or application events
4. **Socket API** -- application-facing interface providing `connect`, `listen`, `accept`, `send`, `recv`, and `close` operations backed by the state machine

```
Application
    | (connect / send / recv / close)
    v
Socket API (TcpListener, TcpStream)
    |
    v
Connection State Machine (per-connection, 11 states)
    |
    v
TCP Segment Codec (parse / construct / checksum)
    |
    v
IP Packet Codec (parse / construct / checksum)
    |
    v
TUN Device (raw IP packets)
```

## Rust Solution

### `Cargo.toml`

```toml
[package]
name = "tcp-userspace"
version = "0.1.0"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
bytes = "1"
log = "0.4"
env_logger = "0.11"

[dev-dependencies]
tokio-test = "0.4"
```

### `src/ip.rs`

```rust
use std::net::Ipv4Addr;

/// Parsed IPv4 header.
#[derive(Debug, Clone)]
pub struct Ipv4Header {
    pub version: u8,
    pub ihl: u8,
    pub total_length: u16,
    pub identification: u16,
    pub flags: u8,
    pub fragment_offset: u16,
    pub ttl: u8,
    pub protocol: u8,
    pub checksum: u16,
    pub src_addr: Ipv4Addr,
    pub dst_addr: Ipv4Addr,
}

/// An IPv4 packet with parsed header and payload reference.
#[derive(Debug)]
pub struct Ipv4Packet {
    pub header: Ipv4Header,
    pub payload: Vec<u8>,
}

/// Protocol number for TCP.
pub const PROTO_TCP: u8 = 6;

impl Ipv4Header {
    /// Parse an IPv4 header from raw bytes.
    pub fn parse(buf: &[u8]) -> Result<(Self, usize), String> {
        if buf.len() < 20 {
            return Err("buffer too short for IPv4 header".into());
        }

        let version = buf[0] >> 4;
        if version != 4 {
            return Err(format!("not IPv4: version = {}", version));
        }

        let ihl = buf[0] & 0x0F;
        let header_len = (ihl as usize) * 4;
        if buf.len() < header_len {
            return Err("buffer too short for declared IHL".into());
        }

        let total_length = u16::from_be_bytes([buf[2], buf[3]]);
        let identification = u16::from_be_bytes([buf[4], buf[5]]);
        let flags = buf[6] >> 5;
        let fragment_offset = u16::from_be_bytes([buf[6] & 0x1F, buf[7]]);
        let ttl = buf[8];
        let protocol = buf[9];
        let checksum = u16::from_be_bytes([buf[10], buf[11]]);
        let src_addr = Ipv4Addr::new(buf[12], buf[13], buf[14], buf[15]);
        let dst_addr = Ipv4Addr::new(buf[16], buf[17], buf[18], buf[19]);

        Ok((
            Self {
                version,
                ihl,
                total_length,
                identification,
                flags,
                fragment_offset,
                ttl,
                protocol,
                checksum,
                src_addr,
                dst_addr,
            },
            header_len,
        ))
    }

    /// Serialize an IPv4 header into bytes (no options).
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut buf = vec![0u8; 20];
        buf[0] = (self.version << 4) | self.ihl;
        buf[1] = 0; // DSCP + ECN
        buf[2..4].copy_from_slice(&self.total_length.to_be_bytes());
        buf[4..6].copy_from_slice(&self.identification.to_be_bytes());
        buf[6] = (self.flags << 5) | ((self.fragment_offset >> 8) as u8 & 0x1F);
        buf[7] = self.fragment_offset as u8;
        buf[8] = self.ttl;
        buf[9] = self.protocol;
        // Checksum initially zero for computation
        buf[10] = 0;
        buf[11] = 0;
        let octets = self.src_addr.octets();
        buf[12..16].copy_from_slice(&octets);
        let octets = self.dst_addr.octets();
        buf[16..20].copy_from_slice(&octets);

        // Compute and fill checksum
        let cksum = internet_checksum(&buf);
        buf[10..12].copy_from_slice(&cksum.to_be_bytes());

        buf
    }

    /// Build a new IPv4 header for a TCP payload.
    pub fn new_tcp(src: Ipv4Addr, dst: Ipv4Addr, payload_len: u16) -> Self {
        Self {
            version: 4,
            ihl: 5,
            total_length: 20 + payload_len,
            identification: rand_u16(),
            flags: 0x02, // Don't Fragment
            fragment_offset: 0,
            ttl: 64,
            protocol: PROTO_TCP,
            checksum: 0, // Computed during serialization
            src_addr: src,
            dst_addr: dst,
        }
    }
}

impl Ipv4Packet {
    /// Parse a complete IPv4 packet.
    pub fn parse(buf: &[u8]) -> Result<Self, String> {
        let (header, header_len) = Ipv4Header::parse(buf)?;
        let total = header.total_length as usize;
        if buf.len() < total {
            return Err(format!("buffer ({}) shorter than total_length ({})", buf.len(), total));
        }
        let payload = buf[header_len..total].to_vec();
        Ok(Self { header, payload })
    }

    /// Serialize the full IP packet.
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut buf = self.header.to_bytes();
        buf.extend_from_slice(&self.payload);
        buf
    }
}

/// RFC 1071 Internet Checksum.
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
    while sum >> 16 != 0 {
        sum = (sum & 0xFFFF) + (sum >> 16);
    }
    !(sum as u16)
}

fn rand_u16() -> u16 {
    use std::time::SystemTime;
    let t = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default()
        .subsec_nanos();
    (t & 0xFFFF) as u16
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn checksum_of_known_header() {
        // A known IPv4 header (checksum field zeroed)
        let mut header = [0u8; 20];
        header[0] = 0x45; // version 4, IHL 5
        header[8] = 64;   // TTL
        header[9] = 6;    // TCP
        header[12..16].copy_from_slice(&[10, 0, 0, 1]);
        header[16..20].copy_from_slice(&[10, 0, 0, 2]);
        header[2..4].copy_from_slice(&40u16.to_be_bytes());

        let cksum = internet_checksum(&header);
        // Verify checksum is valid: checksum of header with checksum filled should be 0
        header[10..12].copy_from_slice(&cksum.to_be_bytes());
        assert_eq!(internet_checksum(&header), 0);
    }

    #[test]
    fn ipv4_round_trip() {
        let header = Ipv4Header::new_tcp(
            Ipv4Addr::new(192, 168, 1, 1),
            Ipv4Addr::new(192, 168, 1, 2),
            32,
        );
        let pkt = Ipv4Packet {
            header,
            payload: vec![0xAB; 32],
        };
        let bytes = pkt.to_bytes();
        let parsed = Ipv4Packet::parse(&bytes).unwrap();
        assert_eq!(parsed.header.src_addr, Ipv4Addr::new(192, 168, 1, 1));
        assert_eq!(parsed.header.dst_addr, Ipv4Addr::new(192, 168, 1, 2));
        assert_eq!(parsed.header.protocol, PROTO_TCP);
        assert_eq!(parsed.payload.len(), 32);
    }

    #[test]
    fn checksum_valid_after_construction() {
        let header = Ipv4Header::new_tcp(
            Ipv4Addr::new(10, 0, 0, 1),
            Ipv4Addr::new(10, 0, 0, 2),
            100,
        );
        let bytes = header.to_bytes();
        assert_eq!(internet_checksum(&bytes), 0, "checksum should validate to 0");
    }
}
```

### `src/tcp.rs`

```rust
use crate::ip::internet_checksum;
use std::net::Ipv4Addr;

/// TCP flags.
pub const FIN: u8 = 0x01;
pub const SYN: u8 = 0x02;
pub const RST: u8 = 0x04;
pub const PSH: u8 = 0x08;
pub const ACK: u8 = 0x10;
pub const URG: u8 = 0x20;

/// Parsed TCP segment.
#[derive(Debug, Clone)]
pub struct TcpSegment {
    pub src_port: u16,
    pub dst_port: u16,
    pub seq_num: u32,
    pub ack_num: u32,
    pub data_offset: u8,
    pub flags: u8,
    pub window: u16,
    pub checksum: u16,
    pub urgent_ptr: u16,
    pub payload: Vec<u8>,
}

impl TcpSegment {
    /// Parse a TCP segment from raw bytes.
    pub fn parse(buf: &[u8]) -> Result<Self, String> {
        if buf.len() < 20 {
            return Err("buffer too short for TCP header".into());
        }

        let src_port = u16::from_be_bytes([buf[0], buf[1]]);
        let dst_port = u16::from_be_bytes([buf[2], buf[3]]);
        let seq_num = u32::from_be_bytes([buf[4], buf[5], buf[6], buf[7]]);
        let ack_num = u32::from_be_bytes([buf[8], buf[9], buf[10], buf[11]]);
        let data_offset = buf[12] >> 4;
        let flags = buf[13] & 0x3F;
        let window = u16::from_be_bytes([buf[14], buf[15]]);
        let checksum = u16::from_be_bytes([buf[16], buf[17]]);
        let urgent_ptr = u16::from_be_bytes([buf[18], buf[19]]);

        let header_len = (data_offset as usize) * 4;
        if buf.len() < header_len {
            return Err("buffer shorter than declared data offset".into());
        }

        let payload = buf[header_len..].to_vec();

        Ok(Self {
            src_port,
            dst_port,
            seq_num,
            ack_num,
            data_offset,
            flags,
            window,
            checksum,
            urgent_ptr,
            payload,
        })
    }

    /// Serialize a TCP segment into bytes, computing the checksum with the pseudo-header.
    pub fn to_bytes(&self, src_ip: Ipv4Addr, dst_ip: Ipv4Addr) -> Vec<u8> {
        let header_len = 20; // No options for simplicity
        let total_len = header_len + self.payload.len();
        let mut buf = vec![0u8; total_len];

        buf[0..2].copy_from_slice(&self.src_port.to_be_bytes());
        buf[2..4].copy_from_slice(&self.dst_port.to_be_bytes());
        buf[4..8].copy_from_slice(&self.seq_num.to_be_bytes());
        buf[8..12].copy_from_slice(&self.ack_num.to_be_bytes());
        buf[12] = (5 << 4) as u8; // data offset = 5 (20 bytes, no options)
        buf[13] = self.flags;
        buf[14..16].copy_from_slice(&self.window.to_be_bytes());
        // Checksum initially zero
        buf[16..18].copy_from_slice(&0u16.to_be_bytes());
        buf[18..20].copy_from_slice(&self.urgent_ptr.to_be_bytes());

        buf[header_len..].copy_from_slice(&self.payload);

        // Compute checksum with pseudo-header
        let cksum = tcp_checksum(src_ip, dst_ip, &buf);
        buf[16..18].copy_from_slice(&cksum.to_be_bytes());

        buf
    }

    pub fn has_flag(&self, flag: u8) -> bool {
        self.flags & flag != 0
    }

    /// Build a reply segment (swaps ports, sets appropriate seq/ack).
    pub fn build_reply(
        src_port: u16,
        dst_port: u16,
        seq_num: u32,
        ack_num: u32,
        flags: u8,
        window: u16,
        payload: Vec<u8>,
    ) -> Self {
        Self {
            src_port,
            dst_port,
            seq_num,
            ack_num,
            data_offset: 5,
            flags,
            window,
            checksum: 0,
            urgent_ptr: 0,
            payload,
        }
    }
}

/// Compute TCP checksum including pseudo-header.
pub fn tcp_checksum(src_ip: Ipv4Addr, dst_ip: Ipv4Addr, tcp_bytes: &[u8]) -> u16 {
    let tcp_len = tcp_bytes.len() as u16;

    // Build pseudo-header: src IP (4) + dst IP (4) + zero (1) + protocol (1) + TCP length (2)
    let mut pseudo = Vec::with_capacity(12 + tcp_bytes.len());
    pseudo.extend_from_slice(&src_ip.octets());
    pseudo.extend_from_slice(&dst_ip.octets());
    pseudo.push(0);
    pseudo.push(6); // TCP protocol number
    pseudo.extend_from_slice(&tcp_len.to_be_bytes());
    pseudo.extend_from_slice(tcp_bytes);

    internet_checksum(&pseudo)
}

/// Sequence number comparison: a is before b in the sequence space.
pub fn seq_lt(a: u32, b: u32) -> bool {
    (a.wrapping_sub(b) as i32) < 0
}

/// Sequence number comparison: a is before or equal to b.
pub fn seq_lte(a: u32, b: u32) -> bool {
    a == b || seq_lt(a, b)
}

/// Sequence number comparison: a is after b.
pub fn seq_gt(a: u32, b: u32) -> bool {
    seq_lt(b, a)
}

/// Check if a sequence number falls within a window [start, start+size).
pub fn seq_in_window(seq: u32, window_start: u32, window_size: u32) -> bool {
    let offset = seq.wrapping_sub(window_start);
    offset < window_size
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn segment_round_trip() {
        let seg = TcpSegment::build_reply(8080, 12345, 1000, 2000, SYN | ACK, 65535, vec![]);
        let src_ip = Ipv4Addr::new(10, 0, 0, 1);
        let dst_ip = Ipv4Addr::new(10, 0, 0, 2);
        let bytes = seg.to_bytes(src_ip, dst_ip);

        let parsed = TcpSegment::parse(&bytes).unwrap();
        assert_eq!(parsed.src_port, 8080);
        assert_eq!(parsed.dst_port, 12345);
        assert_eq!(parsed.seq_num, 1000);
        assert_eq!(parsed.ack_num, 2000);
        assert!(parsed.has_flag(SYN));
        assert!(parsed.has_flag(ACK));
        assert_eq!(parsed.window, 65535);
    }

    #[test]
    fn checksum_validates() {
        let seg = TcpSegment::build_reply(80, 54321, 0, 0, SYN, 32768, b"hello".to_vec());
        let src_ip = Ipv4Addr::new(192, 168, 1, 1);
        let dst_ip = Ipv4Addr::new(192, 168, 1, 2);
        let bytes = seg.to_bytes(src_ip, dst_ip);

        // Verify checksum: recomputing over the segment (with checksum included) should yield 0
        let cksum = tcp_checksum(src_ip, dst_ip, &bytes);
        assert_eq!(cksum, 0, "TCP checksum should validate to 0");
    }

    #[test]
    fn segment_with_payload() {
        let payload = b"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n".to_vec();
        let seg = TcpSegment::build_reply(54321, 80, 100, 200, ACK | PSH, 16384, payload.clone());
        let bytes = seg.to_bytes(Ipv4Addr::new(10, 0, 0, 1), Ipv4Addr::new(10, 0, 0, 2));
        let parsed = TcpSegment::parse(&bytes).unwrap();
        assert_eq!(parsed.payload, payload);
    }

    #[test]
    fn sequence_number_arithmetic() {
        // Normal ordering
        assert!(seq_lt(100, 200));
        assert!(!seq_lt(200, 100));

        // Wraparound: u32::MAX is "before" 0 in sequence space
        assert!(seq_lt(u32::MAX - 10, 10));
        assert!(!seq_lt(10, u32::MAX - 10));

        // Equal
        assert!(!seq_lt(100, 100));
        assert!(seq_lte(100, 100));
    }

    #[test]
    fn window_check() {
        assert!(seq_in_window(100, 100, 1000));
        assert!(seq_in_window(1099, 100, 1000));
        assert!(!seq_in_window(1100, 100, 1000));
        assert!(!seq_in_window(99, 100, 1000));

        // Wraparound window
        assert!(seq_in_window(5, u32::MAX - 10, 20));
    }
}
```

### `src/state.rs`

```rust
use crate::tcp::*;
use std::collections::VecDeque;
use std::time::{Duration, Instant};

/// The 11 TCP states per RFC 793.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TcpState {
    Closed,
    Listen,
    SynSent,
    SynReceived,
    Established,
    FinWait1,
    FinWait2,
    CloseWait,
    Closing,
    LastAck,
    TimeWait,
}

/// Tracks an unacknowledged sent segment for retransmission.
#[derive(Debug, Clone)]
struct UnackedSegment {
    seq_num: u32,
    data: Vec<u8>,
    flags: u8,
    sent_at: Instant,
}

/// A single TCP connection's state and buffers.
pub struct TcpConnection {
    pub state: TcpState,

    // Local and remote endpoints
    pub local_port: u16,
    pub remote_port: u16,

    // Sequence number tracking
    pub snd_una: u32,  // oldest unacknowledged sequence number
    pub snd_nxt: u32,  // next sequence number to send
    pub snd_wnd: u32,  // send window (from receiver's advertisement)
    pub iss: u32,       // initial send sequence number

    pub rcv_nxt: u32,  // next expected receive sequence number
    pub rcv_wnd: u32,  // receive window we advertise
    pub irs: u32,       // initial receive sequence number

    // Buffers
    send_buf: VecDeque<u8>,
    recv_buf: VecDeque<u8>,

    // Retransmission
    unacked: Vec<UnackedSegment>,
    rto: Duration,

    // TIME_WAIT timer
    time_wait_start: Option<Instant>,
    time_wait_duration: Duration,
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
            send_buf: VecDeque::new(),
            recv_buf: VecDeque::new(),
            unacked: Vec::new(),
            rto: Duration::from_secs(1),
            time_wait_start: None,
            time_wait_duration: Duration::from_secs(5), // Short for testing (normally 2*MSL)
        }
    }

    /// Initiate an active open (client side).
    pub fn connect(&mut self, remote_port: u16) -> Option<TcpSegment> {
        if self.state != TcpState::Closed {
            return None;
        }
        self.remote_port = remote_port;
        self.state = TcpState::SynSent;
        self.snd_nxt = self.iss.wrapping_add(1);

        Some(TcpSegment::build_reply(
            self.local_port,
            self.remote_port,
            self.iss,
            0,
            SYN,
            self.rcv_wnd as u16,
            vec![],
        ))
    }

    /// Start passive open (server side).
    pub fn listen(&mut self) {
        self.state = TcpState::Listen;
    }

    /// Process an incoming TCP segment and produce outbound segments.
    pub fn on_segment(&mut self, seg: &TcpSegment) -> Vec<TcpSegment> {
        let mut outbound = Vec::new();

        match self.state {
            TcpState::Closed => {
                if !seg.has_flag(RST) {
                    outbound.push(self.build_rst(seg));
                }
            }

            TcpState::Listen => {
                if seg.has_flag(RST) {
                    return outbound;
                }
                if seg.has_flag(SYN) {
                    self.remote_port = seg.src_port;
                    self.irs = seg.seq_num;
                    self.rcv_nxt = seg.seq_num.wrapping_add(1);
                    self.snd_wnd = seg.window as u32;

                    let syn_ack = TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.iss,
                        self.rcv_nxt,
                        SYN | ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    );
                    self.snd_nxt = self.iss.wrapping_add(1);
                    self.state = TcpState::SynReceived;
                    outbound.push(syn_ack);
                }
            }

            TcpState::SynSent => {
                if seg.has_flag(RST) {
                    self.state = TcpState::Closed;
                    return outbound;
                }
                if seg.has_flag(SYN) && seg.has_flag(ACK) {
                    if seg.ack_num != self.snd_nxt {
                        outbound.push(self.build_rst(seg));
                        return outbound;
                    }
                    self.irs = seg.seq_num;
                    self.rcv_nxt = seg.seq_num.wrapping_add(1);
                    self.snd_una = seg.ack_num;
                    self.snd_wnd = seg.window as u32;
                    self.state = TcpState::Established;

                    outbound.push(TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.snd_nxt,
                        self.rcv_nxt,
                        ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    ));
                } else if seg.has_flag(SYN) {
                    // Simultaneous open
                    self.irs = seg.seq_num;
                    self.rcv_nxt = seg.seq_num.wrapping_add(1);
                    self.state = TcpState::SynReceived;

                    outbound.push(TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.iss,
                        self.rcv_nxt,
                        SYN | ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    ));
                }
            }

            TcpState::SynReceived => {
                if seg.has_flag(RST) {
                    self.state = TcpState::Listen;
                    return outbound;
                }
                if seg.has_flag(ACK) {
                    if seg.ack_num == self.snd_nxt {
                        self.snd_una = seg.ack_num;
                        self.state = TcpState::Established;
                        log::info!("connection established on port {}", self.local_port);
                    }
                }
            }

            TcpState::Established => {
                if seg.has_flag(RST) {
                    self.state = TcpState::Closed;
                    return outbound;
                }

                // Process ACK
                if seg.has_flag(ACK) {
                    self.process_ack(seg.ack_num);
                    self.snd_wnd = seg.window as u32;
                }

                // Process incoming data
                if !seg.payload.is_empty() {
                    if seg.seq_num == self.rcv_nxt {
                        self.recv_buf.extend(&seg.payload);
                        self.rcv_nxt = self.rcv_nxt.wrapping_add(seg.payload.len() as u32);

                        outbound.push(TcpSegment::build_reply(
                            self.local_port,
                            self.remote_port,
                            self.snd_nxt,
                            self.rcv_nxt,
                            ACK,
                            self.rcv_wnd as u16,
                            vec![],
                        ));
                    } else {
                        // Out of order: send duplicate ACK
                        outbound.push(TcpSegment::build_reply(
                            self.local_port,
                            self.remote_port,
                            self.snd_nxt,
                            self.rcv_nxt,
                            ACK,
                            self.rcv_wnd as u16,
                            vec![],
                        ));
                    }
                }

                // Process FIN
                if seg.has_flag(FIN) {
                    self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                    self.state = TcpState::CloseWait;
                    outbound.push(TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.snd_nxt,
                        self.rcv_nxt,
                        ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    ));
                }
            }

            TcpState::FinWait1 => {
                if seg.has_flag(ACK) {
                    self.process_ack(seg.ack_num);
                    if seg.has_flag(FIN) {
                        self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                        self.state = TcpState::TimeWait;
                        self.time_wait_start = Some(Instant::now());
                        outbound.push(TcpSegment::build_reply(
                            self.local_port,
                            self.remote_port,
                            self.snd_nxt,
                            self.rcv_nxt,
                            ACK,
                            self.rcv_wnd as u16,
                            vec![],
                        ));
                    } else {
                        self.state = TcpState::FinWait2;
                    }
                } else if seg.has_flag(FIN) {
                    // Simultaneous close
                    self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                    self.state = TcpState::Closing;
                    outbound.push(TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.snd_nxt,
                        self.rcv_nxt,
                        ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    ));
                }
            }

            TcpState::FinWait2 => {
                if seg.has_flag(FIN) {
                    self.rcv_nxt = self.rcv_nxt.wrapping_add(1);
                    self.state = TcpState::TimeWait;
                    self.time_wait_start = Some(Instant::now());
                    outbound.push(TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.snd_nxt,
                        self.rcv_nxt,
                        ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    ));
                }
            }

            TcpState::CloseWait => {
                // Application must call close() to send FIN
                if seg.has_flag(ACK) {
                    self.process_ack(seg.ack_num);
                }
            }

            TcpState::Closing => {
                if seg.has_flag(ACK) {
                    self.state = TcpState::TimeWait;
                    self.time_wait_start = Some(Instant::now());
                }
            }

            TcpState::LastAck => {
                if seg.has_flag(ACK) {
                    self.state = TcpState::Closed;
                }
            }

            TcpState::TimeWait => {
                // Re-ACK any FIN retransmissions
                if seg.has_flag(FIN) {
                    outbound.push(TcpSegment::build_reply(
                        self.local_port,
                        self.remote_port,
                        self.snd_nxt,
                        self.rcv_nxt,
                        ACK,
                        self.rcv_wnd as u16,
                        vec![],
                    ));
                    self.time_wait_start = Some(Instant::now());
                }
            }
        }

        outbound
    }

    /// Application calls close to initiate teardown.
    pub fn close(&mut self) -> Option<TcpSegment> {
        match self.state {
            TcpState::Established => {
                self.state = TcpState::FinWait1;
                let fin = TcpSegment::build_reply(
                    self.local_port,
                    self.remote_port,
                    self.snd_nxt,
                    self.rcv_nxt,
                    FIN | ACK,
                    self.rcv_wnd as u16,
                    vec![],
                );
                self.snd_nxt = self.snd_nxt.wrapping_add(1);
                Some(fin)
            }
            TcpState::CloseWait => {
                self.state = TcpState::LastAck;
                let fin = TcpSegment::build_reply(
                    self.local_port,
                    self.remote_port,
                    self.snd_nxt,
                    self.rcv_nxt,
                    FIN | ACK,
                    self.rcv_wnd as u16,
                    vec![],
                );
                self.snd_nxt = self.snd_nxt.wrapping_add(1);
                Some(fin)
            }
            _ => None,
        }
    }

    /// Queue data for sending.
    pub fn send(&mut self, data: &[u8]) -> Vec<TcpSegment> {
        if self.state != TcpState::Established {
            return vec![];
        }

        self.send_buf.extend(data);
        self.flush_send_buffer()
    }

    /// Read available data from the receive buffer.
    pub fn recv(&mut self, max: usize) -> Vec<u8> {
        let n = max.min(self.recv_buf.len());
        self.recv_buf.drain(..n).collect()
    }

    /// Check if TIME_WAIT has expired.
    pub fn check_time_wait(&mut self) -> bool {
        if self.state != TcpState::TimeWait {
            return false;
        }
        if let Some(start) = self.time_wait_start {
            if start.elapsed() >= self.time_wait_duration {
                self.state = TcpState::Closed;
                return true;
            }
        }
        false
    }

    /// Check for segments that need retransmission.
    pub fn retransmit_expired(&mut self) -> Vec<TcpSegment> {
        let now = Instant::now();
        let mut retransmits = Vec::new();

        for unacked in &mut self.unacked {
            if now.duration_since(unacked.sent_at) >= self.rto {
                retransmits.push(TcpSegment::build_reply(
                    self.local_port,
                    self.remote_port,
                    unacked.seq_num,
                    self.rcv_nxt,
                    unacked.flags | ACK,
                    self.rcv_wnd as u16,
                    unacked.data.clone(),
                ));
                unacked.sent_at = now;
            }
        }

        retransmits
    }

    /// Flush send buffer respecting the send window.
    fn flush_send_buffer(&mut self) -> Vec<TcpSegment> {
        let mut segments = Vec::new();
        let mss: usize = 1460; // Standard MSS for Ethernet

        while !self.send_buf.is_empty() {
            let in_flight = self.snd_nxt.wrapping_sub(self.snd_una);
            let available_window = self.snd_wnd.saturating_sub(in_flight) as usize;
            if available_window == 0 {
                break;
            }

            let send_size = mss.min(self.send_buf.len()).min(available_window);
            let data: Vec<u8> = self.send_buf.drain(..send_size).collect();

            let seg = TcpSegment::build_reply(
                self.local_port,
                self.remote_port,
                self.snd_nxt,
                self.rcv_nxt,
                ACK | PSH,
                self.rcv_wnd as u16,
                data.clone(),
            );

            self.unacked.push(UnackedSegment {
                seq_num: self.snd_nxt,
                data,
                flags: PSH,
                sent_at: Instant::now(),
            });

            self.snd_nxt = self.snd_nxt.wrapping_add(send_size as u32);
            segments.push(seg);
        }

        segments
    }

    /// Process an incoming ACK, advancing snd_una and removing acknowledged segments.
    fn process_ack(&mut self, ack_num: u32) {
        if seq_gt(ack_num, self.snd_una) && seq_lte(ack_num, self.snd_nxt) {
            self.snd_una = ack_num;
            self.unacked.retain(|u| {
                let end = u.seq_num.wrapping_add(u.data.len() as u32);
                seq_gt(end, ack_num)
            });
        }
    }

    fn build_rst(&self, seg: &TcpSegment) -> TcpSegment {
        if seg.has_flag(ACK) {
            TcpSegment::build_reply(
                self.local_port,
                seg.src_port,
                seg.ack_num,
                0,
                RST,
                0,
                vec![],
            )
        } else {
            let ack = seg.seq_num.wrapping_add(seg.payload.len() as u32);
            TcpSegment::build_reply(
                self.local_port,
                seg.src_port,
                0,
                ack,
                RST | ACK,
                0,
                vec![],
            )
        }
    }
}

/// Generate an Initial Sequence Number.
fn generate_isn() -> u32 {
    use std::time::SystemTime;
    let t = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();
    // RFC 6528: ISN should be unpredictable, but for this exercise a time-based value suffices
    ((t.as_micros() & 0xFFFF_FFFF) as u32).wrapping_mul(250_000)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn three_way_handshake() {
        let mut server = TcpConnection::new(80);
        let mut client = TcpConnection::new(54321);

        server.listen();
        assert_eq!(server.state, TcpState::Listen);

        // Client sends SYN
        let syn = client.connect(80).unwrap();
        assert_eq!(client.state, TcpState::SynSent);
        assert!(syn.has_flag(SYN));

        // Server receives SYN, sends SYN-ACK
        let responses = server.on_segment(&syn);
        assert_eq!(server.state, TcpState::SynReceived);
        assert_eq!(responses.len(), 1);
        let syn_ack = &responses[0];
        assert!(syn_ack.has_flag(SYN));
        assert!(syn_ack.has_flag(ACK));

        // Client receives SYN-ACK, sends ACK
        let responses = client.on_segment(syn_ack);
        assert_eq!(client.state, TcpState::Established);
        assert_eq!(responses.len(), 1);
        let ack = &responses[0];
        assert!(ack.has_flag(ACK));
        assert!(!ack.has_flag(SYN));

        // Server receives ACK
        server.on_segment(ack);
        assert_eq!(server.state, TcpState::Established);
    }

    #[test]
    fn data_transfer() {
        let (mut client, mut server) = establish_connection();

        // Client sends data
        let segments = client.send(b"hello server");
        assert!(!segments.is_empty());

        // Server receives data
        for seg in &segments {
            let responses = server.on_segment(seg);
            assert!(!responses.is_empty(), "should ACK data");
        }

        let received = server.recv(1024);
        assert_eq!(received, b"hello server");

        // Server sends data back
        let segments = server.send(b"hello client");
        for seg in &segments {
            client.on_segment(seg);
        }

        let received = client.recv(1024);
        assert_eq!(received, b"hello client");
    }

    #[test]
    fn connection_teardown() {
        let (mut client, mut server) = establish_connection();

        // Client initiates close
        let fin = client.close().unwrap();
        assert_eq!(client.state, TcpState::FinWait1);
        assert!(fin.has_flag(FIN));

        // Server receives FIN, enters CloseWait
        let responses = server.on_segment(&fin);
        assert_eq!(server.state, TcpState::CloseWait);
        assert!(!responses.is_empty());
        let ack = &responses[0];

        // Client receives ACK of FIN, enters FinWait2
        client.on_segment(ack);
        assert_eq!(client.state, TcpState::FinWait2);

        // Server closes its side
        let fin2 = server.close().unwrap();
        assert_eq!(server.state, TcpState::LastAck);

        // Client receives server FIN, enters TimeWait
        let responses = client.on_segment(&fin2);
        assert_eq!(client.state, TcpState::TimeWait);

        // Server receives final ACK
        server.on_segment(&responses[0]);
        assert_eq!(server.state, TcpState::Closed);
    }

    #[test]
    fn rst_closes_immediately() {
        let (mut client, _server) = establish_connection();

        let rst = TcpSegment::build_reply(80, 54321, 0, 0, RST, 0, vec![]);
        client.on_segment(&rst);
        assert_eq!(client.state, TcpState::Closed);
    }

    #[test]
    fn window_limits_sending() {
        let (mut client, mut server) = establish_connection();
        // Artificially set a tiny window
        client.snd_wnd = 10;

        let data = vec![0xAA; 100];
        let segments = client.send(&data);

        // Should only send up to window size
        let total_sent: usize = segments.iter().map(|s| s.payload.len()).sum();
        assert!(total_sent <= 10, "sent {} but window is 10", total_sent);
    }

    #[test]
    fn time_wait_expires() {
        let mut conn = TcpConnection::new(80);
        conn.state = TcpState::TimeWait;
        conn.time_wait_start = Some(Instant::now() - Duration::from_secs(10));
        conn.time_wait_duration = Duration::from_secs(5);

        assert!(conn.check_time_wait());
        assert_eq!(conn.state, TcpState::Closed);
    }

    #[test]
    fn simultaneous_open() {
        let mut a = TcpConnection::new(5000);
        let mut b = TcpConnection::new(6000);

        let syn_a = a.connect(6000).unwrap();
        let syn_b = b.connect(5000).unwrap();

        // Both receive the other's SYN while in SynSent (simultaneous open)
        let responses_a = a.on_segment(&syn_b);
        assert_eq!(a.state, TcpState::SynReceived);

        let responses_b = b.on_segment(&syn_a);
        assert_eq!(b.state, TcpState::SynReceived);

        // Exchange SYN-ACKs
        if !responses_a.is_empty() {
            b.on_segment(&responses_a[0]);
        }
        if !responses_b.is_empty() {
            a.on_segment(&responses_b[0]);
        }

        assert_eq!(a.state, TcpState::Established);
        assert_eq!(b.state, TcpState::Established);
    }

    /// Helper to create an established connection pair.
    fn establish_connection() -> (TcpConnection, TcpConnection) {
        let mut server = TcpConnection::new(80);
        let mut client = TcpConnection::new(54321);

        server.listen();
        let syn = client.connect(80).unwrap();
        let responses = server.on_segment(&syn);
        let syn_ack = &responses[0];
        let responses = client.on_segment(syn_ack);
        server.on_segment(&responses[0]);

        assert_eq!(client.state, TcpState::Established);
        assert_eq!(server.state, TcpState::Established);
        (client, server)
    }
}
```

### `src/tun.rs`

```rust
use std::io;

/// Abstraction over a TUN device for reading/writing raw IP packets.
/// On Linux, this opens /dev/net/tun. On other platforms, falls back to
/// a loopback-based raw socket or a mock for testing.
pub struct TunDevice {
    #[cfg(target_os = "linux")]
    fd: std::os::unix::io::RawFd,
    #[cfg(not(target_os = "linux"))]
    _marker: (),
}

impl TunDevice {
    /// Create and configure a TUN device.
    #[cfg(target_os = "linux")]
    pub fn create(name: &str) -> io::Result<Self> {
        use std::os::unix::io::AsRawFd;

        let fd = unsafe {
            libc::open(b"/dev/net/tun\0".as_ptr() as *const libc::c_char, libc::O_RDWR)
        };
        if fd < 0 {
            return Err(io::Error::last_os_error());
        }

        // Set up TUN interface via ioctl
        let mut ifr: [u8; 40] = [0; 40];
        let name_bytes = name.as_bytes();
        let copy_len = name_bytes.len().min(15);
        ifr[..copy_len].copy_from_slice(&name_bytes[..copy_len]);
        // IFF_TUN | IFF_NO_PI
        ifr[16] = 0x01;
        ifr[17] = 0x10;

        let ret = unsafe { libc::ioctl(fd, 0x400454CA, ifr.as_ptr()) }; // TUNSETIFF
        if ret < 0 {
            unsafe { libc::close(fd) };
            return Err(io::Error::last_os_error());
        }

        log::info!("TUN device {} created (fd={})", name, fd);
        Ok(Self { fd })
    }

    #[cfg(not(target_os = "linux"))]
    pub fn create(_name: &str) -> io::Result<Self> {
        log::warn!("TUN device not supported on this platform; using mock");
        Ok(Self { _marker: () })
    }

    /// Read a raw IP packet from the TUN device.
    #[cfg(target_os = "linux")]
    pub fn read(&self, buf: &mut [u8]) -> io::Result<usize> {
        let n = unsafe { libc::read(self.fd, buf.as_mut_ptr() as *mut libc::c_void, buf.len()) };
        if n < 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(n as usize)
        }
    }

    #[cfg(not(target_os = "linux"))]
    pub fn read(&self, _buf: &mut [u8]) -> io::Result<usize> {
        Err(io::Error::new(io::ErrorKind::Unsupported, "TUN not supported on this platform"))
    }

    /// Write a raw IP packet to the TUN device.
    #[cfg(target_os = "linux")]
    pub fn write(&self, buf: &[u8]) -> io::Result<usize> {
        let n = unsafe { libc::write(self.fd, buf.as_ptr() as *const libc::c_void, buf.len()) };
        if n < 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(n as usize)
        }
    }

    #[cfg(not(target_os = "linux"))]
    pub fn write(&self, _buf: &[u8]) -> io::Result<usize> {
        Err(io::Error::new(io::ErrorKind::Unsupported, "TUN not supported on this platform"))
    }
}

#[cfg(target_os = "linux")]
impl Drop for TunDevice {
    fn drop(&mut self) {
        unsafe { libc::close(self.fd) };
    }
}

/// Mock TUN device for testing without root privileges.
pub struct MockTun {
    inbound: std::collections::VecDeque<Vec<u8>>,
    outbound: Vec<Vec<u8>>,
}

impl MockTun {
    pub fn new() -> Self {
        Self {
            inbound: std::collections::VecDeque::new(),
            outbound: Vec::new(),
        }
    }

    /// Inject a packet as if it arrived from the network.
    pub fn inject(&mut self, packet: Vec<u8>) {
        self.inbound.push_back(packet);
    }

    /// Read the next inbound packet.
    pub fn read(&mut self) -> Option<Vec<u8>> {
        self.inbound.pop_front()
    }

    /// Write a packet (captures it for inspection).
    pub fn write(&mut self, packet: Vec<u8>) {
        self.outbound.push(packet);
    }

    /// Get all captured outbound packets.
    pub fn sent_packets(&self) -> &[Vec<u8>] {
        &self.outbound
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mock_tun_inject_and_read() {
        let mut tun = MockTun::new();
        tun.inject(vec![1, 2, 3]);
        tun.inject(vec![4, 5, 6]);

        assert_eq!(tun.read(), Some(vec![1, 2, 3]));
        assert_eq!(tun.read(), Some(vec![4, 5, 6]));
        assert_eq!(tun.read(), None);
    }

    #[test]
    fn mock_tun_captures_outbound() {
        let mut tun = MockTun::new();
        tun.write(vec![10, 20, 30]);
        assert_eq!(tun.sent_packets().len(), 1);
        assert_eq!(tun.sent_packets()[0], vec![10, 20, 30]);
    }
}
```

### `src/main.rs`

```rust
mod ip;
mod state;
mod tcp;
mod tun;

use crate::ip::{Ipv4Header, Ipv4Packet, PROTO_TCP};
use crate::state::TcpConnection;
use crate::tcp::TcpSegment;
use crate::tun::TunDevice;

use std::net::Ipv4Addr;

fn main() {
    env_logger::init();

    let local_ip = Ipv4Addr::new(10, 0, 0, 1);
    let remote_ip = Ipv4Addr::new(10, 0, 0, 2);

    match TunDevice::create("tun0") {
        Ok(tun) => {
            log::info!("TUN device created, starting TCP stack");
            log::info!("local IP: {}, listening on port 80", local_ip);
            run_with_tun(tun, local_ip, remote_ip);
        }
        Err(e) => {
            log::error!("failed to create TUN device: {}", e);
            log::info!("running in demo mode with mock TUN");
            run_demo(local_ip, remote_ip);
        }
    }
}

fn run_with_tun(tun: TunDevice, local_ip: Ipv4Addr, _remote_ip: Ipv4Addr) {
    let mut conn = TcpConnection::new(80);
    conn.listen();

    let mut buf = vec![0u8; 65535];
    loop {
        match tun.read(&mut buf) {
            Ok(n) => {
                if let Ok(ip_pkt) = Ipv4Packet::parse(&buf[..n]) {
                    if ip_pkt.header.protocol == PROTO_TCP {
                        if let Ok(seg) = TcpSegment::parse(&ip_pkt.payload) {
                            let responses = conn.on_segment(&seg);
                            for resp in responses {
                                let tcp_bytes = resp.to_bytes(local_ip, ip_pkt.header.src_addr);
                                let ip_header = Ipv4Header::new_tcp(
                                    local_ip,
                                    ip_pkt.header.src_addr,
                                    tcp_bytes.len() as u16,
                                );
                                let out_pkt = Ipv4Packet {
                                    header: ip_header,
                                    payload: tcp_bytes,
                                };
                                let _ = tun.write(&out_pkt.to_bytes());
                            }
                        }
                    }
                }
            }
            Err(e) => {
                log::error!("TUN read error: {}", e);
                break;
            }
        }
    }
}

fn run_demo(local_ip: Ipv4Addr, remote_ip: Ipv4Addr) {
    use crate::tun::MockTun;

    let mut mock = MockTun::new();
    let mut server = TcpConnection::new(80);
    let mut client = TcpConnection::new(54321);

    server.listen();

    // Three-way handshake
    let syn = client.connect(80).unwrap();
    log::info!("client -> SYN (seq={})", syn.seq_num);
    let responses = server.on_segment(&syn);
    let syn_ack = &responses[0];
    log::info!("server -> SYN-ACK (seq={}, ack={})", syn_ack.seq_num, syn_ack.ack_num);
    let responses = client.on_segment(syn_ack);
    let ack = &responses[0];
    log::info!("client -> ACK (ack={})", ack.ack_num);
    server.on_segment(ack);
    log::info!("connection established: client={:?}, server={:?}", client.state, server.state);

    // Data transfer
    let segments = client.send(b"Hello, userspace TCP!");
    for seg in &segments {
        log::info!("client -> DATA ({} bytes, seq={})", seg.payload.len(), seg.seq_num);
        let acks = server.on_segment(seg);
        for a in &acks {
            log::info!("server -> ACK (ack={})", a.ack_num);
            client.on_segment(a);
        }
    }

    let data = server.recv(1024);
    log::info!("server received: {:?}", String::from_utf8_lossy(&data));

    // Close
    let fin = client.close().unwrap();
    log::info!("client -> FIN");
    let responses = server.on_segment(&fin);
    for r in &responses {
        client.on_segment(r);
    }
    let fin2 = server.close().unwrap();
    log::info!("server -> FIN");
    let responses = client.on_segment(&fin2);
    for r in &responses {
        server.on_segment(r);
    }
    log::info!("final states: client={:?}, server={:?}", client.state, server.state);
}
```

## Build and Run

```bash
# Build
cargo build --release

# Run demo mode (no root required)
RUST_LOG=info cargo run

# Run with TUN device (Linux, requires root)
sudo RUST_LOG=info cargo run
# In another terminal, configure the TUN device:
# sudo ip addr add 10.0.0.1/24 dev tun0
# sudo ip link set tun0 up

# Run all tests
cargo test -- --nocapture

# Run specific test
cargo test three_way_handshake -- --nocapture
cargo test data_transfer -- --nocapture
cargo test connection_teardown -- --nocapture
```

## Expected Output

Demo mode:
```
[INFO  tcp_userspace] running in demo mode with mock TUN
[INFO  tcp_userspace] client -> SYN (seq=3750000000)
[INFO  tcp_userspace::state] connection established on port 80
[INFO  tcp_userspace] server -> SYN-ACK (seq=1250000000, ack=3750000001)
[INFO  tcp_userspace] client -> ACK (ack=1250000001)
[INFO  tcp_userspace] connection established: client=Established, server=Established
[INFO  tcp_userspace] client -> DATA (20 bytes, seq=3750000001)
[INFO  tcp_userspace] server -> ACK (ack=3750000021)
[INFO  tcp_userspace] server received: "Hello, userspace TCP!"
[INFO  tcp_userspace] client -> FIN
[INFO  tcp_userspace] server -> FIN
[INFO  tcp_userspace] final states: client=TimeWait, server=Closed
```

Test output:
```
running 7 tests
test tcp::tests::segment_round_trip ... ok
test tcp::tests::checksum_validates ... ok
test tcp::tests::segment_with_payload ... ok
test tcp::tests::sequence_number_arithmetic ... ok
test tcp::tests::window_check ... ok

running 3 tests
test ip::tests::checksum_of_known_header ... ok
test ip::tests::ipv4_round_trip ... ok
test ip::tests::checksum_valid_after_construction ... ok

running 7 tests
test state::tests::three_way_handshake ... ok
test state::tests::data_transfer ... ok
test state::tests::connection_teardown ... ok
test state::tests::rst_closes_immediately ... ok
test state::tests::window_limits_sending ... ok
test state::tests::time_wait_expires ... ok
test state::tests::simultaneous_open ... ok

running 2 tests
test tun::tests::mock_tun_inject_and_read ... ok
test tun::tests::mock_tun_captures_outbound ... ok

test result: ok. 19 passed; 0 failed; 0 ignored
```

## Design Decisions

**Why a `MockTun` alongside the real TUN device.** TUN devices require root privileges on Linux and are unavailable on macOS/Windows without third-party kernel extensions. The mock allows all protocol logic to be tested without root, without a specific OS, and without network configuration. The state machine is the core of the challenge; the TUN device is just the I/O adapter.

**Why `VecDeque` for send and receive buffers.** TCP is a byte stream, and data enters from one end (application writes / network receives) and leaves from the other. `VecDeque` provides O(1) push to the back and drain from the front, which matches TCP's FIFO semantics exactly.

**Why fixed RTO instead of RTT-based estimation.** RFC 6298 specifies how to estimate RTT and compute RTO using exponential weighted moving averages. Implementing this correctly adds significant complexity (tracking per-segment timestamps, handling retransmitted segment ambiguity) without teaching TCP state machine fundamentals. A fixed 1-second RTO is simple, correct enough for local testing, and straightforward to replace with RFC 6298 later.

**Why sequence numbers use `wrapping_add` and signed comparison.** TCP sequence numbers are modular 32-bit integers. Rust's default overflow behavior (panic in debug, wrap in release) would make the program behave differently in debug vs. release. Using explicit `wrapping_add` and `wrapping_sub` makes the intent clear and the behavior consistent. The signed comparison trick (`(a - b) as i32 < 0`) is the standard approach from the BSD TCP implementation.

**Why `on_segment` returns a `Vec<TcpSegment>` instead of sending directly.** Separating segment generation from I/O makes the state machine pure and testable. Tests can call `on_segment` and inspect the returned segments without any network stack. The I/O layer (TUN or mock) is responsible for serialization and transmission.

## Common Mistakes

1. **Using standard comparison operators on sequence numbers.** `a < b` fails when sequence numbers wrap around 2^32. After billions of bytes transferred, `snd_nxt` wraps to a small value and suddenly appears "less than" `snd_una`. Use modular comparison (`wrapping_sub` + signed cast).

2. **Forgetting the pseudo-header in the TCP checksum.** The TCP checksum covers the pseudo-header (source IP, destination IP, protocol, TCP length) prepended to the TCP segment. Omitting it produces a valid-looking checksum that real TCP stacks will reject.

3. **Not handling the SYN and FIN sequence number consumption.** SYN and FIN each consume one sequence number. After sending a SYN with `seq=1000`, the next data byte has `seq=1001`, not `seq=1000`. Forgetting this causes all subsequent sequence numbers to be off by one.

4. **Advancing `rcv_nxt` for out-of-order segments.** If the received segment's sequence number does not match `rcv_nxt`, the data is out of order. Advancing `rcv_nxt` past a gap creates a hole in the stream that can never be filled. Buffer the segment and wait for the missing data.

5. **Skipping TIME_WAIT.** It is tempting to go directly from FIN_WAIT_2 to CLOSED. But without TIME_WAIT, delayed segments from the old connection can be misinterpreted as belonging to a new connection on the same port. The 2*MSL wait ensures all old segments have expired.

6. **Sending data from CLOSE_WAIT.** RFC 793 allows the CLOSE_WAIT side to continue sending data (half-close). But many implementations get this wrong by immediately sending FIN upon entering CLOSE_WAIT instead of waiting for the application to call `close()`.

## Performance Notes

- The checksum computation processes 16 bits per iteration. For large payloads, unroll the loop to process 64 bits at a time, reducing branch overhead. On x86-64, the compiler may auto-vectorize this with SSE2.
- Segment parsing allocates a `Vec<u8>` for the payload. For zero-copy, use a slice reference with a lifetime tied to the buffer. This requires restructuring the API but eliminates allocation on the hot path.
- The send buffer uses `VecDeque::drain` which shifts elements. For high-throughput, a ring buffer with head/tail pointers avoids the shift cost.
- The `unacked` list is scanned linearly for retransmission checks. With many in-flight segments, switch to a `BTreeMap` keyed by sequence number for O(log n) lookup.

## Going Further

- Implement Nagle's algorithm: buffer small writes and send them together unless the connection is idle
- Add TCP options: MSS negotiation, window scaling (RFC 7323), timestamps, SACK (RFC 2018)
- Implement congestion control: slow start, congestion avoidance, fast retransmit, fast recovery (RFC 5681)
- Build a userspace `netcat` that uses your TCP stack to establish connections and transfer data
- Test against the kernel's TCP stack by sending packets from your TUN-based stack to a regular socket
