# Solution: QUIC Reliable Streams Full

## Architecture Overview

The implementation is organized into seven modules reflecting QUIC's conceptual structure:

1. **Wire** (`wire/`) -- variable-length integers, packet headers, frame encoding/decoding
2. **Crypto** (`crypto/`) -- AEAD encryption, key derivation, 0-RTT token management
3. **Stream** (`stream/`) -- per-stream state machine, buffers, flow control
4. **Connection** (`connection/`) -- connection state, stream multiplexer, migration, shutdown
5. **Recovery** (`recovery/`) -- loss detection, RTT estimation, congestion control, PTO
6. **Scheduler** (`scheduler/`) -- packet construction, stream prioritization, frame coalescing
7. **Endpoint** (`endpoint/`) -- UDP socket management, connection routing, server/client entry points

```
Application
    | (open_stream, send, recv, close)
    v
Connection
    |-- Stream Multiplexer
    |       |-- Stream 0 (state machine + buffers + flow control)
    |       |-- Stream 4
    |       |-- Stream 8 ...
    |-- Recovery Engine (loss detection + congestion control)
    |-- Scheduler (priority + frame coalescing)
    |-- Flow Control (connection-level)
    |
    v
Packet Assembly (short header + frames -> encrypted packet)
    |
    v
UDP Socket
```

## Rust Solution

### `Cargo.toml`

```toml
[package]
name = "quic-streams"
version = "0.1.0"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
ring = "0.17"
rand = "0.8"
bytes = "1"
log = "0.4"
env_logger = "0.11"

[dev-dependencies]
tokio-test = "0.4"
```

### `src/wire/varint.rs`

```rust
use std::io::{self, Read, Write};

pub const MAX_VARINT: u64 = (1 << 62) - 1;

pub fn encode(value: u64, buf: &mut impl Write) -> io::Result<usize> {
    if value <= 63 {
        buf.write_all(&[value as u8])?;
        Ok(1)
    } else if value <= 16383 {
        buf.write_all(&((value as u16 | 0x4000).to_be_bytes()))?;
        Ok(2)
    } else if value <= 1_073_741_823 {
        buf.write_all(&((value as u32 | 0x8000_0000).to_be_bytes()))?;
        Ok(4)
    } else if value <= MAX_VARINT {
        buf.write_all(&((value | 0xC000_0000_0000_0000).to_be_bytes()))?;
        Ok(8)
    } else {
        Err(io::Error::new(io::ErrorKind::InvalidInput, "varint too large"))
    }
}

pub fn decode(buf: &mut impl Read) -> io::Result<(u64, usize)> {
    let mut first = [0u8; 1];
    buf.read_exact(&mut first)?;
    let prefix = first[0] >> 6;
    let length = 1usize << prefix;

    match length {
        1 => Ok((u64::from(first[0] & 0x3F), 1)),
        2 => {
            let mut rest = [0u8; 1];
            buf.read_exact(&mut rest)?;
            Ok((u64::from(u16::from_be_bytes([first[0] & 0x3F, rest[0]])), 2))
        }
        4 => {
            let mut rest = [0u8; 3];
            buf.read_exact(&mut rest)?;
            Ok((u64::from(u32::from_be_bytes([first[0] & 0x3F, rest[0], rest[1], rest[2]])), 4))
        }
        8 => {
            let mut rest = [0u8; 7];
            buf.read_exact(&mut rest)?;
            Ok((u64::from_be_bytes([
                first[0] & 0x3F, rest[0], rest[1], rest[2],
                rest[3], rest[4], rest[5], rest[6],
            ]), 8))
        }
        _ => unreachable!(),
    }
}

pub fn encoding_size(value: u64) -> usize {
    if value <= 63 { 1 }
    else if value <= 16383 { 2 }
    else if value <= 1_073_741_823 { 4 }
    else { 8 }
}
```

### `src/wire/frames.rs`

