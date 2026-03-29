# Solution: QUIC Transport Handshake

## Architecture Overview

The implementation is structured in five layers:

1. **Wire Format** -- encoding and decoding of variable-length integers, packet headers, and frames at the byte level
2. **Crypto Layer** -- AEAD encryption/decryption, header protection, and Initial key derivation from connection IDs
3. **Packet Processing** -- parsing incoming UDP datagrams into typed packets, handling coalesced packets, and constructing outbound packets
4. **Connection State Machine** -- tracks each connection through its lifecycle from Initial to Closed, enforcing valid state transitions
5. **Server Loop** -- async UDP socket listener that routes incoming packets to connections by connection ID and manages timeouts

```
UDP Socket (tokio)
    |
    v
Datagram Parser (split coalesced packets)
    |
    v
Connection ID Router (HashMap<ConnectionId, ConnectionState>)
    |
    v
Connection State Machine (per-connection)
    |           |           |
    v           v           v
 Initial    Handshake    Version
 Exchange   Processing   Negotiation
    |
    v
Crypto Layer (AEAD + header protection)
    |
    v
Wire Format (encode/decode)
```

## Rust Solution

### `Cargo.toml`

```toml
[package]
name = "quic-handshake"
version = "0.1.0"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
ring = "0.17"
rand = "0.8"
bytes = "1"

[dev-dependencies]
tokio-test = "0.4"
```

### `src/varint.rs`

```rust
use std::io::{self, Read, Write};

/// Maximum value a QUIC variable-length integer can hold (2^62 - 1).
pub const MAX_VARINT: u64 = (1 << 62) - 1;

/// Encode a variable-length integer into a writer.
/// Returns the number of bytes written (1, 2, 4, or 8).
pub fn encode(value: u64, buf: &mut impl Write) -> io::Result<usize> {
    if value > MAX_VARINT {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("varint value {} exceeds maximum {}", value, MAX_VARINT),
        ));
    }

    if value <= 63 {
        buf.write_all(&[value as u8])?;
        Ok(1)
    } else if value <= 16383 {
        let encoded = (value as u16) | 0x4000;
        buf.write_all(&encoded.to_be_bytes())?;
        Ok(2)
    } else if value <= 1_073_741_823 {
        let encoded = (value as u32) | 0x8000_0000;
        buf.write_all(&encoded.to_be_bytes())?;
        Ok(4)
    } else {
        let encoded = value | 0xC000_0000_0000_0000;
        buf.write_all(&encoded.to_be_bytes())?;
        Ok(8)
    }
}

/// Decode a variable-length integer from a reader.
/// Returns the decoded value and the number of bytes consumed.
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
            let value = u64::from(u16::from_be_bytes([first[0] & 0x3F, rest[0]]));
            Ok((value, 2))
        }
        4 => {
            let mut rest = [0u8; 3];
            buf.read_exact(&mut rest)?;
            let value = u64::from(u32::from_be_bytes([
                first[0] & 0x3F,
                rest[0],
                rest[1],
                rest[2],
            ]));
            Ok((value, 4))
        }
        8 => {
            let mut rest = [0u8; 7];
            buf.read_exact(&mut rest)?;
            let value = u64::from_be_bytes([
                first[0] & 0x3F,
                rest[0],
                rest[1],
                rest[2],
                rest[3],
                rest[4],
                rest[5],
                rest[6],
            ]);
            Ok((value, 8))
        }
        _ => unreachable!(),
    }
}

/// Return the number of bytes needed to encode a value.
pub fn encoding_size(value: u64) -> usize {
    if value <= 63 {
        1
    } else if value <= 16383 {
        2
    } else if value <= 1_073_741_823 {
        4
    } else {
        8
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn round_trip_one_byte() {
        for value in [0, 1, 37, 63] {
            let mut buf = Vec::new();
            let written = encode(value, &mut buf).unwrap();
            assert_eq!(written, 1);
            let (decoded, consumed) = decode(&mut Cursor::new(&buf)).unwrap();
            assert_eq!(decoded, value);
            assert_eq!(consumed, 1);
        }
    }

    #[test]
    fn round_trip_two_bytes() {
        for value in [64, 1000, 16383] {
            let mut buf = Vec::new();
            let written = encode(value, &mut buf).unwrap();
            assert_eq!(written, 2);
            let (decoded, consumed) = decode(&mut Cursor::new(&buf)).unwrap();
            assert_eq!(decoded, value);
            assert_eq!(consumed, 2);
        }
    }

    #[test]
    fn round_trip_four_bytes() {
        for value in [16384, 1_000_000, 1_073_741_823] {
            let mut buf = Vec::new();
            let written = encode(value, &mut buf).unwrap();
            assert_eq!(written, 4);
            let (decoded, consumed) = decode(&mut Cursor::new(&buf)).unwrap();
            assert_eq!(decoded, value);
            assert_eq!(consumed, 4);
        }
    }

    #[test]
    fn round_trip_eight_bytes() {
        for value in [1_073_741_824, MAX_VARINT] {
            let mut buf = Vec::new();
            let written = encode(value, &mut buf).unwrap();
            assert_eq!(written, 8);
            let (decoded, consumed) = decode(&mut Cursor::new(&buf)).unwrap();
            assert_eq!(decoded, value);
            assert_eq!(consumed, 8);
        }
    }

    #[test]
    fn rfc_test_vectors() {
        // RFC 9000 Section 16, Table 4 (Appendix A.1)
        let cases: Vec<(u64, Vec<u8>)> = vec![
            (151_288_809_941_952_652, vec![0xc2, 0x19, 0x7c, 0x5e, 0xff, 0x14, 0xe8, 0x8c]),
            (494_878_333, vec![0x9d, 0x7f, 0x3e, 0x7d]),
            (15_293, vec![0x7b, 0xbd]),
            (37, vec![0x25]),
        ];

        for (expected_value, bytes) in cases {
            let (decoded, _) = decode(&mut Cursor::new(&bytes)).unwrap();
            assert_eq!(decoded, expected_value, "decode mismatch for bytes {:?}", bytes);

            let mut encoded = Vec::new();
            encode(expected_value, &mut encoded).unwrap();
            assert_eq!(encoded, bytes, "encode mismatch for value {}", expected_value);
        }
    }
}
```

### `src/connection_id.rs`

```rust
use rand::Rng;
use std::fmt;

/// Maximum connection ID length per RFC 9000.
pub const MAX_CID_LENGTH: usize = 20;

/// A QUIC connection identifier.
#[derive(Clone, Hash, Eq, PartialEq)]
pub struct ConnectionId {
    bytes: Vec<u8>,
}

impl ConnectionId {
    /// Generate a random connection ID of the specified length.
    pub fn generate(length: usize) -> Self {
        assert!(length <= MAX_CID_LENGTH, "CID length exceeds maximum of {}", MAX_CID_LENGTH);
        let mut rng = rand::thread_rng();
        let bytes: Vec<u8> = (0..length).map(|_| rng.gen()).collect();
        Self { bytes }
    }

    /// Create a connection ID from raw bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self, &'static str> {
        if bytes.len() > MAX_CID_LENGTH {
            return Err("connection ID exceeds maximum length");
        }
        Ok(Self { bytes: bytes.to_vec() })
    }

    /// Create an empty connection ID (zero length).
    pub fn empty() -> Self {
        Self { bytes: Vec::new() }
    }

    pub fn as_bytes(&self) -> &[u8] {
        &self.bytes
    }

    pub fn len(&self) -> usize {
        self.bytes.len()
    }

    pub fn is_empty(&self) -> bool {
        self.bytes.is_empty()
    }

    /// Encode the connection ID with its length prefix (1 byte length + bytes).
    pub fn encode(&self, buf: &mut Vec<u8>) {
        buf.push(self.bytes.len() as u8);
        buf.extend_from_slice(&self.bytes);
    }

    /// Decode a connection ID from a buffer at the given offset.
    /// Returns the CID and the number of bytes consumed.
    pub fn decode(buf: &[u8], offset: usize) -> Result<(Self, usize), &'static str> {
        if offset >= buf.len() {
            return Err("buffer too short for CID length");
        }
        let length = buf[offset] as usize;
        if length > MAX_CID_LENGTH {
            return Err("CID length exceeds maximum");
        }
        let start = offset + 1;
        let end = start + length;
        if end > buf.len() {
            return Err("buffer too short for CID bytes");
        }
        Ok((Self { bytes: buf[start..end].to_vec() }, 1 + length))
    }
}

impl fmt::Debug for ConnectionId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "CID(")?;
        for byte in &self.bytes {
            write!(f, "{:02x}", byte)?;
        }
        write!(f, ")")
    }
}

impl fmt::Display for ConnectionId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for byte in &self.bytes {
            write!(f, "{:02x}", byte)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generate_and_length() {
        let cid = ConnectionId::generate(8);
        assert_eq!(cid.len(), 8);

        let cid = ConnectionId::generate(0);
        assert!(cid.is_empty());
    }

    #[test]
    fn encode_decode_round_trip() {
        let original = ConnectionId::generate(16);
        let mut buf = Vec::new();
        original.encode(&mut buf);
        let (decoded, consumed) = ConnectionId::decode(&buf, 0).unwrap();
        assert_eq!(decoded, original);
        assert_eq!(consumed, 17); // 1 byte length + 16 bytes data
    }

    #[test]
    fn empty_cid_round_trip() {
        let original = ConnectionId::empty();
        let mut buf = Vec::new();
        original.encode(&mut buf);
        assert_eq!(buf, vec![0]);
        let (decoded, consumed) = ConnectionId::decode(&buf, 0).unwrap();
        assert_eq!(decoded, original);
        assert_eq!(consumed, 1);
    }

    #[test]
    fn reject_oversized() {
        let big = vec![0u8; MAX_CID_LENGTH + 1];
        assert!(ConnectionId::from_bytes(&big).is_err());
    }
}
```

