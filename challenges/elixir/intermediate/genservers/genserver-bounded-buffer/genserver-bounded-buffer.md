# Bounded FIFO buffer as a GenServer

**Project**: `bounded_buffer_gs` — a fixed-capacity FIFO queue exposed as a GenServer, used as an in-process job staging area with explicit back-pressure.

---

## Why genserver bounded buffer matters

You're building an ingestion front-end that receives events faster than a
downstream worker can process them. You need a buffer that:

1. Holds events in FIFO order.
2. Has a hard maximum size so the node can't OOM under a traffic spike.
3. Tells producers explicitly when it's full (so they can drop, retry with
   jitter, or shed load — the business decision stays at the call site).

A naive `List` would work but has `O(n)` pops, and more importantly it has
no notion of capacity. This exercise implements the buffer as a GenServer
backed by Erlang's `:queue` (amortized `O(1)` for push/pop) with an explicit
capacity check. The public API returns `{:error, :full}` on overflow — the
canonical Elixir way to surface bounded-queue back-pressure without
hiding failures.

---

## Project structure

```
bounded_buffer_gs/
├── lib/
│   └── bounded_buffer_gs.ex
├── script/
│   └── main.exs
├── test/
│   └── bounded_buffer_gs_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not the raw mailbox?** Unbounded. Production-fatal.
- **Why not `:queue` in an Agent?** Agent has no back-pressure primitive; we need to reject/block on full.

## Core concepts

### 1. `:queue` — Erlang's amortized `O(1)` FIFO

`:queue` is a double-ended queue implemented as two lists. Pushes go on the
"in" list, pops come from the "out" list, which is reversed lazily when
empty. Amortized `O(1)`, far better than `List ++ [x]` (which is `O(n)`).

```
       push ──▶ [in]     [out] ──▶ pop
         (reverses to [out] when [out] empties)
