# 4. Distributed Locking with Leases

<!--
difficulty: advanced
concepts: [distributed-lock, lease, fencing-token, lock-expiry, mutex, clock-drift, lock-safety]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [sync-primitives, goroutines-and-channels, leader-election-bully-algorithm]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of mutual exclusion and synchronization primitives
- Familiarity with distributed coordination concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a distributed lock with lease-based expiry
- **Analyze** the safety and liveness properties of lease-based locking
- **Demonstrate** the fencing token pattern for safe lock usage
- **Identify** failure scenarios: lock holder crash, clock drift, network delays

## Why Distributed Locking Matters

In distributed systems, multiple processes need exclusive access to shared resources: a database record, a file, a task assignment. Unlike local mutexes, distributed locks must handle process crashes (the lock holder dies without releasing), network partitions (the holder is alive but unreachable), and clock drift (lease expiry is timing-dependent). Lease-based locking with fencing tokens is the standard solution.

## The Problem

Build a distributed lock service that supports acquiring locks with a lease duration, releasing locks, auto-expiry on holder failure, and fencing tokens for safe resource access.

## Requirements

1. **Implement a `LockService` struct** that manages named locks:

```go
type Lock struct {
    Name         string
    Owner        string
    FencingToken int64
    ExpiresAt    time.Time
}

type LockService struct {
    locks map[string]*Lock
    mu    sync.Mutex
    nextToken int64
}

func NewLockService() *LockService
func (ls *LockService) Acquire(name, owner string, ttl time.Duration) (token int64, ok bool)
func (ls *LockService) Release(name, owner string, token int64) bool
func (ls *LockService) Renew(name, owner string, token int64, ttl time.Duration) bool
```

2. **Implement lease expiry** -- locks automatically expire after the TTL, allowing another client to acquire:

```go
func (ls *LockService) isExpired(lock *Lock) bool {
    return time.Now().After(lock.ExpiresAt)
}
```

3. **Implement fencing tokens** -- each lock acquisition returns a monotonically increasing token. Resource servers check that the token is not stale:

```go
type ResourceServer struct {
    lastToken int64
}

func (rs *ResourceServer) Execute(token int64, operation func()) bool {
    if token <= rs.lastToken {
        return false // Stale token -- reject
    }
    rs.lastToken = token
    operation()
    return true
}
```

4. **Demonstrate the danger without fencing tokens** -- show a scenario where a slow lock holder's operation arrives after the lock has expired and been re-acquired by another client.

5. **Implement lock renewal** -- a holder can extend the lease before it expires.

6. **Write tests** covering:
   - Basic acquire and release
   - Lock expiry and re-acquisition
   - Fencing token rejection of stale operations
   - Lock renewal before expiry
   - Concurrent lock contention

## Hints

- Leases solve the "dead lock holder" problem: if the holder crashes, the lock automatically expires.
- Fencing tokens solve the "slow lock holder" problem: if the holder is delayed (GC pause, network delay) past the lease, its operations are rejected because a newer token has been issued.
- Clock drift is the enemy of lease-based locking. In production, use a coordination service (etcd, ZooKeeper) with consensus-backed clocks rather than wall clocks.
- Lock renewal creates a "heartbeat" pattern: the holder renews periodically (e.g., every TTL/3) to maintain the lock.
- Use `time.Now().Add(ttl)` for expiry calculation in the simulation.

## Verification

```bash
go run main.go
go test -v -race ./...
```

Confirm that:
1. Only one client holds a lock at a time
2. Expired locks can be acquired by other clients
3. Fencing tokens prevent stale operations
4. Lock renewal extends the lease
5. Concurrent contention is handled correctly (only one winner)

## What's Next

Continue to [05 - Vector Clocks and Causality](../05-vector-clocks/05-vector-clocks.md) to implement causal ordering in distributed systems.

## Summary

- Distributed locks use leases (time-bounded ownership) to handle holder failures
- Fencing tokens (monotonic counters) protect against slow/delayed lock holders
- Without fencing, a delayed operation from an expired holder can corrupt shared state
- Lock renewal extends the lease via periodic heartbeats
- Clock drift is a fundamental challenge -- production systems use consensus-backed coordination
- Lease-based locking trades liveness (must wait for expiry) for safety (no permanent deadlock)

## Reference

- [Martin Kleppmann: How to do distributed locking](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html)
- [Chubby: The Lock Service for Loosely-Coupled Distributed Systems](https://research.google/pubs/pub27897/)
- [etcd distributed locking](https://etcd.io/docs/v3.5/dev-guide/api_concurrency_reference_v3/)
