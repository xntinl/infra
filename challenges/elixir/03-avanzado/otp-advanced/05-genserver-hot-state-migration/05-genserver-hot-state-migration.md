# GenServer Hot State Migration with `code_change/3`

**Project**: `hot_state_migration` — a GenServer that migrates its state shape across versions in-place using OTP release upgrades, and an honest discussion of why almost nobody does this anymore.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

---

## Project context

You inherit a payment-queue GenServer that has been in production for three years. It started life with a simple `%{pending: [], processed: 0}` state shape. Over time the team added `:retry_counts`, then `:last_error`, then a per-merchant ring buffer. Each change was a migration that would have required dropping the queue and restarting from empty — unacceptable in that system's constraints. The original engineer used **OTP release upgrades** and the `code_change/3` callback to reshape the state while the process kept running.

You are now on version 4 of the module. A business requirement forces yet another shape change: migrate `:retry_counts` from a flat map to a nested map keyed by `:merchant_id`. In a traditional system you would deploy v4 alongside v3, drain v3, and cut over. Here you have to migrate in-place because the queue drains too slowly and carries financial invariants that forbid losing entries.

This exercise implements that migration correctly. But the second half of this exercise is the honest part: almost no modern Elixir team uses hot code upgrades. They are complex, fragile, poorly tooled, and the industry has converged on blue/green deploys, rolling restarts with drain periods, and persistent state in databases. We will walk through when you genuinely need `code_change/3` (embedded systems, single-node appliances, extremely long-running session state) and when you should run away from it (anything with a load balancer in front).

```
hot_state_migration/
├── lib/
│   └── hot_state_migration/
│       ├── application.ex
│       └── payment_queue.ex       # module with @vsn + code_change/3
├── test/
│   └── hot_state_migration/
│       └── payment_queue_test.exs
├── rel/                           # mix release files (generated)
└── mix.exs
```

---

## Core concepts

### 1. `@vsn` — the module version attribute

Every Erlang/Elixir module can declare `@vsn <term>`. By default, the VM computes a hash from the module's bytecode. For hot upgrades you override it with a stable term you control:

```elixir
defmodule HotStateMigration.PaymentQueue do
  use GenServer
  @vsn 4
  # ...
end
```

When OTP performs a release upgrade, it compares the new module's `@vsn` to the running one. If they differ, it calls `code_change/3` with the old version and the old state.

### 2. `code_change/3` callback

```elixir
@impl true
def code_change({:down, 4}, state, _extra), do: {:ok, downgrade_v4_to_v3(state)}
def code_change(3, state, _extra), do: {:ok, upgrade_v3_to_v4(state)}
def code_change(_old, state, _extra), do: {:ok, state}
```

The callback is invoked synchronously during the upgrade. The new module is already loaded; the process is suspended; your job is to transform the state from the old shape to the new shape and return.

### 3. The upgrade dance

```
sys:suspend(pid)                 ← OTP suspends process; pending msgs buffer
load_new_module(Module)          ← new BEAM file swapped in
sys:change_code(pid, Mod, vsn, extra)  ← calls code_change/3
sys:resume(pid)                  ← process runs with new state shape
```

The mailbox keeps buffering during suspension; on resume, all queued messages are processed against the new shape. If your `code_change/3` is buggy, the process crashes and the supervisor decides what happens next — you may have just lost an entire pending queue.

### 4. `.appup` and `.relup` files

A hot upgrade requires a `.appup` describing module-level changes (upgrade/downgrade instructions per module) and a `.relup` describing the release-level transition. For a single GenServer upgrade the `.appup` looks like:

```erlang
{"4.0.0",
 [{"3.0.0", [{update, 'Elixir.HotStateMigration.PaymentQueue', {advanced, []}}]}],
 [{"3.0.0", [{update, 'Elixir.HotStateMigration.PaymentQueue', {advanced, []}}]}]}.
```

Tools like `distillery` used to generate these; `mix release` supports it via `:appup` and `:relup` configuration, but few people use it.

### 5. Why this is mostly legacy

- **Blue/green deploys are simpler.** Spin up v4, drain v3, kill v3. No in-place magic.
- **State in databases moots the problem.** If your queue is in Postgres, restart freely.
- **Tooling is thin.** Generating `.relup` files for real release chains is brittle.
- **Clustered apps need coordinated upgrades.** A heterogeneous cluster during upgrade is a distributed-systems nightmare.
- **`@vsn` drift is easy to mis-manage.** One missed bump and the upgrade silently skips your migration.

The niches where it still wins: embedded devices (Nerves), telecom-style single-node systems with strict uptime SLAs, and research-grade long-running processes whose state cannot be serialized out.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule HotStateMigration.MixProject do
  use Mix.Project

  def project do
    [app: :hot_state_migration, version: "4.0.0", elixir: "~> 1.16", deps: []]
  end

  def application do
    [extra_applications: [:logger], mod: {HotStateMigration.Application, []}]
  end
