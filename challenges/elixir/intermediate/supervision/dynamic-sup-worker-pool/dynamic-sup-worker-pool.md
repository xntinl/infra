# DynamicSupervisor + Registry — a dynamic worker pool with lookup

**Project**: `dynamic_worker_pool` — a pool of workers where each worker is addressable by a logical key, not by pid.

---

## Why dynamic sup worker pool matters

`DynamicSupervisor` lets you spawn workers on demand but gives you back a
pid the caller must remember. That's fine for fire-and-forget jobs. But
the common real-world need is: "start a worker for user 42; later I want
to send user 42 a message without remembering which pid that was".

The answer is pairing `DynamicSupervisor` with `Registry`. The Registry
is a process-indexed lookup table owned and cleaned up by the VM, so
dead pids are automatically evicted. Your code looks up workers by
logical key (`{:user, 42}`, `{:session, "abc"}`) instead of handing pids
around.

---

## Project structure

```
dynamic_worker_pool/
├── lib/
│   └── dynamic_worker_pool.ex
├── script/
│   └── main.exs
├── test/
│   └── dynamic_worker_pool_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not poolboy?** Extra dep and its own supervision; DynamicSupervisor + Registry is stdlib-only and more flexible.

## Core concepts

### 1. `Registry` is an in-VM, per-process key-value map

```
  Registry.register(reg, {:user, 42}, value)
      ▲                       ▲
      │                       │
      └── the registry's     └── any associated term (metadata)
          registered name
```

Registering ties the entry to the caller's lifetime. When the caller
dies, the entry is removed automatically — no stale pids.

### 2. `via` tuples let GenServer.call use a logical name

```elixir
GenServer.start_link(__MODULE__, arg, name: {:via, Registry, {Reg, key}})
GenServer.call({:via, Registry, {Reg, key}}, :ping)
```

The `{:via, Registry, {Reg, key}}` tuple tells OTP: "resolve this name
via the Registry module before sending". No cached pids, no stale
references.

### 3. Typical shape: one supervisor, one registry, one worker module

```
       DynamicSupervisor (pool)
          │
          ├── Session(k1)   ─ registered in Registry under {:session, k1}
          ├── Session(k2)   ─ registered under {:session, k2}
          └── ...

       Registry (look up session pid by key)
