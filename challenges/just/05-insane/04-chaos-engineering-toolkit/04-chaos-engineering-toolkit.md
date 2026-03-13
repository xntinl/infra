# 42. Chaos Engineering Toolkit

<!--
difficulty: insane
concepts: [chaos-engineering, fault-injection, safety-guards, process-management, observability, recovery-automation]
tools: [just, bash, stress-ng, tc, kill, iptables]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Linux environment (or Docker container running Linux) for system-level fault
  injection tools
- Root/sudo access for network manipulation (`tc`, `iptables`) and process control
- `stress-ng` installed (or willingness to install it) for CPU/memory/disk stress
  testing
- Understanding of system reliability concepts: circuit breakers, blast radius,
  graceful degradation

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a controlled chaos engineering framework with safety interlocks that
  prevent experiments from causing unrecoverable damage
- **Evaluate** system resilience by designing experiments that expose specific failure
  modes and measuring recovery behavior

## The Challenge

Build a comprehensive chaos engineering toolkit — a miniature Chaos Monkey — as a
justfile. The toolkit must inject controlled failures into running systems, observe
the impact, and automatically recover when experiments complete or when safety limits
are breached. The key word is "controlled": every experiment must have defined
boundaries, automatic timeouts, and a kill-switch that immediately reverses all
injected faults.

The toolkit must support five categories of fault injection. First, process chaos:
randomly killing processes matching a pattern, or sending specific signals (SIGSTOP,
SIGCONT, SIGTERM) to simulate hangs and crashes. Second, CPU and memory stress:
consuming configurable percentages of CPU and RAM to simulate resource contention on
a busy host. Third, disk pressure: filling a target filesystem to a configurable
threshold or introducing I/O latency via underlying filesystem manipulation. Fourth,
network chaos: introducing packet latency, loss, corruption, or partitioning specific
ports using `tc` (traffic control) and `iptables`. Fifth, configuration corruption:
temporarily renaming or modifying config files to simulate misconfiguration, with the
original backed up for automatic recovery.

Safety is non-negotiable. Every experiment must declare a maximum duration (auto-abort
after timeout), a blast radius (which processes/ports/filesystems are in scope —
everything else is off-limits), and a rollback procedure. A global kill-switch recipe
must exist that, when invoked, immediately undoes ALL active experiments — removes `tc`
rules, restores iptables, kills stress processes, restores backed-up config files. The
toolkit must write a lock file when an experiment is active, and refuse to start a new
experiment if one is already running (unless explicitly forced).

The lock file is more than a flag — it is a manifest of the active experiment. It must
record: experiment ID, type, start time, maximum duration, PID of the chaos process (if
any), which resources are affected (interface names, file paths, process patterns), and
the exact commands needed to rollback. This allows the kill-switch to operate without
knowing the experiment's parameters in advance — it reads the lock file and executes
the recorded rollback commands.

Experiment tracking turns chaos into science. Every experiment must be logged to a
structured file (JSON Lines) with: experiment ID, type, parameters, start time, end
time, outcome (completed/aborted/killed), and any observations. A `report` recipe
should summarize past experiments, showing patterns in failures and recovery times. Over
time, this data reveals which failure modes your system handles well and which expose
weaknesses.

Consider also the human operator. Before injecting any fault, the toolkit should
display a clear summary of what will happen: "This experiment will introduce 200ms
latency with 50ms jitter and 5% packet loss on interface eth0 for 60 seconds. Affected
ports: all. Kill-switch: `just kill-switch`." The operator must confirm before the
experiment begins (unless running in non-interactive mode for automation).

## Requirements

1. Implement `chaos-process` recipe: kill or signal random processes matching a
   user-specified pattern, with configurable kill probability (e.g., 30% chance per
   matching process) and signal type (SIGTERM, SIGKILL, SIGSTOP)

2. Implement `chaos-cpu` recipe: consume a configurable percentage of CPU cores for a
   specified duration using `stress-ng` or equivalent, with automatic cleanup

3. Implement `chaos-memory` recipe: allocate a configurable amount of memory (in MB or
   as percentage of total) for a specified duration, with automatic release

4. Implement `chaos-disk` recipe: fill a specified directory with generated data up to
   a configurable threshold (e.g., 90% full), with automatic cleanup on completion or
   timeout

5. Implement `chaos-network` recipe: introduce configurable latency (ms), packet loss
   (%), jitter (ms), and corruption (%) on a specified network interface using
   `tc netem`