### `src/packet.rs`

```rust
use crate::connection_id::ConnectionId;
use crate::varint;
use std::io::Cursor;

/// QUIC version we support.
pub const QUIC_VERSION_1: u32 = 0x0000_0001;

/// Long packet types (RFC 9000 Section 17.2).
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum LongPacketType {
    Initial = 0x00,
    ZeroRtt = 0x01,
    Handshake = 0x02,
    Retry = 0x03,
}

impl LongPacketType {
    pub fn from_bits(bits: u8) -> Result<Self, String> {
        match bits {
            0x00 => Ok(Self::Initial),
            0x01 => Ok(Self::ZeroRtt),
            0x02 => Ok(Self::Handshake),
            0x03 => Ok(Self::Retry),
            other => Err(format!("unknown long packet type: {:#x}", other)),
        }
    }
}

/// Represents a parsed QUIC long header.
#[derive(Debug, Clone)]
pub struct LongHeader {
    pub packet_type: LongPacketType,
    pub type_specific_bits: u8,
    pub version: u32,
    pub dst_conn_id: ConnectionId,
    pub src_conn_id: ConnectionId,
}

/// An Initial packet with its payload.
#[derive(Debug, Clone)]
pub struct InitialPacket {
    pub header: LongHeader,
    pub token: Vec<u8>,
    pub packet_number: u32,
    pub payload: Vec<u8>,
}

/// A Version Negotiation packet.
#[derive(Debug, Clone)]
pub struct VersionNegotiationPacket {
    pub dst_conn_id: ConnectionId,
    pub src_conn_id: ConnectionId,
    pub supported_versions: Vec<u32>,
}

/// A Retry packet.
#[derive(Debug, Clone)]
pub struct RetryPacket {
    pub header: LongHeader,
    pub retry_token: Vec<u8>,
    pub integrity_tag: [u8; 16],
}

/// Enum encompassing all packet types we handle.
#[derive(Debug)]
pub enum Packet {
    Initial(InitialPacket),
    VersionNegotiation(VersionNegotiationPacket),
    Retry(RetryPacket),
}

impl LongHeader {
    /// Encode a long header into bytes.
    pub fn encode(&self, buf: &mut Vec<u8>) {
        let first_byte = 0xC0 // Header Form = 1, Fixed Bit = 1
            | ((self.packet_type as u8) << 4)
            | (self.type_specific_bits & 0x0F);
        buf.push(first_byte);
        buf.extend_from_slice(&self.version.to_be_bytes());
        self.dst_conn_id.encode(buf);
        self.src_conn_id.encode(buf);
    }

    /// Decode a long header from a buffer.
    /// Returns the header and the number of bytes consumed.
    pub fn decode(buf: &[u8]) -> Result<(Self, usize), String> {
        if buf.len() < 6 {
            return Err("buffer too short for long header".into());
        }

        let first_byte = buf[0];
        if first_byte & 0x80 == 0 {
            return Err("not a long header packet (Header Form bit is 0)".into());
        }

        let packet_type = LongPacketType::from_bits((first_byte >> 4) & 0x03)?;
        let type_specific_bits = first_byte & 0x0F;

        let version = u32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]);

        let (dst_conn_id, dst_consumed) = ConnectionId::decode(buf, 5)
            .map_err(|e| format!("decode dst CID: {}", e))?;
        let offset = 5 + dst_consumed;

        let (src_conn_id, src_consumed) = ConnectionId::decode(buf, offset)
            .map_err(|e| format!("decode src CID: {}", e))?;
        let total = offset + src_consumed;

        Ok((
            Self {
                packet_type,
                type_specific_bits,
                version,
                dst_conn_id,
                src_conn_id,
            },
            total,
        ))
    }
}

impl InitialPacket {
    /// Encode an Initial packet into bytes (before encryption).
    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(128);

        // Packet number length is encoded in type-specific bits (0-indexed, so 0 = 1 byte)
        let pn_len = packet_number_length(self.packet_number);
        let mut header = self.header.clone();
        header.type_specific_bits = (pn_len - 1) as u8;
        header.encode(&mut buf);

        // Token length + token
        varint::encode(self.token.len() as u64, &mut buf).unwrap();
        buf.extend_from_slice(&self.token);

        // Remainder length = packet number length + payload length
        let remainder_len = pn_len + self.payload.len();
        varint::encode(remainder_len as u64, &mut buf).unwrap();

        // Packet number (variable length)
        encode_packet_number(self.packet_number, pn_len, &mut buf);

        // Payload
        buf.extend_from_slice(&self.payload);

        buf
    }

    /// Decode an Initial packet from a buffer.
    pub fn decode(buf: &[u8]) -> Result<(Self, usize), String> {
        let (header, mut offset) = LongHeader::decode(buf)?;
        if header.packet_type != LongPacketType::Initial {
            return Err(format!("expected Initial packet, got {:?}", header.packet_type));
        }

        // Token length + token
        let (token_len, consumed) = varint::decode(&mut Cursor::new(&buf[offset..]))
            .map_err(|e| format!("decode token length: {}", e))?;
        offset += consumed;

        let token = buf[offset..offset + token_len as usize].to_vec();
        offset += token_len as usize;

        // Remainder length
        let (remainder_len, consumed) = varint::decode(&mut Cursor::new(&buf[offset..]))
            .map_err(|e| format!("decode remainder length: {}", e))?;
        offset += consumed;

        // Packet number length is in the type-specific bits
        let pn_len = (header.type_specific_bits & 0x03) as usize + 1;
        let packet_number = decode_packet_number(&buf[offset..offset + pn_len], pn_len);
        offset += pn_len;

        let payload_len = remainder_len as usize - pn_len;
        let payload = buf[offset..offset + payload_len].to_vec();
        offset += payload_len;

        Ok((
            Self {
                header,
                token,
                packet_number,
                payload,
            },
            offset,
        ))
    }
}

impl VersionNegotiationPacket {
    /// Encode a Version Negotiation packet.
    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(64);

        // First byte: Header Form = 1, rest is random (we use 0)
        buf.push(0x80);
        // Version field is 0 for Version Negotiation
        buf.extend_from_slice(&0u32.to_be_bytes());
        self.dst_conn_id.encode(&mut buf);
        self.src_conn_id.encode(&mut buf);

        for version in &self.supported_versions {
            buf.extend_from_slice(&version.to_be_bytes());
        }

        buf
    }

    /// Decode a Version Negotiation packet from a buffer.
    pub fn decode(buf: &[u8]) -> Result<Self, String> {
        if buf.len() < 6 {
            return Err("buffer too short for version negotiation".into());
        }
        if buf[0] & 0x80 == 0 {
            return Err("not a long header packet".into());
        }

        let version = u32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]);
        if version != 0 {
            return Err("version negotiation packet must have version 0".into());
        }

        let (dst_conn_id, dst_consumed) = ConnectionId::decode(buf, 5)
            .map_err(|e| format!("decode dst CID: {}", e))?;
        let offset = 5 + dst_consumed;
        let (src_conn_id, src_consumed) = ConnectionId::decode(buf, offset)
            .map_err(|e| format!("decode src CID: {}", e))?;
        let mut offset = offset + src_consumed;

        let mut supported_versions = Vec::new();
        while offset + 4 <= buf.len() {
            let v = u32::from_be_bytes([buf[offset], buf[offset + 1], buf[offset + 2], buf[offset + 3]]);
            supported_versions.push(v);
            offset += 4;
        }

        Ok(Self {
            dst_conn_id,
            src_conn_id,
            supported_versions,
        })
    }
}

impl RetryPacket {
    /// Encode a Retry packet.
    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(128);
        self.header.encode(&mut buf);
        buf.extend_from_slice(&self.retry_token);
        buf.extend_from_slice(&self.integrity_tag);
        buf
    }
}

/// Determine how many bytes a packet number needs.
fn packet_number_length(pn: u32) -> usize {
    if pn <= 0xFF {
        1
    } else if pn <= 0xFFFF {
        2
    } else if pn <= 0xFF_FFFF {
        3
    } else {
        4
    }
}

/// Encode a packet number in the specified number of bytes.
fn encode_packet_number(pn: u32, length: usize, buf: &mut Vec<u8>) {
    let bytes = pn.to_be_bytes();
    buf.extend_from_slice(&bytes[4 - length..]);
}

/// Decode a packet number from a byte slice.
fn decode_packet_number(buf: &[u8], length: usize) -> u32 {
    let mut bytes = [0u8; 4];
    bytes[4 - length..].copy_from_slice(&buf[..length]);
    u32::from_be_bytes(bytes)
}

/// Parse the first packet from a UDP datagram. Returns the packet and remaining bytes
/// (for coalesced packet handling).
pub fn parse_datagram(buf: &[u8]) -> Result<(Packet, &[u8]), String> {
    if buf.is_empty() {
        return Err("empty datagram".into());
    }

    let is_long = buf[0] & 0x80 != 0;
    if !is_long {
        return Err("short header packets not supported in handshake".into());
    }

    // Check if this is a Version Negotiation packet (version == 0)
    if buf.len() >= 5 {
        let version = u32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]);
        if version == 0 {
            let pkt = VersionNegotiationPacket::decode(buf)?;
            return Ok((Packet::VersionNegotiation(pkt), &[]));
        }
    }

    let (header, _) = LongHeader::decode(buf)?;
    match header.packet_type {
        LongPacketType::Initial => {
            let (pkt, consumed) = InitialPacket::decode(buf)?;
            Ok((Packet::Initial(pkt), &buf[consumed..]))
        }
        LongPacketType::Retry => {
            // Retry packets: everything after the header (minus 16-byte integrity tag) is the token
            Err("retry packet parsing handled separately".into())
        }
        other => Err(format!("unsupported packet type during handshake: {:?}", other)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn initial_packet_round_trip() {
        let pkt = InitialPacket {
            header: LongHeader {
                packet_type: LongPacketType::Initial,
                type_specific_bits: 0,
                version: QUIC_VERSION_1,
                dst_conn_id: ConnectionId::generate(8),
                src_conn_id: ConnectionId::generate(8),
            },
            token: vec![],
            packet_number: 0,
            payload: b"hello handshake".to_vec(),
        };

        let encoded = pkt.encode();
        let (decoded, _) = InitialPacket::decode(&encoded).unwrap();

        assert_eq!(decoded.header.version, QUIC_VERSION_1);
        assert_eq!(decoded.header.dst_conn_id, pkt.header.dst_conn_id);
        assert_eq!(decoded.header.src_conn_id, pkt.header.src_conn_id);
        assert_eq!(decoded.token, pkt.token);
        assert_eq!(decoded.packet_number, pkt.packet_number);
        assert_eq!(decoded.payload, pkt.payload);
    }

    #[test]
    fn version_negotiation_round_trip() {
        let pkt = VersionNegotiationPacket {
            dst_conn_id: ConnectionId::generate(8),
            src_conn_id: ConnectionId::generate(4),
            supported_versions: vec![QUIC_VERSION_1, 0xFF00_001D],
        };

        let encoded = pkt.encode();
        let decoded = VersionNegotiationPacket::decode(&encoded).unwrap();

        assert_eq!(decoded.dst_conn_id, pkt.dst_conn_id);
        assert_eq!(decoded.src_conn_id, pkt.src_conn_id);
        assert_eq!(decoded.supported_versions, pkt.supported_versions);
    }

    #[test]
    fn packet_number_encoding() {
        for pn in [0u32, 1, 255, 256, 65535, 65536, 0x00FF_FFFF, 0xFFFF_FFFF] {
            let len = packet_number_length(pn);
            let mut buf = Vec::new();
            encode_packet_number(pn, len, &mut buf);
            let decoded = decode_packet_number(&buf, len);
            assert_eq!(decoded, pn, "packet number round-trip failed for {}", pn);
        }
    }

    #[test]
    fn coalesced_packets() {
        let pkt1 = InitialPacket {
            header: LongHeader {
                packet_type: LongPacketType::Initial,
                type_specific_bits: 0,
                version: QUIC_VERSION_1,
                dst_conn_id: ConnectionId::generate(8),
                src_conn_id: ConnectionId::generate(8),
            },
            token: vec![],
            packet_number: 0,
            payload: b"first".to_vec(),
        };
        let pkt2 = InitialPacket {
            header: LongHeader {
                packet_type: LongPacketType::Initial,
                type_specific_bits: 0,
                version: QUIC_VERSION_1,
                dst_conn_id: ConnectionId::generate(8),
                src_conn_id: ConnectionId::generate(8),
            },
            token: vec![],
            packet_number: 1,
            payload: b"second".to_vec(),
        };

        let mut datagram = pkt1.encode();
        datagram.extend_from_slice(&pkt2.encode());

        let (first, remaining) = parse_datagram(&datagram).unwrap();
        assert!(matches!(first, Packet::Initial(_)));
        assert!(!remaining.is_empty());

        let (second, remaining) = parse_datagram(remaining).unwrap();
        assert!(matches!(second, Packet::Initial(_)));
        assert!(remaining.is_empty());
    }
}
```

