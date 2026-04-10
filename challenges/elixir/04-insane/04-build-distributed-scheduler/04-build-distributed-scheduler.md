# 4. Build a Distributed Job Scheduler with Bin-Packing and Preemption

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, DynamicSupervisor, Process monitoring, :pg, distributed Erlang)
- Mastered: Operating system scheduling theory — priority queues, preemption, resource accounting, bin-packing algorithms
- Familiarity with: Container orchestration concepts (resource requests/limits, node affinity, job preemption), REST API design with Plug
- Reading: The Kubernetes scheduler source code, the Mesos paper (Hindman et al., 2011), the Omega paper (Schwarzkopf et al., 2012)

## Problem Statement

Build a distributed job scheduler in Elixir/OTP that assigns computational jobs to worker nodes based on resource availability, enforces fairness across users, and handles node failures by rescheduling affected jobs. The scheduler must expose a REST API built with Plug (not Phoenix) for job management.

Your system must implement:
1. A resource model where each worker node continuously reports its available CPU (units) and memory (MB) to the scheduler via heartbeats; the scheduler maintains a live view of cluster capacity
2. A bin-packing algorithm for job placement: given a job's resource request (CPU units + memory MB), select the node that satisfies the request while minimizing wasted capacity (best-fit decreasing heuristic)
3. Priority-based preemption: jobs carry a priority level (1–10); when a high-priority job cannot be scheduled due to resource contention, the scheduler may evict one or more lower-priority jobs from a node to make room, provided the evicted jobs are rescheduled elsewhere or queued
4. Fault tolerance: the scheduler monitors all worker nodes; when a node stops heartbeating (death), all jobs running on that node are detected, their resources freed, and the jobs requeued for rescheduling on surviving nodes
5. Fair-share scheduling: no single user's jobs may consume more than a configurable percentage X of total cluster resources; a user exceeding their fair share has new job submissions queued until their consumption drops below the threshold
6. A complete job lifecycle audit trail: every state transition (submitted → queued → scheduled → running → completed/failed/preempted) is recorded with timestamps and node assignment
7. A REST API over Plug with endpoints for: submit job, cancel job, query job status, list all jobs, list nodes and their current resource usage

## Acceptance Criteria

- [ ] **Resource model**: Each worker node GenServer reports CPU and memory every 5 seconds; the scheduler's cluster view reflects changes within one heartbeat cycle; a node that misses 3 consecutive heartbeats is marked unavailable
- [ ] **Bin-packing correctness**: Given 5 nodes with varying available capacity, submit 20 jobs with varying resource requests; verify via audit trail that each job is placed on the node with the tightest fit that still satisfies the request (best-fit)
- [ ] **No overcommit**: The scheduler must never assign a job to a node if the assignment would exceed the node's available CPU or memory; verify this invariant across 10,000 random job submissions
- [ ] **Preemption**: Submit a low-priority (1) job on a nearly full cluster; then submit a high-priority (9) job that requires resources only available via eviction; confirm the low-priority job is preempted, the high-priority job runs, and the preempted job is requeued
- [ ] **Fault tolerance**: With 10 jobs running across 3 nodes, kill one node abruptly; within 30 seconds, all jobs that were on the dead node must be detected, requeued, and rescheduled on surviving nodes
- [ ] **Fair-share enforcement**: Configure max 33% per user; user A submits 100 CPU-intensive jobs; user B submits 10 jobs; verify user A's jobs beyond the fair-share threshold are queued and user B's jobs run without starvation
- [ ] **Job audit trail**: Every state transition for every job is recorded; after 1,000 jobs complete, query the audit trail by job ID and verify each job has a complete, chronologically consistent state history
- [ ] **REST API — submit**: `POST /jobs` with `{user, cpu, memory, priority, command}` returns `{job_id, status: "queued"}`; subsequent `GET /jobs/:id` returns the current state
- [ ] **REST API — cancel**: `DELETE /jobs/:id` cancels a queued or running job; a running job's resources are immediately freed on the worker node
- [ ] **REST API — cluster view**: `GET /nodes` returns each node with its total capacity and current usage; values are accurate within one heartbeat cycle (5 seconds)

## What You Will Learn
- How bin-packing is a variant of the NP-hard knapsack problem and why greedy heuristics (best-fit decreasing) work well in practice for scheduling
- Why preemption requires careful ordering: you must guarantee the evicted job is successfully requeued before committing to the eviction
- How fair-share scheduling differs from strict priority scheduling and how Dominant Resource Fairness (DRF) extends it to multi-resource environments
- The difference between optimistic concurrency (Omega's model) and pessimistic locking (Mesos's two-level scheduling) for cluster state
- How heartbeat-based failure detection works and why it always has a trade-off between false-positive rate and detection latency
- How to build a REST API with Plug directly (routing, request parsing, JSON response encoding) without the Phoenix framework abstraction layer
- How to model a multi-state FSM for job lifecycle in Elixir without any framework — pure GenServer state machines

## Hints

This exercise is intentionally sparse. You are expected to:
- Study the Mesos paper's architecture diagram carefully — understand the difference between the scheduler (global) and the executor (per-node) before designing your process topology
- The Omega paper introduces the concept of optimistic concurrency for scheduling decisions — understand why it outperforms pessimistic locking at scale
- Preemption is deceptively hard: you must handle the case where the preempted job cannot be rescheduled (no available node) — what do you do then?
- Fair-share requires tracking resource usage over a window, not just point-in-time — think about what "consumption" means for a job that ran for 30 seconds
- Your Plug router is pure Elixir; read the Plug documentation for `Plug.Router` and `Plug.Parsers` — do not reach for Phoenix

## Reference Material (Research Required)
- Hindman, B. et al. (2011). *Mesos: A Platform for Fine-Grained Resource Sharing in the Data Center* — study section 3 (architecture) and section 4 (two-level scheduling) deeply
- Schwarzkopf, M. et al. (2013). *Omega: Flexible, Scalable Schedulers for Large Compute Clusters* — focus on the shared-state scheduling model and conflict resolution
- Ghodsi, A. et al. (2011). *Dominant Resource Fairness: Fair Allocation of Multiple Resource Types* — the foundational paper on multi-resource fair sharing
- Kubernetes scheduler source code — `pkg/scheduler/` directory — study `framework.go`, `generic_scheduler.go`, and the filter/score plugin architecture

## Difficulty Rating
★★★★★★

## Estimated Time
4–6 weeks for an experienced Elixir developer with systems programming background
