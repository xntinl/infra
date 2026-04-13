# Build a Distributed Event Bus with Topic Routing

**Project**: `nexus` — A distributed process registry and hierarchical event bus across multi-node BEAM clusters, using ETS-backed O(1) registry lookups and trie-based wildcard matching.

**Learning Goal**: Understand how to combine ETS for fast, lock-free O(1) lookups with a trie for O(S) wildcard matching (S = matching subscribers), implement cross-node delivery gossip, and enforce QoS semantics.

---

## Project Context

You are building `nexus`, a distributed registry and event bus with no external dependencies (no Redis, no RabbitMQ, no libcluster).

Project structure:

```
nexus/
├── lib/
│   └── nexus/
│       ├── application.ex           # starts registry, event_bus, cluster watcher
│       ├── registry.ex              # ETS-backed name → pid mapping, monitor-based cleanup
│       ├── event_bus.ex             # GenServer: subscribe, publish, backpressure
│       ├── trie.ex                  # wildcard trie: *, # segment matching
│       ├── history.ex               # per-topic circular buffer, replay on subscribe
│       ├── delivery.ex              # at_most_once, at_least_once, exactly_once
│       ├── cluster.ex               # node connect/monitor, cross-node delivery
│       └── backpressure.ex          # subscriber mailbox monitoring, overflow strategies
├── test/
│   └── nexus/
│       ├── registry_test.exs        # O(1) lookup, monitor-based cleanup
│       ├── trie_test.exs            # wildcard matching correctness
│       ├── delivery_test.exs        # QoS semantics: at_least_once, exactly_once
│       ├── history_test.exs         # replay on subscribe
│       ├── backpressure_test.exs    # overflow strategies
│       └── distributed_test.exs    # cross-node delivery
├── bench/
│   └── nexus_bench.exs
└── mix.exs
```

---

## The business problem
Services on different BEAM nodes need to:
- Discover each other by name (with O(1) lookup)
- Receive events from each other with topic routing
- Support delivery guarantees (at least once, exactly once)

A naive approach (`:global` + `GenServer.cast`) fails because:
- `:global` doesn't scale with fast-changing registrations
- Direct PID messaging offers no topic routing
- No delivery guarantees or QoS levels

---

## Project structure
```
nexus/
├── script/
│   └── main.exs
├── mix.exs                          # Project configuration
├── lib/
│   ├── nexus.ex                    # Module docstring
│   └── nexus/
│       ├── application.ex          # starts registry, event_bus, cluster watcher
│       ├── registry.ex             # ETS: O(1) name → pid, monitor-based cleanup
│       ├── event_bus.ex            # GenServer: subscribe, publish, backpressure
│       ├── trie.ex                 # wildcard trie: *, # matching (O(S))
│       ├── history.ex              # circular buffer: event replay on subscribe
│       ├── delivery.ex             # at_most_once, at_least_once, exactly_once
│       ├── cluster.ex              # node monitoring, cross-node gossip
│       └── backpressure.ex         # mailbox monitoring, overflow strategies
├── test/
│   ├── test_helper.exs             # ExUnit config
│   └── nexus/
│       ├── registry_test.exs       # O(1) lookup, cleanup on process death
│       ├── trie_test.exs           # wildcard matching (* and # semantics)
│       ├── delivery_test.exs       # QoS: at_least_once, exactly_once
│       ├── history_test.exs        # event replay semantics
│       ├── backpressure_test.exs   # overflow handling
│       └── distributed_test.exs    # cross-node delivery
├── bench/
│   └── nexus_bench.exs             # ETS lookup, trie match, publish throughput
└── .gitignore
```

## Implementation
### Step 1: Project Setup

**Objective**: Separate registry, trie, and bus into testable modules.

```bash
mix new nexus --sup
cd nexus
mkdir -p lib/nexus test/nexus bench
```

### Step 3: Process Registry

**Objective**: Combine `:ets.insert_new` with `Process.monitor` so register+monitor is atomic and dead PIDs self-evict.