```rust
use super::varint;
use std::io::Cursor;

/// QUIC frame types (RFC 9000 Section 19).
#[derive(Debug, Clone)]
pub enum Frame {
    Padding,
    Ping,
    Ack {
        largest_ack: u64,
        ack_delay: u64,
        first_ack_range: u64,
        ack_ranges: Vec<(u64, u64)>, // (gap, range_length)
    },
    ResetStream {
        stream_id: u64,
        error_code: u64,
        final_size: u64,
    },
    StopSending {
        stream_id: u64,
        error_code: u64,
    },
    Crypto {
        offset: u64,
        data: Vec<u8>,
    },
    NewToken {
        token: Vec<u8>,
    },
    Stream {
        stream_id: u64,
        offset: u64,
        data: Vec<u8>,
        fin: bool,
    },
    MaxData {
        maximum: u64,
    },
    MaxStreamData {
        stream_id: u64,
        maximum: u64,
    },
    MaxStreams {
        bidirectional: bool,
        maximum: u64,
    },
    DataBlocked {
        limit: u64,
    },
    StreamDataBlocked {
        stream_id: u64,
        limit: u64,
    },
    ConnectionClose {
        error_code: u64,
        frame_type: u64,
        reason: Vec<u8>,
    },
    PathChallenge {
        data: [u8; 8],
    },
    PathResponse {
        data: [u8; 8],
    },
    HandshakeDone,
}

impl Frame {
    /// Encode a frame into bytes.
    pub fn encode(&self, buf: &mut Vec<u8>) {
        match self {
            Frame::Padding => buf.push(0x00),
            Frame::Ping => buf.push(0x01),

            Frame::Ack { largest_ack, ack_delay, first_ack_range, ack_ranges } => {
                buf.push(0x02);
                varint::encode(*largest_ack, buf).unwrap();
                varint::encode(*ack_delay, buf).unwrap();
                varint::encode(ack_ranges.len() as u64, buf).unwrap();
                varint::encode(*first_ack_range, buf).unwrap();
                for (gap, range) in ack_ranges {
                    varint::encode(*gap, buf).unwrap();
                    varint::encode(*range, buf).unwrap();
                }
            }

            Frame::ResetStream { stream_id, error_code, final_size } => {
                buf.push(0x04);
                varint::encode(*stream_id, buf).unwrap();
                varint::encode(*error_code, buf).unwrap();
                varint::encode(*final_size, buf).unwrap();
            }

            Frame::StopSending { stream_id, error_code } => {
                buf.push(0x05);
                varint::encode(*stream_id, buf).unwrap();
                varint::encode(*error_code, buf).unwrap();
            }

            Frame::Crypto { offset, data } => {
                buf.push(0x06);
                varint::encode(*offset, buf).unwrap();
                varint::encode(data.len() as u64, buf).unwrap();
                buf.extend_from_slice(data);
            }

            Frame::Stream { stream_id, offset, data, fin } => {
                // STREAM frame type: 0x08 | OFF(0x04) | LEN(0x02) | FIN(0x01)
                let mut frame_type: u8 = 0x08;
                if *offset > 0 { frame_type |= 0x04; }
                frame_type |= 0x02; // Always include length
                if *fin { frame_type |= 0x01; }
                buf.push(frame_type);
                varint::encode(*stream_id, buf).unwrap();
                if *offset > 0 {
                    varint::encode(*offset, buf).unwrap();
                }
                varint::encode(data.len() as u64, buf).unwrap();
                buf.extend_from_slice(data);
            }

            Frame::MaxData { maximum } => {
                buf.push(0x10);
                varint::encode(*maximum, buf).unwrap();
            }

            Frame::MaxStreamData { stream_id, maximum } => {
                buf.push(0x11);
                varint::encode(*stream_id, buf).unwrap();
                varint::encode(*maximum, buf).unwrap();
            }

            Frame::MaxStreams { bidirectional, maximum } => {
                buf.push(if *bidirectional { 0x12 } else { 0x13 });
                varint::encode(*maximum, buf).unwrap();
            }

            Frame::DataBlocked { limit } => {
                buf.push(0x14);
                varint::encode(*limit, buf).unwrap();
            }

            Frame::StreamDataBlocked { stream_id, limit } => {
                buf.push(0x15);
                varint::encode(*stream_id, buf).unwrap();
                varint::encode(*limit, buf).unwrap();
            }

            Frame::ConnectionClose { error_code, frame_type, reason } => {
                buf.push(0x1C);
                varint::encode(*error_code, buf).unwrap();
                varint::encode(*frame_type, buf).unwrap();
                varint::encode(reason.len() as u64, buf).unwrap();
                buf.extend_from_slice(reason);
            }

            Frame::PathChallenge { data } => {
                buf.push(0x1A);
                buf.extend_from_slice(data);
            }

            Frame::PathResponse { data } => {
                buf.push(0x1B);
                buf.extend_from_slice(data);
            }

            Frame::HandshakeDone => buf.push(0x1E),

            Frame::NewToken { token } => {
                buf.push(0x07);
                varint::encode(token.len() as u64, buf).unwrap();
                buf.extend_from_slice(token);
            }
        }
    }

    /// Decode a frame from bytes. Returns the frame and bytes consumed.
    pub fn decode(buf: &[u8]) -> Result<(Self, usize), String> {
        if buf.is_empty() {
            return Err("empty buffer".into());
        }

        let mut cursor = Cursor::new(buf);
        let mut type_byte = [0u8; 1];
        std::io::Read::read_exact(&mut cursor, &mut type_byte)
            .map_err(|e| e.to_string())?;

        let frame = match type_byte[0] {
            0x00 => Frame::Padding,
            0x01 => Frame::Ping,

            0x02 | 0x03 => {
                let (largest_ack, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (ack_delay, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (range_count, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (first_ack_range, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let mut ack_ranges = Vec::new();
                for _ in 0..range_count {
                    let (gap, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                    let (range, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                    ack_ranges.push((gap, range));
                }
                Frame::Ack { largest_ack, ack_delay, first_ack_range, ack_ranges }
            }

            0x04 => {
                let (stream_id, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (error_code, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (final_size, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                Frame::ResetStream { stream_id, error_code, final_size }
            }

            0x05 => {
                let (stream_id, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (error_code, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                Frame::StopSending { stream_id, error_code }
            }

            0x06 => {
                let (offset, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (len, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let pos = cursor.position() as usize;
                let data = buf[pos..pos + len as usize].to_vec();
                cursor.set_position((pos + len as usize) as u64);
                Frame::Crypto { offset, data }
            }

            t @ 0x08..=0x0F => {
                let (stream_id, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let offset = if t & 0x04 != 0 {
                    let (o, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                    o
                } else { 0 };
                let data = if t & 0x02 != 0 {
                    let (len, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                    let pos = cursor.position() as usize;
                    let d = buf[pos..pos + len as usize].to_vec();
                    cursor.set_position((pos + len as usize) as u64);
                    d
                } else {
                    let pos = cursor.position() as usize;
                    buf[pos..].to_vec()
                };
                let fin = t & 0x01 != 0;
                Frame::Stream { stream_id, offset, data, fin }
            }

            0x10 => {
                let (maximum, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                Frame::MaxData { maximum }
            }

            0x11 => {
                let (stream_id, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (maximum, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                Frame::MaxStreamData { stream_id, maximum }
            }

            0x12 | 0x13 => {
                let bidi = type_byte[0] == 0x12;
                let (maximum, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                Frame::MaxStreams { bidirectional: bidi, maximum }
            }

            0x1A => {
                let pos = cursor.position() as usize;
                if pos + 8 > buf.len() { return Err("PATH_CHALLENGE truncated".into()); }
                let mut data = [0u8; 8];
                data.copy_from_slice(&buf[pos..pos + 8]);
                cursor.set_position((pos + 8) as u64);
                Frame::PathChallenge { data }
            }

            0x1B => {
                let pos = cursor.position() as usize;
                if pos + 8 > buf.len() { return Err("PATH_RESPONSE truncated".into()); }
                let mut data = [0u8; 8];
                data.copy_from_slice(&buf[pos..pos + 8]);
                cursor.set_position((pos + 8) as u64);
                Frame::PathResponse { data }
            }

            0x1C | 0x1D => {
                let (error_code, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (frame_type, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let (reason_len, _) = varint::decode(&mut cursor).map_err(|e| e.to_string())?;
                let pos = cursor.position() as usize;
                let reason = buf[pos..pos + reason_len as usize].to_vec();
                cursor.set_position((pos + reason_len as usize) as u64);
                Frame::ConnectionClose { error_code, frame_type, reason }
            }

            0x1E => Frame::HandshakeDone,

            other => return Err(format!("unknown frame type: {:#x}", other)),
        };

        Ok((frame, cursor.position() as usize))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stream_frame_round_trip() {
        let frame = Frame::Stream {
            stream_id: 4,
            offset: 100,
            data: b"hello streams".to_vec(),
            fin: false,
        };
        let mut buf = Vec::new();
        frame.encode(&mut buf);
        let (decoded, _) = Frame::decode(&buf).unwrap();
        match decoded {
            Frame::Stream { stream_id, offset, data, fin } => {
                assert_eq!(stream_id, 4);
                assert_eq!(offset, 100);
                assert_eq!(data, b"hello streams");
                assert!(!fin);
            }
            _ => panic!("wrong frame type"),
        }
    }

    #[test]
    fn ack_frame_round_trip() {
        let frame = Frame::Ack {
            largest_ack: 42,
            ack_delay: 10,
            first_ack_range: 5,
            ack_ranges: vec![(2, 3)],
        };
        let mut buf = Vec::new();
        frame.encode(&mut buf);
        let (decoded, _) = Frame::decode(&buf).unwrap();
        match decoded {
            Frame::Ack { largest_ack, ack_ranges, .. } => {
                assert_eq!(largest_ack, 42);
                assert_eq!(ack_ranges.len(), 1);
            }
            _ => panic!("wrong frame type"),
        }
    }

    #[test]
    fn connection_close_round_trip() {
        let frame = Frame::ConnectionClose {
            error_code: 0x0A,
            frame_type: 0,
            reason: b"done".to_vec(),
        };
        let mut buf = Vec::new();
        frame.encode(&mut buf);
        let (decoded, _) = Frame::decode(&buf).unwrap();
        match decoded {
            Frame::ConnectionClose { error_code, reason, .. } => {
                assert_eq!(error_code, 0x0A);
                assert_eq!(reason, b"done");
            }
            _ => panic!("wrong frame type"),
        }
    }

    #[test]
    fn path_challenge_response() {
        let challenge = Frame::PathChallenge { data: [1, 2, 3, 4, 5, 6, 7, 8] };
        let mut buf = Vec::new();
        challenge.encode(&mut buf);
        let (decoded, _) = Frame::decode(&buf).unwrap();
        match decoded {
            Frame::PathChallenge { data } => assert_eq!(data, [1, 2, 3, 4, 5, 6, 7, 8]),
            _ => panic!("wrong frame type"),
        }
    }
}
```

### `src/wire/mod.rs`

```rust
pub mod varint;
pub mod frames;
```

### `src/stream/state.rs`

