# ETS basics — create, insert, lookup, delete

**Project**: `ets_intro` — your first hands-on with Erlang Term Storage: open a
table, put tuples in, pull them out, delete them, and understand who owns the
table and what happens when that owner dies.

---

## Project context

ETS (Erlang Term Storage) is the in-memory key/value store built into the BEAM.
It's what `Registry`, `:pg`, Phoenix's `Presence`, `Mnesia`, `Hackney` pools,
and dozens of other libraries use under the hood. It's **not** a database —
it's a shared-memory tuple store with O(1) or O(log N) access depending on the
table type, and it does not survive a node restart.

This first exercise is deliberately minimal: no GenServer, no supervisor, no
match specs. Just the four fundamental verbs — **new**, **insert**, **lookup**,
**delete** — plus the single most important concept that trips people up the
first time: **table ownership**. When the process that opened a table dies,
the table dies with it. Understanding that rule is the difference between
"ETS is magic" and "ETS is predictable".

## Why ETS and not X

**Why not a `Map` inside a GenServer?** For single-process state it works
fine. The moment you have N reader processes, every `get` becomes a
`GenServer.call` — serialized through one mailbox. ETS lets readers bypass
the owner entirely.

**Why not `:persistent_term`?** `:persistent_term` is faster for reads but
triggers a global GC on every write. It's read-heavy only. ETS handles mixed
read/write cleanly.

**Why not a real database?** Because ETS is in-memory, node-local, and free
of I/O latency. Use a database when you need durability or cross-node
coordination; otherwise ETS is almost always cheaper.

Project structure:

```
ets_intro/
├── lib/
│   └── ets_intro.ex
├── test/
│   └── ets_intro_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:ets.new/2` — creating a table

```elixir
:ets.new(:my_table, [:set, :protected, read_concurrency: true])
```

The first argument is the table name (an atom); the second is a list of
options. The most important options on day one:

- **Type**: `:set` (default, one tuple per key), `:ordered_set`, `:bag`,
  `:duplicate_bag`. You'll explore the differences later.
- **Access**: `:public` (any process can read/write), `:protected` (owner
  writes, everyone reads — default), `:private` (only owner).
- **`:named_table`**: if present, you refer to the table by its atom name;
  otherwise you keep the *reference* returned by `:ets.new/2`.

Without `:named_table`, `:ets.new(:foo, [])` returns an opaque `tid()` — a
reference — and you address the table via that reference, not via `:foo`.
The atom there is just an informational tag.

### 2. Ownership and lifecycle

Every ETS table has exactly one **owner process**. When that process dies,
**the table is destroyed** (unless the owner gave it away via
`:ets.give_away/3` or set a heir with `{:heir, pid, data}`). This is the
single most surprising thing about ETS for newcomers: if you open a table
from an `iex` session experiment, then the `iex` evaluator crashes, your
table vanishes.

In a real app, open ETS tables inside a **long-lived GenServer** (or a
dedicated owner process under your supervision tree). That's how `Registry`,
`:pg`, and friends do it.

### 3. Insert / lookup / delete

```elixir
:ets.insert(table, {:alice, 30})       # overwrites in a :set
:ets.lookup(table, :alice)             # => [{:alice, 30}]  (ALWAYS a list)
:ets.delete(table, :alice)             # removes the key
:ets.delete(table)                     # destroys the TABLE
```

Two things to internalize:

- `:ets.lookup/2` **always returns a list**, even in a `:set`. Empty list
  when the key is absent. This is because bag-type tables can return many
  tuples, and the API stays uniform.
- `:ets.delete/1` (one argument) destroys the entire table. `:ets.delete/2`
  removes one key. Same function name, different arity, very different
  consequences.

### 4. Tuples, not maps

ETS stores **tuples**. By default the key is element 1 (`{:alice, 30}` is
keyed by `:alice`). You can change the key position with the `{:keypos, N}`
option when creating the table. That's how people store structs directly:

