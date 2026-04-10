# 46. Build a LiveView Clone
**Difficulty**: Insane

## Prerequisites
- Mastered: Phoenix channels and WebSocket lifecycle, GenServer and process state management, EEx/HEEx template compilation, OTP supervision trees, PubSub patterns, Plug and Cowboy internals, macro metaprogramming, binary protocol design
- Study first: "Programming Phoenix LiveView" (McCord & Loder), Phoenix.LiveView source on GitHub, Chris McCord's ElixirConf 2019 keynote ("LiveView: Interactive, Real-Time Apps — No Need to Write JavaScript"), "Programming Phoenix 1.4" (McCord, Tate, Valim)

## Problem Statement
Build a real-time UI framework — functionally equivalent to Phoenix LiveView — that renders server-side HTML, maintains stateful connections over WebSockets, and sends only DOM diffs to the client with a thin vanilla-JS client runtime.

1. Implement the WebSocket transport layer: connection handshake, join, heartbeat, and clean disconnect with process cleanup
2. Build the server-side component lifecycle: `mount/3` (called once on connect), `render/1` (pure function returning a template), and a stateful assigns map held in a GenServer per connection
3. Implement a structural DOM diffing algorithm that computes the minimal patch between two rendered trees and serializes it as a compact binary/JSON diff payload — never send full HTML on update
4. Wire client-side event bindings: `phx-click`, `phx-submit`, `phx-change`, `phx-keydown`, `phx-blur` — each must map to a corresponding `handle_event/3` callback on the server, update assigns, and trigger a re-render diff cycle
5. Support `handle_info/2` so that PubSub messages, timers, and arbitrary process messages can update the view without a client-initiated event
6. Implement navigation lifecycle with `handle_params/3`: handle live navigation (pushState URL changes) without full page reload, re-mounting only the changed component tree
7. Build LiveComponent support: stateful sub-components with their own `mount/1`, `update/2`, `handle_event/3`, and `render/1`, scoped assigns, and targeted updates so a parent re-render does not force a full child re-render
8. Create a compile-time template DSL (`~LV` sigil or similar) that: parses HTML at compile time, extracts dynamic expressions into a slot map, and generates a render function that produces a diffable AST — not a raw string
9. Implement `push_event/3` for server-to-client JS interop: the server pushes named events with a payload that the client-side runtime dispatches to registered JS hooks
10. Validate with a load benchmark: sustain 10,000 concurrent LiveView connections on a single BEAM node with total memory overhead under 10 MB above baseline (excluding application data), and deliver diff updates with p99 latency under 50 ms under uniform load

## Acceptance Criteria
- [ ] WebSocket connection lifecycle is fully managed: mount, join, heartbeat keepalive, graceful and abrupt disconnect — all transitions handled without process leaks
- [ ] Assigns are immutable snapshots per render cycle; mutations via `assign/3` or `assign_new/3` produce a new assigns map and schedule a re-render
- [ ] Diffing algorithm produces correct minimal patches for: text content changes, attribute changes, node insertions, node deletions, keyed list reordering — validated by a property-based test suite
- [ ] All five event bindings (`phx-click`, `phx-submit`, `phx-change`, `phx-keydown`, `phx-blur`) route correctly to `handle_event/3` and produce visible state changes
- [ ] `handle_info/2` triggers re-render when called from a PubSub broadcast or a `:timer.send_interval` — demonstrated with a live counter ticking every second
- [ ] `handle_params/3` is called on live navigation; the URL updates via pushState and only the affected component tree re-renders
- [ ] LiveComponent has fully isolated assigns; updating a child component does not trigger a parent re-render; parent can target a child update via `send_update/3`
- [ ] Template DSL parses at compile time; a syntax error in the template raises a `CompileError` with file and line number; runtime renders produce no string concatenation
- [ ] `push_event/3` delivers named events to client JS hooks registered on DOM nodes; demonstrated with a client-side chart or animation triggered by server data
- [ ] Benchmark: 10,000 connections sustained for 60 seconds, memory overhead < 10 MB, p99 diff delivery latency < 50 ms — results produced by a reproducible mix script

## What You Will Learn
- WebSocket protocol internals and stateful connection management at scale on the BEAM
- Structural tree diffing algorithms and minimal-patch serialization for low-bandwidth UI updates
- Compile-time template parsing with macros: separating static from dynamic parts to enable efficient diffing
- The Phoenix Channel protocol: topic/subtopic routing, join authorization, push/reply framing
- Process-per-connection architecture: memory profile, GC behavior, and supervision under 10k concurrent processes
- Server-driven UI patterns: the trade-offs between server state, client state, and optimistic updates
- JS interop design: how a thin client runtime can delegate all logic to the server with minimal JS surface area

## Hints (research topics, NO tutorials)
- Study how Phoenix.LiveView separates "static" from "dynamic" parts of a template at compile time to make diffs O(dynamic expressions), not O(HTML size)
- Look into the `morphdom` algorithm (DOM patching) and compare with the approach LiveView takes on the server side before any JS is involved
- Research how Phoenix Channels use a topic/event/payload framing over WebSocket and how `Phoenix.Socket` multiplexes multiple channels over one connection
- Investigate BEAM process memory: how much does a minimal GenServer cost? What is the overhead of a mailbox, a heap, and a stack at rest?
- Study `EEx.Engine` and how it compiles templates into function clauses — the HEEx engine extends this to track static/dynamic boundaries
- Look into property-based testing with `StreamData` for verifying the diffing algorithm against arbitrary tree mutations
- Research the `phx-key` keyed diffing strategy: why keyed lists require a different algorithm than naive index-based diffing

## Reference Material (papers/docs primarios)
- Phoenix.LiveView source: `https://github.com/phoenixframework/phoenix_live_view`
- Phoenix.Channel protocol specification in the Phoenix docs
- "Real-time web: WebSocket protocol" (RFC 6455)
- "Programming Phoenix LiveView" — McCord & Loder (Pragmatic Bookshelf)
- Chris McCord, "LiveView: Interactive, Real-Time Apps — No Need to Write JavaScript", ElixirConf EU 2019
- "Virtual DOM and diffing algorithm" — React team documentation on reconciliation
- Levenshtein distance and Myers diff algorithm papers (for list diffing foundations)
- BEAM VM memory model: Erlang/OTP documentation on process heap and GC

## Difficulty Rating ★★★★★★★
This exercise requires simultaneously mastering WebSocket protocol engineering, compile-time macro systems, structural diffing algorithms, and BEAM-scale process architecture. The 10k-connection benchmark under tight memory constraints eliminates naive implementations and forces deep understanding of the BEAM process model.

## Estimated Time
200–350 hours