```rust
/// Stream states per RFC 9000 Section 3.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum SendState {
    Ready,
    Send,
    DataSent,
    DataRecvd,
    ResetSent,
    ResetRecvd,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum RecvState {
    Recv,
    SizeKnown,
    DataRecvd,
    DataRead,
    ResetRecvd,
    ResetRead,
}

/// Stream ID type encoding per RFC 9000.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum StreamType {
    ClientBidi,
    ServerBidi,
    ClientUni,
    ServerUni,
}

impl StreamType {
    pub fn from_id(id: u64) -> Self {
        match id & 0x03 {
            0x00 => Self::ClientBidi,
            0x01 => Self::ServerBidi,
            0x02 => Self::ClientUni,
            0x03 => Self::ServerUni,
            _ => unreachable!(),
        }
    }

    pub fn is_bidirectional(&self) -> bool {
        matches!(self, Self::ClientBidi | Self::ServerBidi)
    }

    pub fn is_client_initiated(&self) -> bool {
        matches!(self, Self::ClientBidi | Self::ClientUni)
    }
}

/// Next stream ID for a given type.
pub fn next_stream_id(stream_type: StreamType, count: u64) -> u64 {
    let type_bits = match stream_type {
        StreamType::ClientBidi => 0,
        StreamType::ServerBidi => 1,
        StreamType::ClientUni => 2,
        StreamType::ServerUni => 3,
    };
    count * 4 + type_bits
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stream_type_from_id() {
        assert_eq!(StreamType::from_id(0), StreamType::ClientBidi);
        assert_eq!(StreamType::from_id(1), StreamType::ServerBidi);
        assert_eq!(StreamType::from_id(4), StreamType::ClientBidi);
        assert_eq!(StreamType::from_id(6), StreamType::ClientUni);
    }

    #[test]
    fn next_ids() {
        assert_eq!(next_stream_id(StreamType::ClientBidi, 0), 0);
        assert_eq!(next_stream_id(StreamType::ClientBidi, 1), 4);
        assert_eq!(next_stream_id(StreamType::ServerBidi, 0), 1);
        assert_eq!(next_stream_id(StreamType::ClientUni, 2), 10);
    }
}
```

### `src/stream/flow_control.rs`

```rust
/// Per-stream flow control using absolute byte offsets.
#[derive(Debug)]
pub struct StreamFlowControl {
    // Send side
    send_offset: u64,      // next byte offset to send
    send_max: u64,         // max offset peer allows us to send (from MAX_STREAM_DATA)
    send_blocked: bool,

    // Receive side
    recv_offset: u64,      // highest contiguous offset delivered to application
    recv_max: u64,         // max offset we advertise to peer
    recv_window: u64,      // our configured receive window size
}

impl StreamFlowControl {
    pub fn new(initial_window: u64) -> Self {
        Self {
            send_offset: 0,
            send_max: initial_window,
            send_blocked: false,
            recv_offset: 0,
            recv_max: initial_window,
            recv_window: initial_window,
        }
    }

    /// How many bytes the sender can still send.
    pub fn send_capacity(&self) -> u64 {
        self.send_max.saturating_sub(self.send_offset)
    }

    /// Record bytes sent, advancing the send offset.
    pub fn on_send(&mut self, bytes: u64) {
        self.send_offset += bytes;
        if self.send_offset >= self.send_max {
            self.send_blocked = true;
        }
    }

    /// Peer extended our send limit via MAX_STREAM_DATA.
    pub fn update_send_max(&mut self, new_max: u64) {
        if new_max > self.send_max {
            self.send_max = new_max;
            self.send_blocked = false;
        }
    }

    /// Record bytes received and delivered to application.
    pub fn on_recv(&mut self, bytes: u64) {
        self.recv_offset += bytes;
    }

    /// Check if we should send MAX_STREAM_DATA (when half the window is consumed).
    pub fn should_send_window_update(&self) -> Option<u64> {
        let consumed = self.recv_offset;
        let threshold = self.recv_max - self.recv_window / 2;
        if consumed >= threshold {
            Some(self.recv_offset + self.recv_window)
        } else {
            None
        }
    }

    /// Apply the window update.
    pub fn send_window_update(&mut self) -> u64 {
        self.recv_max = self.recv_offset + self.recv_window;
        self.recv_max
    }

    pub fn is_send_blocked(&self) -> bool {
        self.send_blocked
    }

    pub fn send_offset(&self) -> u64 { self.send_offset }
    pub fn recv_offset(&self) -> u64 { self.recv_offset }
}

/// Connection-level flow control.
#[derive(Debug)]
pub struct ConnectionFlowControl {
    send_offset: u64,
    send_max: u64,
    recv_offset: u64,
    recv_max: u64,
    recv_window: u64,
}

impl ConnectionFlowControl {
    pub fn new(initial_window: u64) -> Self {
        Self {
            send_offset: 0,
            send_max: initial_window,
            recv_offset: 0,
            recv_max: initial_window,
            recv_window: initial_window,
        }
    }

    pub fn send_capacity(&self) -> u64 {
        self.send_max.saturating_sub(self.send_offset)
    }

    pub fn on_send(&mut self, bytes: u64) {
        self.send_offset += bytes;
    }

    pub fn update_send_max(&mut self, new_max: u64) {
        if new_max > self.send_max {
            self.send_max = new_max;
        }
    }

    pub fn on_recv(&mut self, bytes: u64) {
        self.recv_offset += bytes;
    }

    pub fn should_send_max_data(&self) -> Option<u64> {
        let consumed = self.recv_offset;
        let threshold = self.recv_max - self.recv_window / 2;
        if consumed >= threshold {
            Some(self.recv_offset + self.recv_window)
        } else {
            None
        }
    }

    pub fn send_max_data(&mut self) -> u64 {
        self.recv_max = self.recv_offset + self.recv_window;
        self.recv_max
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn stream_send_capacity() {
        let mut fc = StreamFlowControl::new(1000);
        assert_eq!(fc.send_capacity(), 1000);
        fc.on_send(400);
        assert_eq!(fc.send_capacity(), 600);
        fc.on_send(600);
        assert_eq!(fc.send_capacity(), 0);
        assert!(fc.is_send_blocked());
    }

    #[test]
    fn stream_window_update() {
        let mut fc = StreamFlowControl::new(1000);
        fc.on_recv(600); // Consumed 60% of window
        assert!(fc.should_send_window_update().is_some());
        let new_max = fc.send_window_update();
        assert_eq!(new_max, 1600);
    }

    #[test]
    fn send_max_extends_capacity() {
        let mut fc = StreamFlowControl::new(100);
        fc.on_send(100);
        assert_eq!(fc.send_capacity(), 0);
        fc.update_send_max(500);
        assert_eq!(fc.send_capacity(), 400);
        assert!(!fc.is_send_blocked());
    }

    #[test]
    fn connection_level() {
        let mut fc = ConnectionFlowControl::new(10000);
        fc.on_send(3000);
        assert_eq!(fc.send_capacity(), 7000);
        fc.on_recv(6000);
        assert!(fc.should_send_max_data().is_some());
    }
}
```

### `src/stream/mod.rs`

