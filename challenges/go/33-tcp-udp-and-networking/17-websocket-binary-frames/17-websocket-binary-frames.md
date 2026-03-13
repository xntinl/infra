# 17. WebSocket Binary Frames

<!--
difficulty: advanced
concepts: [websocket, binary-frames, message-framing, gorilla-websocket, ping-pong, close-handshake]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [tcp-server-and-client, http-programming, concurrent-tcp-server]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Concurrent TCP Server exercises
- Understanding of the HTTP upgrade mechanism
- Familiarity with binary data encoding in Go (`encoding/binary`, byte slices)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** WebSocket server and client that exchange binary frames using `gorilla/websocket`
- **Design** a binary message protocol over WebSocket with type headers, length prefixes, and payload serialization
- **Analyze** the difference between text and binary frames, and when to use each
- **Apply** ping/pong heartbeats and proper close handshake for production WebSocket connections

## Why Binary WebSocket Frames Matter

WebSocket supports two data frame types: text (UTF-8) and binary. Text frames work for JSON, but binary frames are essential for transmitting structured data, images, compressed payloads, or custom protocols efficiently. Binary framing avoids base64 encoding overhead and allows direct serialization of structured data. Production WebSocket services also need heartbeat (ping/pong) to detect dead connections and proper close handshakes for graceful shutdown.

## The Problem

Build a WebSocket server that receives structured binary messages (sensor data), processes them, and broadcasts aggregated results back as binary frames. The protocol uses a custom binary format with type headers and length-prefixed payloads.

## Requirements

1. **Binary message format** -- define a binary wire format: 1-byte message type, 4-byte big-endian payload length, followed by payload bytes
2. **Message types** -- support at least three types: sensor data submission (type 0x01), aggregation response (type 0x02), and error (type 0xFF)
3. **Sensor data encoding** -- encode sensor readings as binary: 4-byte sensor ID, 8-byte float64 value, 8-byte Unix timestamp (all big-endian)
4. **Server handler** -- accept WebSocket upgrades using `gorilla/websocket.Upgrader`, read binary frames, decode sensor data, compute running averages, and send binary aggregation responses
5. **Ping/pong** -- configure the server to send pings every 10 seconds and close connections that miss two consecutive pongs
6. **Close handshake** -- implement proper WebSocket close with a close message, wait for close reply, then terminate
7. **Client** -- connect via `websocket.Dial`, send binary sensor readings, receive and decode binary responses
8. **Tests** -- test binary encoding/decoding round-trip, server aggregation logic, ping/pong handling, and close handshake

## Hints

<details>
<summary>Hint 1: Binary encoding helpers</summary>

```go
func encodeSensorData(sensorID uint32, value float64, ts int64) []byte {
    buf := make([]byte, 20)
    binary.BigEndian.PutUint32(buf[0:4], sensorID)
    binary.BigEndian.PutUint64(buf[4:12], math.Float64bits(value))
    binary.BigEndian.PutUint64(buf[12:20], uint64(ts))
    return buf
}

func encodeMessage(msgType byte, payload []byte) []byte {
    msg := make([]byte, 5+len(payload))
    msg[0] = msgType
    binary.BigEndian.PutUint32(msg[1:5], uint32(len(payload)))
    copy(msg[5:], payload)
    return msg
}
```

</details>

<details>
<summary>Hint 2: WebSocket server with binary frames</summary>

```go
upgrader := websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
}

func handler(w http.ResponseWriter, r *http.Request) {
    conn, _ := upgrader.Upgrade(w, r, nil)
    defer conn.Close()

    conn.SetPongHandler(func(string) error {
        conn.SetReadDeadline(time.Now().Add(30 * time.Second))
        return nil
    })

    for {
        msgType, data, err := conn.ReadMessage()
        if err != nil { break }
        if msgType == websocket.BinaryMessage {
            // Decode and process binary data
            response := processMessage(data)
            conn.WriteMessage(websocket.BinaryMessage, response)
        }
    }
}
```

</details>

<details>
<summary>Hint 3: Proper close handshake</summary>

```go
// Server-initiated close
msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutting down")
conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(5*time.Second))
// Wait briefly for client's close response, then close the underlying connection
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Binary sensor data round-trips correctly through encode/decode
- Server receives binary frames and sends binary responses
- Aggregation computes correct running averages
- Ping/pong keeps the connection alive during idle periods
- Close handshake completes gracefully from both client and server sides
- Text frames are rejected or handled separately from binary frames

## What's Next

Continue to [18 - Connection Draining](../18-connection-draining/18-connection-draining.md) to implement graceful connection draining for server shutdown.

## Summary

- WebSocket binary frames carry raw bytes without UTF-8 encoding constraints
- Custom binary protocols use type headers and length prefixes for message framing
- `encoding/binary.BigEndian` provides portable, network-byte-order encoding
- Ping/pong frames detect dead connections; set `PongHandler` to extend read deadlines
- The WebSocket close handshake is a two-way exchange: initiator sends close, receiver echoes close, then both sides close
- `gorilla/websocket` distinguishes `TextMessage` and `BinaryMessage` frame types

## Reference

- [gorilla/websocket](https://pkg.go.dev/github.com/gorilla/websocket)
- [RFC 6455 -- The WebSocket Protocol](https://datatracker.ietf.org/doc/html/rfc6455)
- [encoding/binary](https://pkg.go.dev/encoding/binary)
