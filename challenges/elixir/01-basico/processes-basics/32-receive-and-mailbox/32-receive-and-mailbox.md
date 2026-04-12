# Receive and Mailbox: Building a Request/Response Pattern

**Project**: `req_resp` — a minimal request/response abstraction using unique refs,
the same pattern that sits underneath `GenServer.call/3`.

**Difficulty**: ★★☆☆☆
**Time**: 2-3 hours

---

## Project structure

```
req_resp/
├── lib/
│   └── req_resp/
│       ├── mailbox.ex
│       ├── echo_server.ex
│       └── client.ex
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

## Implementation

### Step 1: Create the project

```bash
mix new req_resp
cd req_resp
```

### Step 2: `mix.exs`

```elixir
defmodule ReqResp.MixProject do
  use Mix.Project

  def project do
    [
      app: :req_resp,
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

### Step 3: `.formatter.exs`

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### Step 4: `lib/req_resp/mailbox.ex`

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

### Step 5: `lib/req_resp/echo_server.ex`

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

### Step 6: `lib/req_resp/client.ex`

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

### Step 7: Tests

```elixir
# test/req_resp/echo_server_test.exs
defmodule ReqResp.EchoServerTest do
  use ExUnit.Case, async: true

  alias ReqResp.EchoServer

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
```

```elixir
# test/req_resp/client_test.exs
defmodule ReqResp.ClientTest do
  use ExUnit.Case, async: true

  alias ReqResp.{Client, EchoServer, Mailbox}

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
```

### Step 8: Run

```bash
mix deps.get
mix compile --warnings-as-errors
mix test
mix format
```

---

## Trade-off analysis

| Aspect | Current approach | Alternative |
|--------|-----------------|-------------|
| Server abstraction | Raw `spawn` + `receive` loop | `GenServer` (covered in 02-intermedio) |
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

## Resources

- [`Kernel.send/2` — HexDocs](https://hexdocs.pm/elixir/Kernel.html#send/2)
- [`receive` special form — HexDocs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#receive/1)
- [`Kernel.make_ref/0` — HexDocs](https://hexdocs.pm/elixir/Kernel.html#make_ref/0)
- [`Process` module — HexDocs](https://hexdocs.pm/elixir/Process.html)
- [Erlang efficiency guide — selective receive](https://www.erlang.org/doc/system/eff_guide_processes.html#receiving-messages)