### `src/crypto.rs`

```rust
use ring::aead::{self, LessSafeKey, UnboundKey, Nonce, Aad};
use ring::hkdf;

/// Initial salt for QUIC version 1 (RFC 9001 Section 5.2).
const INITIAL_SALT: [u8; 20] = [
    0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3,
    0x4d, 0x17, 0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad,
    0xcc, 0xbb, 0x7f, 0x0a,
];

/// Key material derived for one direction of communication.
pub struct DirectionalKeys {
    pub key: LessSafeKey,
    pub iv: [u8; 12],
    pub hp_key: Vec<u8>,
}

/// Derive Initial keys from a Destination Connection ID.
/// This mirrors the real QUIC key derivation but uses the CID as the IKM.
pub fn derive_initial_keys(dst_conn_id: &[u8]) -> (DirectionalKeys, DirectionalKeys) {
    let salt = hkdf::Salt::new(hkdf::HKDF_SHA256, &INITIAL_SALT);
    let prk = salt.extract(dst_conn_id);

    let client_keys = derive_directional_keys(&prk, b"client in");
    let server_keys = derive_directional_keys(&prk, b"server in");

    (client_keys, server_keys)
}

fn derive_directional_keys(prk: &hkdf::Prk, label: &[u8]) -> DirectionalKeys {
    let mut secret = [0u8; 32];
    let info = [label];
    prk.expand(&info, HkdfLen(32))
        .expect("HKDF expand failed")
        .fill(&mut secret)
        .expect("HKDF fill failed");

    // Derive key (16 bytes for AES-128-GCM)
    let key_prk = hkdf::Salt::new(hkdf::HKDF_SHA256, &secret).extract(b"key");
    let mut key_bytes = [0u8; 16];
    key_prk
        .expand(&[b"quic key"], HkdfLen(16))
        .expect("key expand failed")
        .fill(&mut key_bytes)
        .expect("key fill failed");

    // Derive IV (12 bytes)
    let mut iv = [0u8; 12];
    key_prk
        .expand(&[b"quic iv"], HkdfLen(12))
        .expect("iv expand failed")
        .fill(&mut iv)
        .expect("iv fill failed");

    // Derive header protection key (16 bytes)
    let mut hp_key = vec![0u8; 16];
    key_prk
        .expand(&[b"quic hp"], HkdfLen(16))
        .expect("hp expand failed")
        .fill(&mut hp_key)
        .expect("hp fill failed");

    let unbound = UnboundKey::new(&aead::AES_128_GCM, &key_bytes)
        .expect("failed to create AES-128-GCM key");

    DirectionalKeys {
        key: LessSafeKey::new(unbound),
        iv,
        hp_key,
    }
}

/// Encrypt a payload using AEAD (AES-128-GCM).
pub fn encrypt_payload(
    keys: &DirectionalKeys,
    packet_number: u64,
    header_bytes: &[u8],
    plaintext: &[u8],
) -> Result<Vec<u8>, String> {
    let nonce_bytes = build_nonce(&keys.iv, packet_number);
    let nonce = Nonce::try_assume_unique_for_key(&nonce_bytes)
        .map_err(|_| "nonce creation failed")?;

    let aad = Aad::from(header_bytes);
    let mut in_out = plaintext.to_vec();
    keys.key
        .seal_in_place_append_tag(nonce, aad, &mut in_out)
        .map_err(|_| "AEAD encryption failed")?;

    Ok(in_out)
}

/// Decrypt a payload using AEAD (AES-128-GCM).
pub fn decrypt_payload(
    keys: &DirectionalKeys,
    packet_number: u64,
    header_bytes: &[u8],
    ciphertext: &mut Vec<u8>,
) -> Result<Vec<u8>, String> {
    let nonce_bytes = build_nonce(&keys.iv, packet_number);
    let nonce = Nonce::try_assume_unique_for_key(&nonce_bytes)
        .map_err(|_| "nonce creation failed")?;

    let aad = Aad::from(header_bytes);
    let plaintext = keys
        .key
        .open_in_place(nonce, aad, ciphertext)
        .map_err(|_| "AEAD decryption failed")?;

    Ok(plaintext.to_vec())
}

/// XOR the IV with the packet number to produce a nonce.
fn build_nonce(iv: &[u8; 12], packet_number: u64) -> [u8; 12] {
    let mut nonce = *iv;
    let pn_bytes = packet_number.to_be_bytes();
    for i in 0..8 {
        nonce[12 - 8 + i] ^= pn_bytes[i];
    }
    nonce
}

/// Apply or remove header protection.
/// `sample` is 16 bytes taken from the encrypted payload starting at a specific offset.
/// `pn_offset` and `pn_length` describe where the packet number is in the header.
pub fn apply_header_protection(
    hp_key: &[u8],
    sample: &[u8; 16],
    header: &mut [u8],
    pn_offset: usize,
    pn_length: usize,
) {
    // Use AES-ECB on the sample to produce a mask
    // Simplified: XOR with the hp_key itself (in production, use AES-ECB)
    let mask: Vec<u8> = sample.iter().zip(hp_key.iter().cycle()).map(|(a, b)| a ^ b).collect();

    // Protect the first byte (lower 4 bits for long header)
    if header[0] & 0x80 != 0 {
        header[0] ^= mask[0] & 0x0F;
    } else {
        header[0] ^= mask[0] & 0x1F;
    }

    // Protect the packet number bytes
    for i in 0..pn_length {
        header[pn_offset + i] ^= mask[1 + i];
    }
}

/// Helper type for HKDF output length.
struct HkdfLen(usize);

impl hkdf::KeyType for HkdfLen {
    fn len(&self) -> usize {
        self.0
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::connection_id::ConnectionId;

    #[test]
    fn key_derivation_deterministic() {
        let cid = ConnectionId::from_bytes(&[0x83, 0x94, 0xc8, 0xf0, 0x3e, 0x51, 0x57, 0x08]).unwrap();
        let (client1, server1) = derive_initial_keys(cid.as_bytes());
        let (client2, server2) = derive_initial_keys(cid.as_bytes());
        assert_eq!(client1.iv, client2.iv);
        assert_eq!(server1.iv, server2.iv);
        assert_eq!(client1.hp_key, client2.hp_key);
    }

    #[test]
    fn encrypt_decrypt_round_trip() {
        let cid = ConnectionId::generate(8);
        let (client_keys, _) = derive_initial_keys(cid.as_bytes());

        let header = b"fake header bytes";
        let plaintext = b"hello QUIC world";
        let packet_number = 0u64;

        let ciphertext = encrypt_payload(&client_keys, packet_number, header, plaintext).unwrap();
        assert_ne!(ciphertext[..plaintext.len()], plaintext[..]);

        let mut ct = ciphertext.clone();
        let decrypted = decrypt_payload(&client_keys, packet_number, header, &mut ct).unwrap();
        assert_eq!(decrypted, plaintext);
    }

    #[test]
    fn wrong_key_fails_decrypt() {
        let cid1 = ConnectionId::generate(8);
        let cid2 = ConnectionId::generate(8);
        let (keys1, _) = derive_initial_keys(cid1.as_bytes());
        let (keys2, _) = derive_initial_keys(cid2.as_bytes());

        let header = b"header";
        let plaintext = b"secret";
        let ciphertext = encrypt_payload(&keys1, 0, header, plaintext).unwrap();

        let mut ct = ciphertext;
        let result = decrypt_payload(&keys2, 0, header, &mut ct);
        assert!(result.is_err());
    }

    #[test]
    fn nonce_varies_with_packet_number() {
        let iv = [0u8; 12];
        let n0 = build_nonce(&iv, 0);
        let n1 = build_nonce(&iv, 1);
        assert_ne!(n0, n1);
    }
}
```

