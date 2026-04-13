# Migrating From :pg2 to :pg (OTP 23+)

**Project**: `pg_migration` — modernizing a legacy distribution layer

---

## Why migrating from :pg2 to :pg matters

You inherit a 6-year-old Elixir codebase (running on OTP 22) that coordinates a fleet of
delivery workers across three data centers using `:pg2` for process group membership. The
payments team has been blocked from upgrading to OTP 26 (required for a new security patch)
because `:pg2` was removed in OTP 24 without a direct drop-in replacement. Every on-call week
somebody notices the `:pg2` deprecation banner and pushes a Jira ticket that nobody owns.

Your job is to migrate the group-membership layer to the new `:pg` module introduced in OTP 23.
`:pg` is not a rename — it is a rewrite with different semantics: eventual-consistency across
nodes, no implicit locks, scoped groups, and a different API surface. A naive
`sed -i s/pg2/pg/` will compile but will silently break membership during netsplits.

---

## The business problem

If a worker thinks it is the sole owner of a route when it isn't, two drivers get
assigned the same order. This already happened once in production with `:pg2` during a
flaky AZ link. Migration must preserve correctness under partitions while unlocking the
OTP upgrade.

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM already provides.
- **External service (Redis, sidecar)**: adds a network hop and extra failure domain.
- **Heavier framework abstraction**: couples the module to a framework lifecycle.

The chosen approach stays inside the BEAM and keeps the contract small.

---

## Project structure

```
pg_migration/
├── lib/
│   └── pg_migration/
│       ├── application.ex
│       ├── group.ex              # Thin wrapper that hides pg vs pg2
│       ├── group_pg2.ex          # Legacy adapter
│       ├── group_pg.ex           # New adapter based on :pg
│       └── worker.ex             # Domain process that joins a group
├── script/
│   └── main.exs
├── test/
│   └── pg_migration/
│       ├── group_pg_test.exs
│       └── cluster_test.exs      # multi-node test
└── mix.exs
```

---

## Design decisions

**Option A — Big-bang**: single release, swap `:pg2` → `:pg` everywhere. Risky: any
caller you missed crashes at runtime.

**Option B — Adapter with compile-time switch**: safer but ships dead code.

**Option C — Adapter with runtime detection** (chosen): check for `:pg2` module presence
at boot; pick the adapter dynamically. Lets you roll the same release across mixed-version
nodes during rolling deploy.

---

## Implementation

### `mix.exs`

```elixir
defmodule PgMigration.MixProject do
  use Mix.Project

  def project do
    [
      app: :pg_migration,
      version: "0.1.0",
      elixir: "~> 1.19",
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {PgMigration.Application, []}
    ]
  end

  defp deps do
    [
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### `lib/pg_migration.ex`

```elixir
defmodule PgMigration.Application do
  @moduledoc false
  use Application

  @scope :pg_migration

  @impl true
  def start(_type, _args) do
    children = [
      %{id: :pg, start: {:pg, :start_link, [@scope]}, type: :worker}
    ]

    opts = [strategy: :one_for_one, name: PgMigration.Supervisor]
    Supervisor.start_link(children, opts)
  end

  def scope, do: @scope
end

defmodule PgMigration.Group.Adapter do
  @callback join(group :: atom() | {atom(), term()}) :: :ok
  @callback leave(group :: atom() | {atom(), term()}) :: :ok
  @callback members(group :: atom() | {atom(), term()}) :: [pid()]
  @callback local_members(group :: atom() | {atom(), term()}) :: [pid()]
end

defmodule PgMigration.Group do
  @moduledoc """
  Thin abstraction over process groups. Delegates to :pg on modern OTP
  and :pg2 on OTP < 23.
  """

  @type group :: atom() | {atom(), term()}

  @spec join(group()) :: :ok
  def join(group), do: adapter().join(group)

  @spec leave(group()) :: :ok
  def leave(group), do: adapter().leave(group)

  @spec members(group()) :: [pid()]
  def members(group), do: adapter().members(group)

  @spec local_members(group()) :: [pid()]
  def local_members(group), do: adapter().local_members(group)

  @spec closest(group()) :: pid() | {:error, :no_members}
  def closest(group) do
    case local_members(group) do
      [] ->
        case members(group) do
          [] -> {:error, :no_members}
          list -> Enum.random(list)
        end

      local ->
        Enum.random(local)
    end
  end

  defp adapter do
    case Code.ensure_loaded(:pg) do
      {:module, :pg} -> PgMigration.Group.Pg
      _ -> PgMigration.Group.Pg2
    end
  end
end

defmodule PgMigration.Group.Pg do
  @moduledoc false
  @behaviour PgMigration.Group.Adapter

  @scope :pg_migration

  @impl true
  def join(group), do: :pg.join(@scope, group, self())

  @impl true
  def leave(group), do: :pg.leave(@scope, group, self())

  @impl true
  def members(group), do: :pg.get_members(@scope, group)

  @impl true
  def local_members(group), do: :pg.get_local_members(@scope, group)
end