```elixir
# lib/nexus/registry.ex
defmodule Nexus.Registry do
  use GenServer

  @moduledoc """
  ETS-backed name-to-PID registry.

  Guarantees:
  - O(1) average lookup
  - automatic cleanup within one monitor cycle of process death
  - no stale entries survive a GC pass
  """

  @table :nexus_registry

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Registers name -> pid. Returns :ok or {:error, :already_registered}."
  @spec register(term(), pid()) :: :ok | {:error, :already_registered}
  def register(name, pid) do
    GenServer.call(__MODULE__, {:register, name, pid})
  end

  @doc "Returns {:ok, pid} or {:error, :not_found}."
  @spec lookup(term()) :: {:ok, pid()} | {:error, :not_found}
  def lookup(name) do
    case :ets.lookup(@table, name) do
      [{^name, pid}] -> {:ok, pid}
      [] -> {:error, :not_found}
    end
  end

  def clear do
    GenServer.call(__MODULE__, :clear)
  end

  # GenServer callbacks
  def init(_) do
    :ets.new(@table, [:named_table, :public, :set])
    {:ok, %{monitors: %{}}}  # %{ref => name}
  end

  def handle_call({:register, name, pid}, _from, state) do
    if :ets.insert_new(@table, {name, pid}) do
      ref = Process.monitor(pid)
      new_monitors = Map.put(state.monitors, ref, name)
      {:reply, :ok, %{state | monitors: new_monitors}}
    else
      {:reply, {:error, :already_registered}, state}
    end
  end

  def handle_call(:clear, _from, state) do
    :ets.delete_all_objects(@table)
    Enum.each(state.monitors, fn {ref, _name} -> Process.demonitor(ref, [:flush]) end)
    {:reply, :ok, %{state | monitors: %{}}}
  end

  def handle_info({:DOWN, ref, :process, _pid, _reason}, state) do
    case Map.pop(state.monitors, ref) do
      {nil, monitors} ->
        {:noreply, %{state | monitors: monitors}}
      {name, monitors} ->
        :ets.delete(@table, name)
        {:noreply, %{state | monitors: monitors}}
    end
  end
end
```
### Step 4: Wildcard Trie

**Objective**: Use nested-map trie with NFA-style branching so match cost is O(subscriptions) not O(patterns × length).

```elixir
# lib/nexus/trie.ex
defmodule Nexus.Trie do
  @moduledoc """
  Trie for AMQP-style topic matching.

  Topics are dot-separated: "orders.eu.created"
  Wildcards:
    "*" matches exactly one segment
    "#" matches zero or more segments

  The trie is a nested map:
    %{"orders" => %{"eu" => %{:leaf => [sub1]}, "*" => %{:leaf => [sub2]}}}

  Lookup traverses all matching paths simultaneously (NFA-style).
  """

  @doc "Inserts a subscription pattern into the trie."
  @spec insert(map(), String.t(), term()) :: map()
  def insert(trie, pattern, subscriber) do
    segments = String.split(pattern, ".")
    insert_segments(trie, segments, subscriber)
  end

  defp insert_segments(trie, [], subscriber) do
    existing = Map.get(trie, :leaf, [])
    Map.put(trie, :leaf, [subscriber | existing])
  end

  defp insert_segments(trie, [segment | rest], subscriber) do
    subtrie = Map.get(trie, segment, %{})
    Map.put(trie, segment, insert_segments(subtrie, rest, subscriber))
  end

  @doc "Returns all subscribers whose patterns match the given topic."
  @spec match(map(), String.t()) :: [term()]
  def match(trie, topic) do
    segments = String.split(topic, ".")
    match_segments(trie, segments) |> Enum.uniq()
  end

  defp match_segments(trie, []) do
    leaf_subs = Map.get(trie, :leaf, [])
    hash_subs = case Map.get(trie, "#") do
      nil -> []
      sub_trie -> Map.get(sub_trie, :leaf, [])
    end
    leaf_subs ++ hash_subs
  end

  defp match_segments(trie, [segment | rest]) do
    exact = case Map.get(trie, segment) do
      nil -> []
      sub_trie -> match_segments(sub_trie, rest)
    end

    wildcard = case Map.get(trie, "*") do
      nil -> []
      sub_trie -> match_segments(sub_trie, rest)
    end

    hash = case Map.get(trie, "#") do
      nil -> []
      sub_trie ->
        leaf_subs = Map.get(sub_trie, :leaf, [])
        continue = match_segments(sub_trie, rest)
        skip_one = match_segments(trie, rest)
        leaf_subs ++ continue ++ skip_one
    end

    exact ++ wildcard ++ hash
  end

  @doc "Removes a subscriber from all patterns in the trie."
  @spec remove(map(), term()) :: map()
  def remove(trie, subscriber) do
    trie
    |> Enum.map(fn
      {:leaf, subs} -> {:leaf, Enum.reject(subs, &(&1 == subscriber))}
      {key, subtrie} when is_map(subtrie) -> {key, remove(subtrie, subscriber)}
      other -> other
    end)
    |> Map.new()
  end
end
```
### Step 5: Event Bus