```

### 4. Lookups are constant-time reads from ETS

`Registry` is implemented on top of ETS with partitioned tables for
write concurrency. Reads scale linearly with core count; writes
(registration) are serialized per-partition. For pools of thousands of
workers, configure `partitions: System.schedulers_online()`.

---

## Design decisions

**Option A — poolboy**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — DynamicSupervisor + Registry (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because stdlib-only, each worker is independently supervised and addressable.

## Implementation

### `mix.exs`

```elixir
defmodule DynamicWorkerPool.MixProject do
  use Mix.Project

  def project do
    [
      app: :dynamic_worker_pool,
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
mix new dynamic_worker_pool --sup
cd dynamic_worker_pool
```

### `lib/dynamic_worker_pool.ex`

```elixir
defmodule DynamicWorkerPool do
  @moduledoc """
  DynamicSupervisor + Registry — a dynamic worker pool with lookup.

  `DynamicSupervisor` lets you spawn workers on demand but gives you back a.
  """
end
```

### `lib/dynamic_worker_pool/session.ex`

**Objective**: Implement `session.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.

```elixir
defmodule DynamicWorkerPool.Session do
  @moduledoc """
  A per-key session worker. Registered in `DynamicWorkerPool.Registry`
  under `{:session, key}` so callers can look it up without holding a pid.
  """

  use GenServer, restart: :transient

  @registry DynamicWorkerPool.Registry

  # ── Public API ──────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    key = Keyword.fetch!(opts, :key)
    GenServer.start_link(__MODULE__, key, name: via(key))
  end

  @spec get_key(term()) :: term()
  def get_key(key), do: GenServer.call(via(key), :key)

  @spec bump(term()) :: :ok
  def bump(key), do: GenServer.cast(via(key), :bump)

  @spec count(term()) :: non_neg_integer()
  def count(key), do: GenServer.call(via(key), :count)

  @spec stop(term()) :: :ok
  def stop(key), do: GenServer.stop(via(key), :normal)

  defp via(key), do: {:via, Registry, {@registry, {:session, key}}}

  # ── Callbacks ───────────────────────────────────────────────────────

  @impl true
  def init(key), do: {:ok, %{key: key, count: 0}}

  @impl true
  def handle_call(:key, _from, %{key: k} = s), do: {:reply, k, s}
  def handle_call(:count, _from, %{count: n} = s), do: {:reply, n, s}

  @impl true
  def handle_cast(:bump, %{count: n} = s), do: {:noreply, %{s | count: n + 1}}
end
```

### `lib/dynamic_worker_pool/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.

```elixir
defmodule DynamicWorkerPool.Supervisor do
  @moduledoc """
  Top-level supervisor: Registry + DynamicSupervisor side by side.
  Registry must start before the DynamicSupervisor so children can
  register at init time.
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children = [
      {Registry, keys: :unique, name: DynamicWorkerPool.Registry},
      {DynamicSupervisor, name: DynamicWorkerPool.DynSup, strategy: :one_for_one}
    ]

    # :rest_for_one so if the Registry crashes, the DynamicSupervisor and
    # all its workers are also restarted — they were registered in the
    # dead Registry and their via-tuples would be dangling.
    Supervisor.init(children, strategy: :rest_for_one)
  end

  @doc """
  Starts a Session for `key` under the DynamicSupervisor. Returns the pid
  or `{:error, {:already_started, pid}}` if one already exists.
  """
  @spec start_session(term()) :: DynamicSupervisor.on_start_child()
  def start_session(key) do
    spec = {DynamicWorkerPool.Session, [key: key]}
    DynamicSupervisor.start_child(DynamicWorkerPool.DynSup, spec)
  end

  @doc "Looks up a Session pid by key, or `nil` if not running."
  @spec lookup(term()) :: pid() | nil
  def lookup(key) do
    case Registry.lookup(DynamicWorkerPool.Registry, {:session, key}) do
      [{pid, _meta}] -> pid
      [] -> nil
    end
  end
end
```

### Step 4: `test/dynamic_worker_pool_test.exs`

**Objective**: Write `dynamic_worker_pool_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule DynamicWorkerPoolTest do
  use ExUnit.Case, async: false

  doctest DynamicWorkerPool

  alias DynamicWorkerPool.{Session, Supervisor, as: _}

  setup do
    start_supervised!(DynamicWorkerPool.Supervisor)
    :ok
  end

  describe "core functionality" do
    test "start_session/1 registers under a unique key" do
      {:ok, pid} = DynamicWorkerPool.Supervisor.start_session("alice")
      assert is_pid(pid)
      assert DynamicWorkerPool.Supervisor.lookup("alice") == pid
      assert Session.get_key("alice") == "alice"
    end

    test "duplicate key returns :already_started" do
      {:ok, pid} = DynamicWorkerPool.Supervisor.start_session("bob")
      assert {:error, {:already_started, ^pid}} =
               DynamicWorkerPool.Supervisor.start_session("bob")
    end

    test "registry auto-evicts on worker exit" do
      {:ok, pid} = DynamicWorkerPool.Supervisor.start_session("carol")
      assert DynamicWorkerPool.Supervisor.lookup("carol") == pid

      ref = Process.monitor(pid)
      Session.stop("carol")
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 500

      # Registry cleaned up on process death.
      assert DynamicWorkerPool.Supervisor.lookup("carol") == nil
    end

    test "callers address workers by key, not pid" do
      {:ok, _} = DynamicWorkerPool.Supervisor.start_session("dave")

      Session.bump("dave")
      Session.bump("dave")
      Session.bump("dave")

      assert Session.count("dave") == 3
    end

    test "many workers can coexist and be looked up independently" do
      keys = for i <- 1..20, do: "user_#{i}"
      for k <- keys, do: {:ok, _} = DynamicWorkerPool.Supervisor.start_session(k)

      for k <- keys do
        assert Session.get_key(k) == k
      end
    end
  end
end
```

### Step 5: Run

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
  defmodule DynamicWorkerPool.Supervisor do
    @moduledoc """
    Top-level supervisor: Registry + DynamicSupervisor side by side.
    Registry must start before the DynamicSupervisor so children can
    register at init time.
    """

    use Supervisor

    @spec start_link(keyword()) :: Supervisor.on_start()
    def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

    @impl true
    def init(:ok) do
      children = [
        {Registry, keys: :unique, name: DynamicWorkerPool.Registry},
        {DynamicSupervisor, name: DynamicWorkerPool.DynSup, strategy: :one_for_one}
      ]

      # :rest_for_one so if the Registry crashes, the DynamicSupervisor and
      # all its workers are also restarted — they were registered in the
      # dead Registry and their via-tuples would be dangling.
      Supervisor.init(children, strategy: :rest_for_one)
    end

    @doc """
    Starts a Session for `key` under the DynamicSupervisor. Returns the pid
    or `{:error, {:already_started, pid}}` if one already exists.
    """
    @spec start_session(term()) :: DynamicSupervisor.on_start_child()
    def start_session(key) do
      spec = {DynamicWorkerPool.Session, [key: key]}
      DynamicSupervisor.start_child(DynamicWorkerPool.DynSup, spec)
    end

    @doc "Looks up a Session pid by key, or `nil` if not running."
    @spec lookup(term()) :: pid() | nil
    def lookup(key) do
      case Registry.lookup(DynamicWorkerPool.Registry, {:session, key}) do
        [{pid, _meta}] -> pid
        [] -> nil
      end
    end
  end

  def main do
    IO.puts("DynamicWorkerPool OK")
  end

end

Main.main()
```

## Benchmark

```elixir
# Registry.lookup vs Process.whereis en un pool de 10k workers
```

Target esperado: <5 µs por lookup en Registry partitioned.

## Trade-offs and production gotchas

**1. Registry auto-cleanup is a killer feature — don't defeat it**
Registering a different pid than `self()` is possible but dangerous:
cleanup is tied to the registering pid's lifetime, not the registered
one. Register from `init/1` of the worker itself (or use the `:via`
tuple, which does exactly that).

**2. Registry is per-node, not distributed**
`Registry` lives in the local VM. For cross-node lookups you need
`:global`, `:pg`, or a library like `Horde.Registry`. Do not reach for
the distributed version until you're actually running multiple nodes;
the local Registry is much faster.

**3. `:rest_for_one` over Registry + DynSup**
If the Registry crashes, every registered via-tuple is dangling. Listing
Registry first and DynamicSupervisor second under `:rest_for_one` means
a Registry crash takes the DynSup down too, starting fresh. Getting the
order or strategy wrong here produces a tree that limps after Registry
crashes.

**4. Partitions matter for write-heavy pools**
At thousands of registrations per second you'll hit the partition lock.
`partitions: System.schedulers_online()` spreads writes across N tables
at the cost of more memory. Measure before tuning.

**5. When NOT to use Registry + DynamicSupervisor**
For a bounded pool with *reused* workers (connection pools), use
`NimblePool` or `poolboy`. Registry + DynSup is the right answer when
workers are logically tied to external state (user id, session id, job
id) that creates and retires naturally.

---

## Reflection

- ¿Cómo evitás que un burst de clientes cree 100k workers? Diseñá el backpressure.

## Resources

- [`Registry` — Elixir stdlib](https://hexdocs.pm/elixir/Registry.html)
- [`DynamicSupervisor`](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [`Horde`](https://hexdocs.pm/horde/) — distributed Registry + DynamicSupervisor
- [`nimble_pool`](https://hexdocs.pm/nimble_pool/) — for connection-style pools

## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/dynamic_worker_pool_test.exs`

```elixir
defmodule DynamicWorkerPoolTest do
  use ExUnit.Case, async: true

  doctest DynamicWorkerPool

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert DynamicWorkerPool.run(:noop) == :ok
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