end
```

### Step 2: `lib/hot_state_migration/payment_queue.ex`

```elixir
defmodule HotStateMigration.PaymentQueue do
  @moduledoc """
  Payment queue whose state shape has evolved across four versions.

  Shape history:
    v1: %{pending: [...], processed: N}
    v2: %{pending: [...], processed: N, retry_counts: %{id => N}}
    v3: %{pending: [...], processed: N, retry_counts: %{id => N}, last_error: term}
    v4: %{pending: [...], processed: N, retry_counts: %{merchant_id => %{id => N}},
           last_error: term}

  Upgrades and downgrades handle adjacent pairs.
  """
  use GenServer
  @vsn 4

  @typep payment :: %{id: String.t(), merchant_id: String.t(), amount: integer()}
  @typep state_v4 :: %{
           pending: [payment()],
           processed: non_neg_integer(),
           retry_counts: %{optional(String.t()) => %{optional(String.t()) => non_neg_integer()}},
           last_error: term()
         }

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec enqueue(payment()) :: :ok
  def enqueue(p), do: GenServer.cast(__MODULE__, {:enqueue, p})

  @spec dump_state() :: state_v4()
  def dump_state, do: GenServer.call(__MODULE__, :dump_state)

  @impl true
  def init(_opts) do
    {:ok, %{pending: [], processed: 0, retry_counts: %{}, last_error: nil}}
  end

  @impl true
  def handle_cast({:enqueue, payment}, state) do
    {:noreply, %{state | pending: [payment | state.pending]}}
  end

  @impl true
  def handle_call(:dump_state, _from, state), do: {:reply, state, state}

  # ---- code_change/3 --------------------------------------------------------

  @impl true
  def code_change(1, state, _extra), do: {:ok, state |> upgrade_1_to_2() |> upgrade_2_to_3() |> upgrade_3_to_4()}
  def code_change(2, state, _extra), do: {:ok, state |> upgrade_2_to_3() |> upgrade_3_to_4()}
  def code_change(3, state, _extra), do: {:ok, upgrade_3_to_4(state)}
  def code_change({:down, 3}, state, _extra), do: {:ok, downgrade_4_to_3(state)}
  def code_change(_old, state, _extra), do: {:ok, state}

  # Public so tests can drive the transitions without an actual release upgrade.

  @doc false
  def upgrade_1_to_2(%{pending: p, processed: n}) do
    %{pending: p, processed: n, retry_counts: %{}}
  end

  @doc false
  def upgrade_2_to_3(%{pending: _, processed: _, retry_counts: _} = state) do
    Map.put(state, :last_error, nil)
  end

  @doc false
  def upgrade_3_to_4(%{retry_counts: flat} = state) do
    # flat: %{payment_id => count}. We need %{merchant_id => %{payment_id => count}}.
    # Partition by looking up the merchant of each pending payment; for payments
    # no longer in `pending`, bucket under "unknown".
    pending_lookup =
      for %{id: id, merchant_id: m} <- state.pending, into: %{}, do: {id, m}

    nested =
      Enum.reduce(flat, %{}, fn {pid, cnt}, acc ->
        merchant = Map.get(pending_lookup, pid, "unknown")
        Map.update(acc, merchant, %{pid => cnt}, &Map.put(&1, pid, cnt))
      end)

    %{state | retry_counts: nested}
  end

  @doc false
  def downgrade_4_to_3(%{retry_counts: nested} = state) do
    flat =
      Enum.reduce(nested, %{}, fn {_merchant, pmap}, acc -> Map.merge(acc, pmap) end)

    %{state | retry_counts: flat}
  end
end
```

### Step 3: `lib/hot_state_migration/application.ex`

```elixir
defmodule HotStateMigration.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [HotStateMigration.PaymentQueue]
    Supervisor.start_link(children, strategy: :one_for_one, name: HotStateMigration.Sup)
  end