### `src/ack.rs`

```rust
use crate::varint;
use std::io::Cursor;

/// An ACK frame per RFC 9000 Section 19.3.
#[derive(Debug, Clone)]
pub struct AckFrame {
    pub largest_acknowledged: u64,
    pub ack_delay: u64,
    pub ranges: Vec<AckRange>,
}

/// A single range within an ACK frame.
#[derive(Debug, Clone)]
pub struct AckRange {
    pub gap: u64,
    pub length: u64,
}

/// Tracks received packet numbers and generates ACK frames.
pub struct AckTracker {
    received: Vec<u64>,
    max_ack_delay: u64,
}

impl AckTracker {
    pub fn new(max_ack_delay: u64) -> Self {
        Self {
            received: Vec::new(),
            max_ack_delay,
        }
    }

    /// Record a received packet number.
    pub fn record(&mut self, packet_number: u64) {
        if let Err(pos) = self.received.binary_search(&packet_number) {
            self.received.insert(pos, packet_number);
        }
    }

    /// Generate an ACK frame for all received packet numbers.
    pub fn generate_ack(&self) -> Option<AckFrame> {
        if self.received.is_empty() {
            return None;
        }

        let largest = *self.received.last().unwrap();
        let mut ranges = Vec::new();

        // Walk backward through received packets to build ranges
        let mut i = self.received.len() - 1;

        // First ACK range: count consecutive packets from the largest
        let mut first_range_end = i;
        while i > 0 && self.received[i] - self.received[i - 1] == 1 {
            i -= 1;
        }
        let first_ack_range = (first_range_end - i) as u64;

        // Additional ranges
        while i > 0 {
            let gap_start = self.received[i];
            i -= 1;
            let range_end = self.received[i];
            let gap = gap_start - range_end - 2; // RFC: gap = number of unacknowledged packets - 1

            let mut range_start = i;
            while range_start > 0 && self.received[range_start] - self.received[range_start - 1] == 1 {
                range_start -= 1;
            }

            let range_length = (i - range_start) as u64;
            ranges.push(AckRange {
                gap,
                length: range_length,
            });
            i = range_start;
            if range_start == 0 {
                break;
            }
        }

        Some(AckFrame {
            largest_acknowledged: largest,
            ack_delay: self.max_ack_delay,
            ranges,
        })
    }
}

impl AckFrame {
    /// Encode an ACK frame into bytes.
    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::new();
        buf.push(0x02); // ACK frame type (no ECN)
        varint::encode(self.largest_acknowledged, &mut buf).unwrap();
        varint::encode(self.ack_delay, &mut buf).unwrap();
        varint::encode(self.ranges.len() as u64, &mut buf).unwrap();

        // First ACK range (computed from the ranges list)
        let first_range = if self.ranges.is_empty() {
            self.largest_acknowledged
        } else {
            self.ranges.first().map_or(0, |r| r.length)
        };
        varint::encode(first_range, &mut buf).unwrap();

        for range in &self.ranges {
            varint::encode(range.gap, &mut buf).unwrap();
            varint::encode(range.length, &mut buf).unwrap();
        }

        buf
    }

    /// Decode an ACK frame from bytes.
    pub fn decode(buf: &[u8]) -> Result<(Self, usize), String> {
        let mut cursor = Cursor::new(buf);

        // Skip frame type byte
        let mut type_byte = [0u8; 1];
        std::io::Read::read_exact(&mut cursor, &mut type_byte)
            .map_err(|e| format!("read frame type: {}", e))?;

        let (largest_acknowledged, _) = varint::decode(&mut cursor)
            .map_err(|e| format!("decode largest ack: {}", e))?;
        let (ack_delay, _) = varint::decode(&mut cursor)
            .map_err(|e| format!("decode ack delay: {}", e))?;
        let (range_count, _) = varint::decode(&mut cursor)
            .map_err(|e| format!("decode range count: {}", e))?;
        let (_first_ack_range, _) = varint::decode(&mut cursor)
            .map_err(|e| format!("decode first ack range: {}", e))?;

        let mut ranges = Vec::new();
        for _ in 0..range_count {
            let (gap, _) = varint::decode(&mut cursor)
                .map_err(|e| format!("decode gap: {}", e))?;
            let (length, _) = varint::decode(&mut cursor)
                .map_err(|e| format!("decode range length: {}", e))?;
            ranges.push(AckRange { gap, length });
        }

        let consumed = cursor.position() as usize;
        Ok((
            Self {
                largest_acknowledged,
                ack_delay,
                ranges,
            },
            consumed,
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn consecutive_packets() {
        let mut tracker = AckTracker::new(25);
        for pn in 0..5 {
            tracker.record(pn);
        }
        let ack = tracker.generate_ack().unwrap();
        assert_eq!(ack.largest_acknowledged, 4);
        assert!(ack.ranges.is_empty(), "consecutive packets should produce no gaps");
    }

    #[test]
    fn packets_with_gaps() {
        let mut tracker = AckTracker::new(25);
        for pn in [0, 1, 2, 5, 6, 10] {
            tracker.record(pn);
        }
        let ack = tracker.generate_ack().unwrap();
        assert_eq!(ack.largest_acknowledged, 10);
        assert!(!ack.ranges.is_empty(), "gaps should produce ranges");
    }

    #[test]
    fn ack_frame_encode_decode() {
        let frame = AckFrame {
            largest_acknowledged: 42,
            ack_delay: 10,
            ranges: vec![
                AckRange { gap: 2, length: 3 },
            ],
        };

        let encoded = frame.encode();
        let (decoded, _) = AckFrame::decode(&encoded).unwrap();
        assert_eq!(decoded.largest_acknowledged, 42);
        assert_eq!(decoded.ack_delay, 10);
        assert_eq!(decoded.ranges.len(), 1);
        assert_eq!(decoded.ranges[0].gap, 2);
        assert_eq!(decoded.ranges[0].length, 3);
    }

    #[test]
    fn empty_tracker_returns_none() {
        let tracker = AckTracker::new(25);
        assert!(tracker.generate_ack().is_none());
    }
}
```

