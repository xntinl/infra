# 40. Distributed SSH Orchestration

<!--
difficulty: insane
concepts: [distributed-execution, ssh, parallel-processing, failure-handling, rolling-updates, inventory-management]
tools: [just, ssh, bash, xargs, tee]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- SSH client installed and configured with key-based authentication
- Access to at least 2 remote machines (local VMs, Docker containers with SSH, or
  cloud instances)
- Familiarity with SSH connection multiplexing and non-interactive remote execution

## Learning Objectives

After completing this challenge, you will be able to:

- **Architect** a distributed task execution framework built entirely on just recipes
  and SSH
- **Evaluate** failure-handling strategies for partial failures in distributed systems
  where some nodes succeed and others fail

## The Challenge

Create a justfile that serves as a lightweight orchestration tool — a minimal Ansible
or pdsh built on just recipes and SSH. The system must execute arbitrary commands and
recipes across a fleet of remote machines, handle the inherent complexity of
distributed failure modes, and provide clear visibility into what happened on every
node.

The node inventory is the foundation. Your system must support loading a list of target
hosts from an inventory file (one host per line, supporting `user@host:port` format
with optional labels) and from an environment variable override
(`NODES="host1,host2,host3"`). Inventory entries should support comments and grouping
— nodes can belong to named groups (e.g., `[web]`, `[db]`, `[cache]`) and recipes
should accept a group filter parameter to target subsets of the fleet.

Parallel execution is critical. Running a command on 20 nodes sequentially is
unacceptable — your system must execute across all targeted nodes concurrently, collect
stdout and stderr from each into separate per-node log files, and present a summary
once all nodes have reported back. The summary must clearly indicate which nodes
succeeded, which failed (with exit codes), and how long each took. Consider using
background processes, `xargs -P`, or GNU parallel if available.

The hardest requirement is rolling updates. Implement a recipe that updates nodes in
configurable batch sizes (e.g., 2 at a time out of 10), waits for each batch to
complete and health-check before proceeding to the next batch, and automatically halts
the rollout if more than a configurable threshold of nodes fail in a single batch. This
requires careful orchestration of parallel-within-sequential execution — each batch runs
its nodes in parallel, but batches themselves are sequential with a gate between them.

Dry-run mode must show exactly which commands would run on which nodes without
executing anything. This is essential for production safety — operators must be able to
preview the blast radius before committing. The dry-run output should be detailed
enough that an operator can review exactly what will happen on each node.

Error handling must be granular. A failure on one node must not prevent execution on
other nodes. After all nodes report, the summary must distinguish between SSH
connection failures (node unreachable), command execution failures (command ran but
returned non-zero), and timeouts (command did not complete within the configured
deadline). Each failure type requires different remediation, so they must be clearly
differentiated.

## Requirements

1. Implement inventory file parsing supporting `user@host:port` format, `# comments`,
   blank lines, and `[group-name]` section headers

2. Support environment variable override: `NODES="host1,host2"` takes precedence over
   the inventory file when set

3. Create a `run` recipe that executes an arbitrary shell command on all targeted nodes
   in parallel, with a configurable timeout per node

4. Create a `run-script` recipe that copies a local script to remote nodes via `scp`,
   executes it, and cleans up the temporary file — even if execution fails

5. Collect stdout and stderr from each node into
   `logs/{timestamp}/{hostname}.stdout` and `logs/{timestamp}/{hostname}.stderr`

6. Display a post-execution summary table showing: hostname, exit code, duration, and
   first line of stderr (if failed) — aligned in columns for readability

7. Implement `rolling-update` recipe with configurable `batch_size` (default 2) and
   `failure_threshold` (default 1) — halt rollout if failures in a batch exceed the
   threshold

8. Implement health checking between rolling update batches: after each batch
   completes, run a configurable health-check command on the updated nodes before
   proceeding to the next batch

9. Implement `dry-run` mode (triggered via `DRY_RUN=1` variable) that prints all
   commands and target nodes without executing anything, formatted identically to
   the real execution summary

10. Support group filtering: `just run group=web cmd="systemctl status nginx"` targets
    only nodes in the `[web]` group

11. Implement SSH connection reuse via ControlMaster to avoid repeated authentication
    handshakes across multiple recipe invocations within a session

12. Create a `ping` recipe that verifies SSH connectivity to all nodes and reports
    which are reachable (with latency) and which are not (with error reason)

## Hints

- SSH's `-o ControlMaster=auto -o ControlPath=/tmp/ssh-%r@%h:%p -o ControlPersist=60`
  options let you reuse connections across multiple invocations, dramatically reducing
  overhead for repeated commands

- `wait` in bash collects exit codes from background processes — this is key to
  parallel execution with per-node status tracking; use `wait $pid; echo $?` to get
  per-process exit codes

- For rolling updates, think of it as an outer sequential loop over batches with an
  inner parallel execution within each batch — the challenge is wiring the exit code
  aggregation between these layers

- `just --justfile /dev/stdin` can parse a justfile from stdin — useful if you want to
  validate generated remote scripts before sending them

- Consider using `mktemp` for per-invocation log directories to avoid collision when
  running multiple operations simultaneously

## Success Criteria

1. `just ping` reports connectivity status for all nodes in the inventory, clearly
   distinguishing reachable from unreachable hosts with error reasons

2. `just run cmd="hostname"` executes on all nodes in parallel and displays a summary
   table with each node's output and exit status within seconds (not sequentially)

3. `just run group=db cmd="pg_isready"` executes only on nodes in the `[db]` inventory
   group, ignoring all other groups

4. After execution, per-node log files exist in the `logs/` directory with correct
   stdout and stderr separation — stderr files are empty for successful nodes

5. `just rolling-update batch_size=2 cmd="apt-get update"` processes nodes 2 at a
   time, pausing between batches for health checks, with visible batch progress output

6. Rolling update halts automatically when failures exceed `failure_threshold` in any
   batch, reporting which batch number caused the halt and which nodes failed

7. `DRY_RUN=1 just run cmd="rm -rf /important"` prints the full execution plan without
   executing any remote commands — verifiable by checking no SSH connections were made

8. The summary table after any execution shows hostname, exit code, duration, and error
   snippets for failed nodes, with columns properly aligned

## Research Resources

- [Just Manual - Environment Variables](https://just.systems/man/en/chapter_40.html)
  -- overriding behavior via env vars for dry-run and inventory source

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- writing multi-line shell blocks for complex orchestration logic

- [OpenSSH ControlMaster](https://man.openbsd.org/ssh_config#ControlMaster)
  -- SSH connection multiplexing for performance optimization

- [GNU Parallel Tutorial](https://www.gnu.org/software/parallel/parallel_tutorial.html)
  -- parallel remote execution patterns and result collection

- [Just Manual - Conditional Expressions](https://just.systems/man/en/chapter_32.html)
  -- branching on dry-run mode and group filters

## What's Next

Proceed to exercise 41, where you will design a domain-specific language using just as
the execution engine for infrastructure management.

## Summary

- **Distributed execution** -- running commands across multiple remote machines concurrently with result aggregation
- **Failure handling** -- managing partial failures where some nodes succeed and others fail, with per-node error classification
- **Rolling updates** -- batched execution with health-check gates and automatic rollback triggers
