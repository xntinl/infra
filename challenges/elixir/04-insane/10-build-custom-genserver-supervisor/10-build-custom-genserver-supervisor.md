# 10. Build a Custom GenServer and Supervisor

**Difficulty**: Insane

## Prerequisites

- Mastered: Elixir processes, `spawn/1`, `send/2`, `receive`, process linking, monitors
- Mastered: OTP concepts (GenServer, Supervisor) as a user — not as an implementor
- Mastered: Pattern matching, tail recursion, message passing protocols
- Familiarity with: Erlang `:gen_server` source code and OTP design principles

## Problem Statement

Implement a fully functional GenServer and Supervisor from scratch in Elixir, using only
raw `spawn`, `send`, `receive`, `Process.link/1`, and `Process.monitor/2`. You may NOT
use `:gen_server`, `GenServer`, `Supervisor`, `Agent`, `Task`, or any OTP behaviour.

Build `MyGenServer` and `MyGenServer.Supervisor` that replicate the essential behaviour of
their OTP counterparts:

1. `MyGenServer` must support synchronous `call/2` (blocking until reply), asynchronous
   `cast/2` (fire and forget), and arbitrary info messages via `handle_info/2`.
2. The call protocol must guarantee that the caller blocks until the server sends back a
   response tagged with the original reference, preventing stale message delivery.
3. `MyGenServer.Supervisor` must accept a list of child specs and start each child,
   linking to it, and restart it automatically when it crashes.
4. Implement `:one_for_one` restart strategy: only the crashed child is restarted.
5. Implement `:one_for_all` restart strategy: when any child crashes, all children are
   terminated and restarted in order.
6. Implement `:rest_for_one` restart strategy: the crashed child and all children that
   were started after it are terminated and restarted in order.
7. Enforce `max_restarts` and `max_seconds`: if more than `max_restarts` restarts occur
   within `max_seconds`, the supervisor itself crashes with reason `:shutdown`.
8. Child specs must support `restart: :permanent` (always restart), `:transient` (restart
   only on abnormal exit), and `:temporary` (never restart).
9. The supervisor must detect child death via monitors (not links) so that a child crash
   does not automatically kill the supervisor before the restart logic runs.
10. Process linking from the user's perspective must still work: `MyGenServer.start_link/2`
    must link the calling process to the server process.

## Acceptance Criteria

- [ ] `MyGenServer.start_link(module, args)` spawns a process running `module.init(args)`,
      links it to the caller, and returns `{:ok, pid}`.
- [ ] `MyGenServer.call(pid, message, timeout \\ 5000)` blocks the caller and returns the
      value from `handle_call/3`, raising on timeout.
- [ ] `MyGenServer.cast(pid, message)` sends asynchronously and returns `:ok` immediately.
- [ ] Stale replies (late responses to a timed-out call) are silently discarded by the caller.
- [ ] `MyGenServer.Supervisor.start_link(children, opts)` starts all children in order and
      monitors each one.
- [ ] `:one_for_one`: crashing child B leaves children A and C untouched; B is restarted.
- [ ] `:one_for_all`: crashing child B causes A, B, and C to all be terminated (in reverse
      start order) and restarted (in forward start order).
- [ ] `:rest_for_one`: crashing child B causes B and C to be terminated and restarted; A is
      unaffected.
- [ ] Supervisor exits with `{:shutdown, :max_restarts_exceeded}` when the restart intensity
      limit is breached.
- [ ] `restart: :temporary` children are never restarted regardless of exit reason.
- [ ] `restart: :transient` children are restarted on `{:EXIT, pid, reason}` where reason
      is not `:normal` and not `:shutdown`.

## What You Will Learn

- The exact message-passing protocol that underpins `:gen_server` `call` and `cast`
- How OTP supervisors use monitors rather than links to decouple crash detection from crash propagation
- The implementation details behind restart intensity tracking (sliding window algorithm)
- How process dictionaries and tail-recursive receive loops replace mutable state in Erlang/Elixir servers
- The subtle ordering guarantees required by `:one_for_all` and `:rest_for_one` to avoid race conditions
- Why OTP's design choices (separate monitor + link, tagged references, selective receive) exist

## Hints

This exercise is intentionally sparse. Research:

- The `{:"$gen_call", {pid, ref}, message}` protocol used by `:gen_server` — inspect it with `:sys.get_status/1`
- How `Process.monitor/1` returns a reference that appears in `{:DOWN, ref, :process, pid, reason}` messages
- Restart intensity: OTP uses a sliding window — store a list of restart timestamps and count those within the window
- The difference between `Process.exit(pid, :kill)` (untrappable) and `Process.exit(pid, :shutdown)` (trappable) when stopping children
- `Process.flag(:trap_exit, true)` converts exit signals into messages — consider whether your supervisor needs this

## Reference Material

- Erlang/OTP source: `lib/stdlib/src/gen_server.erl` and `lib/stdlib/src/supervisor.erl`
- "Designing for Scalability with Erlang/OTP" — Cesarini & Vinoski, Chapters 4–6
- OTP Design Principles — Process Supervision section: https://www.erlang.org/doc/design_principles/sup_princ
- "The Zen of Erlang" — Fred Hebert: https://ferd.ca/the-zen-of-erlang.html

## Difficulty Rating

★★★★★★

## Estimated Time

35–50 hours
