# 50. GitOps Reconciliation Engine

<!--
difficulty: insane
concepts: [gitops, drift-detection, reconciliation-loops, approval-gates, audit-logging, rollback]
tools: [just, git, bash, jq, kubectl, terraform]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Git installed with a repository to use as the desired-state source
- Either `kubectl` (with a Kubernetes cluster, minikube, or kind) or `terraform` (with
  any provider) for actual reconciliation — the system should support at least one
- `jq` for JSON state processing
- Understanding of GitOps principles: Git as single source of truth, declarative desired
  state, automated reconciliation, immutable audit trail

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a poll-based GitOps reconciliation engine that detects drift between desired
  state in Git and actual running infrastructure
- **Justify** design decisions around approval gates, audit trails, and rollback
  mechanisms in automated infrastructure management systems

## The Challenge

Build a GitOps reconciliation engine — the core loop that powers tools like ArgoCD and
Flux, but implemented entirely with just recipes and shell scripting. The engine watches
a Git repository (via polling), detects when the desired state (manifests or configs in
Git) diverges from the actual state (what is actually running), computes the diff,
optionally requires approval for production changes, applies the changes, and logs
everything to an immutable audit trail.

The reconciliation loop is the heart of the system. On each tick (configurable interval,
e.g., every 60 seconds), the engine pulls the latest commit from the watched repository,
compares it against the last-reconciled commit hash (stored in a state file), and if
they differ, triggers a reconciliation cycle. The cycle computes what changed (which
files were modified, added, or deleted between the two commits), maps those changes to
infrastructure operations (apply modified manifests, delete resources for removed
manifests), and executes them. This loop must be robust: it must handle Git fetch
failures gracefully (retry on next tick), apply failures (log and continue), partial
applies (record which resources succeeded and which failed), and its own crash-and-
restart (picking up where it left off by reading the state file).

Drift detection operates independently of Git changes. Even if no new commits have
arrived, the actual state of the infrastructure may have drifted from the desired state.
Someone may have manually edited a Kubernetes resource, or Terraform state may have
drifted due to external changes. A periodic drift-detection check must compare the live
infrastructure against the desired state in Git and report discrepancies — which
resource, which attribute, desired vs actual value. Drift can be resolved automatically
(revert to desired state) or flagged for manual review, depending on a configuration
setting per path or resource type.

Approval gates add safety for production environments. Changes affecting files in
certain configurable path patterns (e.g., `production/**`, `*.critical.yaml`) must not
be automatically applied. Instead, they are queued in a pending-approvals file with the
change details: which files changed, what the diff looks like, which commit introduced
the change, and when it was detected. An operator must run an explicit approval recipe
to unblock the change. Rejection is also possible, with a required reason that is logged.
This mimics the pull-request-like approval workflow that production GitOps systems
provide.

The audit log is immutable and comprehensive. Every action the engine takes — fetching
from Git, detecting changes, computing diffs, requesting approval, applying changes,
detecting drift, reverting drift, rolling back — must be logged with timestamps, commit
hashes, resource names, operator identity (if applicable), and outcomes (success,
failure, skipped). The audit log must be append-only: the engine must never modify or
truncate it. This log is the forensic trail for answering "who changed what, when, and
why" — a critical compliance requirement in production environments.

Rollback must be possible to any previously reconciled commit. The engine tracks the
reconciliation history (commit hash, timestamp, success/failure, list of applied
resources). A rollback recipe takes a target commit hash, verifies it was previously
reconciled successfully (you should only roll back to known-good states), checks out
that commit's desired state, and applies it — effectively reverting the infrastructure
to a known-good state. The rollback itself is logged as a reconciliation event in the
audit trail.

## Requirements

1. Implement a `watch` recipe that polls a configured Git repository at a configurable
   interval (default 60 seconds), detects new commits on the watched branch, and
   triggers reconciliation when changes are detected — with visible logging of each
   poll cycle

2. Store reconciliation state in `state.json`: last-reconciled commit hash, timestamp,
   status (success/failure/partial), list of applied resources with per-resource status,
   and last drift-check timestamp

3. Implement `reconcile` recipe that computes the diff between last-reconciled commit
   and current HEAD, identifies changed manifest files (by extension: `.yaml`, `.yml`,
   `.tf`, `.json`), and applies them using `kubectl apply` and/or `terraform apply`

4. Implement `drift-detect` recipe that compares desired state (manifests in Git)
   against actual state (live infrastructure via `kubectl get` or `terraform plan`) and
   reports specific discrepancies: resource name, attribute, desired value, actual value

5. Implement configurable drift resolution: `auto` mode automatically reconciles drift
   back to desired state, `report` mode only logs the drift without making changes —
   controlled via a configuration variable, optionally per-path

6. Implement approval gates: changes to configurable path patterns (e.g.,
   `production/**`) are not auto-applied but queued in `pending-approvals.json` with
   change details, diff preview, and originating commit hash

