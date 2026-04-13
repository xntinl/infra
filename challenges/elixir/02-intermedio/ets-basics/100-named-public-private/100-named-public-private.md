# ETS access modes — `:named_table`, `:public`, `:protected`, `:private`

**Project**: `ets_access_modes` — observe how `:public`, `:protected`, and
`:private` change who can read and write a table, and how
`:read_concurrency` / `:write_concurrency` change the shape of concurrent
access.

---

## Project context

ETS has four orthogonal access-shape decisions:

1. Named or anonymous (`:named_table`).
2. Access mode: `:public` / `:protected` / `:private`.
3. Read concurrency (`read_concurrency: true`).
4. Write concurrency (`write_concurrency: true`).

Libraries pick these carefully based on the workload. `Registry` and `:pg`
use `:public` + `:read_concurrency: true`. Process-local caches are
`:protected`. Secret per-process state is `:private`. Getting this wrong
costs you either correctness (another process mutating your "private"
state) or throughput (everyone serializing through a single owner).

This exercise builds one small tally table four different ways and probes
what each access mode allows.

## Why tune access modes and not default everything

**Why not always `:public`?** Because `:public` means any process — including
buggy ones — can mutate your table. `:protected` gives you read-sharing
without losing write authority.

**Why not always `:protected`?** Because in a shared cache / registry shape
(many writers, e.g. `Registry`), routing all writes through one owner
serializes the hot path unnecessarily.

**Why not skip `:read_concurrency`/`:write_concurrency` and rely on defaults?**
At low throughput, correct. At high throughput, these flags are the
difference between 50k and 500k ops/sec. Profiling tells you when it matters.

Project structure:

```
ets_access_modes/
├── lib/
│   └── ets_access_modes.ex
├── test/
│   └── ets_access_modes_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:public`, `:protected`, `:private` in one line each

- **`:public`**: any process may read and write. Use for shared caches,
  registries, lock-free counters. Pair with `:write_concurrency: true` if
  writes are concurrent and hot.
- **`:protected`** (default): owner may read and write; other processes
  may **only read**. Use when one process is the source of truth and many
  processes consume. This is the most common mode.
- **`:private`**: only the owner may read or write. Use when the table is
  an internal implementation detail of one process — think of it as an
  extension of that process's heap that doesn't get garbage collected on
  minor GCs (a real perf trick in long-running GenServers with big state).

### 2. `:named_table` vs anonymous

With `:named_table`, you address the table by its atom: `:ets.lookup(:cache, k)`.
Without it, you use the reference returned from `:ets.new/2`. Named tables
live in a global atom registry; they collide across libraries. Rule of thumb:
**only name your table if its purpose is cross-module and you own the name**
(e.g. `MyApp.UserCache`).

### 3. `:read_concurrency: true`

Enables a read path optimized for many processes reading at once. It has
slightly higher write cost (it has to keep readers and writers from
stepping on each other), so it's a trade: fast parallel reads, slightly
slower writes. The typical signature is "one writer, many readers" —
essentially the shape of most caches.