```

Key functions: `:queue.new/0`, `:queue.in/2`, `:queue.out/1`, `:queue.len/1`.

### 2. Bounded vs unbounded — the capacity invariant

Every in-memory buffer in production MUST be bounded. An unbounded queue
transforms a backlog into a memory crash. The capacity should come from
config, not be hard-coded, because different deployments tolerate different
amounts of in-flight work.

### 3. Explicit failure on overflow

Three options when full:
- **Drop silently** — dangerous, hides problems.
- **Block the caller** — possible via `{:noreply, state}` + deferred reply,
  but trades memory pressure for timeout errors and complicates the API.
- **Return `{:error, :full}`** — the caller decides. Most flexible, simplest
  to reason about. This is what we implement.

### 4. Why all operations are `call`

`push`, `pop`, `size` all need a reply: `push` reports `:ok | {:error, :full}`,
`pop` returns the item (or `:empty`), `size` returns the count. Using `cast`
for `push` would defeat the purpose — the caller couldn't even learn that
the buffer is full.

---

## Design decisions

**Option A — unbounded mailbox + cast producers**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — bounded queue with `call` back-pressure (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because unbounded mailboxes become OOM bombs under sustained producer excess.

## Implementation

### `mix.exs`

```elixir
defmodule BoundedBufferGs.MixProject do
  use Mix.Project

  def project do
    [
      app: :bounded_buffer_gs,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
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

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new bounded_buffer_gs
cd bounded_buffer_gs
```

### `lib/bounded_buffer_gs.ex`

**Objective**: Implement `bounded_buffer_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

```elixir
defmodule BoundedBufferGs do
  @moduledoc """
  A fixed-capacity FIFO buffer implemented as a GenServer, backed by Erlang's
  `:queue`. Producers get `{:error, :full}` when the buffer is at capacity,
  which makes back-pressure explicit at the call site.
  """

  use GenServer

  @default_capacity 100

  defmodule State do
    @moduledoc false
    defstruct [:queue, :size, :capacity]

    @type t :: %__MODULE__{
            queue: :queue.queue(term()),
            size: non_neg_integer(),
            capacity: pos_integer()
          }
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @doc """
  Starts the buffer. Options:

    * `:capacity` — positive integer, max items held (default 100).
    * `:name` — optional process name.
  """
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    {capacity, opts} = Keyword.pop(opts, :capacity, @default_capacity)

    unless is_integer(capacity) and capacity > 0 do
      raise ArgumentError, "capacity must be a positive integer, got: #{inspect(capacity)}"
    end

    GenServer.start_link(__MODULE__, capacity, opts)
  end

  @doc "Pushes an item. Returns `:ok` or `{:error, :full}`."
  @spec push(GenServer.server(), term()) :: :ok | {:error, :full}
  def push(server, item), do: GenServer.call(server, {:push, item})

  @doc "Pops the oldest item. Returns `{:ok, item}` or `:empty`."
  @spec pop(GenServer.server()) :: {:ok, term()} | :empty
  def pop(server), do: GenServer.call(server, :pop)

  @doc "Current number of items in the buffer."
  @spec size(GenServer.server()) :: non_neg_integer()
  def size(server), do: GenServer.call(server, :size)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(capacity) do
    {:ok, %State{queue: :queue.new(), size: 0, capacity: capacity}}
  end

  @impl true
  def handle_call({:push, _item}, _from, %State{size: size, capacity: capacity} = state)
      when size >= capacity do
    {:reply, {:error, :full}, state}
  end

  def handle_call({:push, item}, _from, %State{queue: q, size: size} = state) do
    {:reply, :ok, %{state | queue: :queue.in(item, q), size: size + 1}}
  end

  def handle_call(:pop, _from, %State{queue: q, size: size} = state) do
    case :queue.out(q) do
      {{:value, item}, q2} ->
        {:reply, {:ok, item}, %{state | queue: q2, size: size - 1}}

      {:empty, _q} ->
        {:reply, :empty, state}
    end
  end

  def handle_call(:size, _from, %State{size: size} = state) do
    {:reply, size, state}
  end
end
```

### Step 3: `test/bounded_buffer_gs_test.exs`

**Objective**: Write `bounded_buffer_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule BoundedBufferGsTest do
  use ExUnit.Case, async: true

  doctest BoundedBufferGs

  describe "push/pop FIFO ordering" do
    test "items come out in the order they went in" do
      {:ok, buf} = BoundedBufferGs.start_link(capacity: 5)

      for i <- 1..3, do: :ok = BoundedBufferGs.push(buf, i)

      assert {:ok, 1} = BoundedBufferGs.pop(buf)
      assert {:ok, 2} = BoundedBufferGs.pop(buf)
      assert {:ok, 3} = BoundedBufferGs.pop(buf)
      assert :empty = BoundedBufferGs.pop(buf)
    end
  end

  describe "capacity enforcement" do
    test "returns {:error, :full} when at capacity" do
      {:ok, buf} = BoundedBufferGs.start_link(capacity: 2)

      assert :ok = BoundedBufferGs.push(buf, :a)
      assert :ok = BoundedBufferGs.push(buf, :b)
      assert {:error, :full} = BoundedBufferGs.push(buf, :c)

      # Existing items must remain intact after a rejected push.
      assert BoundedBufferGs.size(buf) == 2
      assert {:ok, :a} = BoundedBufferGs.pop(buf)
      assert {:ok, :b} = BoundedBufferGs.pop(buf)
    end

    test "room freed by pop allows new pushes" do
      {:ok, buf} = BoundedBufferGs.start_link(capacity: 1)

      :ok = BoundedBufferGs.push(buf, :first)
      assert {:error, :full} = BoundedBufferGs.push(buf, :second)

      {:ok, :first} = BoundedBufferGs.pop(buf)
      assert :ok = BoundedBufferGs.push(buf, :second)
      assert {:ok, :second} = BoundedBufferGs.pop(buf)
    end
  end

  describe "size/1" do
    test "tracks the current count" do
      {:ok, buf} = BoundedBufferGs.start_link(capacity: 10)
      assert BoundedBufferGs.size(buf) == 0

      for i <- 1..4, do: :ok = BoundedBufferGs.push(buf, i)
      assert BoundedBufferGs.size(buf) == 4

      BoundedBufferGs.pop(buf)
      assert BoundedBufferGs.size(buf) == 3
    end
  end

  describe "validation" do
    test "rejects non-positive capacity" do
      assert_raise ArgumentError, fn -> BoundedBufferGs.start_link(capacity: 0) end
      assert_raise ArgumentError, fn -> BoundedBufferGs.start_link(capacity: -1) end
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.

### `script/main.exs`

```elixir
defmodule Main do
  defmodule BoundedBufferGs do
    @moduledoc """
    A fixed-capacity FIFO buffer implemented as a GenServer, backed by Erlang's
    `:queue`. Producers get `{:error, :full}` when the buffer is at capacity,
    which makes back-pressure explicit at the call site.
    """

    use GenServer

    @default_capacity 100

    defmodule State do
      @moduledoc false
      defstruct [:queue, :size, :capacity]

      @type t :: %__MODULE__{
              queue: :queue.queue(term()),
              size: non_neg_integer(),
              capacity: pos_integer()
            }
    end

    # ── Public API ──────────────────────────────────────────────────────────

    @doc """
    Starts the buffer. Options:

      * `:capacity` — positive integer, max items held (default 100).
      * `:name` — optional process name.
    """
    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts \\ []) do
      {capacity, opts} = Keyword.pop(opts, :capacity, @default_capacity)

      unless is_integer(capacity) and capacity > 0 do
        raise ArgumentError, "capacity must be a positive integer, got: #{inspect(capacity)}"
      end

      GenServer.start_link(__MODULE__, capacity, opts)
    end

    @doc "Pushes an item. Returns `:ok` or `{:error, :full}`."
    @spec push(GenServer.server(), term()) :: :ok | {:error, :full}
    def push(server, item), do: GenServer.call(server, {:push, item})

    @doc "Pops the oldest item. Returns `{:ok, item}` or `:empty`."
    @spec pop(GenServer.server()) :: {:ok, term()} | :empty
    def pop(server), do: GenServer.call(server, :pop)

    @doc "Current number of items in the buffer."
    @spec size(GenServer.server()) :: non_neg_integer()
    def size(server), do: GenServer.call(server, :size)

    # ── Callbacks ───────────────────────────────────────────────────────────

    @impl true
    def init(capacity) do
      {:ok, %State{queue: :queue.new(), size: 0, capacity: capacity}}
    end

    @impl true
    def handle_call({:push, _item}, _from, %State{size: size, capacity: capacity} = state)
        when size >= capacity do
      {:reply, {:error, :full}, state}
    end

    def handle_call({:push, item}, _from, %State{queue: q, size: size} = state) do
      {:reply, :ok, %{state | queue: :queue.in(item, q), size: size + 1}}
    end

    def handle_call(:pop, _from, %State{queue: q, size: size} = state) do
      case :queue.out(q) do
        {{:value, item}, q2} ->
          {:reply, {:ok, item}, %{state | queue: q2, size: size - 1}}

        {:empty, _q} ->
          {:reply, :empty, state}
      end
    end

    def handle_call(:size, _from, %State{size: size} = state) do
      {:reply, size, state}
    end
  end

  def main do
    {:ok, buf} = BoundedBufferGs.start_link(capacity: 3)
  
    :ok = BoundedBufferGs.push(buf, :a)
    :ok = BoundedBufferGs.push(buf, :b)
    :ok = BoundedBufferGs.push(buf, :c)
    IO.puts("Pushed 3 items, size: #{BoundedBufferGs.size(buf)}")
  
    {:error, :full} = BoundedBufferGs.push(buf, :d)
    IO.puts("Cannot push when full")
  
    {:ok, :a} = BoundedBufferGs.pop(buf)
    IO.puts("Popped :a, size: #{BoundedBufferGs.size(buf)}")
  
    :ok = BoundedBufferGs.push(buf, :d)
    IO.puts("Now can push, size: #{BoundedBufferGs.size(buf)}")
  
    IO.puts("✓ BoundedBufferGs works correctly")
  end

