# ETS access modes — `:named_table`, `:public`, `:protected`, `:private`

**Project**: `ets_access_modes` — observe how `:public`, `:protected`, and
`:private` change who can read and write a table, and how
`:read_concurrency` / `:write_concurrency` change the shape of concurrent
access.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

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

## Implementation

### Step 1: Create the project

```bash
mix new ets_access_modes
cd ets_access_modes
```

### Step 2: `lib/ets_access_modes.ex`

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

```bash
mix test
```

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

## Resources

- [`:ets.new/2` — all options](https://www.erlang.org/doc/man/ets.html#new-2)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — access-mode walkthrough
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — the ETS chapter on `read_concurrency` / `write_concurrency` in production
- [`Registry`](https://hexdocs.pm/elixir/Registry.html) — real-world `:public` + `:read_concurrency` ETS under the hood
- [OTP release notes for `write_concurrency: :auto`](https://www.erlang.org/blog/my-otp-24-highlights/) — modern default