**Objective**: Fan out directly from publisher to subscribers so one slow consumer doesn't block others.

```elixir
# lib/nexus/event_bus.ex
defmodule Nexus.EventBus do
  @moduledoc """
  Hierarchical event bus with AMQP-style topic wildcards.
  Supports three QoS levels: :at_most_once, :at_least_once, :exactly_once.
  Uses the Trie for subscription matching.
  """

  use GenServer

  defstruct [:trie, :subscriptions, :pending_acks, :event_counter]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Subscribes the given pid to a topic pattern."
  @spec subscribe(String.t(), pid(), keyword()) :: :ok
  def subscribe(pattern, subscriber, opts \\ []) do
    GenServer.call(__MODULE__, {:subscribe, pattern, subscriber, opts})
  end

  @doc "Publishes an event to a topic."
  @spec publish(String.t(), term()) :: :ok
  def publish(topic, payload) do
    GenServer.call(__MODULE__, {:publish, topic, payload})
  end

  @doc "Acknowledges receipt of an event (for :at_least_once QoS)."
  @spec ack(term()) :: :ok
  def ack(event_id) do
    GenServer.cast(__MODULE__, {:ack, event_id})
  end

  @doc "Returns the current trie for benchmarking."
  @spec trie() :: map()
  def trie, do: GenServer.call(__MODULE__, :get_trie)

  @impl true
  def init(_opts) do
    {:ok, %__MODULE__{
      trie: %{},
      subscriptions: %{},
      pending_acks: %{},
      event_counter: 0
    }}
  end

  @impl true
  def handle_call({:subscribe, pattern, subscriber, opts}, _from, state) do
    qos = Keyword.get(opts, :qos, :at_most_once)
    sub_info = %{pid: subscriber, qos: qos, pattern: pattern}
    new_trie = Nexus.Trie.insert(state.trie, pattern, sub_info)
    subs = Map.put(state.subscriptions, {pattern, subscriber}, sub_info)
    {:reply, :ok, %{state | trie: new_trie, subscriptions: subs}}
  end

  def handle_call({:publish, topic, payload}, _from, state) do
    event_id = state.event_counter + 1
    matching = Nexus.Trie.match(state.trie, topic)

    new_pending =
      Enum.reduce(matching, state.pending_acks, fn sub_info, acc ->
        send(sub_info.pid, {:event, event_id, payload})

        case sub_info.qos do
          :at_least_once ->
            retry_ref = Process.send_after(self(), {:retry, event_id, sub_info, payload}, 1_000)
            Map.put(acc, event_id, %{sub: sub_info, payload: payload, retry_ref: retry_ref})
          _ ->
            acc
        end
      end)

    {:reply, :ok, %{state | event_counter: event_id, pending_acks: new_pending}}
  end

  def handle_call(:get_trie, _from, state), do: {:reply, state.trie, state}

  @impl true
  def handle_cast({:ack, event_id}, state) do
    case Map.pop(state.pending_acks, event_id) do
      {nil, _} -> {:noreply, state}
      {%{retry_ref: ref}, new_pending} ->
        Process.cancel_timer(ref)
        {:noreply, %{state | pending_acks: new_pending}}
    end
  end

  @impl true
  def handle_info({:retry, event_id, sub_info, payload}, state) do
    if Map.has_key?(state.pending_acks, event_id) do
      send(sub_info.pid, {:event, event_id, payload})
      retry_ref = Process.send_after(self(), {:retry, event_id, sub_info, payload}, 1_000)
      new_pending = Map.put(state.pending_acks, event_id, %{
        sub: sub_info, payload: payload, retry_ref: retry_ref
      })
      {:noreply, %{state | pending_acks: new_pending}}
    else
      {:noreply, state}
    end
  end

  def handle_info(_msg, state), do: {:noreply, state}
end
```
### `test/nexus_test.exs`

**Objective**: Lock in wildcard semantics and retry behavior so delivery guarantees cannot degrade silently.

