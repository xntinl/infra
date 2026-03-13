# 46. Incident Response Automation

<!--
difficulty: insane
concepts: [incident-response, runbooks, diagnostics-collection, state-machines, notification, post-mortem-generation]
tools: [just, bash, curl, jq, systemctl, journalctl, ss, df]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Linux environment with `systemctl`, `journalctl`, `ss`, `df`, `free`, `top` available
  (or Docker container simulating a production server)
- `curl` for webhook notifications
- Understanding of incident management practices: severity levels, escalation,
  timelines, post-mortems

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** an automated incident response framework that gathers diagnostics, executes
  runbooks, and manages incident lifecycle state
- **Design** a post-mortem generation system that transforms collected incident data into
  structured analysis documents

## The Challenge

Automate the critical first 15 minutes of a production incident. When something breaks
at 3 AM, the on-call engineer's brain is foggy, adrenaline is high, and mistakes are
easy. Your justfile becomes the calm, methodical responder: it gathers diagnostics,
executes predefined runbook steps, tracks the incident timeline, sends notifications,
and when the dust settles, generates a post-mortem template pre-populated with
everything that was collected and done.

The incident lifecycle is a state machine: `open` -> `investigating` -> `identified` ->
`mitigated` -> `resolved`. Each state transition must be explicit (via a recipe),
timestamped, and logged. Moving from `investigating` to `identified` requires a
description of the root cause hypothesis. Moving from `identified` to `mitigated`
requires a description of the mitigation applied. The system must reject invalid
transitions (you cannot go from `open` to `resolved` without passing through
intermediate states) and must track who triggered each transition (via a `RESPONDER`
environment variable or parameter).

Diagnostics collection is the first action when an incident opens. Your system must
automatically gather: system resource usage (CPU, memory, disk, open file descriptors),
network state (listening ports, established connections, connection counts by state),
recent logs (last 500 lines from configurable log sources), recent deployments (check
git log in deployment directories, or a deployment log file), running processes sorted
by resource consumption, and DNS resolution checks for critical endpoints. All
diagnostics must be saved to an incident-specific directory:
`incidents/{incident-id}/diagnostics/`.

Each diagnostic collector must be independently resilient. If the DNS check fails
because `dig` is not installed, the other diagnostics must still be collected. A
diagnostic that errors out should save the error message to its output file rather than
crashing the entire collection pipeline.

Runbook execution adds structure to the chaos. Define runbooks as YAML files mapping
symptom categories to ordered remediation steps. A runbook for "high memory usage" might
include: identify top memory consumers, check for known memory leak processes, restart
the offending service, verify memory drops after restart. The justfile reads the
runbook, presents each step to the operator, executes it upon confirmation, logs the
output, and moves to the next step. If a step fails, the operator can retry, skip, or
abort the runbook.

Notifications must fire at key moments: incident opened, severity changed, state
transitions, and resolution. Support webhook-based notifications (Slack-compatible JSON
payloads sent via `curl`) with a configurable webhook URL. If no webhook is configured,
fall back to writing notifications to a local file. Notification content should be
context-rich: include the incident ID, current severity, state, time elapsed, and a
summary of what has been done so far.

The post-mortem generator is the capstone. After an incident is resolved, a recipe reads
all collected data — diagnostics, timeline, runbook execution logs, state transitions,
notifications sent — and assembles a structured Markdown document. The document should
have pre-populated sections for timeline of events, diagnostics summary, root cause
analysis, mitigation actions, impact assessment, and empty sections for "lessons
learned" and "action items" that the team fills in during the post-mortem review.

## Requirements

1. Implement `incident-open` recipe that creates an incident with a unique ID
   (timestamp-based), severity level (P1-P4), title, and initial description; creates
   the incident directory structure and triggers automatic diagnostics

2. Implement the state machine: recipes for `investigate`, `identify cause="..."`,
   `mitigate action="..."`, and `resolve summary="..."` — each validates the current
   state before transitioning and rejects invalid transitions with a clear message

3. Store incident state in `incidents/{id}/state.json` with full history: every state
   transition with timestamp, responder name, and associated notes or descriptions

4. Implement `gather-diagnostics` recipe that collects: CPU/memory/disk usage, network
   connections by state, recent logs (configurable sources), process list sorted by
   resource usage, DNS checks, and open file descriptor count — saving each to separate
   files in the incident's diagnostics directory

5. Implement automatic diagnostics: opening an incident automatically triggers full
   diagnostics collection without requiring a separate command