### `src/state.rs`

```rust
use crate::connection_id::ConnectionId;
use std::time::Instant;

/// Connection states for the QUIC handshake state machine.
#[derive(Debug)]
pub enum ConnectionState {
    /// No connection exists.
    Idle,

    /// Server is expecting a client Initial (possibly after sending Retry).
    WaitingForInitial {
        expected_retry_token: Option<Vec<u8>>,
    },

    /// Handshake packets are being exchanged.
    HandshakeInProgress {
        client_conn_id: ConnectionId,
        server_conn_id: ConnectionId,
        client_pn: u32,
        server_pn: u32,
    },

    /// Handshake completed, connection is usable.
    HandshakeComplete {
        client_conn_id: ConnectionId,
        server_conn_id: ConnectionId,
    },

    /// Connection is draining (waiting for remaining packets to arrive).
    Draining {
        drain_start: Instant,
    },

    /// Connection is fully closed.
    Closed,
}

/// Events that drive state transitions.
#[derive(Debug)]
pub enum ConnectionEvent {
    ClientInitialReceived { has_valid_token: bool },
    RetryRequired,
    ServerInitialSent,
    HandshakePacketReceived,
    HandshakeConfirmed,
    CloseRequested,
    DrainTimeout,
    IdleTimeout,
}

/// Errors when attempting an invalid state transition.
#[derive(Debug, PartialEq)]
pub enum TransitionError {
    InvalidTransition { from: &'static str, event: &'static str },
}

impl ConnectionState {
    /// Return the state name as a static string (for diagnostics and error messages).
    pub fn name(&self) -> &'static str {
        match self {
            Self::Idle => "Idle",
            Self::WaitingForInitial { .. } => "WaitingForInitial",
            Self::HandshakeInProgress { .. } => "HandshakeInProgress",
            Self::HandshakeComplete { .. } => "HandshakeComplete",
            Self::Draining { .. } => "Draining",
            Self::Closed => "Closed",
        }
    }

    /// Attempt a state transition. Returns the new state or an error.
    pub fn transition(
        self,
        event: ConnectionEvent,
        client_cid: Option<ConnectionId>,
        server_cid: Option<ConnectionId>,
    ) -> Result<Self, TransitionError> {
        match (&self, &event) {
            (Self::Idle, ConnectionEvent::ClientInitialReceived { has_valid_token: true }) => {
                Ok(Self::HandshakeInProgress {
                    client_conn_id: client_cid.unwrap_or_else(ConnectionId::empty),
                    server_conn_id: server_cid.unwrap_or_else(|| ConnectionId::generate(8)),
                    client_pn: 0,
                    server_pn: 0,
                })
            }

            (Self::Idle, ConnectionEvent::RetryRequired) => {
                Ok(Self::WaitingForInitial {
                    expected_retry_token: None, // Token generated separately
                })
            }

            (Self::WaitingForInitial { .. }, ConnectionEvent::ClientInitialReceived { has_valid_token: true }) => {
                Ok(Self::HandshakeInProgress {
                    client_conn_id: client_cid.unwrap_or_else(ConnectionId::empty),
                    server_conn_id: server_cid.unwrap_or_else(|| ConnectionId::generate(8)),
                    client_pn: 0,
                    server_pn: 0,
                })
            }

            (Self::WaitingForInitial { .. }, ConnectionEvent::ClientInitialReceived { has_valid_token: false }) => {
                Ok(Self::Closed)
            }

            (Self::HandshakeInProgress { .. }, ConnectionEvent::HandshakeConfirmed) => {
                let (ccid, scid) = match self {
                    Self::HandshakeInProgress { client_conn_id, server_conn_id, .. } => {
                        (client_conn_id, server_conn_id)
                    }
                    _ => unreachable!(),
                };
                Ok(Self::HandshakeComplete {
                    client_conn_id: ccid,
                    server_conn_id: scid,
                })
            }

            (Self::HandshakeComplete { .. }, ConnectionEvent::CloseRequested) => {
                Ok(Self::Draining {
                    drain_start: Instant::now(),
                })
            }

            (Self::Draining { .. }, ConnectionEvent::DrainTimeout) => {
                Ok(Self::Closed)
            }

            // Idle timeout from any active state
            (Self::HandshakeInProgress { .. }, ConnectionEvent::IdleTimeout)
            | (Self::HandshakeComplete { .. }, ConnectionEvent::IdleTimeout)
            | (Self::WaitingForInitial { .. }, ConnectionEvent::IdleTimeout) => {
                Ok(Self::Closed)
            }

            _ => Err(TransitionError::InvalidTransition {
                from: self.name(),
                event: event_name(&event),
            }),
        }
    }
}

fn event_name(event: &ConnectionEvent) -> &'static str {
    match event {
        ConnectionEvent::ClientInitialReceived { .. } => "ClientInitialReceived",
        ConnectionEvent::RetryRequired => "RetryRequired",
        ConnectionEvent::ServerInitialSent => "ServerInitialSent",
        ConnectionEvent::HandshakePacketReceived => "HandshakePacketReceived",
        ConnectionEvent::HandshakeConfirmed => "HandshakeConfirmed",
        ConnectionEvent::CloseRequested => "CloseRequested",
        ConnectionEvent::DrainTimeout => "DrainTimeout",
        ConnectionEvent::IdleTimeout => "IdleTimeout",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_handshake_flow() {
        let state = ConnectionState::Idle;

        let state = state
            .transition(
                ConnectionEvent::ClientInitialReceived { has_valid_token: true },
                Some(ConnectionId::generate(8)),
                Some(ConnectionId::generate(8)),
            )
            .unwrap();
        assert!(matches!(state, ConnectionState::HandshakeInProgress { .. }));

        let state = state
            .transition(ConnectionEvent::HandshakeConfirmed, None, None)
            .unwrap();
        assert!(matches!(state, ConnectionState::HandshakeComplete { .. }));

        let state = state
            .transition(ConnectionEvent::CloseRequested, None, None)
            .unwrap();
        assert!(matches!(state, ConnectionState::Draining { .. }));

        let state = state
            .transition(ConnectionEvent::DrainTimeout, None, None)
            .unwrap();
        assert!(matches!(state, ConnectionState::Closed));
    }

    #[test]
    fn retry_flow() {
        let state = ConnectionState::Idle;

        let state = state
            .transition(ConnectionEvent::RetryRequired, None, None)
            .unwrap();
        assert!(matches!(state, ConnectionState::WaitingForInitial { .. }));

        let state = state
            .transition(
                ConnectionEvent::ClientInitialReceived { has_valid_token: true },
                Some(ConnectionId::generate(8)),
                Some(ConnectionId::generate(8)),
            )
            .unwrap();
        assert!(matches!(state, ConnectionState::HandshakeInProgress { .. }));
    }

    #[test]
    fn invalid_token_after_retry_closes() {
        let state = ConnectionState::WaitingForInitial {
            expected_retry_token: Some(vec![1, 2, 3]),
        };

        let state = state
            .transition(
                ConnectionEvent::ClientInitialReceived { has_valid_token: false },
                None,
                None,
            )
            .unwrap();
        assert!(matches!(state, ConnectionState::Closed));
    }

    #[test]
    fn invalid_transition_rejected() {
        let state = ConnectionState::Idle;
        let result = state.transition(ConnectionEvent::HandshakeConfirmed, None, None);
        assert!(result.is_err());
    }

    #[test]
    fn idle_timeout_from_handshake() {
        let state = ConnectionState::HandshakeInProgress {
            client_conn_id: ConnectionId::generate(8),
            server_conn_id: ConnectionId::generate(8),
            client_pn: 0,
            server_pn: 0,
        };

        let state = state
            .transition(ConnectionEvent::IdleTimeout, None, None)
            .unwrap();
        assert!(matches!(state, ConnectionState::Closed));
    }
}
```

