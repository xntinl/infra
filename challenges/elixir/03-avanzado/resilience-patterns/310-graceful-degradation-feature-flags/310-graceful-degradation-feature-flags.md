# Graceful Degradation with Feature Flags

**Project**: `checkout_degrader` — feature flags that let the checkout flow disable non-critical features (recommendations, loyalty-point accrual, fraud scoring) on demand so the core purchase path survives upstream outages.

## Project context

Your checkout calls five services: payments (critical), fraud scoring (slow but important), recommendations (nice to have), loyalty-point accrual (async, optional), and analytics emission (best-effort). When fraud scoring degrades, the whole checkout times out even though the payment itself would succeed.

Graceful degradation means the system *continues operating with reduced functionality* when dependencies fail, rather than failing entirely. Feature flags let operators toggle individual features off within seconds — without a deploy.

This exercise builds runtime-toggleable flags backed by ETS, evaluated in O(1), with a `with_feature/3` macro that inlines the check and provides a fallback.

```
checkout_degrader/
├── lib/
│   └── checkout_degrader/
│       ├── application.ex
│       ├── flags.ex                # public API + ETS owner
│       └── checkout.ex             # example use site
├── test/
│   └── checkout_degrader/
│       └── flags_test.exs
└── mix.exs
```

## Why feature flags and not config files

Config files require a deploy to change. A deploy takes 5–30 minutes. An outage takes seconds to escalate. Flags toggle in milliseconds via an admin endpoint, allowing immediate mitigation without code changes.

## Why ETS and not GenServer

Flag checks are on the hot path of every checkout. `:ets.lookup/2` with `read_concurrency: true` is ~200ns. `GenServer.call` is 2-5µs minimum. On a checkout that runs 20 flag checks, the difference is 100µs vs. 4000ns — meaningful.

## Why not Fun Farm / FunWithFlags

`FunWithFlags` is a mature library with percentage rollouts, group actors, and a UI. For this exercise we build a minimal version so you see how the primitive works. In production, prefer FunWithFlags for non-trivial flag systems.

## Core concepts

### 1. Flag evaluation
```
:ets.lookup(:flags_table, {:feature, name}) 
  |> case do
       [{_, true}]  → enabled
       [{_, false}] → disabled
       []           → default (usually :disabled for new flags)
     end
```

### 2. `with_feature/3` — degradation at the call site
```elixir
with_feature :fraud_scoring, fallback: :allow do
  FraudAPI.score(request)
end
```
If the flag is off, `fallback` is returned. If on, the block runs.

### 3. Kill switches vs. flags
- **Kill switch**: on/off. Binary.
- **Percentage rollout**: 0-100% traffic.
- **Tenant flag**: on per customer-id.
We implement kill switches here; the extension paths are obvious.

## Design decisions

- **Option A — Compile-time flags (module attributes)**: zero runtime cost, requires deploy to change.
- **Option B — Runtime flags (ETS)**: tiny runtime cost, toggle without deploy.
→ Chose **B**. The entire point of graceful degradation is runtime control.

