# Process Model: spawn, send, and self() — Chat Relay

**Project**: `chat_relay` — a minimal coordinator + two worker processes that exchange messages via raw spawn/send

**Difficulty**: ★★☆☆☆
**Estimated time**: 2-3 hours

---

## Core concepts in this exercise

1. **`spawn`, `send`, `receive`, `self()`** — the raw BEAM process primitives that
   every higher-level abstraction (`Task`, `Agent`, `GenServer`) is built on.
2. **The mailbox model** — messages queue FIFO per process; `receive` pattern-matches
   against them; failure to match leaves the message in the mailbox.

You will deliberately NOT use `GenServer` here. Understanding the primitives before
the abstractions is what makes later exercises on OTP click.

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

## Implementation

### Step 1: Create the project

```bash
mix new chat_relay
cd chat_relay
```

### Step 2: `mix.exs`

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

```bash
mix test --trace
mix compile --warnings-as-errors
```

All 5 tests must pass.

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
reply/timeout/error-handling boilerplate factored out. When you move to
`GenServer` in the next exercises, you'll recognize every callback as one of
these `receive` clauses.

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

## Resources

- [Elixir `Process` module — HexDocs](https://hexdocs.pm/elixir/Process.html) — `monitor`, `link`, `exit`, `alive?`
- [Elixir Getting Started — Processes](https://hexdocs.pm/elixir/processes.html) — the official walkthrough of the primitives
- [Erlang Efficiency Guide — processes](https://www.erlang.org/doc/efficiency_guide/processes.html) — the performance characteristics you care about in production
- [The Little Elixir & OTP Guidebook — Ch. 3](https://www.manning.com/books/the-little-elixir-and-otp-guidebook) — the spawn/send/receive chapter, written exactly at this level