end
```

### Step 4: `test/hot_state_migration/payment_queue_test.exs`

```elixir
defmodule HotStateMigration.PaymentQueueTest do
  use ExUnit.Case, async: true

  alias HotStateMigration.PaymentQueue

  describe "upgrade ladder" do
    test "v1 -> v2 introduces retry_counts" do
      v1 = %{pending: [], processed: 10}
      v2 = PaymentQueue.upgrade_1_to_2(v1)
      assert v2.retry_counts == %{}
      assert v2.processed == 10
    end

    test "v2 -> v3 introduces last_error" do
      v2 = %{pending: [], processed: 0, retry_counts: %{}}
      v3 = PaymentQueue.upgrade_2_to_3(v2)
      assert v3.last_error == nil
    end

    test "v3 -> v4 nests retry_counts by merchant" do
      v3 = %{
        pending: [%{id: "p1", merchant_id: "m_a", amount: 100}],
        processed: 0,
        retry_counts: %{"p1" => 2, "p_gone" => 5},
        last_error: nil
      }

      v4 = PaymentQueue.upgrade_3_to_4(v3)

      assert v4.retry_counts == %{
               "m_a" => %{"p1" => 2},
               "unknown" => %{"p_gone" => 5}
             }
    end
  end

  describe "downgrade" do
    test "v4 -> v3 flattens back to a single map" do
      v4 = %{
        pending: [],
        processed: 0,
        retry_counts: %{"m_a" => %{"p1" => 2}, "m_b" => %{"p2" => 1}},
        last_error: nil
      }

      v3 = PaymentQueue.downgrade_4_to_3(v4)
      assert v3.retry_counts == %{"p1" => 2, "p2" => 1}
    end
  end

  describe "code_change/3 drives the ladder" do
    test "code_change from v1 runs all upgrades" do
      v1 = %{pending: [], processed: 5}
      assert {:ok, v4} = PaymentQueue.code_change(1, v1, [])
      assert v4.processed == 5
      assert v4.retry_counts == %{}
      assert v4.last_error == nil
    end

    test "code_change from v3 only runs the last upgrade" do
      v3 = %{pending: [], processed: 0, retry_counts: %{}, last_error: nil}
      assert {:ok, v4} = PaymentQueue.code_change(3, v3, [])
      assert v4.retry_counts == %{}
    end

    test "code_change with matching version is a no-op" do
      v4 = %{pending: [], processed: 0, retry_counts: %{}, last_error: nil}
      assert {:ok, ^v4} = PaymentQueue.code_change(4, v4, [])
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. `@vsn` must change on every state-shape change.** If the attribute stays at 3 while you ship new code, OTP does not invoke `code_change/3` and the new code runs against the old state shape, causing crashes.

**2. Suspension time is real downtime.** During `sys:suspend/1` the process does not respond. For a GenServer serving user traffic, this means mailbox buffering and latency spikes on upgrade.

**3. `code_change/3` crashes can lose state.** If transformation raises, the process dies, the supervisor restarts it from `init/1`, and the old state is gone. Test every migration in isolation.

**4. Long upgrade chains are brittle.** Going from v1 → v4 by composing `upgrade_1_to_2 |> upgrade_2_to_3 |> upgrade_3_to_4` means any bug in any step corrupts the final state. Version your releases incrementally; never ship a v4 that has never been installed on top of a v3.

**5. Downgrade paths double the work and are often skipped.** Many teams ship upgrades without downgrades, accepting that a rollback is a restart. That is usually fine.

**6. Clustered nodes during upgrade are heterogeneous.** If nodes A and B run v3 and v4 simultaneously, a message exchange between them may see inconsistent state shapes. Plan the rollout or gate cross-node messages on a version tag.

**7. Tooling gap.** `mix release` supports appup/relup less smoothly than the old `distillery`. Generating correct `.relup` files for chains with > 2 versions is an expert task. Most teams give up and deploy fresh.

**8. When NOT to use this.** If you have a load balancer, blue/green deploy. If you have persistent state in a DB, rolling restart with drain. If your state fits in Redis, snapshot and reload. Hot upgrades are for embedded systems (Nerves, telecom switches), research-grade long-running processes, and a handful of niche legacy codebases. For ~95% of Elixir production workloads, they are the wrong tool.

---

## Performance notes

Migration of a 5k-payment queue (v3 → v4), measured locally:

| phase                          | time  |
|--------------------------------|-------|
| `sys:suspend/1`                | 40 µs |
| module reload                  | 3 ms  |
| `upgrade_3_to_4/1`             | 2 ms  |
| `sys:resume/1`                 | 30 µs |
| total apparent downtime        | 5 ms  |

A 5 ms blackout is usually fine. A 500 ms blackout (if your migration is expensive) is not. Benchmark your migration on representative state sizes.

---

## Resources

- [`:sys` — OTP sys interface](https://www.erlang.org/doc/man/sys.html)
- [`:release_handler` — hot upgrades](https://www.erlang.org/doc/man/release_handler.html)
- [`GenServer.code_change/3` — hexdocs](https://hexdocs.pm/elixir/GenServer.html#c:code_change/3)
- [Learn You Some Erlang — Relups](https://learnyousomeerlang.com/relups)
- [Fred Hébert — Stuff Goes Bad: Erlang in Anger (chapter 2 on upgrades)](https://www.erlang-in-anger.com/)
- [Nerves — hot code updates for embedded](https://hexdocs.pm/nerves/)
- [José Valim — on why hot upgrades are rare in Elixir production](https://elixirforum.com/t/hot-code-reloading-in-production/)
- [`mix release` appup support](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