Source: [erlang.org/doc/man/ets.html — new/2](https://www.erlang.org/doc/man/ets.html#new-2).

### 4. `:write_concurrency: true`

Allows concurrent writes to different keys without serialization. Under the
hood the table is sharded by hash across multiple locks. Biggest win when
multiple processes bump different keys concurrently (counter tables,
per-user state). Not supported on `:ordered_set` (the tree is global-lock).

OTP 24+ also has `write_concurrency: :auto` which lets the VM adapt to
contention, and specific modes like `{write_concurrency, true}` vs
`{write_concurrency, {auto, true}}`. The `:auto` value is the safer modern
default.

### 5. The combinations that matter in practice

| Workload                | Mode                  |
|--------------------------|-----------------------|
| Per-process scratch      | `:protected` (default) |
| Hot cache, many readers  | `:public` + `:read_concurrency` |
| Hot counters, many writers | `:public` + `:write_concurrency` |
| One writer + many readers | `:protected` + `:read_concurrency` |
| Secret per-process state | `:private` |

---

## Design decisions

**Option A — One table, parameterized mode at creation**
- Pros: Mode is a fact about the table; clients don't need to know it.
- Cons: Testing needs a clean process-ownership split to observe
  `:protected`/`:private` semantics.

**Option B — Spawned owner process per test** (chosen)
- Pros: The owner and the client are genuinely different processes, which
  is the only way to see `:private` fail and `:protected` accept reads.
- Cons: A tiny ad-hoc `loop/1` owner replaces what a real app would do with
  a GenServer.

→ Chose **B** because the **observable difference** between access modes
requires a real process boundary. A GenServer would be correct in production
but noisy for a teaching example.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new ets_access_modes
cd ets_access_modes
```

### Step 2: `lib/ets_access_modes.ex`

**Objective**: Implement `ets_access_modes.ex` — the access pattern that exposes the trade-off between ETS concurrency flags, match specs, and lookup cost.


```elixir
defmodule EtsAccessModes do
  @moduledoc """
  Demonstrates the three ETS access modes (`:public`, `:protected`, `:private`)
  by opening tables from a deliberately separate *owner process* and letting
  a *client process* attempt reads and writes. The rules are enforced by the
  runtime, so the tests boil down to "does this `:ets` call raise `:badarg`?".
  """

  @type mode :: :public | :protected | :private

  @doc """
  Spawns an owner process that opens a table with the given mode and
  replies with the table reference. The owner stays alive until it receives
  `:stop`, so tests can interact with it.
  """
  @spec start_owner(mode(), keyword()) :: {pid(), :ets.tid()}
  def start_owner(mode, extra_opts \\ []) do
    parent = self()

    pid =
      spawn_link(fn ->
        t = :ets.new(:tally, [:set, mode | extra_opts])
        send(parent, {:table, self(), t})
        loop(t)
      end)

    receive do
      {:table, ^pid, t} -> {pid, t}
    after
      1_000 -> raise "owner did not return its table"
    end
  end

  # Owner loop: handles a couple of messages so the test can use it as an
  # agent-of-sorts without pulling in GenServer here.
  defp loop(t) do
    receive do
      {:write, from, key, value} ->
        result =
          try do
            :ets.insert(t, {key, value})
          rescue
            e -> {:error, e}
          end

        send(from, {:write_result, result})
        loop(t)

      :stop ->
        :ets.delete(t)
        :ok
    end
  end

  @doc "Asks the owner to insert a tuple on its own behalf."
  @spec owner_write(pid(), term(), term()) :: term()
  def owner_write(owner, key, value) do
    send(owner, {:write, self(), key, value})

    receive do
      {:write_result, r} -> r
    after
      1_000 -> raise "owner did not reply"
    end
  end

  @doc """
  Attempts to read from a table as a non-owner. Returns `{:ok, value}`,
  `:empty`, or `{:error, reason}` if the access mode forbids it.
  """
  @spec foreign_read(:ets.tid(), term()) :: {:ok, term()} | :empty | {:error, term()}
  def foreign_read(table, key) do
    case :ets.lookup(table, key) do
      [{^key, v}] -> {:ok, v}
      [] -> :empty
    end
  rescue
    e -> {:error, e}
  end

  @doc """
  Attempts to write to a table as a non-owner. Returns `:ok` or
  `{:error, reason}` if the access mode forbids it (`:protected`, `:private`).
  """
  @spec foreign_write(:ets.tid(), term(), term()) :: :ok | {:error, term()}
  def foreign_write(table, key, value) do
    :ets.insert(table, {key, value})
    :ok
  rescue
    e -> {:error, e}
  end

  @doc "Shuts down the owner process (and its table)."
  @spec stop_owner(pid()) :: :ok
  def stop_owner(owner) do
    ref = Process.monitor(owner)
    send(owner, :stop)

    receive do
      {:DOWN, ^ref, :process, ^owner, _} -> :ok
    after
      500 -> :ok
    end
  end
end
```

### Step 3: `test/ets_access_modes_test.exs`

**Objective**: Write `ets_access_modes_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule EtsAccessModesTest do
  use ExUnit.Case, async: true

  describe ":public — any process reads and writes" do
    test "foreign write and read both succeed" do
      {owner, t} = EtsAccessModes.start_owner(:public)
      on_exit(fn -> EtsAccessModes.stop_owner(owner) end)

      assert EtsAccessModes.foreign_write(t, :k, 1) == :ok
      assert EtsAccessModes.foreign_read(t, :k) == {:ok, 1}
    end
  end

  describe ":protected — owner writes, everyone reads" do
    test "owner can write; foreign write raises; everyone can read" do
      {owner, t} = EtsAccessModes.start_owner(:protected)
      on_exit(fn -> EtsAccessModes.stop_owner(owner) end)

      assert EtsAccessModes.owner_write(owner, :k, 1) == true
      assert EtsAccessModes.foreign_read(t, :k) == {:ok, 1}

      # Foreign write must fail with :badarg (wrapped in ArgumentError).
      assert {:error, %ArgumentError{}} =
               EtsAccessModes.foreign_write(t, :k, 999)
    end
  end

  describe ":private — only owner reads or writes" do
    test "both foreign read and foreign write raise" do
      {owner, t} = EtsAccessModes.start_owner(:private)
      on_exit(fn -> EtsAccessModes.stop_owner(owner) end)

      assert EtsAccessModes.owner_write(owner, :k, 1) == true
      assert {:error, %ArgumentError{}} = EtsAccessModes.foreign_write(t, :k, 2)
      assert {:error, %ArgumentError{}} = EtsAccessModes.foreign_read(t, :k)
    end
  end

  describe ":named_table — address by atom" do
    test "named public table is reachable by its atom from anywhere" do
      {owner, _t} = EtsAccessModes.start_owner(:public, [:named_table])
      on_exit(fn -> EtsAccessModes.stop_owner(owner) end)

      :ets.insert(:tally, {:x, 42})
      assert :ets.lookup(:tally, :x) == [{:x, 42}]
    end
  end

  describe "read_concurrency flag is just a perf hint" do
    test "table still works with `read_concurrency: true`" do
      {owner, t} =
        EtsAccessModes.start_owner(:public, read_concurrency: true)

      on_exit(fn -> EtsAccessModes.stop_owner(owner) end)

      :ets.insert(t, {:k, :v})
      # Spawn a handful of parallel readers; none should error.
      tasks = for _ <- 1..50, do: Task.async(fn -> :ets.lookup(t, :k) end)
      results = Enum.map(tasks, &Task.await/1)

      assert Enum.all?(results, &(&1 == [{:k, :v}]))
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

ETS access enforcement is implemented at the VM level: `:ets.insert/2` from
a non-owner against a `:protected` or `:private` table raises `:badarg` before
any data is touched. Tests exercise this by spawning an owner in one process
and attempting reads/writes from the test process — each mode's rule falls
out as a predictable `ArgumentError` pattern or a successful call.

---


## Key Concepts: Named vs. Anonymous Tables and Access Control

When you create an ETS table with `:ets.new(:my_table, [:set, :named_table])`, the atom `:my_table` becomes a global identifier across the entire BEAM node. Any process can call `:ets.lookup(:my_table, key)` without holding a reference—just the atom. This is convenient for caches you want globally accessible (e.g., `Registry` uses a named `:_registry_supervisor_cache` table internally). Without `:named_table`, `:ets.new(:info, [:set])` returns an opaque `tid()` reference, and only processes holding that reference can access the table.

Access control is separate. `:public` tables allow any process to insert, update, and delete. `:protected` (the default) allows only the owner to write but any process to read—this is the most common pattern for shared caches. `:private` restricts all access to the owner. A best practice: use non-named tables (the returned reference) for library internal state, and only use `:named_table` when you're building a VM-global service (and document the atom to avoid collisions). For applications, named tables should be clearly scoped: `:my_app.cache`, not just `:cache`.


## Benchmark

```elixir
# Effect of :read_concurrency on parallel read throughput.
for flag <- [false, true] do
  t = :ets.new(:b, [:set, :public, read_concurrency: flag])
  for i <- 1..10_000, do: :ets.insert(t, {i, i})

  {us, _} = :timer.tc(fn ->
    Task.async_stream(1..1_000, fn _ ->
      for k <- 1..100, do: :ets.lookup(t, k)
    end, max_concurrency: System.schedulers_online())
    |> Stream.run()
  end)

  IO.puts("read_concurrency=#{flag}: #{us}µs")
  :ets.delete(t)
end
```

Target esperado: en máquinas de 8+ cores, `read_concurrency: true` reduce
el tiempo total en 20–50% bajo alta concurrencia de lectura. Con bajo
paralelismo (<4 lectores), la diferencia puede ser negativa por el costo
extra de escritura.

---

## Key Concepts

ETS tables can be named (lookup by atom) or unnamed (lookup by table ID), and public (all processes) or private (creator only). Named tables are convenient for singletons (one cache per app) but create coupling—any code can access them, which is sometimes intended and sometimes dangerous. Unnamed tables are safer but require passing the table ID. Public tables allow concurrent access from any process; private tables are accessible only from the creating process. Choose based on isolation needs: a cache shared across the app uses named+public; a temporary work table uses unnamed+private. A producer-consumer pair using a work queue might use unnamed+public (producer knows the ID, passes to consumer). The naming choice is a design decision: public named tables are convenient but create implicit dependencies across your code.

---

## Trade-offs and production gotchas

**1. `:public` + `:write_concurrency` is not free**
Sharded locks add memory overhead and slightly more expensive
`select_*`/`match_*` operations that have to traverse all shards. Don't
turn it on reflexively — only when profiling shows writer contention.

**2. `:ordered_set` ignores `write_concurrency`**
The tree needs a global lock. If you need range queries AND concurrent
writes, shard manually into multiple `:ordered_set` tables keyed by prefix.

**3. `:protected` is the default — be explicit if you mean `:public`**
The default is `:protected`, which means another library's table is
read-only to you unless they opted in. Conversely, if you build a shared
cache and forget to pass `:public`, nobody else can write to it.

**4. Named tables live in a global atom namespace**
Two libraries that both `:ets.new(:cache, [:named_table])` will crash the
second one at startup with `:badarg`. Prefix your atom with your OTP
application name (`:my_app_cache`) or use anonymous tables + a Registry.

**5. `:private` can be surprisingly fast for big state**
A GenServer with a million tuples of state pays the full-heap GC cost on
every major collection of its own heap. Move that state into a `:private`
ETS table owned by the GenServer and GC walks a much smaller heap.
Trade-off: reads/writes now cost term copies instead of pointer
dereferences. Profile before doing this.

**6. `:read_concurrency` ≠ "make reads atomic"**
All ETS reads are atomic per-operation regardless of this flag. The flag
tunes the **code path** the VM uses for readers; it trades slightly
higher write cost for parallelism on the read side. For a write-heavy
table with rare reads, leaving it off is fine.

**7. When NOT to bother**
For a table with a dozen entries and no concurrency, default options
(`:set`, `:protected`, no flags) are fine. These options become relevant
at scale (thousands of ops/sec, many cores, hot keys).

---

## Reflection

- You inherit a codebase where every ETS table is `:public` "just in case".
  Pick one table (session store) and justify tightening it to `:protected`.
  What in the existing code needs to change?
- A GenServer holds 500MB of state across millions of tuples and spends
  most of its time in GC. You're considering moving the state into a
  `:private` ETS table the GenServer owns. What do you benchmark before
  committing, and what could go worse?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule EtsAccessModes do
    @moduledoc """
    Demonstrates the three ETS access modes (`:public`, `:protected`, `:private`)
    by opening tables from a deliberately separate *owner process* and letting
    a *client process* attempt reads and writes. The rules are enforced by the
    runtime, so the tests boil down to "does this `:ets` call raise `:badarg`?".
    """

    @type mode :: :public | :protected | :private

    @doc """
    Spawns an owner process that opens a table with the given mode and
    replies with the table reference. The owner stays alive until it receives
    `:stop`, so tests can interact with it.
    """
    @spec start_owner(mode(), keyword()) :: {pid(), :ets.tid()}
    def start_owner(mode, extra_opts \\ []) do
      parent = self()

      pid =
        spawn_link(fn ->
          t = :ets.new(:tally, [:set, mode | extra_opts])
          send(parent, {:table, self(), t})
          loop(t)
        end)

      receive do
        {:table, ^pid, t} -> {pid, t}
      after
        1_000 -> raise "owner did not return its table"
      end
    end

    # Owner loop: handles a couple of messages so the test can use it as an
    # agent-of-sorts without pulling in GenServer here.
    defp loop(t) do
      receive do
        {:write, from, key, value} ->
          result =
            try do
              :ets.insert(t, {key, value})
            rescue
              e -> {:error, e}
            end

          send(from, {:write_result, result})
          loop(t)

        :stop ->
          :ets.delete(t)
          :ok
      end
    end

    @doc "Asks the owner to insert a tuple on its own behalf."
    @spec owner_write(pid(), term(), term()) :: term()
    def owner_write(owner, key, value) do
      send(owner, {:write, self(), key, value})

      receive do
        {:write_result, r} -> r
      after
        1_000 -> raise "owner did not reply"
      end
    end

    @doc """
    Attempts to read from a table as a non-owner. Returns `{:ok, value}`,
    `:empty`, or `{:error, reason}` if the access mode forbids it.
    """
    @spec foreign_read(:ets.tid(), term()) :: {:ok, term()} | :empty | {:error, term()}
    def foreign_read(table, key) do
      case :ets.lookup(table, key) do
        [{^key, v}] -> {:ok, v}
        [] -> :empty
      end
    rescue
      e -> {:error, e}
    end

    @doc """
    Attempts to write to a table as a non-owner. Returns `:ok` or
    `{:error, reason}` if the access mode forbids it (`:protected`, `:private`).
    """
    @spec foreign_write(:ets.tid(), term(), term()) :: :ok | {:error, term()}
    def foreign_write(table, key, value) do
      :ets.insert(table, {key, value})
      :ok
    rescue
      e -> {:error, e}
    end

    @doc "Shuts down the owner process (and its table)."
    @spec stop_owner(pid()) :: :ok
    def stop_owner(owner) do
      ref = Process.monitor(owner)
      send(owner, :stop)

      receive do
        {:DOWN, ^ref, :process, ^owner, _} -> :ok
      after
        500 -> :ok
      end
    end
  end

  def main do
    # Demo: modos de acceso ETS (:public, :protected, :private)
  
    # Test :public — cualquier proceso puede leer y escribir
    {owner_pub, t_pub} = EtsAccessModes.start_owner(:public)
    assert EtsAccessModes.foreign_write(t_pub, :k, 1) == :ok
    assert EtsAccessModes.foreign_read(t_pub, :k) == {:ok, 1}
    EtsAccessModes.stop_owner(owner_pub)
    IO.puts("✓ :public — lectura y escritura permitidas para procesos ajenos")
  
    # Test :protected — owner escribe, todos leen
    {owner_prot, t_prot} = EtsAccessModes.start_owner(:protected)
    assert EtsAccessModes.owner_write(owner_prot, :k, 2) == true
    assert EtsAccessModes.foreign_read(t_prot, :k) == {:ok, 2}
    # Foreign write debe fallar
    error_result = EtsAccessModes.foreign_write(t_prot, :k, 999)
    assert match?({:error, _}, error_result)
    EtsAccessModes.stop_owner(owner_prot)
    IO.puts("✓ :protected — solo owner escribe, otros leen")
  
    # Test :private — solo owner
    {owner_priv, t_priv} = EtsAccessModes.start_owner(:private)
    assert EtsAccessModes.owner_write(owner_priv, :k, 3) == true
    # Foreign read debe fallar
    read_result = EtsAccessModes.foreign_read(t_priv, :k)
    assert match?({:error, _}, read_result)
    EtsAccessModes.stop_owner(owner_priv)
    IO.puts("✓ :private — solo owner puede leer y escribir")
  
    IO.puts("\nEtsAccessModes: demostración de modos de acceso exitosa")
  end

end

Main.main()
```


## Resources

- [`:ets.new/2` — all options](https://www.erlang.org/doc/man/ets.html#new-2)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — access-mode walkthrough
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — the ETS chapter on `read_concurrency` / `write_concurrency` in production
- [`Registry`](https://hexdocs.pm/elixir/Registry.html) — real-world `:public` + `:read_concurrency` ETS under the hood
- [OTP release notes for `write_concurrency: :auto`](https://www.erlang.org/blog/my-otp-24-highlights/) — modern default