end

Main.main()
```

## Key Concepts: Back-Pressure and Flow Control in GenServer

A bounded buffer enforces a maximum queue depth: when the buffer is full, callers either block (via `call`) or are rate-limited. This prevents memory unbounded growth when producers outrun consumers. Implementing this in a GenServer requires returning `{:noreply, new_state}` from `handle_cast` and only letting the client know when the buffer has space (via a separate callback or channel).

The trade-off: bounded buffers add complexity (tracking pending callers, managing waitlists) but prevent cascading failures in overloaded systems. Without bounds, a fast upstream data source can crash the GenServer's process heap. GenStage and Flow solve this more elegantly via explicit demand-based backpressure, but a GenServer with a manual bounded buffer is sometimes the right tool for simple point-to-point scenarios.

## Benchmark

```elixir
# Medí throughput de put/take con 1 productor y 1 consumidor
```

Target esperado: >500k ops/s sostenidos; memoria acotada por `max_size`.

## Trade-offs and production gotchas

**1. A single GenServer is a serialization point**
All pushes and pops route through one process's mailbox. If throughput
exceeds what one process can handle (~tens of thousands of ops/s for simple
work), you need sharding (`PartitionSupervisor`) or an ETS-backed queue.
Measure before you optimize.

**2. `:queue` items live forever if the buffer leaks**
If nothing pops, items remain referenced by the GenServer state until the
process dies. That's fine when bounded — but if you decide to "just raise the
capacity" to silence `{:error, :full}`, you've reinvented an unbounded queue.
Full signals a real problem; fix the consumer, don't hide the symptom.

**3. `call` timeouts under burst load**
If pushes pile up faster than the server can serve them, producers see
`:timeout` on `GenServer.call`. Tune `call` timeout and/or `start_link`
options, but again — this is a signal, not something to mask.

**4. No persistence across restarts**
When the GenServer dies, the queue dies with it. For durability, back the
state with a disk log (`:disk_log`), Mnesia, or a real broker (RabbitMQ,
Kafka). A GenServer buffer is for in-process staging only.

**5. Consider drop-oldest vs drop-newest policies**
Some systems prefer to drop the oldest item when full (keep recent data,
e.g. telemetry). That's a one-line change: pop then push. Choose the policy
that matches your semantic — never leave it implicit.

**6. When NOT to use a GenServer bounded buffer**
If you need cross-node durability, a broker is the right answer. If you need
non-blocking concurrent producers, consider `:ets` with `:public` access or
`GenStage`. A GenServer shines for correctness and simplicity, not peak
throughput.

---

## Reflection

- ¿Qué pasa si el consumidor tarda 10x más que el productor durante 1 minuto? Diseñá la política de rechazo y justificala.

## Resources

- [`:queue` — Erlang stdlib](https://www.erlang.org/doc/man/queue.html)
- [`GenServer` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html)
- [`GenStage` — for multi-stage back-pressure pipelines](https://hexdocs.pm/gen_stage/)
- [Fred Hébert — "Queues Don't Fix Overload"](https://ferd.ca/queues-don-t-fix-overload.html) — essential reading before adding any queue to a system
- [`PartitionSupervisor` — horizontal scaling for GenServers](https://hexdocs.pm/elixir/PartitionSupervisor.html)

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/bounded_buffer_gs_test.exs`

```elixir
defmodule BoundedBufferGsTest do
  use ExUnit.Case, async: true

  doctest BoundedBufferGs

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert BoundedBufferGs.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