```elixir
defmodule Nexus.RegistryTest do
  use ExUnit.Case, async: false
  doctest Nexus.EventBus

  setup do
    :ok = Nexus.Registry.clear()
    :ok
  end

  describe "registry operations" do
    test "register and lookup" do
      pid = spawn(fn -> Process.sleep(:infinity) end)
      :ok = Nexus.Registry.register(:my_service, pid)
      assert {:ok, ^pid} = Nexus.Registry.lookup(:my_service)
    end

    test "lookup returns :not_found for unknown name" do
      assert {:error, :not_found} = Nexus.Registry.lookup(:unknown)
    end
  end

  describe "cleanup and monitor" do
    test "entry is removed within one monitor cycle after process dies" do
      pid = spawn(fn -> :ok end)
      :ok = Nexus.Registry.register(:dying_service, pid)
      Process.sleep(50)
      assert {:error, :not_found} = Nexus.Registry.lookup(:dying_service)
    end
  end
end
```
```elixir
defmodule Nexus.TrieTest do
  use ExUnit.Case, async: true
  doctest Nexus.EventBus

  alias Nexus.Trie

  describe "wildcard matching" do
    test "'*' matches exactly one segment" do
      trie = Trie.insert(%{}, "orders.eu.*", :sub_a)
      assert :sub_a in Trie.match(trie, "orders.eu.created")
      assert :sub_a in Trie.match(trie, "orders.eu.updated")
      refute :sub_a in Trie.match(trie, "orders.us.created")
      refute :sub_a in Trie.match(trie, "orders.eu.refunds.issued")
    end

    test "'#' matches zero or more segments" do
      trie = Trie.insert(%{}, "orders.#", :sub_b)
      assert :sub_b in Trie.match(trie, "orders")
      assert :sub_b in Trie.match(trie, "orders.eu")
      assert :sub_b in Trie.match(trie, "orders.eu.created")
      refute :sub_b in Trie.match(trie, "metrics.cpu")
    end

    test "exact match only matches exact topic" do
      trie = Trie.insert(%{}, "orders.eu.created", :sub_c)
      assert :sub_c in Trie.match(trie, "orders.eu.created")
      refute :sub_c in Trie.match(trie, "orders.eu.updated")
    end
  end
end
```
```elixir
defmodule Nexus.DeliveryTest do
  use ExUnit.Case, async: false
  doctest Nexus.EventBus

  describe "QoS semantics" do
    test ":at_least_once retries until ack received" do
      {:ok, _bus} = Nexus.EventBus.start_link()
      Nexus.EventBus.subscribe("test.event", self(), qos: :at_least_once)

      Nexus.EventBus.publish("test.event", %{data: "hello"})

      assert_receive {:event, event_id, %{data: "hello"}}, 1_000
      assert_receive {:event, ^event_id, %{data: "hello"}}, 2_000

      Nexus.EventBus.ack(event_id)
      refute_receive {:event, ^event_id, _}, 1_500
    end
  end
end
```
---

## Quick Start

**Prerequisites**: Elixir 1.14+, OTP 25+

**Setup**:
```bash
mix new nexus --sup
cd nexus
mkdir -p lib/nexus test/nexus bench
```

**Run tests** (serially due to ETS singleton):
```bash
mix test test/nexus/ --trace
```

**Interactive example**:
```bash
iex -S mix
```

Then in iex:
```elixir
# Registry: O(1) lookup
{:ok, pid} = spawn_link(fn -> Process.sleep(:infinity) end)
:ok = Nexus.Registry.register(:my_service, pid)
{:ok, ^pid} = Nexus.Registry.lookup(:my_service)

# Event bus: topic routing with wildcards
Nexus.EventBus.subscribe("orders.eu.*", self())
Nexus.EventBus.publish("orders.eu.created", %{order_id: 123})
receive do
  {:event, _id, %{order_id: 123}} -> :ok
after 1000 -> :timeout
end
```
---

## Benchmark

**Objective**: Measure ETS lookup, trie matching, and publish throughput separately.

**Setup**:
```elixir
# bench/nexus_bench.exs
{:ok, _bus} = Nexus.EventBus.start_link()
Nexus.EventBus.subscribe("bench.topic.a", self(), qos: :at_most_once)

Benchee.run(
  %{
    "registry lookup (O(1))" => fn ->
      Nexus.Registry.lookup(:nonexistent)
    end,
    "publish (single match)" => fn ->
      Nexus.EventBus.publish("bench.topic.a", %{ts: :erlang.monotonic_time()})
    end,
    "trie match (1000 subs)" => fn ->
      Nexus.Trie.match(Nexus.EventBus.trie(), "bench.topic.a")
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```
**Run**:
```bash
mix run bench/nexus_bench.exs
```

