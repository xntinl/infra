<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Raw Socket Packet Capture

## The Challenge

Build a network packet capture and analysis tool in Go using raw sockets that can sniff packets off a network interface, decode protocol headers at multiple layers (Ethernet, IP, TCP, UDP, ICMP, DNS, HTTP), display them in a human-readable format similar to tcpdump, and support BPF-based filtering to capture only packets matching specified criteria. Your tool must handle packet reassembly for TCP streams, compute and verify checksums, support promiscuous mode for capturing packets not destined for the local machine, and write captured packets to pcap files compatible with Wireshark. This is a deep networking exercise that requires understanding protocol header formats at the byte level.

## Requirements

1. Open a raw socket using `syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, htons(ETH_P_ALL))` to capture all Ethernet frames on a specified network interface; set the interface to promiscuous mode using `setsockopt` with `PACKET_ADD_MEMBERSHIP` and `PACKET_MR_PROMISC`.
2. Implement Ethernet frame parsing: extract destination MAC, source MAC, and EtherType (0x0800 for IPv4, 0x0806 for ARP, 0x86DD for IPv6); handle VLAN tags (EtherType 0x8100) by parsing the VLAN header and extracting the inner EtherType.
3. Implement IPv4 header parsing: extract version, IHL (header length), DSCP, total length, identification, flags (DF, MF), fragment offset, TTL, protocol (6=TCP, 17=UDP, 1=ICMP), header checksum, source IP, and destination IP; verify the header checksum.
4. Implement TCP header parsing: extract source port, destination port, sequence number, acknowledgment number, data offset, flags (SYN, ACK, FIN, RST, PSH, URG), window size, checksum, and urgent pointer; verify the TCP checksum using the pseudo-header.
5. Implement UDP header parsing (source port, destination port, length, checksum) and ICMP header parsing (type, code, checksum, payload depending on type).
6. Implement DNS message parsing: decode the DNS header (ID, flags, question count, answer count) and at least the question section (QNAME with label compression, QTYPE, QCLASS) for packets on port 53.
7. Implement BPF filtering: compile simple filter expressions (e.g., "tcp port 80", "host 192.168.1.1", "icmp") into classic BPF bytecode and attach the filter to the raw socket using `setsockopt` with `SO_ATTACH_FILTER`, so the kernel drops non-matching packets before they reach userspace.
8. Implement pcap file output: write captured packets in the pcap file format (global header followed by per-packet headers with timestamp, captured length, and original length, followed by packet data) compatible with Wireshark/tcpdump.

## Hints

- Use `htons(ETH_P_ALL)` which is `((ETH_P_ALL & 0xFF) << 8) | ((ETH_P_ALL & 0xFF00) >> 8)` since Go doesn't have a built-in `htons`.
- Network byte order is big-endian; use `binary.BigEndian.Uint16/Uint32` for parsing multi-byte header fields.
- The IPv4 header checksum is the one's complement of the one's complement sum of all 16-bit words in the header (with the checksum field set to zero).
- TCP checksum uses a pseudo-header: source IP, destination IP, zero byte, protocol (6), TCP length, followed by the TCP segment.
- DNS name compression: if the top two bits of a label length byte are set (0xC0), the remaining 14 bits are a pointer (offset) into the message.
- BPF filter compilation: for "tcp port 80", generate instructions to check EtherType==0x0800, IP protocol==6, and (TCP src port==80 OR TCP dst port==80).
- Pcap global header: magic number 0xa1b2c3d4, version 2.4, timezone 0, sigfigs 0, snaplen 65535, link type 1 (Ethernet).
- Raw sockets require `CAP_NET_RAW` capability or root privileges.

## Success Criteria

1. The tool captures and displays Ethernet frames with correct MAC addresses and EtherType on a live interface.
2. IPv4 headers are correctly parsed and the checksum verification succeeds for valid packets.
3. TCP three-way handshake (SYN, SYN-ACK, ACK) is correctly identified by parsing TCP flags during a `curl` to a web server.
4. DNS queries and responses are decoded showing the queried domain name.
5. BPF filtering correctly captures only matching packets: "tcp port 80" filter shows only HTTP traffic.
6. The pcap output file opens correctly in Wireshark with all packet data intact.
7. VLAN-tagged frames are correctly parsed with the inner EtherType and VLAN ID visible.
8. The tool handles 10,000 packets/sec without dropping packets (verified by comparing with `tcpdump` count).

## Research Resources

- "TCP/IP Illustrated, Volume 1" (Stevens, 2011) -- protocol header formats
- Linux packet socket man page -- `man 7 packet`
- pcap file format -- https://wiki.wireshark.org/Development/LibpcapFileFormat
- RFC 791 (IPv4), RFC 793 (TCP), RFC 768 (UDP), RFC 792 (ICMP), RFC 1035 (DNS)
- BPF filter man page -- `man 7 bpf`, `man 7 pcap-filter`
- gopacket library -- https://github.com/google/gopacket -- reference implementation (study, don't use)