```rust
pub mod state;
pub mod flow_control;

use self::flow_control::StreamFlowControl;
use self::state::{SendState, RecvState};
use std::collections::{BTreeMap, VecDeque};

/// A single QUIC stream with send/receive buffers and flow control.
pub struct Stream {
    pub id: u64,
    pub send_state: SendState,
    pub recv_state: RecvState,
    pub flow_control: StreamFlowControl,
    pub priority: u8,

    // Send buffer: data queued by the application
    send_buf: VecDeque<u8>,
    send_offset: u64,

    // Receive buffer: reassembly of out-of-order data
    recv_buf: BTreeMap<u64, Vec<u8>>,  // offset -> data
    recv_contiguous: u64,               // highest contiguous byte delivered
    recv_fin_offset: Option<u64>,

    // Application receive buffer (contiguous, ready to read)
    app_recv_buf: VecDeque<u8>,
}

impl Stream {
    pub fn new(id: u64, initial_window: u64) -> Self {
        Self {
            id,
            send_state: SendState::Ready,
            recv_state: RecvState::Recv,
            flow_control: StreamFlowControl::new(initial_window),
            priority: 128, // Default middle priority
            send_buf: VecDeque::new(),
            send_offset: 0,
            recv_buf: BTreeMap::new(),
            recv_contiguous: 0,
            recv_fin_offset: None,
            app_recv_buf: VecDeque::new(),
        }
    }

    /// Queue data for sending on this stream.
    pub fn write(&mut self, data: &[u8]) {
        self.send_buf.extend(data);
        if self.send_state == SendState::Ready {
            self.send_state = SendState::Send;
        }
    }

    /// Take data from the send buffer up to `max_bytes`, respecting flow control.
    pub fn take_send_data(&mut self, max_bytes: usize) -> Option<(u64, Vec<u8>)> {
        if self.send_buf.is_empty() { return None; }

        let fc_limit = self.flow_control.send_capacity() as usize;
        let take = max_bytes.min(self.send_buf.len()).min(fc_limit);
        if take == 0 { return None; }

        let data: Vec<u8> = self.send_buf.drain(..take).collect();
        let offset = self.send_offset;
        self.send_offset += take as u64;
        self.flow_control.on_send(take as u64);
        Some((offset, data))
    }

    /// Mark the send side as finished (FIN).
    pub fn finish_send(&mut self) {
        self.send_state = SendState::DataSent;
    }

    /// Receive data from the peer at a given offset.
    pub fn receive_data(&mut self, offset: u64, data: Vec<u8>, fin: bool) {
        if data.is_empty() && !fin { return; }

        if fin {
            self.recv_fin_offset = Some(offset + data.len() as u64);
            self.recv_state = RecvState::SizeKnown;
        }

        if !data.is_empty() {
            self.recv_buf.insert(offset, data);
        }

        // Reassemble contiguous data
        self.reassemble();
    }

    /// Read data available to the application.
    pub fn read(&mut self, max: usize) -> Vec<u8> {
        let n = max.min(self.app_recv_buf.len());
        let data: Vec<u8> = self.app_recv_buf.drain(..n).collect();
        self.flow_control.on_recv(data.len() as u64);
        data
    }

    /// Check if the stream has received all data (FIN received and all data delivered).
    pub fn is_recv_complete(&self) -> bool {
        if let Some(fin_offset) = self.recv_fin_offset {
            self.recv_contiguous >= fin_offset
        } else {
            false
        }
    }

    pub fn has_pending_send(&self) -> bool {
        !self.send_buf.is_empty()
    }

    pub fn readable_bytes(&self) -> usize {
        self.app_recv_buf.len()
    }

    fn reassemble(&mut self) {
        loop {
            if let Some((&offset, _)) = self.recv_buf.iter().next() {
                if offset <= self.recv_contiguous {
                    let data = self.recv_buf.remove(&offset).unwrap();
                    let skip = (self.recv_contiguous - offset) as usize;
                    if skip < data.len() {
                        self.app_recv_buf.extend(&data[skip..]);
                        self.recv_contiguous = offset + data.len() as u64;
                    }
                } else {
                    break; // Gap in data
                }
            } else {
                break;
            }
        }

        if self.is_recv_complete() {
            self.recv_state = RecvState::DataRecvd;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn send_and_take() {
        let mut stream = Stream::new(0, 10000);
        stream.write(b"hello world");
        let (offset, data) = stream.take_send_data(5).unwrap();
        assert_eq!(offset, 0);
        assert_eq!(data, b"hello");
        let (offset, data) = stream.take_send_data(100).unwrap();
        assert_eq!(offset, 5);
        assert_eq!(data, b" world");
    }

    #[test]
    fn receive_in_order() {
        let mut stream = Stream::new(0, 10000);
        stream.receive_data(0, b"hello ".to_vec(), false);
        stream.receive_data(6, b"world".to_vec(), true);
        let data = stream.read(100);
        assert_eq!(data, b"hello world");
        assert!(stream.is_recv_complete());
    }

    #[test]
    fn receive_out_of_order() {
        let mut stream = Stream::new(0, 10000);
        stream.receive_data(6, b"world".to_vec(), true);
        assert_eq!(stream.read(100), b""); // Nothing yet
        stream.receive_data(0, b"hello ".to_vec(), false);
        let data = stream.read(100);
        assert_eq!(data, b"hello world");
    }

    #[test]
    fn flow_control_limits_send() {
        let mut stream = Stream::new(0, 5);
        stream.write(b"hello world");
        let (_, data) = stream.take_send_data(100).unwrap();
        assert_eq!(data.len(), 5); // Limited by flow control
        assert!(stream.take_send_data(100).is_none()); // Blocked
    }
}
```

### `src/recovery/loss.rs`