6. Implement runbook support: read YAML runbook files from a `runbooks/` directory,
   present steps to the operator with descriptions, execute each step upon confirmation,
   and log step output and duration to the incident timeline

7. Log every action taken during the incident to `incidents/{id}/timeline.jsonl`:
   timestamp, action type, description, output summary, responder, and duration

8. Implement `notify` helper that sends webhook notifications (Slack-compatible JSON)
   via `curl` for key events; fall back to appending to
   `incidents/{id}/notifications.log` if no webhook URL is configured

9. Implement `set-severity` recipe to escalate or de-escalate incident severity,
   triggering appropriate notifications (P1 escalation sends urgent notification with
   alert formatting, P4 sends informational)

10. Implement `generate-postmortem` recipe that produces a Markdown post-mortem document
    from all collected data: incident summary, timeline table, diagnostics overview,
    actions taken, state transitions, root cause, mitigation, resolution, and template
    sections for lessons learned and action items

11. Implement `list-incidents` recipe showing all incidents with their current state,
    severity, age, title, and responder in a formatted table

12. Implement `reopen` recipe that transitions a resolved incident back to investigating
    state, with a required reason parameter, for cases where the issue recurs — logging
    the reopen event and notifying

## Hints

- A JSON file with an array of state transitions is easier to work with than a flat
  log; `jq` can query the current state with
  `jq '.transitions[-1].state' state.json`

- For the state machine validation, define valid transitions as a lookup table:
  `open->investigating`, `investigating->identified`, `identified->mitigated`,
  `mitigated->resolved` — check the current state against allowed next states before
  proceeding

- `ss -tunap` gives comprehensive network connection information; `df -h` for disk;
  `free -m` for memory; `ps aux --sort=-%mem | head -20` for top memory consumers —
  wrap each in its own function for clean, independent diagnostics collection

- Slack webhook payloads are simple JSON:
  `{"text": "Incident #123 opened: API latency spike", "username": "incident-bot"}` —
  `curl -X POST -H 'Content-Type: application/json' -d @payload.json $WEBHOOK_URL`

- For the post-mortem template, read the timeline JSONL and format each entry as a
  Markdown table row — with good incident data, the post-mortem practically writes itself

## Success Criteria

1. `just incident-open severity=P1 title="API latency spike"` creates an incident
   directory, assigns an ID, sets initial state to `open`, and triggers diagnostics
   collection automatically

2. State transitions enforce ordering: `just resolve` on an incident in `open` state
   fails with an error explaining the required state progression path

3. `incidents/{id}/diagnostics/` contains separate files for CPU, memory, disk,
   network, processes, DNS checks, and log snapshots after diagnostics collection

4. Running a runbook pauses between steps for operator confirmation, logging each
   step's output and duration to the timeline

5. `just notify` sends a properly formatted JSON payload to the configured webhook URL
   (verifiable by pointing to a request capture service like webhook.site)

6. `just generate-postmortem id=<incident-id>` produces a well-structured Markdown
   document with pre-populated timeline, diagnostics summary, and resolution details

7. `just list-incidents` displays all incidents with their current state, severity, age,
   and title in a clean formatted table

8. The complete incident lifecycle — from open through investigate, identify, mitigate,
   resolve, and post-mortem generation — works end-to-end with all data consistently
   tracked across all files

## Research Resources

- [PagerDuty Incident Response Documentation](https://response.pagerduty.com/)
  -- industry best practices for incident management workflows and roles

- [Just Manual - Recipe Parameters](https://just.systems/man/en/chapter_37.html)
  -- accepting incident details as recipe parameters with defaults

- [Slack Incoming Webhooks](https://api.slack.com/messaging/webhooks)
  -- webhook notification payload format and rich message formatting

- [Just Manual - Conditional Expressions](https://just.systems/man/en/chapter_32.html)
  -- state machine transition validation and feature detection

- [jq Manual - Update Expressions](https://jqlang.github.io/jq/manual/#update)
  -- updating JSON state files with new transitions non-destructively

- [Google SRE Book - Postmortem Culture](https://sre.google/sre-book/postmortem-culture/)
  -- post-mortem structure, content expectations, and blameless culture

## What's Next

Proceed to exercise 47, where you will build a polyrepo orchestrator that manages
operations across multiple independent Git repositories.

## Summary

- **Incident lifecycle** -- implementing a state machine that enforces valid transitions through investigation and resolution
- **Automated diagnostics** -- collecting comprehensive system state data in the critical first minutes of an incident
- **Post-mortem generation** -- transforming collected incident data into structured analysis documents automatically