```elixir
:ets.new(:users, [:set, keypos: 2])
:ets.insert(:users, %User{id: 1, name: "Alice"})  # if you match the shape
```

For this exercise we'll keep it boring: plain `{key, value}` tuples.

---

## Design decisions

**Option A — Wrap every ETS call in a GenServer API**
- Pros: Centralized access control; easy to add metrics / logging.
- Cons: You lose ETS's read-without-call advantage — every `get/2` pays
  a message round-trip.

**Option B — Thin module of pure ETS calls, caller owns the table** (chosen)
- Pros: No serialization; the exercise demonstrates the raw ownership rule
  without a GenServer hiding it.
- Cons: Production code would wrap this in a supervised owner; here we keep
  it minimal.

→ Chose **B** because the lesson is the **lifecycle** rule, and a GenServer
would just pay you to forget it. Real apps add the GenServer back on top.

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


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new ets_intro
cd ets_intro
```

### Step 2: `lib/ets_intro.ex`

**Objective**: Implement `ets_intro.ex` — the access pattern that exposes the trade-off between ETS concurrency flags, match specs, and lookup cost.


```elixir
defmodule EtsIntro do
  @moduledoc """
  Minimal ETS playground. Creates an owned table, exposes insert/lookup/delete,
  and demonstrates the lifecycle rule: when the owner dies, the table dies.

  The owner is the process that calls `open/1`. In a real system this would be
  a GenServer; here it's just the calling process to keep the concepts bare.
  """

  @type table :: :ets.tid() | atom()

  @doc """
  Creates a new ETS table owned by the calling process.

  Options forwarded to `:ets.new/2`. Defaults to `[:set, :protected]`.
  The caller is the owner — if it dies, this table is destroyed.
  """
  @spec open(atom(), keyword()) :: table()
  def open(name, opts \\ [:set, :protected]) do
    :ets.new(name, opts)
  end

  @doc "Inserts a `{key, value}` tuple. Overwrites on a `:set` table."
  @spec put(table(), any(), any()) :: true
  def put(table, key, value) do
    :ets.insert(table, {key, value})
  end

  @doc """
  Looks up a key. Returns `{:ok, value}` or `:error`.

  `:ets.lookup/2` returns a list — we unwrap it to a more idiomatic shape for
  the common `:set` case. For `:bag` tables, you'd want the raw list.
  """
  @spec get(table(), any()) :: {:ok, any()} | :error
  def get(table, key) do
    case :ets.lookup(table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> :error
    end
  end

  @doc "Deletes a single key from the table."
  @spec delete(table(), any()) :: true
  def delete(table, key), do: :ets.delete(table, key)

  @doc "Destroys the entire table. After this call the reference is invalid."
  @spec close(table()) :: true
  def close(table), do: :ets.delete(table)

  @doc "Returns how many tuples the table holds."
  @spec size(table()) :: non_neg_integer()
  def size(table), do: :ets.info(table, :size)
end
```

### Step 3: `test/ets_intro_test.exs`

**Objective**: Write `ets_intro_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule EtsIntroTest do
  use ExUnit.Case, async: true

  describe "basic CRUD" do
    setup do
      # Each test owns its own anonymous table — no :named_table means no
      # global-atom collision between async tests.
      table = EtsIntro.open(:t, [:set, :protected])
      on_exit(fn -> if :ets.info(table) != :undefined, do: :ets.delete(table) end)
      %{table: table}
    end

    test "put then get round-trips", %{table: t} do
      EtsIntro.put(t, :alice, 30)
      assert EtsIntro.get(t, :alice) == {:ok, 30}
    end

    test "get on missing key returns :error", %{table: t} do
      assert EtsIntro.get(t, :nobody) == :error
    end

    test "put on existing key overwrites (because :set)", %{table: t} do
      EtsIntro.put(t, :k, 1)
      EtsIntro.put(t, :k, 2)
      assert EtsIntro.get(t, :k) == {:ok, 2}
      assert EtsIntro.size(t) == 1
    end

    test "delete removes a key but keeps the table alive", %{table: t} do
      EtsIntro.put(t, :k, 1)
      EtsIntro.delete(t, :k)
      assert EtsIntro.get(t, :k) == :error
      # Table still exists and accepts new inserts.
      EtsIntro.put(t, :k2, 2)
      assert EtsIntro.get(t, :k2) == {:ok, 2}
    end
  end

  describe "ownership lifecycle" do
    test "table dies when its owner process dies" do
      # Spawn a short-lived owner that creates a named table and exits.
      test_pid = self()

      owner =
        spawn(fn ->
          t = :ets.new(:owned_by_me, [:set, :public, :named_table])
          :ets.insert(t, {:x, 1})
          send(test_pid, :ready)
          receive do
            :die -> :ok
          end
        end)

      assert_receive :ready, 500

      # From the outside we can read the table while the owner is alive.
      assert :ets.lookup(:owned_by_me, :x) == [{:x, 1}]

      # Kill the owner; wait for it to actually be down.
      ref = Process.monitor(owner)
      send(owner, :die)
      assert_receive {:DOWN, ^ref, :process, ^owner, _}, 500

      # The table is gone with the owner — calling :ets.info returns :undefined.
      assert :ets.info(:owned_by_me) == :undefined
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

`:ets.new/2` creates a table owned by the calling process; the VM tracks
ownership and tears the table down when the owner exits. The tests exploit
this by `spawn`ing an owner, waiting for `:DOWN`, then asserting
`:ets.info/1 == :undefined`. CRUD operations (`insert`, `lookup`, `delete`)
are all single-step and atomic per operation — no cross-key transactions,
but no torn writes either.

## Key Concepts: ETS Ownership and Memory Model

Erlang Term Storage (ETS) is fundamentally different from a GenServer-wrapped Map because it allows multiple reader processes to bypass the owner's mailbox entirely. When you call `:ets.lookup/2` from any process with read access, the kernel transfers that tuple directly to your heap—no message passing, no queueing. This is why ETS scales where GenServer-based state stores hit a ceiling: at 100 concurrent readers, a GenServer can handle ~10k get operations per second; ETS handles millions.

Ownership, however, is the gotcha that humbles every newcomer. The table exists **only while its owner process is alive**. A common mistake is opening an ETS table in an IEx experiment, crashing the evaluator, and being confused why the table vanished. In production, tables live inside long-lived GenServers under a supervision tree. When the owner receives a `:DOWN` signal from the supervisor, clean up explicitly or accept that the table's data is lost. The `:heir` option (`{:heir, heir_pid, heir_data}`) lets you transfer ownership on exit, but that's rarely used—most apps treat ETS as a volatile cache that rebuilds on startup.

---

## Key Concepts

ETS (Erlang Term Storage) is Elixir's primary in-process key-value store—mutable, fast, and shared across processes. `ets:new/2` creates a table (named or unnamed); `ets:insert/2` and `ets:lookup/2` perform atomic operations. ETS is valuable when you need shared read-heavy state (caches, registries, counters) but making a GenServer would serialize all access. The table lives as long as the creating process; if that process dies, the table disappears. This couples table lifetime to process lifecycle—wrap ETS creation in a supervised process to keep tables alive. Concurrency semantics vary: `set` tables provide atomic inserts (last write wins), `ordered_set` maintains sort order, `bag` allows duplicates. Production systems use ETS for caches (fast reads, occasional rebuilds), rate-limit counters, and session lookups—never for transactional data requiring rollback. Understanding ETS limits is crucial: there's no transactions, no query language beyond pattern matching, and no persistence to disk. For larger datasets or complex queries, a real database is more appropriate. The performance difference between ETS and GenServer access is orders of magnitude—ETS is the right choice for high-frequency state access.

---

## Benchmark

```elixir
# Compare ETS vs Map for 100k sequential inserts then 100k lookups,
# single process, no concurrency.
n = 100_000

{us_ets_insert, t} = :timer.tc(fn ->
  t = :ets.new(:b, [:set, :public])
  for i <- 1..n, do: :ets.insert(t, {i, i})
  t
end)
{us_ets_lookup, _} = :timer.tc(fn ->
  for i <- 1..n, do: :ets.lookup(t, i)
end)
:ets.delete(t)

{us_map_insert, m} = :timer.tc(fn ->
  Enum.reduce(1..n, %{}, fn i, acc -> Map.put(acc, i, i) end)
end)
{us_map_lookup, _} = :timer.tc(fn ->
  for i <- 1..n, do: Map.get(m, i)
end)

IO.puts("ETS insert=#{us_ets_insert}µs lookup=#{us_ets_lookup}µs")
IO.puts("Map insert=#{us_map_insert}µs lookup=#{us_map_lookup}µs")
```

Target esperado: para 100k elementos en un solo proceso, `Map` es típicamente
más rápido en lookup (sin copy-out), pero más lento en inserts (persistent
structure cost). La inversión ocurre en el momento en que múltiples procesos
necesitan leer.

---

## Trade-offs and production gotchas

**1. Ownership is invisible until it bites**
The first time a supervisor restarts the GenServer that owned your ETS table,
the table is gone — all its data with it. Either (a) treat ETS as a cache
that's fine to rebuild on startup, or (b) use `:ets.give_away/3` / the `:heir`
option to transfer ownership to a long-lived process before the owner exits.
See erlang.org/doc/man/ets.html#give_away-3.

**2. `lookup/2` copies data out of the table**
Every read is a **term copy** from ETS memory into the caller's heap. Looking
up a 1 MB binary means copying 1 MB. For large shared blobs, store them in
`:persistent_term` or use refc binaries carefully.

**3. `:set` overwrites silently**
`:ets.insert/2` on a `:set` with an existing key silently replaces it. If you
need "insert only if absent", use `:ets.insert_new/2`, which returns `false`
instead of overwriting.

**4. Named tables are a global namespace**
`:named_table` puts the atom in a VM-wide registry. Two libraries that both
open a `:cache` named table will collide. For library code, prefer
non-named tables (use the returned reference) unless the atom is clearly
yours (e.g. `MyApp.Cache`).

**5. ETS is not a database**
It's in-memory, non-transactional across multiple keys, and dies with the
node. If you need persistence, look at DETS (disk), Mnesia, or an external
store. The "Learn You Some Erlang" ETS chapter hammers this point and it's
worth reading before you build anything serious on top of ETS.

**6. When NOT to use ETS**
- When a plain `Map` inside a single GenServer fits: you avoid cross-process
  copying and you serialize access for free.
- When you need persistence or ACID transactions.
- When the data is tiny, read-only, and process-local — `@module_attr` or
  `:persistent_term` may be cheaper.

---

## Reflection

- You open an ETS table in an `iex` session for quick experiments. The
  `iex` evaluator crashes. Why is your table gone, and how would you
  redesign if the data must survive the crash but stay in-memory?
- A GenServer owns a 100MB ETS table for a cache. When the GenServer is
  restarted by its supervisor, the cache is empty. Is that a bug or a
  feature? Design both answers and the trade-off between them.

---

## Resources

- [Erlang `ets` module — official docs](https://www.erlang.org/doc/man/ets.html)
- [Elixir `:ets` wrapper guide — Elixir School](https://elixirschool.com/en/lessons/storage/ets)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — the canonical tour
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — chapter on ETS operational pitfalls
- [`:ets.info/1` reference](https://www.erlang.org/doc/man/ets.html#info-1) — the first tool you reach for when debugging
