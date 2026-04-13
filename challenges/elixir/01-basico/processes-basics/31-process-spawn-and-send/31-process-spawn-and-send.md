# Process Model: spawn, send, and self() — Chat Relay

**Project**: `chat_relay` — a minimal coordinator + two worker processes that exchange messages via raw spawn/send

---

## Core concepts in this exercise

1. **`spawn`, `send`, `receive`, `self()`** — the raw BEAM process primitives that
   every higher-level abstraction (`Task`, `Agent`, `GenServer`) is built on.
2. **The mailbox model** — messages queue FIFO per process; `receive` pattern-matches
   against them; failure to match leaves the message in the mailbox.

You will deliberately NOT use `GenServer` here. Understanding the primitives before
the abstractions makes it much easier to debug concurrency issues and understand how 
higher-level frameworks like `GenServer` and `Phoenix.PubSub` work under the hood.

---

## Why this matters for a senior developer

Every Elixir concurrency feature delegates to these three operations:

- `Task.async` → `spawn` + `send` on completion
- `GenServer.call` → `send` + `receive` with a timeout and a ref
- `Phoenix.PubSub` → a registry of pids + `send`

When a production system deadlocks, blows up its mailbox, or silently drops
messages, you debug it at this layer, not at the framework layer. Knowing what
`spawn(fn -> loop() end)` actually does is the difference between guessing and
diagnosing.

---

## Project structure

```
chat_relay/
├── lib/
│   └── chat_relay/
│       ├── coordinator.ex
│       └── worker.ex
├── test/
│   └── chat_relay/
│       └── relay_test.exs
└── mix.exs
```

---

## The business problem

You're prototyping a message routing core for a chat product. Before touching any
framework, the team wants a small executable model:

1. A **coordinator** process owns a routing table `%{name => pid}`.
2. Two **worker** processes register with the coordinator under a human-readable name.
3. A worker can send a chat message to another worker *by name*, and the coordinator
   relays it.
4. Every worker keeps an inbox of received messages that you can query synchronously.

No supervisors, no GenServers — just `spawn`, `send`, `receive`. Getting this right
is the mental model everything else is built on.

---

## Why raw primitives and not `GenServer`

- `GenServer` is the right answer in production — but it hides the exact `send`/`receive` plumbing you need to understand when diagnosing a stuck process.
- `Task` gives you async-return-a-value, not a long-lived mailbox loop.
- `Agent` gives you shared state but no custom message protocol.

For a coordinator that owns a routing table and a worker that keeps an inbox, the primitives are a 25-line model that matches the abstraction layer we want to teach.

---

## Design decisions

**Option A — fire-and-forget `register/3` (just `send`, no reply)**
- Pros: simpler code; no ref, no receive on the caller side.
- Cons: caller races ahead and may `relay` before the coordinator stores the pid. Intermittent test flakes.

**Option B — synchronous `register/3` with `make_ref/0` + timeout** (chosen)
- Pros: caller has a happens-before guarantee; timeouts make stuck coordinators loud; test for "register returns :ok before relays" is meaningful.
- Cons: more code per public call; introduces the manual ref/receive dance.

→ Chose **B** because the whole point of the exercise is to see the ref-based request/reply pattern `GenServer.call` later hides. `relay/3` stays fire-and-forget on purpose — chat senders shouldn't block on recipient processing.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"phoenix", "~> 1.0"},
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
defp deps do
  # No runtime deps — BEAM primitives only.
  []