7. Implement `approve` recipe that takes a pending change ID, marks it as approved
   (recording the approver identity and timestamp), and triggers its immediate
   application

8. Implement `reject` recipe that removes a pending change from the queue with a
   required reason parameter, logged to the audit trail

9. Maintain an append-only audit log (`audit.log` in JSON Lines format) recording every
   engine action: type (fetch, diff, apply, drift-detect, approve, reject, rollback),
   timestamp, commit hash, resource names, operator identity, outcome, and error
   details if applicable

10. Implement `rollback` recipe that takes a target commit hash, verifies it exists in
    the reconciliation history as a successful reconciliation, checks out that commit's
    manifests, and applies them as a new reconciliation event

11. Implement `history` recipe showing the reconciliation timeline: commit hashes,
    timestamps, status, number of resources applied, pending approvals resolved, and
    operator names for approved changes

12. Implement `status` recipe displaying: current reconciled commit (short hash and
    subject), pending approvals count with age of oldest, last drift check result and
    timestamp, time since last reconciliation, and any resources currently in error
    state

## Hints

- `git rev-parse HEAD` in the watched repo gives the current commit; comparing it
  against the stored last-reconciled commit tells you if reconciliation is needed —
  store this in `state.json` and read it with `jq -r '.last_commit' state.json`

- `git diff --name-only $last_commit..$current_commit` gives you exactly which files
  changed between two commits — filter by file extension (`.yaml`, `.tf`) to find
  infrastructure manifests that need to be applied

- For Kubernetes drift detection, `kubectl diff -f manifest.yaml` (Kubernetes 1.18+)
  shows differences between the manifest and the live resource — exit code 1 means
  drift exists, exit code 0 means no drift

- For Terraform drift detection, `terraform plan -detailed-exitcode` returns exit code
  2 if changes are detected, 0 if no changes, and 1 on error — this is your drift
  signal without needing to parse output

- The watch loop can be implemented as `while true; do just reconcile; sleep $interval; done`
  — but handle SIGTERM gracefully to update state before exiting, using `trap` to
  catch the signal

- For approval gates, match changed file paths against configurable glob patterns:
  `[[ "$file" == production/* ]]` determines if a change requires approval — store the
  patterns in a config file for easy modification

## Success Criteria

1. `just watch` polls the repository and automatically triggers reconciliation when new
   commits are detected, logging each poll cycle with timestamp and commit comparison

2. After successful reconciliation, `state.json` contains the correct commit hash,
   timestamp, and list of applied resources with their individual statuses

3. `just drift-detect` correctly identifies resources whose live state differs from the
   desired state in Git, reporting specific resource names and attribute differences

4. Changes to files matching approval-gate path patterns are queued rather than
   auto-applied, and appear in `just status` output as pending approvals with age

5. `just approve id=<change-id>` unblocks a pending change, applies it immediately, and
   records the approval in the audit log with the approver's identity and timestamp

6. The audit log contains a complete, chronological record of all engine actions, and is
   never truncated or modified by the engine — only appended to

7. `just rollback commit=<hash>` successfully reverts infrastructure to the state of a
   previously reconciled commit, records the rollback as a new entry in history, and
   updates the current reconciled commit in state.json

8. `just history` displays the full reconciliation timeline in a readable format showing
   commit hashes, timestamps, resource counts, and outcomes for each reconciliation

## Research Resources

- [GitOps Principles](https://opengitops.dev/)
  -- the four foundational principles your engine must implement

- [ArgoCD Architecture](https://argo-cd.readthedocs.io/en/stable/operator-manual/architecture/)
  -- how a production GitOps tool structures its reconciliation loop and state management

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- multi-line shell blocks for the reconciliation loop and complex apply logic

- [Kubernetes kubectl diff](https://kubernetes.io/docs/reference/generated/kubectl/kubectl-commands#diff)
  -- detecting drift between desired and actual Kubernetes resource state

- [Terraform Plan Exit Codes](https://developer.hashicorp.com/terraform/cli/commands/plan#detailed-exitcode)
  -- using detailed exit codes for programmatic drift detection

- [Just Manual - Dotenv Integration](https://just.systems/man/en/chapter_41.html)
  -- loading configuration (watch interval, approval paths, repo URL) from dotenv files

## What's Next

Congratulations on completing all 50 exercises. You have progressed from basic just
syntax through advanced orchestration to building production-grade automation systems.
You now have the skills to use just as the backbone for sophisticated DevOps tooling
in any project.

## Summary

- **GitOps reconciliation** -- implementing the core detect-diff-apply loop that synchronizes Git-declared desired state with live infrastructure
- **Drift detection** -- comparing live infrastructure against declared desired state to identify unauthorized or accidental changes
- **Approval gates and audit trails** -- adding human oversight for sensitive environments with complete, immutable logging of all actions