- **Option A — Explicit `Flags.enabled?(:name)` everywhere**: maximum flexibility, most verbose.
- **Option B — `with_feature` macro**: less boilerplate at call site, hides the check.
→ Support **both**. The macro is sugar over the primitive.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule CheckoutDegrader.MixProject do
  use Mix.Project
  def project, do: [app: :checkout_degrader, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [mod: {CheckoutDegrader.Application, []}, extra_applications: [:logger]]
end
```

### Step 1: Application

```elixir
defmodule CheckoutDegrader.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [CheckoutDegrader.Flags]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Flags (`lib/checkout_degrader/flags.ex`)

```elixir
defmodule CheckoutDegrader.Flags do
  use GenServer

  @table :checkout_degrader_flags

  # --------------------------------------------------------------------------
  # Public API
  # --------------------------------------------------------------------------

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec enabled?(atom()) :: boolean()
  def enabled?(name) when is_atom(name) do
    case :ets.lookup(@table, name) do
      [{^name, enabled}] -> enabled
      [] -> false
    end
  end

  @spec enable(atom()) :: :ok
  def enable(name), do: GenServer.call(__MODULE__, {:set, name, true})

  @spec disable(atom()) :: :ok
  def disable(name), do: GenServer.call(__MODULE__, {:set, name, false})

  @spec all() :: %{atom() => boolean()}
  def all do
    :ets.tab2list(@table) |> Map.new()
  end

  # --------------------------------------------------------------------------
  # Macro
  # --------------------------------------------------------------------------

  defmacro with_feature(name, opts, do: block) do
    quote do
      if unquote(__MODULE__).enabled?(unquote(name)) do
        unquote(block)
      else
        Keyword.fetch!(unquote(opts), :fallback)
      end
    end
  end

  # --------------------------------------------------------------------------
  # Lifecycle
  # --------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    {:ok, %{}}
  end

  @impl true
  def handle_call({:set, name, value}, _from, state) do
    :ets.insert(@table, {name, value})
    :telemetry.execute([:checkout_degrader, :flag, :set], %{}, %{name: name, value: value})
    {:reply, :ok, state}
  end
end
```

### Step 3: Example use (`lib/checkout_degrader/checkout.ex`)

```elixir
defmodule CheckoutDegrader.Checkout do
  require CheckoutDegrader.Flags
  alias CheckoutDegrader.Flags

  def run(order) do
    with {:ok, _} <- charge_payment(order),
         :ok <- score_fraud(order),
         :ok <- accrue_loyalty(order) do
      emit_analytics(order)
      {:ok, :confirmed}
    end
  end

  defp charge_payment(order), do: {:ok, order}

  defp score_fraud(order) do
    Flags.with_feature :fraud_scoring, fallback: :ok do
      # imagine a real call — may fail
      if order.risk_override, do: :ok, else: :ok
    end
  end

  defp accrue_loyalty(_order) do
    Flags.with_feature :loyalty_accrual, fallback: :ok do
      :ok
    end
  end

  defp emit_analytics(_order) do
    Flags.with_feature :analytics, fallback: :ok do
      :ok
    end
  end
end
```

## Why this works

- **Read path is lock-free** — `:ets.lookup/2` on a `read_concurrency: true` table scales to all schedulers without serialization. A million checkouts per second doing 20 flag reads each = 20M ETS lookups, each ~200ns.
- **Write path is coordinated** — only the GenServer writes. No races; `:telemetry.execute` emits an observable change event.
- **Macro inlines the check** — `with_feature` compiles to a plain `if Flags.enabled?(:name), do: block, else: fallback`. No function-value dispatch on hot path.
- **Unknown flag defaults to disabled** — so a typo in `enabled?(:fruad_scoring)` returns false and disables the block, which is usually safer than silently passing.

## Tests

```elixir
defmodule CheckoutDegrader.FlagsTest do
  use ExUnit.Case, async: false
  alias CheckoutDegrader.{Checkout, Flags}
  require Flags

  setup do
    Flags.disable(:fraud_scoring)
    Flags.disable(:loyalty_accrual)
    Flags.disable(:analytics)
    :ok
  end

  describe "enabled?/1" do
    test "unknown flag returns false" do
      refute Flags.enabled?(:never_seen_before)
    end

    test "enabled after enable/1" do
      Flags.enable(:demo_flag)
      assert Flags.enabled?(:demo_flag)
    end

    test "disabled after disable/1" do
      Flags.enable(:demo_flag)
      Flags.disable(:demo_flag)
      refute Flags.enabled?(:demo_flag)
    end
  end

  describe "with_feature macro" do
    test "evaluates block when enabled" do
      Flags.enable(:block_test)

      result =
        Flags.with_feature :block_test, fallback: :nope do
          :ran
        end

      assert result == :ran
    end

    test "returns fallback when disabled" do
      result =
        Flags.with_feature :block_test, fallback: :nope do
          :ran
        end

      assert result == :nope
    end

    test "does not evaluate block when disabled" do
      Agent.start_link(fn -> false end, name: :side_effect)

      Flags.with_feature :never_enabled, fallback: :ok do
        Agent.update(:side_effect, fn _ -> true end)
        :ok
      end

      refute Agent.get(:side_effect, & &1)
    end
  end

  describe "checkout degradation" do
    test "checkout succeeds with all side features disabled" do
      assert {:ok, :confirmed} = Checkout.run(%{id: 1, risk_override: false})
    end

    test "checkout succeeds when fraud_scoring is enabled" do
      Flags.enable(:fraud_scoring)
      assert {:ok, :confirmed} = Checkout.run(%{id: 1, risk_override: false})
    end
  end
end
```

## Benchmark

```elixir
# Target: flag check < 300ns under load.
{:ok, _} = Application.ensure_all_started(:checkout_degrader)
CheckoutDegrader.Flags.enable(:bench_flag)

{t, _} =
  :timer.tc(fn ->
    for _ <- 1..1_000_000, do: CheckoutDegrader.Flags.enabled?(:bench_flag)
  end)

IO.puts("avg: #{t / 1_000_000} µs per check")
```

Expected: ~0.2µs (200ns) per check.

## Trade-offs and production gotchas

**1. Flag drift** — flags added for a specific incident often outlive the incident by years. Add an expiry date to every flag; review quarterly.

**2. Default to disabled** — a new flag you just added should be off in production until you explicitly enable it. `enabled?` returning false on unknown flags codifies this.

**3. No per-tenant rollout here** — if you need "enable fraud_scoring for 10% of traffic", hash the tenant-id and compare against a bucket. This exercise is binary.

**4. Coupling kills the value** — if `enabled?(:foo)` is called inside a hot loop that runs 10M times in one request, even 200ns adds 2 seconds. Pull the check above the loop.

**5. Flag state loss on restart** — ETS owned by a GenServer dies with it. For durability, persist flag changes to a DB or `:dets` and reload on `init`.

**6. When NOT to use this** — for config (DB URL, API keys), use `Application.get_env` or `System.get_env`. Flags are for *behavior*, not *configuration*.

## Reflection

You disable `:loyalty_accrual` during an outage of the loyalty service. The incident ends but nobody re-enables the flag. Three weeks later an audit finds missing loyalty points for 12M transactions. What operational practice would have caught this?

## Resources

- [FunWithFlags — Elixir library](https://github.com/tompave/fun_with_flags)
- [Feature toggles — Martin Fowler](https://martinfowler.com/articles/feature-toggles.html)
- [LaunchDarkly — commercial flag service](https://launchdarkly.com/)
- [Google SRE book — graceful degradation chapter](https://sre.google/sre-book/addressing-cascading-failures/)
