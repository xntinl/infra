# 13. Build an Alternative Actor Framework

**Difficulty**: Insane

## Prerequisites

- Mastered: GenServer, Supervisor trees, process messaging, hot code reloading (`:code.load_binary`)
- Mastered: Metaprogramming with macros (`defmacro`, `__using__`, `Module.register_attribute`)
- Familiarity with: Akka actor model, Erlang `:sys` protocol, Hyrum's Law of APIs

## Problem Statement

Design and implement an actor framework for Elixir with a deliberately different API from
OTP's GenServer. The framework must provide equivalent reliability guarantees to OTP while
exposing a programming model closer to typed actors (as in Akka Typed or Proto.Actor):

1. Define an `Actor` behaviour with a declarative message-dispatch macro. Rather than a
   single `handle_call/handle_cast`, actors declare typed message handlers using a DSL.
2. Each actor has a declared message protocol: a set of message types it can receive.
   Sending a message of an undeclared type raises a compile-time or runtime error.
3. Build a supervision tree for actors with restart strategies equivalent to OTP:
   `:one_for_one`, `:one_for_all`, and `:rest_for_one`.
4. Implement location transparency: the framework provides an `ActorRef` abstraction.
   Sending a message to an `ActorRef` works whether the actor is local or on a remote node,
   without the sender knowing which node hosts the actor.
5. Implement hot state update: the framework allows updating the message-handling logic
   of a running actor without restarting it and without losing accumulated state.
6. Benchmark your implementation against native GenServer across three workloads: pure
   message throughput, mixed call/cast, and supervised crash recovery throughput.

## Acceptance Criteria

- [ ] An actor module declares its message protocol with a compile-time macro:
      `receive_message CreateUser, do: ...` and `receive_message DeleteUser, do: ...`.
- [ ] Sending an undeclared message type raises `Actor.UnknownMessageError` at runtime
      with the actor's declared types listed in the error message.
- [ ] `ActorRef.send(ref, message)` delivers the message regardless of whether the actor
      is local (same BEAM node) or remote (connected cluster node).
- [ ] `ActorRef.call(ref, message, timeout)` blocks and returns a reply, with the same
      semantics as `GenServer.call/3`.
- [ ] The supervision tree restarts crashed actors; all three restart strategies produce
      the same observable behavior as OTP equivalents under test.
- [ ] `Actor.update_behavior(ref, new_module)` swaps the actor's dispatch module at
      runtime; subsequent messages are handled by the new module; state is preserved.
- [ ] Hot update does not drop any in-flight messages or replies.
- [ ] Benchmark results are documented: throughput is within 30% of native GenServer for
      pure cast workloads and within 20% for call workloads.
- [ ] The framework compiles with `mix compile` with zero warnings under `--warnings-as-errors`.

## What You Will Learn

- How to design a macro-based DSL that enforces structural constraints at compile time
- The actor identity vs. location problem and how `ActorRef` abstractions decouple them
- How Erlang's `:sys` protocol provides the underpinning for hot code upgrades in OTP
- The performance cost of abstraction layers over raw process messaging
- Type-dispatch patterns in dynamically typed languages: tagged tuples, protocol dispatch, map-based routing
- The API design tension between ergonomics and safety in actor systems

## Hints

This exercise is intentionally sparse. Research:

- Use `Module.register_attribute` and `@before_compile` to accumulate declared message types and generate a dispatch function
- Location transparency: store `{:local, pid}` or `{:remote, node, name}` inside `ActorRef`; on send, pattern-match and use `send/2` vs `:rpc.cast/4`
- For hot update, the `:sys` protocol in Erlang uses `{:system, from, {:change_code, ...}}`; study how `:gen_server` implements `code_change/3`
- Benchmarking: use `Benchee` with `formatters: [Benchee.Formatters.Console]` and document the methodology, not just the numbers
- Typed message dispatch: represent each message type as a struct module; the dispatch macro pattern-matches on `%MessageModule{}` struct keys

## Reference Material

- Akka Typed documentation: https://doc.akka.io/docs/akka/current/typed/index.html
- Proto.Actor design: https://proto.actor/docs/
- Erlang `:sys` module and `:gen` source: `lib/stdlib/src/sys.erl`, `lib/stdlib/src/gen.erl`
- "Programming Erlang" — Joe Armstrong, Chapter 14 (Behaviors)
- OTP hot code upgrade guide: https://www.erlang.org/doc/design_principles/release_handling

## Difficulty Rating

★★★★★★

## Estimated Time

45–60 hours
