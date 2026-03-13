<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Membership Protocol

## The Challenge

Implement a SWIM-based (Scalable Weakly-consistent Infection-style Membership) protocol that enables nodes in your distributed key-value store to detect failures, track cluster membership, and disseminate membership changes without relying on a central coordinator. Each node must maintain a local membership list, periodically probe random peers to detect failures, use indirect probes through third-party nodes to reduce false positives, and propagate membership updates (joins, leaves, failures) via piggybacked gossip on protocol messages. The protocol must converge within a bounded time, handle network partitions gracefully, and support voluntary graceful shutdown where a leaving node announces its departure before exiting.

## Requirements

1. Implement the SWIM failure detection protocol: each node selects a random peer every `T` milliseconds (default 200 ms) and sends a `ping`; if no `ack` is received within a timeout, the node sends `ping-req` to `k` random other members (default k=3) asking them to probe the suspect on its behalf.
2. If neither direct ping nor any indirect ping-req yields an ack within a second timeout window, mark the peer as `suspect`; after a configurable suspicion timeout (default 5 seconds), transition the peer to `dead` and remove it from the membership list.
3. Implement infection-style dissemination: piggyback membership updates (join, leave, suspect, dead events) on ping, ping-req, and ack messages, with each update carrying an incarnation number and a Lamport timestamp for ordering.
4. Support incarnation numbers for refuting false suspicions: when a node learns it has been marked as `suspect`, it increments its own incarnation number and broadcasts an `alive` message that overrides the suspicion.
5. Implement voluntary graceful leave: a node intending to shut down broadcasts a `leave` message with its current incarnation number, and other nodes immediately mark it as `dead` without waiting for the suspicion timeout.
6. The membership list must support at least these states per member: `alive`, `suspect`, `dead`, `left`, with state transitions governed by incarnation number comparisons (higher incarnation always wins, except `dead`/`left` are terminal for a given incarnation).
7. Integrate with the hash ring: when a member transitions to `dead` or `left`, trigger partition reassignment; when a new member joins (`alive`), trigger partition rebalancing.
8. Use UDP for all SWIM protocol messages (ping, ping-req, ack) with a maximum message size of 1400 bytes to fit within a single Ethernet frame; piggyback up to 6 membership updates per message using a compact binary encoding.

## Hints

- SWIM uses UDP for the protocol messages; each message should be a single datagram under 1400 bytes.
- Piggybacked gossip entries should be prioritized by recency: newer events get disseminated first, and events that have been piggybacked more than `log(N)` times can be dropped.
- Use `net.UDPConn` with `ReadFromUDP`/`WriteToUDP` for the transport layer.
- Randomize the probe target selection to ensure uniform failure detection across all members.
- Test network partitions by dropping packets between specific node pairs using a simulated network layer.
- HashiCorp memberlist is a production SWIM implementation in Go -- study its design but implement your own from scratch.
- The suspicion sub-protocol (incarnation-based refutation) is the trickiest part; draw the state machine on paper first.

## Success Criteria

1. A 5-node cluster converges to a consistent membership view within 2 seconds of startup.
2. When one node is killed, all surviving nodes detect the failure and remove it from their membership lists within 10 seconds.
3. A node marked as `suspect` that is actually alive successfully refutes the suspicion by incrementing its incarnation number, and all other nodes restore it to `alive` state.
4. Graceful leave propagates to all nodes within 1 second.
5. Partition reassignment is triggered when membership changes: killing a node causes its partitions to be reassigned to the remaining nodes.
6. The protocol operates correctly with simulated packet loss of up to 20%.
7. Gossip convergence for a membership update reaches all nodes in O(log N) protocol rounds.
8. All protocol messages fit within 1400 bytes even with 6 piggybacked updates.

## Research Resources

- "SWIM: Scalable Weakly-consistent Infection-style Process Group Membership Protocol" (Das et al., 2002)
- "Lifeguard: Local Health Awareness for More Accurate Failure Detection" (Dadgar et al., 2017) -- HashiCorp's SWIM extensions
- HashiCorp memberlist source code -- https://github.com/hashicorp/memberlist
- "A Gossip-Style Failure Detection Service" (van Renesse et al., 1998)
- Go `net` package UDP documentation
- Go `encoding/binary` for compact message serialization