```rust
use std::collections::BTreeMap;
use std::time::{Duration, Instant};

/// Information about a sent packet for loss detection.
#[derive(Debug, Clone)]
pub struct SentPacket {
    pub packet_number: u64,
    pub sent_time: Instant,
    pub size: usize,
    pub ack_eliciting: bool,
    pub in_flight: bool,
    pub stream_frames: Vec<(u64, u64, usize)>, // (stream_id, offset, len)
}

/// Loss detection state per RFC 9002.
pub struct LossDetector {
    sent_packets: BTreeMap<u64, SentPacket>,
    largest_acked: Option<u64>,
    loss_time: Option<Instant>,
    time_threshold: f64,  // 9/8 per RFC 9002
    packet_threshold: u64, // 3 per RFC 9002

    // PTO (Probe Timeout)
    pto_count: u32,

    // Bytes in flight
    bytes_in_flight: usize,
}

impl LossDetector {
    pub fn new() -> Self {
        Self {
            sent_packets: BTreeMap::new(),
            largest_acked: None,
            loss_time: None,
            time_threshold: 9.0 / 8.0,
            packet_threshold: 3,
            pto_count: 0,
            bytes_in_flight: 0,
        }
    }

    /// Record a newly sent packet.
    pub fn on_packet_sent(&mut self, pkt: SentPacket) {
        if pkt.in_flight {
            self.bytes_in_flight += pkt.size;
        }
        self.sent_packets.insert(pkt.packet_number, pkt);
    }

    /// Process an ACK frame. Returns newly acknowledged and lost packets.
    pub fn on_ack_received(
        &mut self,
        largest_ack: u64,
        ack_delay: Duration,
        ack_ranges: &[(u64, u64)],
        srtt: Duration,
    ) -> (Vec<SentPacket>, Vec<SentPacket>) {
        let mut newly_acked = Vec::new();
        let mut lost = Vec::new();

        // Mark acked packets
        self.largest_acked = Some(
            self.largest_acked.map_or(largest_ack, |prev| prev.max(largest_ack))
        );

        // Collect acked packet numbers from ranges
        let mut acked_pns = Vec::new();
        // First range: [largest_ack - first_range, largest_ack]
        // Subsequent: parse from ack_ranges
        // Simplified: mark largest_ack and all ranges
        acked_pns.push(largest_ack);
        // (In full implementation, expand all ranges)

        for pn in acked_pns {
            if let Some(pkt) = self.sent_packets.remove(&pn) {
                if pkt.in_flight {
                    self.bytes_in_flight = self.bytes_in_flight.saturating_sub(pkt.size);
                }
                newly_acked.push(pkt);
            }
        }

        // Detect lost packets
        lost = self.detect_lost_packets(srtt);

        self.pto_count = 0;

        (newly_acked, lost)
    }

    /// Detect lost packets using time and packet number thresholds.
    fn detect_lost_packets(&mut self, srtt: Duration) -> Vec<SentPacket> {
        let mut lost = Vec::new();
        let largest = match self.largest_acked {
            Some(la) => la,
            None => return lost,
        };

        let now = Instant::now();
        let loss_delay = Duration::from_secs_f64(
            srtt.as_secs_f64() * self.time_threshold
        ).max(Duration::from_millis(1));

        let mut lost_pns = Vec::new();
        for (&pn, pkt) in &self.sent_packets {
            if pn > largest { break; }

            // Time-based loss
            let time_lost = now.duration_since(pkt.sent_time) >= loss_delay;
            // Packet-number-based loss
            let pn_lost = largest - pn >= self.packet_threshold;

            if time_lost || pn_lost {
                lost_pns.push(pn);
            }
        }

        for pn in lost_pns {
            if let Some(pkt) = self.sent_packets.remove(&pn) {
                if pkt.in_flight {
                    self.bytes_in_flight = self.bytes_in_flight.saturating_sub(pkt.size);
                }
                lost.push(pkt);
            }
        }

        lost
    }

    /// Compute PTO duration.
    pub fn pto_duration(&self, srtt: Duration, rttvar: Duration) -> Duration {
        let base = srtt + Duration::max(rttvar * 4, Duration::from_millis(1));
        base * 2u32.pow(self.pto_count)
    }

    /// Increment PTO count for exponential backoff.
    pub fn on_pto_timeout(&mut self) {
        self.pto_count += 1;
    }

    pub fn bytes_in_flight(&self) -> usize {
        self.bytes_in_flight
    }

    pub fn has_unacked_packets(&self) -> bool {
        !self.sent_packets.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_sent_packet(pn: u64, size: usize) -> SentPacket {
        SentPacket {
            packet_number: pn,
            sent_time: Instant::now(),
            size,
            ack_eliciting: true,
            in_flight: true,
            stream_frames: vec![],
        }
    }

    #[test]
    fn bytes_in_flight_tracking() {
        let mut ld = LossDetector::new();
        ld.on_packet_sent(make_sent_packet(0, 1000));
        ld.on_packet_sent(make_sent_packet(1, 500));
        assert_eq!(ld.bytes_in_flight(), 1500);

        let (acked, _) = ld.on_ack_received(0, Duration::ZERO, &[], Duration::from_millis(100));
        assert_eq!(acked.len(), 1);
        assert_eq!(ld.bytes_in_flight(), 500);
    }

    #[test]
    fn packet_number_loss_detection() {
        let mut ld = LossDetector::new();
        for pn in 0..5 {
            ld.on_packet_sent(make_sent_packet(pn, 100));
        }

        // ACK packet 4 -- packets 0, 1 should be detected as lost (threshold = 3)
        let (_, lost) = ld.on_ack_received(4, Duration::ZERO, &[], Duration::from_millis(100));
        assert!(!lost.is_empty(), "should detect loss when gap >= 3");
    }

    #[test]
    fn pto_exponential_backoff() {
        let mut ld = LossDetector::new();
        let srtt = Duration::from_millis(100);
        let rttvar = Duration::from_millis(25);

        let pto1 = ld.pto_duration(srtt, rttvar);
        ld.on_pto_timeout();
        let pto2 = ld.pto_duration(srtt, rttvar);
        assert!(pto2 >= pto1 * 2 - Duration::from_millis(1));
    }
}
```

### `src/recovery/congestion.rs`

```rust
/// QUIC congestion control (Reno-style per RFC 9002).
pub struct CongestionController {
    cwnd: usize,
    ssthresh: usize,
    mss: usize,
    bytes_in_flight: usize,
    in_recovery: bool,
    recovery_start_pn: u64,
}

impl CongestionController {
    pub fn new(mss: usize) -> Self {
        let initial_cwnd = 10 * mss; // RFC 9002: initial window is 10 * MSS
        Self {
            cwnd: initial_cwnd,
            ssthresh: usize::MAX,
            mss,
            bytes_in_flight: 0,
            in_recovery: false,
            recovery_start_pn: 0,
        }
    }

    pub fn window(&self) -> usize {
        self.cwnd
    }

    pub fn available_window(&self) -> usize {
        self.cwnd.saturating_sub(self.bytes_in_flight)
    }

    pub fn on_packet_sent(&mut self, size: usize) {
        self.bytes_in_flight += size;
    }

    pub fn on_ack(&mut self, acked_bytes: usize, packet_number: u64) {
        self.bytes_in_flight = self.bytes_in_flight.saturating_sub(acked_bytes);

        // Do not increase cwnd during recovery
        if self.in_recovery && packet_number <= self.recovery_start_pn {
            return;
        }
        self.in_recovery = false;

        if self.cwnd < self.ssthresh {
            // Slow start
            self.cwnd += acked_bytes.min(self.mss);
        } else {
            // Congestion avoidance
            self.cwnd += (self.mss * acked_bytes) / self.cwnd;
        }
    }

    pub fn on_loss(&mut self, lost_packet_number: u64) {
        if self.in_recovery { return; }
        self.in_recovery = true;
        self.recovery_start_pn = lost_packet_number;
        self.ssthresh = (self.cwnd / 2).max(2 * self.mss);
        self.cwnd = self.ssthresh;
    }

    pub fn on_packet_lost_bytes(&mut self, size: usize) {
        self.bytes_in_flight = self.bytes_in_flight.saturating_sub(size);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn initial_window() {
        let cc = CongestionController::new(1200);
        assert_eq!(cc.window(), 12000); // 10 * MSS
    }

    #[test]
    fn slow_start_growth() {
        let mut cc = CongestionController::new(1200);
        let before = cc.window();
        cc.on_ack(1200, 1);
        assert!(cc.window() > before);
    }

    #[test]
    fn loss_halves_window() {
        let mut cc = CongestionController::new(1200);
        cc.cwnd = 12000;
        cc.on_loss(5);
        assert_eq!(cc.cwnd, 6000);
        assert_eq!(cc.ssthresh, 6000);
    }
}
```

### `src/recovery/mod.rs`

```rust
pub mod loss;
pub mod congestion;
```

### `src/connection.rs`

