# Raw processes: `spawn`, `send`, `receive` before GenServer

**Project**: `process_primitives` — build a tiny stateful worker from scratch with only the three core BEAM primitives.

---

## Project context

Before reaching for `GenServer`, every Elixir engineer should understand the
three primitives it is built on. A `GenServer` is roughly 400 lines of Erlang
code that implements a disciplined *receive loop* around `spawn`, `send`, and
`receive`. If those three tools feel opaque, every GenServer abstraction
(callbacks, replies, timeouts, state) will feel like magic — and magic is hard
to debug when a production node is misbehaving.

In this exercise you will implement a stateful counter as a raw process — no
`use GenServer`, no OTP. You will see exactly what `GenServer.call/2`
does under the hood: tag a message, send it, wait for a tagged reply, time out
if no one answers. This is the mental model you'll reuse for the rest of your
OTP career.

Project structure:

```
process_primitives/
├── lib/
│   └── process_primitives.ex
├── test/
│   └── process_primitives_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For procesos spawn send receive, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. `spawn/1` — creating a process

`spawn(fn -> ... end)` creates a new, isolated BEAM process and returns its
pid. The new process runs the function; when the function returns, the process
dies with reason `:normal`. Two processes share nothing — no memory, no
variables. They communicate only by message passing.

### 2. `send/2` — asynchronous, never blocks, never fails

`send(pid, message)` puts `message` in `pid`'s mailbox and returns immediately.
It does **not** wait for the target to read it. It does **not** fail if the
target is dead — the message is silently dropped. This is why production code
uses monitors or links to detect dead peers.

### 3. `receive` — selective mailbox scan

```elixir
receive do
  {:get, from} -> send(from, {:value, 42})
  :stop        -> :ok
after
  5_000 -> :timeout
end
```

`receive` pulls the **first** message in the mailbox that matches any clause.
Messages that don't match are **left in the mailbox** — this is "selective
receive" and it is the source of most BEAM mailbox bugs. A long mailbox with
no matching clause is O(n) per scan.

### 4. The request/reply pattern

`send` is one-way. To implement request/reply you must:
1. Include `self()` in the request so the server knows where to reply.
2. Tag the message with a unique reference (`make_ref/0`) so *your* reply
   isn't confused with anyone else's.
3. `receive` the tagged reply, with an `after` timeout.

This is exactly what `GenServer.call/2` does — plus monitor-based crash
detection so a dead server doesn't leave you blocked forever.

---

## Design decisions

**Option A — `spawn` with ad-hoc protocols**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `spawn_link` + tagged messages (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because linking makes failures observable; tagging prevents mailbox confusion.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new process_primitives
cd process_primitives
```

### Step 2: `lib/process_primitives.ex`

```elixir
defmodule ProcessPrimitives do
  @moduledoc """
  A stateful counter implemented without GenServer — only `spawn`, `send`,
  and `receive`. Demonstrates exactly what OTP abstractions build on top of.
  """

  @doc """
  Spawns a counter process starting at `initial`. Returns the pid.

  The spawned process runs a tail-recursive receive loop. Each iteration
  reads one message, handles it, and recurses with the (possibly updated)
  state. When the loop function returns without recursing, the process exits.
  """
  @spec start(integer()) :: pid()
  def start(initial \\ 0) when is_integer(initial) do
    spawn(fn -> loop(initial) end)
  end

  @doc """
  Synchronous increment. Uses the tag-reply-timeout idiom that GenServer.call
  formalizes: we include `self()` and a fresh ref, then wait for exactly that
  ref to come back so concurrent callers cannot see each other's replies.
  """
  @spec increment(pid(), timeout()) :: {:ok, integer()} | {:error, :timeout}
  def increment(pid, timeout \\ 5_000) do
    call(pid, :increment, timeout)
  end

  @doc "Synchronous read of the current value."
  @spec value(pid(), timeout()) :: {:ok, integer()} | {:error, :timeout}
  def value(pid, timeout \\ 5_000) do
    call(pid, :value, timeout)
  end

  @doc """
  Fire-and-forget stop. The server exits with reason `:normal` — no reply.
  This is the raw-process equivalent of `GenServer.cast`.
  """
  @spec stop(pid()) :: :ok
  def stop(pid) do
    send(pid, :stop)
    :ok
  end

  # ── Private ─────────────────────────────────────────────────────────────

  # The tail-recursive receive loop IS the process. Each clause returns by
  # calling `loop/1` again with new state, except `:stop` which returns,
  # ending the function and killing the process with reason :normal.
  defp loop(state) do
    receive do
      {:call, from, ref, :increment} ->
        new_state = state + 1
        send(from, {ref, new_state})
        loop(new_state)

      {:call, from, ref, :value} ->
        send(from, {ref, state})
        loop(state)

      :stop ->
        :ok

      _other ->
        # Unknown messages would otherwise stay in the mailbox forever and
        # slow down every selective receive. Drain and ignore.
        loop(state)
    end
  end

  # The `call` helper: tag, send, wait-for-exactly-our-ref, timeout.
  defp call(pid, request, timeout) do
    ref = make_ref()
    send(pid, {:call, self(), ref, request})

    receive do
      {^ref, reply} -> {:ok, reply}
    after
      timeout -> {:error, :timeout}
    end
  end
end
```

