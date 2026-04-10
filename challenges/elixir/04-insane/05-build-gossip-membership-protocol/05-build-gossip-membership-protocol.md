# 5. Build a Gossip-Based Membership Protocol with Failure Detection

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, UDP sockets via `:gen_udp`, node monitoring, distributed Erlang)
- Mastered: Probability theory relevant to gossip — infection rate, convergence bounds, false positive/negative analysis
- Familiarity with: The SWIM protocol, Hashicorp's memberlist library, Cassandra's gossip implementation
- Reading: The SWIM paper (Das et al., 2002), the Hashicorp memberlist source code (Go)

## Problem Statement

Build a gossip-based cluster membership protocol in Elixir/OTP from scratch using UDP for communication. Do not use Erlang's built-in distributed node detection (`:net_kernel`, `Node.connect/1`, or `epmd`). Your protocol must discover, track, and maintain a consistent view of cluster membership across all nodes, detect node failures probabilistically without a central coordinator, and propagate membership changes efficiently.

Your system must implement:
1. A gossip dissemination engine: each node maintains a membership list; on each gossip round (configurable interval, e.g. 500ms), a node selects K random peers and shares its full or delta membership state; a new event (join/leave/failure) must reach all N nodes in O(log N) rounds in expectation
2. The SWIM failure detection protocol: instead of direct pinging, when node A suspects node B has failed, A asks K other nodes to ping B indirectly (indirect probe); only if all K indirect probes fail is B marked as `suspect`; after a configurable suspicion timeout, a `suspect` node becomes `dead`
3. A suspicion mechanism with incarnation numbers: when a node receives a rumor that it is dead, it can refute the rumor by broadcasting an `alive` message with a higher incarnation number — this prevents falsely evicting a slow-but-alive node
4. Merge semantics for membership views: when two partitioned sub-clusters reconnect, their membership views must converge to a single consistent view via anti-entropy gossip — no manual reconciliation
5. A cluster simulation that spawns 100 virtual nodes as Elixir processes on a single machine, each running the full gossip and failure detection logic, communicating via process messaging (not UDP) for simulation purposes
6. Observable metrics: propagation time per event, false positive rate of failure detection, message overhead per node per round, convergence time after a partition heals

## Acceptance Criteria

- [ ] **O(log N) propagation**: In a 100-node simulation, inject a single join event; measure the number of gossip rounds until all 100 nodes have received the event; confirm it is ≤ 2 * ceil(log2(100)) = 14 rounds in the median case
- [ ] **SWIM indirect probe**: Simulate a slow node (artificial delay of 2× the probe timeout); confirm that direct probes fail but indirect probes succeed (3 indirect probers); confirm the node is not marked dead due to indirect probe success
- [ ] **Failure detection — true positive**: Kill a node abruptly (no leave message); confirm the node is marked `suspect` after 3 failed probe rounds; confirm it is marked `dead` after the suspicion timeout; confirm the dead event propagates to all surviving nodes
- [ ] **False positive rate**: In a 100-node cluster under 10% message drop rate, run for 1 hour; confirm the false positive rate (live nodes incorrectly marked dead) is below 1% of node-rounds
- [ ] **Incarnation refutation**: Mark a live node as `suspect` by directly injecting a suspect rumor; confirm the targeted node detects the rumor and broadcasts an `alive` message with an incremented incarnation number; confirm all nodes update their view to `alive`
- [ ] **Partition and merge**: Split 100 nodes into two groups of 50 with a simulated partition (no messages cross); let each partition run for 10 gossip rounds; remove the partition; confirm both partitions' membership views converge to a single consistent view within 20 rounds
- [ ] **Anti-entropy reconciliation**: After a merge, verify that every node has identical incarnation numbers for every other node — no node holds a stale `dead` view of a node that the rest of the cluster knows is `alive`
- [ ] **Visualization**: After every 10 gossip rounds, output a text table showing each node's view of every other node (alive/suspect/dead) — this must reveal convergence visually as rounds progress

## What You Will Learn
- Why gossip protocols achieve O(log N) convergence and how the infection model (SIR model from epidemiology) explains the math
- The fundamental trade-off in failure detection: timeout too short → high false positive rate; timeout too long → slow detection; SWIM's indirect probing shifts this trade-off
- Why incarnation numbers are the correct mechanism for refutation — and why vector clocks are unnecessary for this specific problem
- How anti-entropy (full state exchange between random pairs) complements gossip (delta exchange) for eventual convergence
- The difference between "eventually consistent membership" and "strongly consistent membership" — and which applications can tolerate which
- How to build a faithful simulation of a distributed protocol using Elixir processes without real network — and the limits of this approach
- Why memberlist (Hashicorp) chose to layer a reliable delivery mechanism on top of UDP for certain membership events (PUSH/PULL)

## Hints

This exercise is intentionally sparse. You are expected to:
- Read the SWIM paper in full — every algorithm described there (probing, suspicion, dissemination) has a precise formulation; implement it exactly before considering optimizations
- Study the Hashicorp memberlist source code for how the Go implementation handles the same problems — the `memberlist.go` and `state.go` files are most relevant
- The gossip fanout K is the key tuning parameter: too low → slow convergence; too high → bandwidth explosion; derive the optimal K for your target N from the convergence bound formula in the SWIM paper
- Do not simulate message drops as random coin flips — model them as a configurable loss probability on each virtual link; this gives you reproducible test scenarios
- Your metrics collection must be outside the hot path — use a separate process to sample state rather than embedding measurement in the gossip loop

## Reference Material (Research Required)
- Das, A., Gupta, I. & Motivala, A. (2002). *SWIM: Scalable Weakly-Consistent Infection-Style Process Group Membership Protocol* — the primary source; do not read summaries
- Van Renesse, R. et al. (1998). *A Gossip-Style Failure Detection Service* — the predecessor to SWIM; understanding this makes SWIM's improvements clear
- Hashicorp memberlist source code — `memberlist.go`, `state.go`, `suspicion.go` — the production-grade Go implementation with extensive comments on protocol choices
- Cassandra gossip implementation — Apache Cassandra source, `src/java/org/apache/cassandra/gms/` — a real-world adaptation of gossip for database cluster management

## Difficulty Rating
★★★★★

## Estimated Time
3–5 weeks for an experienced Elixir developer with distributed systems and probability background
