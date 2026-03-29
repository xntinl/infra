# 149. Chaos Engineering Fault Injector

```yaml
difficulty: insane
languages: [go]
time_estimate: 40-60 hours
tags: [chaos-engineering, fault-injection, resilience, distributed-systems, observability, experiment-lifecycle]
bloom_level: [create]
```

## Prerequisites

- Systems programming: Linux process management, network namespaces, cgroups, `tc` (traffic control), `iptables`
- Network fundamentals: TCP/UDP sockets, DNS resolution internals, packet loss simulation
- Go concurrency: goroutines, context cancellation, signal handling, graceful shutdown
- Distributed systems: health checks, steady-state metrics, failure modes, blast radius concepts
- CLI design: subcommands, flag parsing, structured output

## Learning Objectives

After completing this challenge you will be able to:

- **Create** a fault injection engine that introduces controlled network, process, disk, and DNS failures
- **Create** an experiment lifecycle manager that validates steady-state hypotheses before, during, and after fault injection
- **Create** blast radius controls that limit fault scope to specific targets, durations, and percentages of traffic
- **Create** automatic rollback mechanisms that restore system state on abort, timeout, or panic

## The Challenge

Build a chaos engineering toolkit from scratch. No Chaos Monkey, no LitmusChaos, no external fault injection frameworks. Your system defines experiments declaratively, validates that the target system is healthy (steady-state hypothesis), injects precise faults (network latency, packet loss, process kills, disk I/O errors, DNS failures), monitors the system during the experiment, verifies the hypothesis still holds (or records the deviation), and rolls back all injected faults on completion or abort.

This is the experiment lifecycle described in the Principles of Chaos Engineering, built as a composable CLI tool.

## Requirements

1. **Network fault injection**: Inject network latency (configurable delay with jitter using `tc netem`), packet loss (percentage-based), network partition (block traffic between specific IPs/ports using `iptables`), and bandwidth throttling. Target specific interfaces, ports, or IP ranges. All network faults must be fully reversible.

2. **Process fault injection**: Kill processes by name or PID (SIGKILL, SIGTERM, configurable signal). Pause and resume processes (SIGSTOP/SIGCONT). Inject CPU stress (spin goroutines consuming CPU). Inject memory pressure (allocate and hold memory). Target processes by name regex, PID, or container ID.

3. **Disk fault injection**: Inject I/O errors on specific mount points using FUSE overlay or `dm-error` device mapper targets. Simulate slow disk by adding latency to read/write syscalls. Fill disk to a target percentage. All disk faults must be reversible (unmount overlay, remove device mapper target, delete fill files).

4. **DNS fault injection**: Intercept DNS queries by running a local DNS proxy. Return NXDOMAIN for targeted domains, inject latency into DNS resolution, return incorrect IP addresses for specified domains. Redirect the system resolver to the proxy during the experiment and restore on completion.

5. **Experiment definition**: Define experiments in YAML. Each experiment specifies: a descriptive name, a target (host, process, container), one or more fault actions with parameters, duration, blast radius (percentage of requests or instances affected), and a steady-state hypothesis.

6. **Steady-state hypothesis**: Before injection, verify the target system is healthy by checking configurable health endpoints (HTTP status codes, response time thresholds, custom metric queries). During the experiment, continuously monitor these same checks. After rollback, verify the system returns to steady state within a configurable recovery timeout.

7. **Experiment lifecycle**: Execute experiments through phases: validate config, check steady-state, inject faults, monitor (with continuous hypothesis checks), rollback faults, verify recovery. Log each phase transition with timestamps. Support dry-run mode that validates config and checks steady-state without injecting faults.

8. **Rollback and safety**: Register cleanup handlers for every fault injected. On SIGINT, SIGTERM, panic, or experiment timeout, execute all rollback handlers in reverse order. Rollback must be idempotent (safe to call multiple times). Track rollback success/failure and report any faults that could not be cleaned up.

9. **CLI interface**: Subcommands: `run <experiment.yaml>`, `validate <experiment.yaml>`, `rollback <experiment-id>` (manual rollback of a stuck experiment), and `list-faults` (show available fault types and their parameters). Output structured JSON logs and a human-readable summary.

10. **Experiment journal**: Persist experiment results to a local journal (JSON files). Record: experiment ID, start/end times, fault parameters, steady-state results (before/during/after), any deviations detected, and rollback status. Support querying past experiments by ID.

## Hints

1. Build the rollback system first. Every fault injector should return a `Rollback` function on success. Store these in a stack (LIFO) and execute them all on any exit path. If rollback is solid, you can fearlessly experiment with injection methods.

2. For network faults without root access or `tc`, you can implement an application-level proxy that introduces delay and drops in the forwarding path. This is less realistic than `tc netem` but testable anywhere.

3. The experiment lifecycle is a state machine: `Init -> SteadyStateCheck -> Injecting -> Monitoring -> RollingBack -> Verifying -> Complete/Failed`. Model it explicitly, and every state transition should log and check abort conditions.

## Acceptance Criteria

- [ ] Network latency injection adds configurable delay (verified by measuring RTT before and after injection)
- [ ] Packet loss injection drops configurable percentage of packets (verified over 1000+ packets)
- [ ] Network partition blocks traffic between two specified endpoints; traffic resumes after rollback
- [ ] Process kill terminates target process; process pause freezes it and resume continues it
- [ ] CPU stress drives CPU usage above 90% on target cores; stress stops on rollback
- [ ] Disk fault injection causes I/O errors on target path; normal I/O resumes after rollback
- [ ] DNS injection returns NXDOMAIN for targeted domains; correct resolution resumes after rollback
- [ ] Steady-state hypothesis verified before injection; deviations during injection are recorded
- [ ] SIGINT during experiment triggers full rollback of all injected faults within 5 seconds
- [ ] Experiment journal records complete lifecycle with queryable results
- [ ] Dry-run mode validates config and checks steady-state without injecting any faults

## Resources

- [Principles of Chaos Engineering](https://principlesofchaos.org/) - Foundational principles and experiment methodology
- [Rosenthal et al.: "Chaos Engineering" (O'Reilly, 2020)](https://www.oreilly.com/library/view/chaos-engineering/9781492043850/) - Comprehensive guide to chaos practices
- [Netflix Chaos Monkey](https://netflix.github.io/chaosmonkey/) - Original chaos engineering tool
- [Linux `tc netem`](https://man7.org/linux/man-pages/man8/tc-netem.8.html) - Network emulation for testing
- [LitmusChaos Architecture](https://litmuschaos.io/) - Cloud-native chaos engineering framework
- [Basiri et al.: "Automating Chaos Experiments in Production" (ICSE 2019)](https://arxiv.org/abs/1905.04648) - Netflix chaos automation at scale
