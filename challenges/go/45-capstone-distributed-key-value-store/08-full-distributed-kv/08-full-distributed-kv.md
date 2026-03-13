<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 6h+
-->

# Full Distributed Key-Value Store

## The Challenge

Integrate all components from the previous seven exercises into a complete, production-grade distributed key-value store that can be deployed as a cluster of independent processes communicating over the network. Your system must boot a cluster from a seed node, automatically discover peers via the SWIM membership protocol, partition data across nodes using consistent hashing, replicate with tunable quorum consistency, repair divergence via read repair and anti-entropy with Merkle trees, buffer writes for failed nodes via hinted handoff, and expose a client-facing API through the client protocol library. You must also add operational features not covered in individual exercises: a CLI tool for cluster administration, an HTTP status endpoint for monitoring, graceful cluster rolling restarts, and a comprehensive integration test suite that validates the entire system under realistic failure scenarios including node crashes, network partitions, and split-brain recovery.

## Requirements

1. Build a single Go binary (`dkv`) that starts a node with a configuration file or flags specifying: listen address for client connections, listen address for inter-node RPC, listen address for SWIM protocol, data directory, replication factor N, read quorum R, write quorum W, seed node addresses for bootstrap, and virtual nodes count.
2. On startup, the node joins the cluster by contacting seed nodes via the SWIM protocol, receives the current membership list and hash ring, and takes ownership of its assigned partitions by requesting data transfer from existing owners.
3. Integrate all subsystems: partitioned storage (exercise 1), replication and consistency (exercise 2), anti-entropy with Merkle trees (exercise 3), hinted handoff (exercise 4), read repair (exercise 5), membership protocol (exercise 6), and client protocol (exercise 7) into a cohesive system with clean internal interfaces.
4. Build a CLI tool (`dkv-admin`) that supports: `cluster status` (show all nodes, their states, and partition ownership), `node drain <addr>` (gracefully migrate partitions away from a node before decommission), `node join <addr>` (manually trigger a node join), and `repair <partition>` (force an anti-entropy repair).
5. Implement an HTTP status endpoint on each node (default port: node port + 1000) that exposes JSON metrics: cluster membership, partition ownership map, per-partition key counts, replication lag estimates, hint queue depth, anti-entropy statistics, and operation latency histograms.
6. Support graceful rolling restart: draining a node causes its partitions to be temporarily reassigned, the node restarts with new configuration or binary, rejoins, and reclaims its partitions with minimal data transfer using its persisted state.
7. Write an integration test suite with at least these scenarios: (a) 3-node cluster bootstrap from seed, (b) write 10,000 keys and read them all back, (c) kill one node and verify reads still succeed at QUORUM, (d) restart the killed node and verify it recovers via hinted handoff and anti-entropy, (e) network partition between two groups and verify writes succeed on the majority side, (f) heal the partition and verify convergence, (g) rolling restart of all nodes with zero data loss.
8. Handle split-brain scenarios: when a network partition heals, nodes on both sides may have accepted conflicting writes; ensure vector clocks correctly identify conflicts and the system surfaces siblings to the client rather than silently losing data.

## Hints

- Use a layered architecture: transport layer (TCP/UDP), protocol layer (message encoding/decoding), service layer (storage, replication, membership), and API layer (client-facing protocol).
- The `dkv` binary should use `cobra` or `flag` for CLI parsing and `signal.NotifyContext` for graceful shutdown on SIGTERM/SIGINT.
- Integration tests can use `exec.Command` to start multiple `dkv` processes on different ports, or run everything in-process with different goroutines and `net.Listen("tcp", ":0")`.
- For the HTTP status endpoint, use `net/http` with `encoding/json`; expose histograms using a simple P50/P95/P99 percentile struct.
- Rolling restart: the drain operation should wait for all partitions to be fully replicated elsewhere before signaling ready-to-stop.
- Split-brain test: use `iptables`-style packet filtering in the test (or a simulated network layer) to create a partition, then remove the filter to heal it.
- Keep the data directory structure clean: `<datadir>/partitions/<id>/wal/`, `<datadir>/partitions/<id>/sstables/`, `<datadir>/hints/<target-node-id>/`.

## Success Criteria

1. A 5-node cluster bootstraps from a single seed node in under 10 seconds, with all nodes agreeing on the membership list and partition ownership.
2. Writing 100,000 keys with `W=2` and reading them all back with `R=2` returns every key correctly.
3. Killing 1 out of 5 nodes (N=3, R=2, W=2) results in zero failed reads for keys with at least 2 surviving replicas.
4. The killed node, upon restart, recovers all missing data via hinted handoff (for recent writes) and anti-entropy (for any remaining gaps) within 60 seconds.
5. A network partition isolating 2 nodes from 3 allows the majority side to continue serving reads and writes; after healing, all 5 nodes converge to a consistent state within 30 seconds.
6. Rolling restart of all 5 nodes (one at a time, with drain/rejoin) completes with zero data loss verified by a full read of all keys.
7. The `dkv-admin cluster status` command correctly shows all node states, partition counts, and replication health.
8. The HTTP metrics endpoint returns valid JSON with accurate operation counts and latency percentiles.

## Research Resources

- "Dynamo: Amazon's Highly Available Key-Value Store" (DeCandia et al., 2007) -- the complete system design
- "Designing Data-Intensive Applications" (Kleppmann, 2017) -- chapters on replication, partitioning, and consistency
- etcd architecture documentation -- https://etcd.io/docs/v3.5/learning/architecture/
- CockroachDB design docs -- https://github.com/cockroachdb/cockroach/tree/master/docs/design
- Riak KV architecture -- https://docs.riak.com/riak/kv/latest/learn/concepts/
- Go `os/exec` package for integration test process management
- Go `testing` package best practices for long-running integration tests