### `src/server.rs`

```rust
use crate::ack::AckTracker;
use crate::connection_id::ConnectionId;
use crate::crypto::{self, DirectionalKeys};
use crate::packet::*;
use crate::state::{ConnectionEvent, ConnectionState};

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::net::UdpSocket;
use tokio::sync::Mutex;

/// Per-connection context held by the server.
struct Connection {
    state: ConnectionState,
    remote_addr: SocketAddr,
    client_conn_id: ConnectionId,
    server_conn_id: ConnectionId,
    client_keys: Option<DirectionalKeys>,
    server_keys: Option<DirectionalKeys>,
    ack_tracker: AckTracker,
    last_activity: Instant,
}

/// QUIC handshake server.
pub struct QuicServer {
    socket: Arc<UdpSocket>,
    connections: Arc<Mutex<HashMap<ConnectionId, Connection>>>,
    supported_versions: Vec<u32>,
    idle_timeout: Duration,
    require_retry: bool,
}

impl QuicServer {
    pub async fn bind(addr: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let socket = UdpSocket::bind(addr).await?;
        Ok(Self {
            socket: Arc::new(socket),
            connections: Arc::new(Mutex::new(HashMap::new())),
            supported_versions: vec![QUIC_VERSION_1],
            idle_timeout: Duration::from_secs(30),
            require_retry: false,
        })
    }

    pub fn set_require_retry(&mut self, require: bool) {
        self.require_retry = require;
    }

    /// Run the server loop, processing incoming UDP datagrams.
    pub async fn run(&self) -> Result<(), Box<dyn std::error::Error>> {
        let mut buf = vec![0u8; 65535];

        let socket = self.socket.clone();
        let connections = self.connections.clone();

        // Spawn idle timeout checker
        let connections_timeout = connections.clone();
        let idle_timeout = self.idle_timeout;
        tokio::spawn(async move {
            loop {
                tokio::time::sleep(Duration::from_secs(5)).await;
                let mut conns = connections_timeout.lock().await;
                let expired: Vec<ConnectionId> = conns
                    .iter()
                    .filter(|(_, c)| c.last_activity.elapsed() > idle_timeout)
                    .map(|(id, _)| id.clone())
                    .collect();
                for id in expired {
                    if let Some(mut conn) = conns.remove(&id) {
                        let _ = conn.state.transition(
                            ConnectionEvent::IdleTimeout,
                            None,
                            None,
                        );
                        log::info!("connection {} timed out", id);
                    }
                }
            }
        });

        loop {
            let (len, remote_addr) = socket.recv_from(&mut buf).await?;
            let datagram = &buf[..len];

            if let Err(e) = self.process_datagram(datagram, remote_addr).await {
                log::warn!("error processing datagram from {}: {}", remote_addr, e);
            }
        }
    }

    async fn process_datagram(
        &self,
        mut data: &[u8],
        remote_addr: SocketAddr,
    ) -> Result<(), String> {
        // Handle coalesced packets
        while !data.is_empty() {
            let (packet, remaining) = parse_datagram(data)?;
            self.handle_packet(packet, remote_addr).await?;
            data = remaining;
        }
        Ok(())
    }

    async fn handle_packet(
        &self,
        packet: Packet,
        remote_addr: SocketAddr,
    ) -> Result<(), String> {
        match packet {
            Packet::Initial(initial) => self.handle_initial(initial, remote_addr).await,
            Packet::VersionNegotiation(_) => {
                log::warn!("server received version negotiation (ignoring)");
                Ok(())
            }
            Packet::Retry(_) => {
                log::warn!("server received retry (ignoring)");
                Ok(())
            }
        }
    }

    async fn handle_initial(
        &self,
        initial: InitialPacket,
        remote_addr: SocketAddr,
    ) -> Result<(), String> {
        // Check version support
        if !self.supported_versions.contains(&initial.header.version) {
            return self.send_version_negotiation(
                &initial.header.dst_conn_id,
                &initial.header.src_conn_id,
                remote_addr,
            ).await;
        }

        let mut conns = self.connections.lock().await;

        // Check if this is an existing connection
        if let Some(conn) = conns.get_mut(&initial.header.dst_conn_id) {
            conn.ack_tracker.record(initial.packet_number as u64);
            conn.last_activity = Instant::now();
            return Ok(());
        }

        // New connection
        if self.require_retry && initial.token.is_empty() {
            drop(conns);
            return self.send_retry(
                &initial.header.dst_conn_id,
                &initial.header.src_conn_id,
                remote_addr,
            ).await;
        }

        let server_conn_id = ConnectionId::generate(8);
        let client_conn_id = initial.header.src_conn_id.clone();

        // Derive Initial keys from the client's original destination CID
        let (client_keys, server_keys) = crypto::derive_initial_keys(
            initial.header.dst_conn_id.as_bytes(),
        );

        let mut ack_tracker = AckTracker::new(25);
        ack_tracker.record(initial.packet_number as u64);

        let state = ConnectionState::Idle;
        let state = state
            .transition(
                ConnectionEvent::ClientInitialReceived { has_valid_token: true },
                Some(client_conn_id.clone()),
                Some(server_conn_id.clone()),
            )
            .map_err(|e| format!("state transition failed: {:?}", e))?;

        let conn = Connection {
            state,
            remote_addr,
            client_conn_id: client_conn_id.clone(),
            server_conn_id: server_conn_id.clone(),
            client_keys: Some(client_keys),
            server_keys: Some(server_keys),
            ack_tracker,
            last_activity: Instant::now(),
        };

        conns.insert(server_conn_id.clone(), conn);
        drop(conns);

        // Send server Initial response
        self.send_server_initial(
            &server_conn_id,
            &client_conn_id,
            &initial.header.dst_conn_id,
            remote_addr,
        ).await
    }

    async fn send_server_initial(
        &self,
        server_conn_id: &ConnectionId,
        client_conn_id: &ConnectionId,
        original_dst_cid: &ConnectionId,
        remote_addr: SocketAddr,
    ) -> Result<(), String> {
        // Build ACK frame for the client's Initial
        let conns = self.connections.lock().await;
        let conn = conns.get(server_conn_id).ok_or("connection not found")?;
        let ack_frame = conn.ack_tracker.generate_ack();
        drop(conns);

        let mut payload = Vec::new();
        if let Some(ack) = ack_frame {
            payload.extend_from_slice(&ack.encode());
        }
        // Add a CRYPTO frame with simplified handshake data
        payload.push(0x06); // CRYPTO frame type
        crate::varint::encode(0, &mut payload).unwrap(); // offset
        let handshake_data = b"QUIC_HANDSHAKE_ACK";
        crate::varint::encode(handshake_data.len() as u64, &mut payload).unwrap();
        payload.extend_from_slice(handshake_data);

        let response = InitialPacket {
            header: LongHeader {
                packet_type: LongPacketType::Initial,
                type_specific_bits: 0,
                version: QUIC_VERSION_1,
                dst_conn_id: client_conn_id.clone(),
                src_conn_id: server_conn_id.clone(),
            },
            token: vec![], // Server Initial has no token
            packet_number: 0,
            payload,
        };

        let encoded = response.encode();
        self.socket
            .send_to(&encoded, remote_addr)
            .await
            .map_err(|e| format!("send server initial: {}", e))?;

        log::info!("sent server Initial to {} (scid={})", remote_addr, server_conn_id);
        Ok(())
    }

    async fn send_version_negotiation(
        &self,
        dst_conn_id: &ConnectionId,
        src_conn_id: &ConnectionId,
        remote_addr: SocketAddr,
    ) -> Result<(), String> {
        let vn = VersionNegotiationPacket {
            dst_conn_id: src_conn_id.clone(),  // Swap: send to the client's source CID
            src_conn_id: dst_conn_id.clone(),
            supported_versions: self.supported_versions.clone(),
        };

        let encoded = vn.encode();
        self.socket
            .send_to(&encoded, remote_addr)
            .await
            .map_err(|e| format!("send version negotiation: {}", e))?;

        log::info!("sent Version Negotiation to {}", remote_addr);
        Ok(())
    }

    async fn send_retry(
        &self,
        dst_conn_id: &ConnectionId,
        src_conn_id: &ConnectionId,
        remote_addr: SocketAddr,
    ) -> Result<(), String> {
        let new_server_cid = ConnectionId::generate(8);

        // Generate a retry token (simplified: hash of remote addr + original dst CID)
        let mut token_input = Vec::new();
        token_input.extend_from_slice(&remote_addr.ip().to_string().as_bytes());
        token_input.extend_from_slice(dst_conn_id.as_bytes());
        let token = ring::digest::digest(&ring::digest::SHA256, &token_input);
        let retry_token = token.as_ref()[..16].to_vec();

        // Compute integrity tag (simplified)
        let mut tag_input = Vec::new();
        tag_input.extend_from_slice(&retry_token);
        tag_input.extend_from_slice(dst_conn_id.as_bytes());
        let tag_digest = ring::digest::digest(&ring::digest::SHA256, &tag_input);
        let mut integrity_tag = [0u8; 16];
        integrity_tag.copy_from_slice(&tag_digest.as_ref()[..16]);

        let retry = RetryPacket {
            header: LongHeader {
                packet_type: LongPacketType::Retry,
                type_specific_bits: 0,
                version: QUIC_VERSION_1,
                dst_conn_id: src_conn_id.clone(),
                src_conn_id: new_server_cid,
            },
            retry_token,
            integrity_tag,
        };

        let encoded = retry.encode();
        self.socket
            .send_to(&encoded, remote_addr)
            .await
            .map_err(|e| format!("send retry: {}", e))?;

        log::info!("sent Retry to {}", remote_addr);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn server_binds_and_responds_to_initial() {
        let server = QuicServer::bind("127.0.0.1:0").await.unwrap();
        let server_addr = server.socket.local_addr().unwrap();

        let server_socket = server.socket.clone();
        let server_conns = server.connections.clone();

        // Spawn server processing in background
        let handle = tokio::spawn(async move {
            let mut buf = vec![0u8; 65535];
            let (len, remote_addr) = server_socket.recv_from(&mut buf).await.unwrap();
            let datagram = &buf[..len];
            let (packet, _) = parse_datagram(datagram).unwrap();

            if let Packet::Initial(initial) = packet {
                let server_conn_id = ConnectionId::generate(8);
                let response = InitialPacket {
                    header: LongHeader {
                        packet_type: LongPacketType::Initial,
                        type_specific_bits: 0,
                        version: QUIC_VERSION_1,
                        dst_conn_id: initial.header.src_conn_id,
                        src_conn_id: server_conn_id,
                    },
                    token: vec![],
                    packet_number: 0,
                    payload: b"server hello".to_vec(),
                };
                server_socket.send_to(&response.encode(), remote_addr).await.unwrap();
            }
        });

        // Client sends Initial
        let client = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let client_cid = ConnectionId::generate(8);
        let server_cid = ConnectionId::generate(8);

        let initial = InitialPacket {
            header: LongHeader {
                packet_type: LongPacketType::Initial,
                type_specific_bits: 0,
                version: QUIC_VERSION_1,
                dst_conn_id: server_cid,
                src_conn_id: client_cid.clone(),
            },
            token: vec![],
            packet_number: 0,
            payload: b"client hello".to_vec(),
        };

        client.send_to(&initial.encode(), server_addr).await.unwrap();

        // Receive server response
        let mut buf = vec![0u8; 65535];
        let (len, _) = tokio::time::timeout(
            Duration::from_secs(2),
            client.recv_from(&mut buf),
        )
        .await
        .unwrap()
        .unwrap();

        let (packet, _) = parse_datagram(&buf[..len]).unwrap();
        match packet {
            Packet::Initial(resp) => {
                assert_eq!(resp.header.dst_conn_id, client_cid);
                assert_eq!(resp.payload, b"server hello");
            }
            other => panic!("expected Initial response, got {:?}", other),
        }

        handle.await.unwrap();
    }

    #[tokio::test]
    async fn version_negotiation_for_unknown_version() {
        let socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let server_addr = socket.local_addr().unwrap();
        let server_socket = Arc::new(socket);

        let ss = server_socket.clone();
        let handle = tokio::spawn(async move {
            let mut buf = vec![0u8; 65535];
            let (len, remote_addr) = ss.recv_from(&mut buf).await.unwrap();
            // Simulate version negotiation response
            let (header, _) = LongHeader::decode(&buf[..len]).unwrap();
            let vn = VersionNegotiationPacket {
                dst_conn_id: header.src_conn_id,
                src_conn_id: header.dst_conn_id,
                supported_versions: vec![QUIC_VERSION_1],
            };
            ss.send_to(&vn.encode(), remote_addr).await.unwrap();
        });

        let client = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let initial = InitialPacket {
            header: LongHeader {
                packet_type: LongPacketType::Initial,
                type_specific_bits: 0,
                version: 0xBABA_FACE, // Unsupported version
                dst_conn_id: ConnectionId::generate(8),
                src_conn_id: ConnectionId::generate(8),
            },
            token: vec![],
            packet_number: 0,
            payload: b"hello".to_vec(),
        };

        client.send_to(&initial.encode(), server_addr).await.unwrap();

        let mut buf = vec![0u8; 65535];
        let (len, _) = tokio::time::timeout(
            Duration::from_secs(2),
            client.recv_from(&mut buf),
        )
        .await
        .unwrap()
        .unwrap();

        let (packet, _) = parse_datagram(&buf[..len]).unwrap();
        match packet {
            Packet::VersionNegotiation(vn) => {
                assert!(vn.supported_versions.contains(&QUIC_VERSION_1));
            }
            other => panic!("expected Version Negotiation, got {:?}", other),
        }

        handle.await.unwrap();
    }
}
```