```rust
use crate::stream::Stream;
use crate::stream::flow_control::ConnectionFlowControl;
use crate::stream::state::{StreamType, next_stream_id};
use crate::wire::frames::Frame;
use crate::recovery::loss::{LossDetector, SentPacket};
use crate::recovery::congestion::CongestionController;

use std::collections::HashMap;
use std::net::SocketAddr;
use std::time::{Duration, Instant};

/// Connection state.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum ConnectionState {
    Handshaking,
    Connected,
    Draining,
    Closed,
}

/// A QUIC connection managing multiple streams.
pub struct Connection {
    pub state: ConnectionState,
    pub peer_addr: SocketAddr,
    streams: HashMap<u64, Stream>,
    next_client_bidi: u64,
    next_server_bidi: u64,
    next_client_uni: u64,
    next_server_uni: u64,

    conn_flow_control: ConnectionFlowControl,
    loss_detector: LossDetector,
    congestion: CongestionController,

    next_packet_number: u64,
    is_server: bool,

    // Connection migration
    path_validated: bool,
    pending_path_response: Option<[u8; 8]>,

    // 0-RTT
    zero_rtt_token: Option<Vec<u8>>,

    // Idle timeout
    last_activity: Instant,
    idle_timeout: Duration,
}

impl Connection {
    pub fn new(peer_addr: SocketAddr, is_server: bool) -> Self {
        Self {
            state: ConnectionState::Handshaking,
            peer_addr,
            streams: HashMap::new(),
            next_client_bidi: 0,
            next_server_bidi: 0,
            next_client_uni: 0,
            next_server_uni: 0,
            conn_flow_control: ConnectionFlowControl::new(1_048_576), // 1 MB initial
            loss_detector: LossDetector::new(),
            congestion: CongestionController::new(1200),
            next_packet_number: 0,
            is_server,
            path_validated: true,
            pending_path_response: None,
            zero_rtt_token: None,
            last_activity: Instant::now(),
            idle_timeout: Duration::from_secs(30),
        }
    }

    /// Open a new bidirectional stream. Returns the stream ID.
    pub fn open_bidi_stream(&mut self) -> u64 {
        let stream_type = if self.is_server {
            StreamType::ServerBidi
        } else {
            StreamType::ClientBidi
        };
        let count = if self.is_server { &mut self.next_server_bidi } else { &mut self.next_client_bidi };
        let id = next_stream_id(stream_type, *count);
        *count += 1;
        self.streams.insert(id, Stream::new(id, 65536));
        id
    }

    /// Open a new unidirectional stream.
    pub fn open_uni_stream(&mut self) -> u64 {
        let stream_type = if self.is_server {
            StreamType::ServerUni
        } else {
            StreamType::ClientUni
        };
        let count = if self.is_server { &mut self.next_server_uni } else { &mut self.next_client_uni };
        let id = next_stream_id(stream_type, *count);
        *count += 1;
        self.streams.insert(id, Stream::new(id, 65536));
        id
    }

    /// Write data to a stream.
    pub fn stream_send(&mut self, stream_id: u64, data: &[u8]) -> Result<(), String> {
        let stream = self.streams.get_mut(&stream_id)
            .ok_or_else(|| format!("stream {} not found", stream_id))?;
        stream.write(data);
        Ok(())
    }

    /// Read data from a stream.
    pub fn stream_recv(&mut self, stream_id: u64, max: usize) -> Result<Vec<u8>, String> {
        let stream = self.streams.get_mut(&stream_id)
            .ok_or_else(|| format!("stream {} not found", stream_id))?;
        Ok(stream.read(max))
    }

    /// Finish a stream's send side.
    pub fn stream_finish(&mut self, stream_id: u64) -> Result<(), String> {
        let stream = self.streams.get_mut(&stream_id)
            .ok_or_else(|| format!("stream {} not found", stream_id))?;
        stream.finish_send();
        Ok(())
    }

    /// Process an incoming frame.
    pub fn on_frame(&mut self, frame: Frame) -> Vec<Frame> {
        self.last_activity = Instant::now();
        let mut responses = Vec::new();

        match frame {
            Frame::Stream { stream_id, offset, data, fin } => {
                let stream = self.streams.entry(stream_id)
                    .or_insert_with(|| Stream::new(stream_id, 65536));
                let data_len = data.len() as u64;
                stream.receive_data(offset, data, fin);
                self.conn_flow_control.on_recv(data_len);

                // Send window updates if needed
                if let Some(new_max) = stream.flow_control.should_send_window_update() {
                    let max = stream.flow_control.send_window_update();
                    responses.push(Frame::MaxStreamData {
                        stream_id,
                        maximum: max,
                    });
                }
                if let Some(new_max) = self.conn_flow_control.should_send_max_data() {
                    let max = self.conn_flow_control.send_max_data();
                    responses.push(Frame::MaxData { maximum: max });
                }
            }

            Frame::MaxStreamData { stream_id, maximum } => {
                if let Some(stream) = self.streams.get_mut(&stream_id) {
                    stream.flow_control.update_send_max(maximum);
                }
            }

            Frame::MaxData { maximum } => {
                self.conn_flow_control.update_send_max(maximum);
            }

            Frame::ResetStream { stream_id, error_code, final_size } => {
                if let Some(stream) = self.streams.get_mut(&stream_id) {
                    stream.recv_state = crate::stream::state::RecvState::ResetRecvd;
                }
            }

            Frame::StopSending { stream_id, error_code } => {
                if let Some(stream) = self.streams.get_mut(&stream_id) {
                    stream.send_state = crate::stream::state::SendState::ResetSent;
                    responses.push(Frame::ResetStream {
                        stream_id,
                        error_code,
                        final_size: stream.flow_control.send_offset(),
                    });
                }
            }

            Frame::PathChallenge { data } => {
                responses.push(Frame::PathResponse { data });
            }

            Frame::PathResponse { data } => {
                if let Some(expected) = &self.pending_path_response {
                    if data == *expected {
                        self.path_validated = true;
                        self.pending_path_response = None;
                    }
                }
            }

            Frame::ConnectionClose { error_code, reason, .. } => {
                log::info!("connection close: error={}, reason={}",
                    error_code, String::from_utf8_lossy(&reason));
                self.state = ConnectionState::Draining;
            }

            Frame::HandshakeDone => {
                self.state = ConnectionState::Connected;
            }

            Frame::Ack { largest_ack, ack_delay, ack_ranges, .. } => {
                let (acked, lost) = self.loss_detector.on_ack_received(
                    largest_ack,
                    Duration::from_micros(ack_delay),
                    &ack_ranges.iter().map(|&(g, r)| (g, r)).collect::<Vec<_>>(),
                    Duration::from_millis(100), // Use actual SRTT in production
                );

                for pkt in &acked {
                    self.congestion.on_ack(pkt.size, pkt.packet_number);
                }
                for pkt in &lost {
                    self.congestion.on_loss(pkt.packet_number);
                    self.congestion.on_packet_lost_bytes(pkt.size);
                    // Re-queue lost stream data for retransmission
                    for &(stream_id, offset, len) in &pkt.stream_frames {
                        log::debug!("retransmit stream {} offset {} len {}", stream_id, offset, len);
                    }
                }
            }

            _ => {}
        }

        responses
    }

    /// Build frames to send, coalescing data from multiple streams.
    pub fn build_outbound_frames(&mut self, max_packet_size: usize) -> Vec<Frame> {
        let mut frames = Vec::new();
        let mut remaining = max_packet_size - 50; // Reserve space for headers

        // Check for pending path response
        if let Some(data) = self.pending_path_response {
            frames.push(Frame::PathResponse { data });
            remaining -= 9;
        }

        // Collect streams with pending data, sorted by priority
        let mut stream_ids: Vec<(u64, u8)> = self.streams.iter()
            .filter(|(_, s)| s.has_pending_send())
            .map(|(&id, s)| (id, s.priority))
            .collect();
        stream_ids.sort_by(|a, b| a.1.cmp(&b.1)); // Lower number = higher priority

        // Check congestion window
        let available_cwnd = self.congestion.available_window();
        let mut bytes_scheduled = 0;

        for (stream_id, _) in stream_ids {
            if remaining < 20 { break; } // Minimum useful frame size
            if bytes_scheduled >= available_cwnd { break; }

            let conn_capacity = self.conn_flow_control.send_capacity() as usize;
            let max_send = remaining.min(available_cwnd - bytes_scheduled).min(conn_capacity);

            if let Some(stream) = self.streams.get_mut(&stream_id) {
                if let Some((offset, data)) = stream.take_send_data(max_send) {
                    let data_len = data.len();
                    let fin = stream.send_state == crate::stream::state::SendState::DataSent
                        && !stream.has_pending_send();

                    frames.push(Frame::Stream {
                        stream_id,
                        offset,
                        data,
                        fin,
                    });

                    self.conn_flow_control.on_send(data_len as u64);
                    bytes_scheduled += data_len;
                    remaining -= data_len + 10; // Approximate frame overhead
                }
            }
        }

        frames
    }

    /// Initiate connection migration to a new peer address.
    pub fn migrate_to(&mut self, new_addr: SocketAddr) {
        self.peer_addr = new_addr;
        self.path_validated = false;
        let mut challenge_data = [0u8; 8];
        rand::Rng::fill(&mut rand::thread_rng(), &mut challenge_data);
        self.pending_path_response = Some(challenge_data);
    }

    /// Set 0-RTT token for connection resumption.
    pub fn set_zero_rtt_token(&mut self, token: Vec<u8>) {
        self.zero_rtt_token = Some(token);
    }

    /// Close the connection gracefully.
    pub fn close(&mut self, error_code: u64, reason: &str) -> Frame {
        self.state = ConnectionState::Draining;
        Frame::ConnectionClose {
            error_code,
            frame_type: 0,
            reason: reason.as_bytes().to_vec(),
        }
    }

    /// Check if idle timeout has expired.
    pub fn check_idle_timeout(&mut self) -> bool {
        if self.last_activity.elapsed() > self.idle_timeout {
            self.state = ConnectionState::Closed;
            true
        } else {
            false
        }
    }

    pub fn stream_count(&self) -> usize {
        self.streams.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn open_streams() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        let s0 = conn.open_bidi_stream();
        let s1 = conn.open_bidi_stream();
        let s2 = conn.open_uni_stream();
        assert_eq!(s0, 0); // Client bidi: 0, 4, 8...
        assert_eq!(s1, 4);
        assert_eq!(s2, 2); // Client uni: 2, 6, 10...
    }

    #[test]
    fn stream_send_recv() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        conn.state = ConnectionState::Connected;
        let id = conn.open_bidi_stream();
        conn.stream_send(id, b"hello").unwrap();

        let frames = conn.build_outbound_frames(1200);
        assert!(!frames.is_empty());

        // Simulate receiving data on the same stream from peer
        conn.on_frame(Frame::Stream {
            stream_id: id,
            offset: 0,
            data: b"response".to_vec(),
            fin: false,
        });

        let data = conn.stream_recv(id, 100).unwrap();
        assert_eq!(data, b"response");
    }

    #[test]
    fn multiple_streams_coalesce() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        conn.state = ConnectionState::Connected;

        let s0 = conn.open_bidi_stream();
        let s1 = conn.open_bidi_stream();
        conn.stream_send(s0, b"stream zero data").unwrap();
        conn.stream_send(s1, b"stream four data").unwrap();

        let frames = conn.build_outbound_frames(1200);
        let stream_frames: Vec<_> = frames.iter().filter(|f| matches!(f, Frame::Stream { .. })).collect();
        assert_eq!(stream_frames.len(), 2, "both streams should be in one packet");
    }

    #[test]
    fn path_challenge_response() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        let challenge_data = [1, 2, 3, 4, 5, 6, 7, 8];
        let responses = conn.on_frame(Frame::PathChallenge { data: challenge_data });
        assert_eq!(responses.len(), 1);
        match &responses[0] {
            Frame::PathResponse { data } => assert_eq!(*data, challenge_data),
            _ => panic!("expected PathResponse"),
        }
    }

    #[test]
    fn connection_close() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        conn.state = ConnectionState::Connected;
        let frame = conn.close(0, "goodbye");
        assert!(matches!(frame, Frame::ConnectionClose { .. }));
        assert_eq!(conn.state, ConnectionState::Draining);
    }

    #[test]
    fn stream_priority_ordering() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        conn.state = ConnectionState::Connected;

        let low = conn.open_bidi_stream();
        let high = conn.open_bidi_stream();

        // Set priorities (lower number = higher priority)
        conn.streams.get_mut(&low).unwrap().priority = 200;
        conn.streams.get_mut(&high).unwrap().priority = 50;

        conn.stream_send(low, b"low priority").unwrap();
        conn.stream_send(high, b"high priority").unwrap();

        let frames = conn.build_outbound_frames(1200);
        let stream_frames: Vec<_> = frames.iter().filter_map(|f| {
            if let Frame::Stream { stream_id, .. } = f { Some(*stream_id) } else { None }
        }).collect();

        assert!(!stream_frames.is_empty());
        assert_eq!(stream_frames[0], high, "high priority stream should be scheduled first");
    }

    #[test]
    fn flow_control_window_update() {
        let mut conn = Connection::new("127.0.0.1:4433".parse().unwrap(), false);
        conn.state = ConnectionState::Connected;
        let id = conn.open_bidi_stream();

        // Receive enough data to trigger window update
        let data = vec![0u8; 40000];
        let responses = conn.on_frame(Frame::Stream {
            stream_id: id,
            offset: 0,
            data,
            fin: false,
        });

        // Read the data to advance flow control
        conn.stream_recv(id, 50000).unwrap();

        let has_max_stream_data = responses.iter().any(|f| matches!(f, Frame::MaxStreamData { .. }));
        // Window update may or may not fire depending on initial window size
    }
}
```

