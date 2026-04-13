# Receive and Mailbox: Building a Request/Response Pattern

**Project**: `req_resp` — a minimal request/response abstraction using unique refs,
the same pattern that sits underneath `GenServer.call/3`.

---

## Project structure

```
req_resp/
├── lib/
│   └── req_resp/
│       ├── mailbox.ex
│       ├── echo_server.ex
│       └── client.ex
├── script/
│   └── main.exs
├── test/
│   └── req_resp/
│       ├── echo_server_test.exs
│       └── client_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Core concepts

This exercise covers two primitives: `receive` and the process **mailbox**.

Every BEAM process has a private FIFO queue of messages called a mailbox. `send/2`
writes to a target mailbox asynchronously and always returns immediately. `receive`
is the only way to read from *your own* mailbox. You cannot peek into another
process's mailbox from the outside — that isolation is what makes the BEAM's
concurrency model safe.

Two subtleties that every Elixir senior must internalize:

**1. Selective receive.** `receive` scans the mailbox from oldest to newest and
picks the *first message that matches any clause*. Messages that don't match stay
in the mailbox. This means a slow mailbox (thousands of unmatched messages) makes
every `receive` O(n) — a classic production bug.

**2. Refs as correlation IDs.** `send` is fire-and-forget. If you want a *response*,
you must include a unique tag so the reply can be matched unambiguously. `make_ref/0`
produces a value that is unique within the cluster. This is exactly how
`GenServer.call/3` pairs a request with its reply.

---

## The business problem

You need a lightweight request/response helper for internal services. The client
sends a request, waits for the matching reply, and times out cleanly if no reply
arrives. The server (a plain process — no GenServer yet) loops on `receive`,
handles each request, and replies with the same ref that came in.

This is the atomic pattern that every sync BEAM RPC builds on.

---

## Design decisions

**Option A — tag replies with a monotonic integer counter in the process dict**
- Pros: avoids allocating a ref per call; "feels" cheaper.
- Cons: counter is process-local — across nodes you need global uniqueness; mutable state in process dict is ugly; collisions after overflow are catastrophic.

**Option B — `make_ref/0` per call + `^ref` match** (chosen)
- Pros: unique within the cluster; zero bookkeeping; matches exactly how `GenServer.call/3` works internally; easy to reason about.
- Cons: one ref allocation per call (cheap — nanoseconds on the BEAM).

→ Chose **B** because refs are the canonical BEAM correlation primitive. Any deviation requires justification; there is none at this scale.

---

## Implementation

### Step 1: Create the project

**Objective**: Split mailbox helpers, server, and client into separate modules so each concept of the request/response pattern is independently testable.

```bash
mix new req_resp
cd req_resp
```

### `mix.exs`
**Objective**: Configure the OTP application with no external deps, proving `spawn`, `send`, and `receive` alone are enough to build the request/response primitive.

```elixir
defmodule ReqResp.MixProject do
  use Mix.Project

  def project do
    [
      app: :req_resp,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end

end
```

### Step 3: `.formatter.exs`

**Objective**: Pin formatter rules so concurrency-heavy code reads uniformly across reviewers, where subtle indentation usually hides real bugs.

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### `lib/req_resp.ex`

```elixir
defmodule ReqResp do
  @moduledoc """
  Receive and Mailbox: Building a Request/Response Pattern.

  Mastering **Receive and Mailbox** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code....
  """
end
```

### `lib/req_resp/mailbox.ex`

**Objective**: Expose the raw mailbox so callers see the FIFO ordering and selective-receive mechanics that `GenServer` normally hides behind its API.

```elixir
defmodule ReqResp.Mailbox do
  @moduledoc """
  Helpers that make the raw mailbox visible for learning purposes.

  In production code you rarely inspect the mailbox directly — but knowing how
  to measure its size is essential when debugging slow `receive` performance.
  """

  @doc """
  Returns the current number of messages queued in the given process's mailbox.

  Uses `Process.info/2` which returns `nil` if the process is dead.
  """
  @spec size(pid()) :: non_neg_integer() | :dead
  def size(pid) when is_pid(pid) do
    # `:message_queue_len` is the cheapest way to observe mailbox pressure.
    # It does not copy any message, only reads a counter inside the VM.
    case Process.info(pid, :message_queue_len) do
      {:message_queue_len, n} -> n
      nil -> :dead
    end
  end

  @doc """
  Drains all pending messages from the CURRENT process's mailbox.

  Returns them as a list in arrival order. Useful in tests to assert what a
  process received, and to reset state between assertions.
  """
  @spec drain(timeout()) :: [term()]
  def drain(timeout \\ 0) do
    # `after 0` fires immediately if no message is ready — this turns `receive`
    # into a non-blocking "try receive", which is the only safe way to poll.
    do_drain([], timeout)
  end

  defp do_drain(acc, timeout) do
    receive do
      msg -> do_drain([msg | acc], timeout)
    after
      timeout -> Enum.reverse(acc)
    end
  end
end
```

### `lib/req_resp/echo_server.ex`

**Objective**: Build a loop over `receive` that echoes payloads back to the caller's pid, demonstrating the bidirectional message shape GenServer encodes.

```elixir
defmodule ReqResp.EchoServer do
  @moduledoc """
  A plain process that replies to requests with the same payload.

  This is deliberately NOT a GenServer — the goal is to see the receive loop
  without any abstraction hiding it. Every GenServer in the world is, at its
  core, exactly this loop plus some callbacks and supervision.
  """

  @doc """
  Spawns the server process and returns its pid.

  The server runs `loop/0` forever, matching tagged request tuples.
  """
  @spec start() :: pid()
  def start do
    spawn(fn -> loop() end)
  end

  @doc """
  Gracefully stops the server by sending a `:stop` message.

  We use a normal message rather than `Process.exit/2` so that any queued work
  finishes first — killing the process would throw away pending requests.
  """
  @spec stop(pid()) :: :ok
  def stop(pid) do
    send(pid, :stop)
    :ok
  end

  defp loop do
    receive do
      # The request contract: {:req, from_pid, unique_ref, payload}.
      # The ref is what lets the client distinguish THIS reply from any other
      # message that might land in its mailbox concurrently.
      {:req, from, ref, payload} when is_pid(from) and is_reference(ref) ->
        send(from, {:resp, ref, {:ok, payload}})
        loop()

      :stop ->
        :ok

      # Catch-all to drain unknown messages. Without this, an unexpected message
      # would sit in the mailbox forever, degrading `receive` performance
      # message after message — a common production pitfall.
      other ->
        :logger.warning("echo_server dropping unknown message: #{inspect(other)}")
        loop()
    end
  end
end
```

### `lib/req_resp/client.ex`

**Objective**: Tag each request with a unique `make_ref/0` so replies cannot be confused with unrelated messages already sitting in the mailbox.

```elixir
defmodule ReqResp.Client do
  @moduledoc """
  Synchronous request/response client.

  Mirrors the shape of `GenServer.call/3`: send a tagged request, block on the
  matching reply, time out if no reply arrives. The `ref` is what keeps this
  safe even when multiple calls are in flight from the same caller.
  """

  @default_timeout 5_000

  @doc """
  Sends `payload` to `server_pid` and waits for the reply.

  Returns `{:ok, reply}` on success, `{:error, :timeout}` if no reply arrives
  within `timeout` ms, and `{:error, :noproc}` if the server is already dead.
  """
  @spec call(pid(), term(), timeout()) ::
          {:ok, term()} | {:error, :timeout | :noproc}
  def call(server_pid, payload, timeout \\ @default_timeout) do
    if Process.alive?(server_pid) do
      do_call(server_pid, payload, timeout)
    else
      {:error, :noproc}
    end
  end

  defp do_call(server_pid, payload, timeout) do
    # A fresh ref per call — this is the single most important line in the
    # module. Without it, two concurrent calls could match each other's reply.
    ref = make_ref()
    send(server_pid, {:req, self(), ref, payload})

    receive do
      # Pattern-match on BOTH `:resp` and the specific ref. Any other message
      # (a late reply from a previous call, an unrelated cast) stays in the
      # mailbox untouched and will be handled by whoever is waiting for it.
      {:resp, ^ref, {:ok, reply}} -> {:ok, reply}
    after
      timeout ->
        # IMPORTANT: on timeout we do NOT remove a late reply that may still
        # arrive. In production code, you must also flush stale replies to
        # prevent the mailbox from growing. See `flush_stale/1` below.
        flush_stale(ref)
        {:error, :timeout}
    end
  end

  @doc """
  Drains any late reply carrying `ref` that arrived after a timeout.

  Called automatically on timeout. Exposed so tests can assert the behaviour.
  """
  @spec flush_stale(reference()) :: :ok
  def flush_stale(ref) do
    receive do
      {:resp, ^ref, _} -> flush_stale(ref)
    after
      0 -> :ok
    end
  end
end
```

### `test/req_resp_test.exs`

**Objective**: Cover timeouts, stale messages, and interleaved requests to prove the ref-tag protocol is immune to out-of-order delivery.

```elixir
defmodule ReqResp.EchoServerTest do
  use ExUnit.Case, async: true
  doctest ReqResp.Client

  alias ReqResp.EchoServer

  describe "core functionality" do
    test "spawns a live process" do
      pid = EchoServer.start()
      assert Process.alive?(pid)
      EchoServer.stop(pid)
    end

    test "replies with the same payload and ref" do
      pid = EchoServer.start()
      ref = make_ref()
      send(pid, {:req, self(), ref, "hello"})

      assert_receive {:resp, ^ref, {:ok, "hello"}}, 500
      EchoServer.stop(pid)
    end

    test "stops cleanly on :stop message" do
      pid = EchoServer.start()
      EchoServer.stop(pid)
      # Give the VM a moment to mark the process as dead.
      Process.sleep(20)
      refute Process.alive?(pid)
    end

    test "drops unknown messages and keeps running" do
      pid = EchoServer.start()
      send(pid, :garbage)

      # After garbage, a real request should still be answered.
      ref = make_ref()
      send(pid, {:req, self(), ref, 42})
      assert_receive {:resp, ^ref, {:ok, 42}}, 500

      EchoServer.stop(pid)
    end
  end
end
```

```elixir
defmodule ReqResp.ClientTest do
  use ExUnit.Case, async: true
  doctest ReqResp.Client

  alias ReqResp.{Client, EchoServer, Mailbox}

  describe "core functionality" do
    test "call/3 returns {:ok, reply} for a live server" do
      pid = EchoServer.start()
      assert {:ok, "ping"} = Client.call(pid, "ping", 500)
      EchoServer.stop(pid)
    end

    test "call/3 returns {:error, :noproc} when server is dead" do
      pid = EchoServer.start()
      EchoServer.stop(pid)
      Process.sleep(20)
      assert {:error, :noproc} = Client.call(pid, "x", 100)
    end

    test "call/3 returns {:error, :timeout} when no reply arrives" do
      # A bare process that never replies — simulates a wedged server.
      silent = spawn(fn -> Process.sleep(:infinity) end)
      assert {:error, :timeout} = Client.call(silent, "x", 50)
      Process.exit(silent, :kill)
    end

    test "concurrent calls do not mix up their replies" do
      pid = EchoServer.start()

      tasks =
        for i <- 1..50 do
          Task.async(fn -> Client.call(pid, i, 1_000) end)
        end

      results = Task.await_many(tasks, 2_000)
      assert Enum.all?(results, fn {:ok, _} -> true end)
      # Every task got its OWN reply value back.
      values = Enum.map(results, fn {:ok, v} -> v end) |> Enum.sort()
      assert values == Enum.to_list(1..50)

      EchoServer.stop(pid)
    end

    test "flush_stale/1 removes a late reply from the caller's mailbox" do
      ref = make_ref()
      send(self(), {:resp, ref, {:ok, :late}})
      assert Mailbox.size(self()) >= 1

      :ok = Client.flush_stale(ref)
      # Drain any unrelated system messages the test runner may have produced.
      drained = Mailbox.drain()
      refute Enum.any?(drained, fn
               {:resp, ^ref, _} -> true
               _ -> false
             end)
    end
  end
end
```

### Step 8: Run

**Objective**: Compile with strict warnings to catch dead `receive` clauses, which are a classic mailbox-blocking bug in hand-rolled processes.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test
mix format
```

### Why this works

`receive` with `^ref` does selective receive on a unique correlation tag, so even with 50 concurrent calls into the same `EchoServer` the replies can't be confused — the BEAM scans the caller's mailbox and extracts only the matching message. `flush_stale/1` with `after 0` does a non-blocking drain of any reply that arrived after the timeout window closed, which prevents the caller's mailbox from accumulating stale messages that would slow every future `receive` to O(n). The server's `other ->` catch-all keeps unknown messages from wedging the mailbox.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ReqResp: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create `lib/req_resp.ex` and test in `iex`:

```elixir
defmodule ReqResp.EchoServer do
  def start do
    spawn(fn -> loop() end)
  end

  defp loop do
    receive do
      {:call, value, caller_pid, ref} ->
        send(caller_pid, {:reply, ref, value})
        loop()
      :stop ->
        :ok
    after
      10_000 ->
        IO.puts("Echo server timeout, stopping")
        :ok
    end
  end
end

defmodule ReqResp.Client do
  def call(server_pid, value, timeout) do
    ref = make_ref()
    send(server_pid, {:call, value, self(), ref})
    receive do
      {:reply, ^ref, result} -> {:ok, result}
    after
      timeout -> {:error, :timeout}
    end
  end
end

# Test it
pid = ReqResp.EchoServer.start()

{:ok, result} = ReqResp.Client.call(pid, 42, 1000)
IO.inspect(result)  # 42

{:ok, result2} = ReqResp.Client.call(pid, "hello", 1000)
IO.inspect(result2)  # "hello"

# Test timeout
{:error, :timeout} = ReqResp.Client.call(spawn(fn -> :ok end), 99, 100)
IO.puts("Timeout correctly detected")

# Clean up
send(pid, :stop)
```

## Benchmark

```elixir
# bench/call.exs
pid = ReqResp.EchoServer.start()

{t_sync, _} = :timer.tc(fn ->
  Enum.each(1..100_000, fn i -> {:ok, ^i} = ReqResp.Client.call(pid, i, 1_000) end)
end)

IO.puts("100k sync calls: #{t_sync} µs — #{t_sync / 100_000} µs/call")
ReqResp.EchoServer.stop(pid)
```

Target: < 10 µs per synchronous call on modern hardware (alive? + send + receive-with-ref). This is the same order of magnitude as `GenServer.call/3` — any abstraction on top adds microseconds, not milliseconds.

---

## Trade-off analysis

| Aspect | Current approach | Alternative |
|--------|-----------------|-------------|
| Server abstraction | Raw `spawn` + `receive` loop | `GenServer` (covered elsewhere) |
| Reply correlation | `make_ref/0` | Monotonic integer counter |
| Timeout handling | `receive ... after` | External timer process |
| Liveness check | `Process.alive?/1` before send | `Process.monitor/1` |

`Process.alive?/1` is racy: the process can die between the check and the `send`.
For robust code you want `Process.monitor/1`, which is the next step up — but it
requires handling `:DOWN` messages in the receive, which you'll learn with
GenServer. For a teaching example, the alive? check is enough.

---

## Common production mistakes

**1. Forgetting the ref and matching only on the tag.**
`receive do {:resp, reply} -> ...` will match ANY `:resp` message, including
stale ones from previous calls, causing the wrong value to surface. Always
match `^ref`.

**2. Not flushing stale replies after timeout.**
If you time out but the server eventually replies, that message sits in your
mailbox forever. Over a long-running process, stale messages compound and
every `receive` becomes O(n).

**3. Using `:infinity` as a default timeout.**
A sync call with no timeout can wedge a request handler permanently. Always
pick a concrete ceiling.

**4. Catch-all clauses in the wrong place.**
A `receive` with `_other -> loop()` at the top swallows legitimate messages
from the supervisor or linked processes. If you add a catch-all, put it LAST
and log what you dropped.

**5. Unbounded mailbox growth.**
A process that receives faster than it processes builds up messages until the
VM OOMs. In production, observe `:message_queue_len` via telemetry.

---

## When NOT to use this pattern

- You need supervised lifecycle, state, and introspection — use `GenServer`.
- The work is fire-and-forget — a plain `send/2` is enough, no ref needed.
- You need parallel fan-out to N workers — use `Task.async_stream/3`.
- You need pub/sub to many subscribers — use `Registry` or `Phoenix.PubSub`.

This pattern is the minimum viable sync RPC on the BEAM. Learn it, then let
`GenServer` hide it for you.

---

## Reflection

- Your `:message_queue_len` telemetry shows one specific process regularly sits at 50k messages. `receive` has now degraded to O(n). What structural change fixes this — bounded mailbox, sharding the process, or moving the unmatched-message handling inside the receive? Justify.
- You replace `Process.alive?/1 + send` with `Process.monitor/1`. What exactly changes in the receive clauses of `Client.call/3`, and which failure mode becomes detectable that wasn't before?

---

## Resources

- [`Kernel.send/2` — HexDocs](https://hexdocs.pm/elixir/Kernel.html#send/2)
- [`receive` special form — HexDocs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#receive/1)
- [`Kernel.make_ref/0` — HexDocs](https://hexdocs.pm/elixir/Kernel.html#make_ref/0)
- [`Process` module — HexDocs](https://hexdocs.pm/elixir/Process.html)
- [Erlang efficiency guide — selective receive](https://www.erlang.org/doc/system/eff_guide_processes.html#receiving-messages)

---

## Why Receive and Mailbox matters

Mastering **Receive and Mailbox** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. `receive` Blocks Until a Matching Message Arrives

Every process has a mailbox. Messages pile up in order. `receive` pattern-matches on the mailbox, waiting for the first message that matches a clause. The `after` clause handles timeouts. If no clause matches and no timeout is set, the process waits forever.

### 2. Message Ordering is Per-Process, Not Global

If Process A sends messages to Process B, they arrive in order. But if multiple senders send to B, the order depends on timing—not guaranteed. Never rely on ordering across multiple senders without explicit coordination.

### 3. Selective Receive: Patterns Determine Which Messages Are Handled

Only messages matching your patterns are removed from the mailbox. Other messages stay queued. If you have unmatched messages and no catch-all clause, `receive` waits forever. Make sure your patterns cover all message types you expect.

---