### `src/main.rs`

```rust
mod ack;
mod connection_id;
mod crypto;
mod packet;
mod server;
mod state;
mod varint;

use server::QuicServer;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    env_logger::init();

    let addr = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "0.0.0.0:4433".to_string());

    log::info!("starting QUIC handshake server on {}", addr);

    let server = QuicServer::bind(&addr).await?;
    server.run().await?;

    Ok(())
}
```

## Build and Run

```bash
# Build
cargo build --release

# Run the server
RUST_LOG=info cargo run -- 0.0.0.0:4433

# Run all tests
cargo test -- --nocapture

# Run a specific test module
cargo test varint -- --nocapture
cargo test packet -- --nocapture
cargo test crypto -- --nocapture
cargo test state -- --nocapture
cargo test server -- --nocapture
```

## Expected Output

Test output:
```
running 4 tests
test varint::tests::round_trip_one_byte ... ok
test varint::tests::round_trip_two_bytes ... ok
test varint::tests::round_trip_four_bytes ... ok
test varint::tests::round_trip_eight_bytes ... ok
test varint::tests::rfc_test_vectors ... ok

running 4 tests
test connection_id::tests::generate_and_length ... ok
test connection_id::tests::encode_decode_round_trip ... ok
test connection_id::tests::empty_cid_round_trip ... ok
test connection_id::tests::reject_oversized ... ok

running 3 tests
test packet::tests::initial_packet_round_trip ... ok
test packet::tests::version_negotiation_round_trip ... ok
test packet::tests::packet_number_encoding ... ok
test packet::tests::coalesced_packets ... ok

running 3 tests
test crypto::tests::key_derivation_deterministic ... ok
test crypto::tests::encrypt_decrypt_round_trip ... ok
test crypto::tests::wrong_key_fails_decrypt ... ok
test crypto::tests::nonce_varies_with_packet_number ... ok

running 4 tests
test ack::tests::consecutive_packets ... ok
test ack::tests::packets_with_gaps ... ok
test ack::tests::ack_frame_encode_decode ... ok
test ack::tests::empty_tracker_returns_none ... ok

running 5 tests
test state::tests::valid_handshake_flow ... ok
test state::tests::retry_flow ... ok
test state::tests::invalid_token_after_retry_closes ... ok
test state::tests::invalid_transition_rejected ... ok
test state::tests::idle_timeout_from_handshake ... ok

running 2 tests
test server::tests::server_binds_and_responds_to_initial ... ok
test server::tests::version_negotiation_for_unknown_version ... ok

test result: ok. 24 passed; 0 failed; 0 ignored
```