defmodule PgMigration.Group.Pg2 do
  @moduledoc false
  @behaviour PgMigration.Group.Adapter

  @impl true
  def join(group) do
    :pg2.create(group)
    :pg2.join(group, self())
  end

  @impl true
  def leave(group), do: :pg2.leave(group, self())

  @impl true
  def members(group) do
    case :pg2.get_members(group) do
      {:error, {:no_such_group, _}} -> []
      list -> list
    end
  end

  @impl true
  def local_members(group) do
    case :pg2.get_local_members(group) do
      {:error, {:no_such_group, _}} -> []
      list -> list
    end
  end
end

defmodule PgMigration.Worker do
  use GenServer

  def start_link(opts) do
    group = Keyword.fetch!(opts, :group)
    GenServer.start_link(__MODULE__, group)
  end

  @impl true
  def init(group) do
    PgMigration.Group.join(group)
    {:ok, %{group: group}}
  end

  @impl true
  def terminate(_reason, %{group: group}) do
    PgMigration.Group.leave(group)
    :ok
  end
end
```

### `test/pg_migration_test.exs`

```elixir
defmodule PgMigration.GroupPgTest do
  use ExUnit.Case, async: true

  alias PgMigration.Group

  describe "PgMigration.Group" do
    test "join appears in members and local_members" do
      task = Task.async(fn ->
        Group.join(:test_group)
        receive do
          :stop -> :ok
        end
      end)

      Process.sleep(20)

      assert task.pid in Group.members(:test_group)
      assert task.pid in Group.local_members(:test_group)

      send(task.pid, :stop)
      Task.await(task)
    end

    test "closest/1 prefers local" do
      task = Task.async(fn ->
        Group.join(:closest_group)
        receive do
          :stop -> :ok
        end
      end)

      Process.sleep(20)

      assert Group.closest(:closest_group) == task.pid
      send(task.pid, :stop)
      Task.await(task)
    end

    test "dead processes are auto-removed" do
      {pid, ref} =
        spawn_monitor(fn ->
          Group.join(:auto_cleanup)
          receive do
            :stop -> :ok
          end
        end)

      Process.sleep(20)
      assert pid in Group.members(:auto_cleanup)

      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :killed}
      Process.sleep(20)
      refute pid in Group.members(:auto_cleanup)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== :pg Migration Demo ===\n")

    Application.ensure_all_started(:pg_migration)

    group = :demo_group
    {:ok, pid} = Agent.start_link(fn -> %{name: "member1"} end)

    :ok = PgMigration.Group.join(group)
    members = PgMigration.Group.members(group)

    IO.puts("Joined group: #{inspect(group)}")
    IO.inspect(members, label: "Members")

    PgMigration.Group.leave(group)
    Agent.stop(pid)

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

## Key concepts

### 1. Why :pg2 was removed
`:pg2` dates back to Erlang/OTP R13 (2009). It was a global, strongly-consistent process
group registry that relied on `:global` locks for every mutation. In a 5-node cluster,
joining a group triggered a lock acquisition across all nodes. The cost grew quadratically
and during netsplits mutations blocked indefinitely waiting for quorum. `:pg` replaces it
with eventual consistency: writes are local and replicated asynchronously.

### 2. API diff at a glance

| Operation | `:pg2` (deprecated) | `:pg` (OTP 23+) |
|-----------|--------------------|-----------------|
| Create group | `:pg2.create(name)` | implicit — created on first join |
| Join | `:pg2.join(name, pid)` | `:pg.join(scope, name, pid)` |
| Leave | `:pg2.leave(name, pid)` | `:pg.leave(scope, name, pid)` |
| List members | `:pg2.get_members(name)` | `:pg.get_members(scope, name)` |
| Get closest pid | `:pg2.get_closest_pid(name)` | not provided (implement manually) |
| Consistency | strong (global locks) | eventual (gossip) |

### 3. Scopes
The scope parameter is new: it is an atom identifying an independent `:pg` instance. The
default scope is `:pg` itself. You can start additional scopes via `:pg.start_link/1` to
isolate group namespaces.

### 4. Eventual consistency under netsplit
Two sides of a split happily accept joins. On heal, `:pg` merges — it does not reconcile
or elect. If you need "one leader per group", layer a leader-election protocol on top
(e.g., `:ra` or `:global.trans` with a registered name).

### 5. Monitoring membership changes
`:pg.monitor/2` sends messages on membership changes. This is the right primitive for
cache invalidation, consistent hashing rebuilds, or broadcasting to group members.
Polling `get_members/1` on every request does not scale.

### 6. Production gotchas

- No `:pg2.get_closest_pid/1` equivalent — build it yourself to avoid crossing the network.
- Scopes must match across nodes — inconsistent scopes see empty groups.
- Consistency window is ~50-200ms; don't assert membership within 10ms of a remote join.
- Netsplit behavior is permissive, not safe — `:pg` alone is NOT a leader election primitive.
- `:pg.monitor/2` leaks if you never demonitor long-lived processes.
- If you need transactions or leader election with fencing tokens, `:pg` is the wrong tool.

---

## Resources

- [`:pg` module — Erlang docs](https://www.erlang.org/doc/man/pg.html)
- [`:pg2` deprecation notice — OTP 24 release notes](https://www.erlang.org/blog/otp-24-highlights/)
- [Process groups in OTP](https://hexdocs.pm/elixir/Process.html)
- [`:ra` — Raft consensus for the BEAM](https://github.com/rabbitmq/ra)
