<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 6h
-->

# Custom Network Protocol Stack

## The Challenge

Implement a userspace TCP/IP network protocol stack in Go that processes raw Ethernet frames from a TAP device and implements the ARP, IPv4, ICMP, and TCP protocols from scratch, capable of establishing a TCP connection with a standard Linux networking stack. Your stack must respond to ARP requests, reply to ICMP echo (ping), perform the TCP three-way handshake, implement TCP reliable data transfer with sequence numbers and acknowledgments, handle flow control via the sliding window protocol, implement congestion control (slow start and congestion avoidance), and support graceful connection teardown. This is a comprehensive networking exercise that requires implementing each layer of the stack and integrating them into a working system that can serve HTTP responses to a standard web browser.

## Requirements

1. Set up a TAP device using `ioctl` with `TUNSETIFF` to create a virtual network interface that delivers raw Ethernet frames to your userspace program; configure the TAP device with an IP address and bring it up using Netlink or `ip` commands.
2. Implement the Ethernet layer: parse incoming frames (destination MAC, source MAC, EtherType), dispatch to the appropriate upper-layer handler (ARP for 0x0806, IPv4 for 0x0800), and construct outgoing frames with correct headers.
3. Implement ARP: maintain an ARP cache mapping IP addresses to MAC addresses, respond to ARP requests for the stack's own IP address, send ARP requests for unknown destinations, and queue outgoing packets while waiting for ARP resolution.
4. Implement IPv4: parse incoming IP packets, validate the header checksum, handle TTL decrement, and construct outgoing IP packets with correct headers including checksum computation; support fragmentation for outgoing packets exceeding the MTU (optional but valuable).
5. Implement ICMP: respond to echo requests (ping) with echo replies, including correct checksum computation and ID/sequence number copying.
6. Implement TCP connection establishment: perform the three-way handshake (SYN, SYN-ACK, ACK) as both initiator and responder, managing connection state transitions (LISTEN, SYN-SENT, SYN-RECEIVED, ESTABLISHED) and selecting initial sequence numbers.
7. Implement TCP reliable data transfer: segment outgoing data, attach sequence numbers, retransmit unacknowledged segments using a retransmission timer (exponential backoff), process incoming ACKs to advance the send window, and deliver in-order data to the application (buffering out-of-order segments).
8. Implement TCP connection teardown (FIN, FIN-ACK, ACK) with the TIME_WAIT state (abbreviated for testing), and implement a simple HTTP/1.0 server on top of your TCP stack that serves a "Hello, World!" HTML page to a standard web browser.

## Hints

- Use `/dev/net/tun` to create the TAP device: `open("/dev/net/tun", O_RDWR)`, then `ioctl(fd, TUNSETIFF, &ifr)` with `IFF_TAP | IFF_NO_PI`.
- Configure the TAP device's peer address in a different subnet: your stack is 10.0.0.2, the TAP peer is 10.0.0.1, so packets from the host to 10.0.0.2 are delivered to your program.
- TCP state machine: implement as an explicit state machine with states as an enum and transitions triggered by incoming segments and application calls.
- Retransmission timer: start at 1 second, double on each timeout (exponential backoff), reset on receiving a new ACK; use Karn's algorithm (don't update RTT estimates on retransmitted segments).
- TCP checksum uses the same pseudo-header approach as in the packet capture exercise.
- For the sliding window, maintain `SND.UNA` (oldest unacknowledged), `SND.NXT` (next to send), and `SND.WND` (window size); only send data in the window `[SND.UNA, SND.UNA + SND.WND)`.
- For the HTTP server, just handle `GET /` requests: parse the request line, send a hardcoded response with `Content-Length`, and close the connection.
- Test with `ping 10.0.0.2` (ICMP), `curl http://10.0.0.2/` (TCP+HTTP), and `arping` (ARP).

## Success Criteria

1. `ping 10.0.0.2` from the host receives correct ICMP echo replies from the userspace stack with correct checksums.
2. ARP resolution works: `arping -I tap0 10.0.0.2` receives ARP replies and the host's ARP cache is populated.
3. TCP three-way handshake completes successfully: `curl http://10.0.0.2/` establishes a connection (verified in Wireshark).
4. The HTTP server responds with "Hello, World!" to `curl http://10.0.0.2/` and the connection is torn down cleanly.
5. TCP retransmission works: artificially dropping a segment (by ignoring one incoming ACK) triggers retransmission and the data is eventually delivered.
6. The stack handles concurrent connections (two simultaneous `curl` requests) without confusion.
7. TCP sequence numbers and acknowledgment numbers are correct throughout the connection lifecycle (verified in Wireshark).
8. The sliding window flow control limits the sender to the receiver's advertised window size.

## Research Resources

- "TCP/IP Illustrated, Volume 1" (Stevens, 2011) -- essential reference for all protocols
- "TCP/IP Illustrated, Volume 2: The Implementation" (Wright & Stevens, 1995) -- BSD TCP/IP stack implementation
- RFC 791 (IPv4), RFC 793 (TCP), RFC 826 (ARP), RFC 792 (ICMP)
- saminiir/level-ip -- userspace TCP/IP stack in C -- https://github.com/saminiir/level-ip
- google/netstack -- userspace network stack in Go (part of gVisor) -- https://github.com/google/gvisor/tree/master/pkg/tcpip
- TUN/TAP documentation -- https://www.kernel.org/doc/html/latest/networking/tuntap.html