end
```


### Step 1: Create the project

**Objective**: Set up a plain Mix project so raw `spawn`/`send`/`receive` are the only primitives on the stage, with no OTP abstraction leaking in.

```bash
mix new chat_relay
cd chat_relay
```

### Step 2: `mix.exs`

**Objective**: Declare an empty dep list to prove the BEAM's primitives alone implement a working chat relay — no Registry, no GenServer required.

```elixir
defmodule ChatRelay.MixProject do
  use Mix.Project

  def project do
    [
      app: :chat_relay,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

### Step 3: `lib/chat_relay/coordinator.ex`

**Objective**: Hold the name→pid table inside a single process's state so routing stays consistent without a shared mutable structure.

```elixir
defmodule ChatRelay.Coordinator do
  @moduledoc """
  Owns a name → pid registry and relays messages between workers.

  Runs as a plain process (no GenServer). The public functions are thin wrappers
  that send tagged tuples and block on a reply where necessary.
  """

  @doc """
  Spawns the coordinator and returns its pid.
  """
  @spec start() :: pid()
  def start do
    # `spawn/1` takes a zero-arity fun and returns a pid immediately. The child
    # runs concurrently — there is no OS thread, this is a BEAM-scheduled process.
    spawn(fn -> loop(%{}) end)
  end

  @doc """
  Registers a worker under a name. Returns :ok after the coordinator acks.

  This is a synchronous request/reply: we `send` a message tagged with our pid
  and then `receive` the reply. The ref is what makes this safe — without it,
  an unrelated message in our mailbox could be mistaken for the reply.
  """
  @spec register(pid(), atom(), pid()) :: :ok
  def register(coord, name, worker_pid) do
    ref = make_ref()
    send(coord, {:register, self(), ref, name, worker_pid})

    receive do
      {^ref, :ok} -> :ok
    after
      1_000 -> exit(:coordinator_timeout)
    end
  end

  @doc """
  Relays `text` from `from_name` to `to_name`. Fire-and-forget.

  Why no ack: in a chat system you don't want the sender blocked until the
  recipient processes the message. Delivery confirmation would be a separate
  concern (acks, read receipts) built on top.
  """
  @spec relay(pid(), atom(), atom(), binary()) :: :ok
  def relay(coord, from_name, to_name, text) do
    send(coord, {:relay, from_name, to_name, text})
    :ok
  end

  # ---------------------------------------------------------------------------
  # Internal loop
  # ---------------------------------------------------------------------------

  defp loop(registry) do
    receive do
      {:register, reply_to, ref, name, pid} ->
        # `Process.monitor/1` so if the worker dies, we receive a :DOWN message
        # and can evict it from the registry. Not required for the tests but it's
        # the responsible pattern and worth seeing at this level.
        Process.monitor(pid)
        send(reply_to, {ref, :ok})
        loop(Map.put(registry, name, pid))

      {:relay, from_name, to_name, text} ->
        case Map.fetch(registry, to_name) do
          {:ok, target_pid} ->
            # We include the sender NAME (not pid) in the delivered envelope —
            # workers shouldn't need to know each other's pids.
            send(target_pid, {:chat, from_name, text})

          :error ->
            # Unknown recipient. A real system would dead-letter; we drop.
            :ok
        end

        loop(registry)

      {:DOWN, _mref, :process, pid, _reason} ->
        # Remove the dead pid from the registry so future relays don't target it.
        loop(Map.reject(registry, fn {_name, p} -> p == pid end))
    end
  end
end
```

### Step 4: `lib/chat_relay/worker.ex`

**Objective**: Build a long-running receive loop whose tail-recursive state shows how BEAM processes maintain memory without mutable variables.

```elixir
defmodule ChatRelay.Worker do
  @moduledoc """
  A chat worker. Receives `{:chat, from, text}` envelopes and accumulates them.

  Exposes a synchronous `inbox/1` call so tests can assert on state. Like the
  coordinator, this is plain spawn/send/receive — no GenServer.
  """

  @doc """
  Starts a worker. Returns its pid immediately.
  """
  @spec start() :: pid()
  def start do
    # Initial inbox: empty list. New messages are prepended (O(1)) and reversed
    # when `inbox/1` is called. This is the standard list-as-queue idiom.
    spawn(fn -> loop([]) end)
  end

  @doc """
  Returns the worker's current inbox in arrival order.
  """
  @spec inbox(pid(), timeout()) :: [{atom(), binary()}]
  def inbox(worker, timeout \\ 1_000) do
    ref = make_ref()
    send(worker, {:inbox, self(), ref})

    receive do
      {^ref, messages} -> messages
    after
      timeout -> exit(:worker_timeout)
    end
  end

  defp loop(inbox) do
    receive do
      {:chat, from, text} ->
        # Prepend is O(1). Appending with `inbox ++ [msg]` would be O(n) per
        # message — catastrophic in a chat server under load.
        loop([{from, text} | inbox])

      {:inbox, reply_to, ref} ->
        # Reverse once, only on read. Amortized O(1) per message.
        send(reply_to, {ref, Enum.reverse(inbox)})
        loop(inbox)
    end
  end
end
```

**Why this shape:**

- Tail-recursive `loop/1` is how every BEAM server is built under the hood. The
  recursion does not grow the stack — the BEAM optimizes it away.
- State lives in the function argument. No mutable variables, no locks, no shared
  memory. Each message is processed to completion before the next begins, giving
  us serializability for free.
- `make_ref/0` produces a globally unique reference. Using it for reply correlation
  is the correct primitive — a hardcoded atom tag could collide if someone sends
  a matching message.

### Step 5: Tests

**Objective**: Drive the relay from the test process itself — using `self()` as a reply target proves the mailbox is first-class and address-based.

```elixir
# test/chat_relay/relay_test.exs
defmodule ChatRelay.RelayTest do
  use ExUnit.Case, async: true

  alias ChatRelay.{Coordinator, Worker}

  describe "end-to-end relay" do
    test "alice sends a message that bob receives" do
      coord = Coordinator.start()
      alice = Worker.start()
      bob = Worker.start()

      :ok = Coordinator.register(coord, :alice, alice)
      :ok = Coordinator.register(coord, :bob, bob)

      :ok = Coordinator.relay(coord, :alice, :bob, "hello bob")

      # Poll until bob's inbox has the message. Messages are async so we can't
      # just assert immediately — the relay has to traverse: us → coord → bob.
      Process.sleep(20)
      assert [{:alice, "hello bob"}] = Worker.inbox(bob)
    end

    test "preserves FIFO order across multiple relays" do
      coord = Coordinator.start()
      a = Worker.start()
      b = Worker.start()
      :ok = Coordinator.register(coord, :a, a)
      :ok = Coordinator.register(coord, :b, b)

      for i <- 1..5 do
        Coordinator.relay(coord, :a, :b, "msg-#{i}")
      end

      Process.sleep(20)
      messages = Worker.inbox(b)
      assert Enum.map(messages, fn {_from, text} -> text end) ==
               ["msg-1", "msg-2", "msg-3", "msg-4", "msg-5"]
    end

    test "messages to unknown recipients are dropped silently" do
      coord = Coordinator.start()
      a = Worker.start()
      :ok = Coordinator.register(coord, :a, a)

      # :ghost is not registered. The coordinator must not crash.
      :ok = Coordinator.relay(coord, :a, :ghost, "whisper")
      Process.sleep(20)

      # a's own inbox is empty and the coordinator is still alive.
      assert [] = Worker.inbox(a)
      assert Process.alive?(coord)
    end

    test "two-way conversation between workers" do
      coord = Coordinator.start()
      alice = Worker.start()
      bob = Worker.start()
      :ok = Coordinator.register(coord, :alice, alice)
      :ok = Coordinator.register(coord, :bob, bob)

      :ok = Coordinator.relay(coord, :alice, :bob, "hi bob")
      :ok = Coordinator.relay(coord, :bob, :alice, "hi alice")
      Process.sleep(20)

      assert [{:alice, "hi bob"}] = Worker.inbox(bob)
      assert [{:bob, "hi alice"}] = Worker.inbox(alice)
    end

    test "register/3 returns :ok before accepting relays" do
      coord = Coordinator.start()
      a = Worker.start()

      # If register were fire-and-forget, the next relay could race ahead of
      # the registration message. The ref-based reply in register/3 prevents that.
      assert :ok = Coordinator.register(coord, :a, a)
    end
  end
end
```

### Step 6: Run and verify

**Objective**: Run with warnings-as-errors so unused messages or unreachable receive clauses — typical hand-rolled-process bugs — fail the build.

```bash
mix test --trace
mix compile --warnings-as-errors
```

All 5 tests must pass.

### Why this works

The BEAM gives each process its own mailbox and its own heap — no shared memory, no locks. `receive` walks the mailbox in order and runs the first clause that matches, so a tail-recursive `loop(state)` is a legitimate server shape: the function argument IS the state, and the BEAM TCO's the recursion so the stack doesn't grow. `make_ref/0` returns a term unique across the BEAM node, which is what makes the synchronous `register/3` safe — no stray message can impersonate the reply. `Process.monitor/1` on each worker means dead pids are evicted from the registry before they can be relayed to.

---

## Executable Example

Create `lib/chat_relay.ex` and test in `iex`:

```elixir
defmodule ChatRelay.Worker do
  def start do
    spawn(fn -> loop([]) end)
  end

  def send_message(pid, message) do
    send(pid, {:message, message})
  end

  defp loop(messages) do
    receive do
      {:message, msg} ->
        loop([msg | messages])
      {:fetch, caller_pid, ref} ->
        send(caller_pid, {:reply, ref, Enum.reverse(messages)})
        loop(messages)
      :stop ->
        :ok
    end
  end
end

defmodule ChatRelay.Coordinator do
  def start do
    spawn(fn -> loop(%{}) end)
  end

  def register(coord_pid, name, worker_pid) do
    ref = make_ref()
    send(coord_pid, {:register, name, worker_pid, self(), ref})
    receive do
      {:registered, ^ref} -> :ok
    after
      5000 -> {:error, :timeout}
    end
  end

  def relay(coord_pid, from, to, message) do
    send(coord_pid, {:relay, from, to, message})
  end

  defp loop(registry) do
    receive do
      {:register, name, worker_pid, caller, ref} ->
        Monitor.monitor(worker_pid)
        new_registry = Map.put(registry, name, worker_pid)
        send(caller, {:registered, ref})
        loop(new_registry)

      {:relay, from, to, message} ->
        case Map.get(registry, to) do
          nil -> :ok
          target_pid -> ChatRelay.Worker.send_message(target_pid, "#{from}: #{message}")
        end
        loop(registry)

      {:DOWN, _ref, :process, pid, _reason} ->
        new_registry = Enum.filter(registry, fn {_k, v} -> v != pid end) |> Enum.into(%{})
        loop(new_registry)
    end
  end
end

defmodule Monitor do
  def monitor(pid) do
    Process.monitor(pid)
  end
end

# Test it
coord = ChatRelay.Coordinator.start()
a = ChatRelay.Worker.start()
b = ChatRelay.Worker.start()

:ok = ChatRelay.Coordinator.register(coord, :alice, a)
:ok = ChatRelay.Coordinator.register(coord, :bob, b)

ChatRelay.Coordinator.relay(coord, "alice", "bob", "Hello Bob!")
IO.puts("Message relayed from Alice to Bob")

ChatRelay.Coordinator.relay(coord, "bob", "alice", "Hi Alice!")
IO.puts("Message relayed from Bob to Alice")
```

---
## Key Concepts

### 1. Processes Are Isolated, Lightweight Actors

Each process is a separate entity with its own heap and message queue. Sending a message does not block the sender. The process handles it asynchronously. This isolation is the foundation of fault tolerance: if one process crashes, others continue.

### 2. Message Passing is the Only Way to Share Data Between Processes

There is no global state, no shared memory, no locks. One process sends a message, the other receives it. This prevents data races and makes concurrent systems easier to reason about.

### 3. `spawn/1` Returns a PID Immediately

The spawned function runs asynchronously. If you need to wait for a result, use `Task.await/2` or implement your own reply mechanism. Forgetting this leads to race conditions where you assume a process has finished when it has not.

---
## Benchmark

```elixir
# bench/relay.exs
coord = ChatRelay.Coordinator.start()
a = ChatRelay.Worker.start()
b = ChatRelay.Worker.start()
:ok = ChatRelay.Coordinator.register(coord, :a, a)
:ok = ChatRelay.Coordinator.register(coord, :b, b)

{t, _} = :timer.tc(fn ->
  Enum.each(1..100_000, fn i ->
    ChatRelay.Coordinator.relay(coord, :a, :b, "msg-#{i}")
  end)
end)

IO.puts("100k relays: #{t} µs — #{t / 100_000} µs/relay")
```

Target: < 5 µs per relay on modern hardware (two `send` hops + a Map.fetch). If the worker inbox grows unbounded (no draining in the benchmark), memory scales linearly — a real deployment would bound it.

---

## Trade-off analysis

| Concern                       | Raw spawn/send (this exercise)    | GenServer                       |
|-------------------------------|-----------------------------------|----------------------------------|
| Lines of code for a simple loop | ~25                            | ~50 with boilerplate            |
| Crash behavior                | Silent exit                       | Linked to supervisor            |
| Debugging tools (`:sys.trace`) | Not wired up                      | Works out of the box            |
| `:observer` visibility        | Shows a bare pid                  | Shows module + state            |
| Hot code upgrades             | Manual                            | Built-in via `code_change`      |
| Reply correlation             | Manual `make_ref` plumbing        | Automatic in `call/2`           |
| Learning the model            | Everything explicit               | Machinery hidden                |

The lesson: `GenServer` is not magic. It is the same loop you wrote, with the
reply/timeout/error-handling boilerplate factored out. When you encounter `GenServer`
callbacks (handle_call, handle_cast, handle_info), you'll recognize each as a 
specialized `receive` clause from this pattern.

---

## Common production mistakes

**1. `receive` with no catch-all clause and unmatched messages**
If a process receives `{:unknown, _}` and no clause matches, the message stays
in the mailbox forever. The mailbox grows, scan time for every subsequent
`receive` degrades to O(n), and the BEAM starts spending all its time walking
an effectively-dead queue. Always add a catch-all that logs and drops, or use
selective receive with care.

**2. `send(pid, ...)` when the pid is dead**
`send/2` to a dead pid silently succeeds and returns the message. No error.
You think you delivered; you didn't. Always `Process.monitor/1` or use linked
processes when delivery matters.

**3. Using atoms as message tags without refs for replies**
```elixir
send(server, {:get, self()})
receive do
  {:value, v} -> v
end
```
If another process sends you `{:value, _}` for any reason, you'll grab the wrong
reply. Always `make_ref/0` and match on the ref.

**4. Appending to lists in the loop state**
`inbox ++ [msg]` is O(n). A worker that accumulates 10k messages and appends
each time will do 50 million cons operations. Prepend and reverse on read.

**5. Blocking the loop with a long computation**
If your `receive` clause calls a function that takes 500ms, the process's
mailbox fills during that window and latency spikes for everyone. Spawn a child
task for heavy work, or offload to a pool.

---

## When NOT to use raw spawn/send

- You need supervisor restart semantics on crash → `GenServer` under a supervisor.
- You need a call/reply with timeouts, debug tracing, and code upgrade hooks →
  `GenServer`.
- You need a one-shot async computation whose result you want back → `Task`.
- You need simple shared mutable state among many readers → `Agent` or ETS.

Raw `spawn`/`send` is correct when you are learning the model, when you are
writing a brand-new abstraction on top of the primitives, or when you want
an extremely lightweight process with no OTP overhead (rare).

---

## Reflection

- Your coordinator is a single process. At 50k relays/sec, its mailbox becomes the bottleneck. What do you shard on (sender name? recipient? random), and what does that break in the "registered name" abstraction?
- Workers currently accumulate every message forever. In a long-lived chat session this is an unbounded memory leak. How do you bound the inbox without losing the "query synchronously" ergonomics?

---

## Resources

- [Elixir `Process` module — HexDocs](https://hexdocs.pm/elixir/Process.html) — `monitor`, `link`, `exit`, `alive?`
- [Elixir Getting Started — Processes](https://hexdocs.pm/elixir/processes.html) — the official walkthrough of the primitives
- [Erlang Efficiency Guide — processes](https://www.erlang.org/doc/efficiency_guide/processes.html) — the performance characteristics you care about in production
- [The Little Elixir & OTP Guidebook — Ch. 3](https://www.manning.com/books/the-little-elixir-and-otp-guidebook) — the spawn/send/receive chapter, written exactly at this level