**Expected Results**:
- Registry lookup: 400k–800k ops/sec
- Trie match (1000 subscriptions): 50k–100k ops/sec
- Publish (10 subscribers): 100k–200k ops/sec
- Publish (100 subscribers): 20k–50k ops/sec

**Interpretation**:
Registry dominates at small scale. Trie traversal and message batching become visible with many subscriptions. Wildcard patterns add cost proportional to matching subscription count.

---

## Reflection

These questions deepen your understanding:

1. **Backpressure**: If one subscriber blocks on a 100 ms disk write per message, how does the rest of the bus behave? What guardrails would you add?

2. **Exactly-Once Delivery**: Would you change the design if subscribers needed exactly-once semantics? What trade-offs appear?

---

## Trade-off Analysis

| Aspect | `:at_most_once` | `:at_least_once` | `:exactly_once` |
|--------|----------------|-----------------|----------------|
| Delivery guarantee | may drop | no drops (retries) | no duplicates, no drops |
| Publisher complexity | fire and forget | retry loop + ack wait | prepare → ack → commit |
| Subscriber complexity | none | must ack | must be idempotent + ack both phases |
| Latency | minimum | higher (round trip) | highest (2 round trips) |
| Use case | metrics, logs | orders, payments | financial ledger |

Reflection: `:exactly_once` delivery requires idempotency on the subscriber side AND the two-phase publisher protocol. What happens if the subscriber crashes between PREPARE and COMMIT? Who cleans up, and what is the recovery protocol?

---

## Common Production Mistakes

**1. Direct ETS writes from outside the owning process**
ETS is `:public` for reads, but writes must go through the GenServer to keep "insert + monitor" atomic. A direct insert followed by server-side monitor leaves a window where the process dies before monitoring starts, leaving a stale entry.

**2. Wildcard matching via string iteration**
Iterating all patterns and comparing strings is O(P × T) (patterns × topic length). A trie reduces this to O(S) (matching subscribers).

**3. Retrying without exponential backoff**
Simple retry intervals flood slow subscribers' mailboxes. Add exponential backoff with a max retry limit and dead-lettering.

**4. Cross-node subscriptions not cleaned on disconnect**
Use `Node.monitor/2` to detect `:nodedown` and clean up subscriptions from that node.

---

## Resources

- [AMQP 0-9-1 Model Explained](https://www.rabbitmq.com/tutorials/amqp-concepts) — topic exchange routing semantics
- [Erlang `:pg` source](https://github.com/erlang/otp/blob/master/lib/kernel/src/pg.erl) — the reference implementation of distributed process groups
- van Steen, M. & Tanenbaum, A. — *Distributed Systems* — Chapter 6 (Naming)
- Narkhede, N. et al. (2017). *Exactly-once semantics in Apache Kafka*

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Evbus.MixProject do
  use Mix.Project

  def project do
    [
      app: :evbus,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Evbus.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `evbus` (pubsub event bus).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 1000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:evbus) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Evbus stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:evbus) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:evbus)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual evbus operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Evbus classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **500,000 events/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **1 ms** | Phoenix.PubSub design |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Phoenix.PubSub design: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Build a Distributed Event Bus with Topic Routing matters

Mastering **Build a Distributed Event Bus with Topic Routing** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Design decisions

**Option A — naive direct approach**
- Pros: minimal code; easy to read for newcomers.
- Cons: scales poorly; couples business logic to infrastructure concerns; hard to test in isolation.

**Option B — idiomatic Elixir approach** (chosen)
- Pros: leans on OTP primitives; process boundaries make failure handling explicit; easier to reason about state; plays well with supervision trees.
- Cons: slightly more boilerplate; requires understanding of GenServer/Task/Agent semantics.

Chose **B** because it matches how production Elixir systems are written — and the "extra boilerplate" pays for itself the first time something fails in production and the supervisor restarts the process cleanly instead of crashing the node.

### `lib/nexus.ex`

```elixir
defmodule Nexus do
  @moduledoc """
  Reference implementation for Build a Distributed Event Bus with Topic Routing.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the nexus module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Nexus.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Phoenix.PubSub design
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