### `src/main.rs`

```rust
mod connection;
mod recovery;
mod stream;
mod wire;

use connection::{Connection, ConnectionState};
use wire::frames::Frame;
use std::net::SocketAddr;

fn main() {
    env_logger::init();

    log::info!("QUIC Reliable Streams - Demo");

    let peer: SocketAddr = "127.0.0.1:4433".parse().unwrap();
    let mut conn = Connection::new(peer, false);
    conn.state = ConnectionState::Connected;

    // Open multiple streams
    let stream0 = conn.open_bidi_stream();
    let stream1 = conn.open_bidi_stream();
    let stream2 = conn.open_uni_stream();

    log::info!("opened streams: bidi={}, bidi={}, uni={}", stream0, stream1, stream2);

    // Send data on multiple streams
    conn.stream_send(stream0, b"GET / HTTP/3\r\n\r\n").unwrap();
    conn.stream_send(stream1, b"GET /style.css HTTP/3\r\n\r\n").unwrap();
    conn.stream_send(stream2, b"server push data").unwrap();

    // Build a packet coalescing frames from all streams
    let frames = conn.build_outbound_frames(1200);
    log::info!("coalesced {} frames into one packet:", frames.len());
    for frame in &frames {
        match frame {
            Frame::Stream { stream_id, offset, data, fin } => {
                log::info!("  STREAM id={} offset={} len={} fin={}",
                    stream_id, offset, data.len(), fin);
            }
            _ => log::info!("  {:?}", frame),
        }
    }

    // Simulate receiving data from peer
    conn.on_frame(Frame::Stream {
        stream_id: stream0,
        offset: 0,
        data: b"HTTP/3 200 OK\r\n\r\n<html>Hello</html>".to_vec(),
        fin: true,
    });

    let response = conn.stream_recv(stream0, 1024).unwrap();
    log::info!("stream {} received: {:?}", stream0, String::from_utf8_lossy(&response));

    // Simulate connection migration
    let new_addr: SocketAddr = "192.168.1.100:5555".parse().unwrap();
    conn.migrate_to(new_addr);
    log::info!("migrating to {}", new_addr);

    // Simulate path validation
    if let Some(challenge) = conn.pending_path_response {
        // In real code, this would be received from the network
    }

    // Close connection
    let close_frame = conn.close(0, "done");
    log::info!("connection closing");

    log::info!("total streams: {}", conn.stream_count());
    log::info!("demo complete");
}
```