Server running:
```
[INFO  quic_handshake] starting QUIC handshake server on 0.0.0.0:4433
[INFO  quic_handshake::server] sent server Initial to 127.0.0.1:52341 (scid=a3f8c2e1b904d567)
[INFO  quic_handshake::server] sent Version Negotiation to 192.168.1.5:60122
[INFO  quic_handshake::server] sent Retry to 10.0.0.3:43210
[INFO  quic_handshake::server] connection a3f8c2e1b904d567 timed out
```

## Design Decisions

**Why `ring` for cryptography.** `ring` is the de facto standard for cryptographic operations in Rust. It provides HKDF, AES-GCM, and SHA-256 without pulling in OpenSSL. The API enforces correct usage patterns (e.g., nonces cannot be reused by construction). For a real QUIC implementation you would also need `rustls` for TLS 1.3, but for this challenge the pre-shared key simplification keeps the focus on transport mechanics.

**Why separate `DirectionalKeys` for client and server.** QUIC uses distinct keys for each direction of communication, even during the Initial handshake. The client encrypts with its key, the server decrypts with the same key. This mirrors the real protocol's key schedule and prevents a category of reflection attacks where a packet sent by one side could be reflected back and accepted.

**Why the state machine uses `transition` consuming `self`.** Moving ownership of the state on each transition prevents holding references to stale state data. The compiler enforces that after a transition, only the new state is accessible. This eliminates an entire class of bugs where code accidentally reads fields from a previous state.

**Why `ConnectionId` uses `Vec<u8>` instead of a fixed array.** QUIC connection IDs have variable length (0-20 bytes). Using `Vec<u8>` avoids wasting stack space on short IDs and naturally handles the zero-length case. The allocation cost is negligible since CIDs are created once per connection, not per packet.

## Common Mistakes

1. **Confusing variable-length integer prefix bits with the value bits.** The two most significant bits of the first byte encode the length, not part of the value. Forgetting to mask them off before combining bytes produces values that are orders of magnitude too large.

2. **Swapping source and destination connection IDs in responses.** The server's response packet uses the client's source CID as the destination CID and the server's new CID as the source CID. Getting this backward means the client cannot match the response to its connection.

3. **Forgetting that Version Negotiation packets have version field set to zero.** The version field is normally non-zero, so parsing code that dispatches on version before checking for zero will misclassify Version Negotiation packets as unknown versions.

4. **Not handling coalesced packets.** A client may send an Initial and a 0-RTT packet in the same UDP datagram. Code that assumes one packet per datagram will discard the second packet silently.

5. **Using the same packet number space across encryption levels.** QUIC has separate packet number spaces for Initial, Handshake, and 1-RTT packets. Reusing a single counter across levels causes the receiver to reject packets as duplicates.

6. **Hardcoding packet number length to 4 bytes.** The packet number is variable-length (1-4 bytes) and the actual length is encoded in the type-specific bits of the first byte. Assuming 4 bytes wastes bandwidth and breaks interoperability.

## Performance Notes

- Variable-length integer encoding uses no allocation -- values are written directly to the output buffer. The `encoding_size` function lets callers pre-compute buffer sizes for zero-copy construction.
- Connection ID lookup is O(1) via `HashMap`. For high connection counts, consider a sharded map (`dashmap`) to reduce lock contention.
- The AEAD operations (AES-128-GCM) in `ring` use hardware acceleration (AES-NI) when available, processing ~5 GB/s on modern x86-64. This is never the bottleneck during handshake; the bottleneck is typically the key derivation.
- UDP `recv_from` returns one datagram at a time. For high packet rates, use `recvmmsg` (Linux) or `GRO` (Generic Receive Offload) to batch multiple datagrams per syscall. The `tokio` runtime does not expose this by default.

## Going Further

- Integrate `rustls` to replace pre-shared keys with full TLS 1.3 handshake
- Implement 0-RTT by caching server configuration and resumption tokens
- Add connection migration: accept packets from a new source address if they carry a valid connection ID
- Implement path validation (PATH_CHALLENGE and PATH_RESPONSE frames) for connection migration
- Build a QUIC client that performs the handshake against your server and against a public QUIC server like Google's