### Step 3: `test/process_primitives_test.exs`

```elixir
defmodule ProcessPrimitivesTest do
  use ExUnit.Case, async: true

  describe "start/1 and value/1" do
    test "starts with the given initial value" do
      pid = ProcessPrimitives.start(42)
      assert {:ok, 42} = ProcessPrimitives.value(pid)
    end

    test "defaults to zero" do
      pid = ProcessPrimitives.start()
      assert {:ok, 0} = ProcessPrimitives.value(pid)
    end
  end

  describe "increment/1" do
    test "increments the counter and returns the new value" do
      pid = ProcessPrimitives.start()
      assert {:ok, 1} = ProcessPrimitives.increment(pid)
      assert {:ok, 2} = ProcessPrimitives.increment(pid)
      assert {:ok, 2} = ProcessPrimitives.value(pid)
    end
  end

  describe "concurrent callers" do
    test "each caller only sees its own reply (refs isolate conversations)" do
      pid = ProcessPrimitives.start()
      parent = self()

      # Spawn 20 incrementers and collect their replies. If the ref isolation
      # were broken, callers would receive each other's tags and crash.
      for _ <- 1..20 do
        spawn(fn ->
          send(parent, ProcessPrimitives.increment(pid))
        end)
      end

      for _ <- 1..20 do
        assert_receive {:ok, _v}, 500
      end

      assert {:ok, 20} = ProcessPrimitives.value(pid)
    end
  end

  describe "stop/1" do
    test "stops the process" do
      pid = ProcessPrimitives.start()
      ref = Process.monitor(pid)
      ProcessPrimitives.stop(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 500
    end
  end

  describe "unknown messages" do
    test "drained and ignored, counter still works" do
      pid = ProcessPrimitives.start()
      send(pid, :garbage)
      send(pid, {:weird, "stuff"})
      assert {:ok, 1} = ProcessPrimitives.increment(pid)
    end
  end

  describe "timeout" do
    test "returns {:error, :timeout} when the server never replies" do
      # A dead pid never replies — we'll hit the timeout branch.
      dead = spawn(fn -> :ok end)
      # Give it a moment to actually die.
      Process.sleep(10)

      assert {:error, :timeout} = ProcessPrimitives.increment(dead, 50)
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `send/2` to a dead pid is silent**
No error, no warning — your message just vanishes. In real systems you
always pair request/reply with `Process.monitor/1` so you get a `:DOWN`
message when the callee dies. `GenServer.call` does this for you; a raw
`send` does not.

**2. Selective receive is O(n) over the mailbox**
Every `receive` scans the mailbox head-to-tail against all clauses. If
garbage messages accumulate (you forgot a catch-all, or a peer keeps
sending stale replies), latency grows linearly. Always have an `_other`
catch-all that drains unknown messages.

**3. No supervisor, no restart**
A raw-spawned process that crashes is simply gone. There is no
`Supervisor` backing it up, no restart strategy, no shutdown ordering.
This is fine for quick-and-dirty tools, never for production.

**4. Tagging with `make_ref/0` matters**
Without a unique ref, two concurrent callers could receive each other's
replies. `make_ref/0` is guaranteed unique within the node and lets
`receive` match on `^ref` to isolate conversations. Skipping this is the
#1 bug in hand-rolled request/reply code.

**5. `spawn` is not `spawn_link`**
A bare `spawn`'d process is invisible to its parent. If it crashes, no
one is notified unless you monitor it. For background workers that must
die with their owner, use `spawn_link`. For merely-observed workers,
`spawn` + `Process.monitor` is the right shape.

**6. When NOT to use raw processes**
In production, use `GenServer` (for servers), `Task` (for one-off async
work), or `Agent` (for simple state). Raw processes belong in exercises,
libraries that need non-standard protocols, and BEAM internals. This
exercise exists only so the abstractions above stop feeling magical.

---


## Reflection

- ¿Cuándo deberías escribir tu propio receive-loop en lugar de usar GenServer? Dá 2 casos reales.

## Resources

- [Elixir: Processes — getting started](https://hexdocs.pm/elixir/processes.html)
- [`Process` module — Elixir stdlib](https://hexdocs.pm/elixir/Process.html)
- [`Kernel.send/2` docs](https://hexdocs.pm/elixir/Kernel.html#send/2)
- [Erlang source of `gen_server.erl`](https://github.com/erlang/otp/blob/master/lib/stdlib/src/gen_server.erl) — read once; everything becomes clearer
- Fred Hébert, *Learn You Some Erlang for Great Good!* — chapter "The Hitchhiker's Guide to Concurrency"