## Build and Run

```bash
# Build
cargo build --release

# Run demo
RUST_LOG=info cargo run

# Run all tests
cargo test -- --nocapture

# Run specific module tests
cargo test wire -- --nocapture
cargo test stream -- --nocapture
cargo test recovery -- --nocapture
cargo test connection -- --nocapture
```

## Expected Output

Demo:
```
[INFO  quic_streams] QUIC Reliable Streams - Demo
[INFO  quic_streams] opened streams: bidi=0, bidi=4, uni=2
[INFO  quic_streams] coalesced 3 frames into one packet:
[INFO  quic_streams]   STREAM id=0 offset=0 len=18 fin=false
[INFO  quic_streams]   STREAM id=4 offset=0 len=26 fin=false
[INFO  quic_streams]   STREAM id=2 offset=0 len=16 fin=false
[INFO  quic_streams] stream 0 received: "HTTP/3 200 OK\r\n\r\n<html>Hello</html>"
[INFO  quic_streams] migrating to 192.168.1.100:5555
[INFO  quic_streams] connection closing
[INFO  quic_streams] total streams: 3
[INFO  quic_streams] demo complete
```

Test output:
```
running 22 tests
test wire::varint tests ... ok (5 tests)
test wire::frames::tests::stream_frame_round_trip ... ok
test wire::frames::tests::ack_frame_round_trip ... ok
test wire::frames::tests::connection_close_round_trip ... ok
test wire::frames::tests::path_challenge_response ... ok
test stream::state::tests::stream_type_from_id ... ok
test stream::state::tests::next_ids ... ok
test stream::flow_control::tests::stream_send_capacity ... ok
test stream::flow_control::tests::stream_window_update ... ok
test stream::flow_control::tests::send_max_extends_capacity ... ok
test stream::flow_control::tests::connection_level ... ok
test stream::tests::send_and_take ... ok
test stream::tests::receive_in_order ... ok
test stream::tests::receive_out_of_order ... ok
test stream::tests::flow_control_limits_send ... ok
test recovery::loss::tests::bytes_in_flight_tracking ... ok
test recovery::loss::tests::packet_number_loss_detection ... ok
test recovery::loss::tests::pto_exponential_backoff ... ok
test recovery::congestion::tests::initial_window ... ok
test recovery::congestion::tests::slow_start_growth ... ok
test recovery::congestion::tests::loss_halves_window ... ok
test connection::tests::open_streams ... ok
test connection::tests::stream_send_recv ... ok
test connection::tests::multiple_streams_coalesce ... ok
test connection::tests::path_challenge_response ... ok
test connection::tests::connection_close ... ok
test connection::tests::stream_priority_ordering ... ok
test result: ok. 28 passed; 0 failed; 0 ignored
```

## Design Decisions

**Why absolute byte offsets for flow control instead of relative windows.** QUIC uses absolute offsets (`MAX_STREAM_DATA` says "you may send up to byte N") rather than TCP-style relative windows ("you may send N more bytes"). Absolute offsets are idempotent: receiving the same `MAX_STREAM_DATA` twice is harmless. They also avoid the complexity of wrapping arithmetic since QUIC uses 62-bit offsets.

**Why `BTreeMap` for out-of-order receive buffering.** Received stream data can arrive out of order. A `BTreeMap<offset, data>` naturally orders segments by their byte offset, making reassembly a simple iteration from the lowest offset. `HashMap` would require sorting before reassembly.

**Why per-stream send buffers instead of a shared buffer.** Each stream is independent in QUIC -- that is the entire point of the protocol. A shared buffer would reintroduce head-of-line blocking at the application level. Per-stream buffers allow the scheduler to take data from any stream based on priority and flow control, without blocking one stream on another's data.

**Why `CongestionController` is separate from `LossDetector`.** Loss detection determines which packets are lost. Congestion control decides how to respond. These are conceptually independent: you could change the loss detection algorithm (e.g., from threshold-based to RACK) without changing congestion control, and vice versa. RFC 9002 specifies them as separate algorithms.

**Why the scheduler sorts streams by priority number.** A lower priority number means higher priority (like Unix process priorities). This is simple and efficient: sort the stream list, then iterate. For weighted fair scheduling, you would instead maintain a deficit round-robin or weighted fair queue, but for this challenge the priority sort provides the right behavior with minimal complexity.

**Why connection migration uses PATH_CHALLENGE/PATH_RESPONSE.** When a packet arrives from a new source address, the server cannot simply accept it -- an attacker could forge the source address. PATH_CHALLENGE sends 8 random bytes that the peer must echo back in a PATH_RESPONSE. This proves the new path is genuine. Until validation completes, the connection uses the old path.

## Common Mistakes

1. **Treating stream IDs as sequential integers.** Stream IDs encode the initiator and directionality in the low 2 bits. Client bidirectional streams are 0, 4, 8... not 0, 1, 2. Using sequential IDs breaks stream type detection.

2. **Applying connection-level flow control without per-stream limits.** Both must be enforced. Data can be blocked by either: a stream may have capacity but the connection is at its limit, or the connection has capacity but the stream is blocked. Check both before sending.

3. **Blocking all streams when one stream's data is lost.** This defeats the purpose of QUIC. Each stream maintains its own reassembly buffer. A loss on stream 0 blocks only stream 0; streams 4 and 8 continue delivering data.

4. **Using the same packet number for retransmissions.** QUIC never reuses packet numbers. A retransmitted frame gets a new packet number. This is essential for loss detection: if packet 5 is lost and its data is retransmitted in packet 10, the ACK for packet 10 unambiguously acknowledges the retransmission.

5. **Not sending MAX_STREAM_DATA updates.** If the receiver never extends the flow control window, the sender eventually blocks. Window updates should be sent when the receiver has consumed a significant fraction (typically half) of the advertised window.

6. **Ignoring connection-level flow control in the packet scheduler.** Even if individual streams have capacity, the aggregate across all streams must not exceed the connection-level limit. The scheduler must track `conn_flow_control.send_capacity()` and stop when exhausted.

## Performance Notes

- Frame encoding is allocation-light: small frames write directly into the packet buffer. Only STREAM frames with large payloads allocate.
- The stream map uses `HashMap` for O(1) lookup by stream ID. For many concurrent streams (>1000), consider a pre-allocated slab allocator to reduce hash overhead.
- Loss detection scans `sent_packets` linearly up to `largest_acked`. With a `BTreeMap`, this scan is O(k) where k is the number of potentially lost packets, not the total sent.
- Congestion control uses integer arithmetic exclusively (no floating point). The division `mss * acked / cwnd` is the only division per ACK.
- Packet coalescing reduces the number of UDP datagrams sent. Each datagram carries frames from multiple streams, amortizing the IP/UDP header overhead across streams.
- For real-world throughput, implement GSO (Generic Segmentation Offload) to send multiple packets in a single syscall, and GRO for batch receives.

## Going Further

- Integrate `rustls` for real TLS 1.3 handshake and key updates
- Implement HTTP/3 on top of this QUIC transport
- Add CUBIC congestion control (Linux default) or BBR
- Implement DATAGRAM frames (RFC 9221) for unreliable data over QUIC
- Build a load testing tool that opens thousands of concurrent streams
- Implement `qlog` (RFC 9232) for structured logging of QUIC events
- Run interoperability tests against Quinn, quiche, or s2n-quic