6. Implement `chaos-config` recipe: back up a specified config file, inject a specified
   corruption type (truncate, shuffle lines, inject invalid syntax, delete random
   lines), and schedule automatic restoration after a timeout

7. Implement a global `kill-switch` recipe that reads the experiment lock file and
   immediately reverses ALL active chaos — terminate stress processes, remove `tc`
   rules, flush iptables chaos rules, restore config files from backups

8. Enforce safety guards: maximum experiment duration (configurable, default 5 minutes),
   blast radius declarations in the lock file, and a lock file preventing concurrent
   experiments

9. Write experiment logs to `experiments/log.jsonl` with structured entries: experiment
   ID (timestamp-based), type, parameters, start time, end time, exit status, rollback
   status, and operator notes

10. Create a `report` recipe that summarizes the experiment log: total experiments run,
    breakdown by type, success/abort/kill rates, average durations, and most common
    failure modes observed

11. Implement `dry-run` mode that describes exactly what the experiment would do (which
    processes would be targeted, what network rules would be applied, which files would
    be modified) without actually injecting any fault

12. Create a `steady-state` recipe that captures baseline metrics (CPU usage, memory
    usage, disk free, network latency to a target, process count) before and after an
    experiment for comparison, outputting a before/after diff

## Hints

- `tc qdisc add dev eth0 root netem delay 200ms 50ms loss 5%` is the core command for
  network chaos — but remember to `tc qdisc del dev eth0 root` to clean up; store the
  interface name in the lock file so the kill-switch knows what to clean

- `trap` in bash is your friend for safety: `trap cleanup EXIT` ensures cleanup runs
  even if the recipe is interrupted with Ctrl+C — but remember that just runs each line
  in a separate shell by default unless you use a multi-line `sh` block

- For the lock file, store the PID of the chaos process so the kill-switch can target
  it specifically; `flock` can also prevent concurrent experiment starts at the
  filesystem level

- `stress-ng --cpu 4 --timeout 60s --metrics-brief` provides both stress injection and
  metrics output — capture the metrics for your experiment log

- Experiment IDs based on `date +%s%N` (nanosecond timestamp) are unique enough for
  local use and sort chronologically for the report

## Success Criteria

1. `just chaos-cpu percent=80 duration=30s` consumes approximately 80% CPU for 30
   seconds and then automatically cleans up, with the experiment logged

2. `just chaos-network iface=eth0 latency=200ms loss=5 duration=60s` introduces
   measurable network degradation that disappears after 60 seconds

3. `just kill-switch` immediately terminates all active experiments and restores the
   system to its pre-chaos state, regardless of which experiment types are running

4. Attempting to start a second experiment while one is active fails with a clear error
   message indicating the active experiment's ID, type, and remaining duration

5. `just chaos-config file=/etc/myapp/config.yaml corruption=truncate duration=30s`
   corrupts the file and restores it automatically after 30 seconds, with a backup
   verifiable at a known path

6. The experiment log contains structured entries for every experiment run, including
   those that were killed via kill-switch or aborted due to timeout

7. `DRY_RUN=1 just chaos-process pattern="myapp" probability=50` prints which
   processes would be targeted and the kill probability without sending any signals

8. `just report` produces a human-readable summary of all past experiments grouped by
   type with success/failure statistics

## Research Resources

- [Principles of Chaos Engineering](https://principlesofchaos.org/)
  -- foundational concepts for designing meaningful chaos experiments

- [tc-netem Manual](https://man7.org/linux/man-pages/man8/tc-netem.8.html)
  -- network emulation for latency, loss, jitter, and corruption

- [stress-ng Documentation](https://wiki.ubuntu.com/Kernel/Reference/stress-ng)
  -- system stress testing tool usage and options

- [Just Manual - Private Recipes](https://just.systems/man/en/chapter_38.html)
  -- hiding internal helper recipes from the user-facing recipe list

- [Just Manual - Error Handling](https://just.systems/man/en/chapter_49.html)
  -- controlling recipe behavior on command failure for safety-critical cleanup

- [Bash Trap Handling](https://www.gnu.org/software/bash/manual/bash.html#Signals)
  -- ensuring cleanup runs even on interrupt signals

## What's Next

Proceed to exercise 43, where you will build a multi-cloud orchestrator that manages
resources across AWS, GCP, and Azure from a single justfile.

## Summary

- **Chaos engineering** -- systematically injecting failures to discover system weaknesses before they manifest in production
- **Safety interlocks** -- designing kill-switches, blast radius limits, and automatic recovery to keep experiments controlled
- **Experiment tracking** -- logging and reporting on chaos experiments to turn random failures into actionable reliability data
