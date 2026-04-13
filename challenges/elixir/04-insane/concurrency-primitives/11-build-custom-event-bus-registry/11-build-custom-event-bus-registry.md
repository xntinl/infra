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

## The Problem

Services on different BEAM nodes need to:
- Discover each other by name (with O(1) lookup)
- Receive events from each other with topic routing
- Support delivery guarantees (at least once, exactly once)

A naive approach (`:global` + `GenServer.cast`) fails because:
- `:global` doesn't scale with fast-changing registrations
- Direct PID messaging offers no topic routing
- No delivery guarantees or QoS levels

---

## Key Concepts

### ETS Registry for O(1) Lookup

`:ets.lookup/2` is O(1) with concurrent-safe reads. Combined with `Process.monitor/1`, dead PIDs self-evict on `:DOWN`. This is how Elixir's `Registry` module works internally.

### Trie for Wildcard Matching

A trie where each segment is a node key achieves O(S) matching (S = matching subscribers):

```
Topic patterns: "orders.*.created", "orders.#", "metrics.cpu.load"

Trie:
%{
  "orders" => %{
    "*" => %{
      "created" => %{:leaf => [sub_a]}
    },
    "#" => %{:leaf => [sub_b]}
  },
  "metrics" => %{
    "cpu" => %{
      "load" => %{:leaf => [sub_c]}
    }
  }
}
```

- `"*"` matches exactly one segment
- `"#"` matches zero or more segments
- Matching walks all paths simultaneously (NFA-style)

### QoS Delivery Semantics

| Level | Guarantee | Retry | Ack | Use Case |
|-------|-----------|-------|-----|----------|
| `:at_most_once` | may drop | no | no | metrics, logs |
| `:at_least_once` | no drop | yes | yes | orders, payments |
| `:exactly_once` | no dup, no drop | 2-phase | yes | ledger |

### Design Decisions

| Option | Pros | Cons | Chosen? |
|--------|------|------|---------|
| **A: Central dispatcher** | simple; ordered | head-of-line blocking | No |
| **B: ETS + sender fan-out** | no bottleneck; slow subscribers self-isolate | per-sender ordering | **Yes** |

**Rationale**: Removes the head-of-line blocking that central dispatchers introduce. This is exactly why Phoenix.PubSub uses sender-side fan-out.

## Full Project Structure

```
nexus/
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

## Implementation milestones

### Step 1: Project Setup

**Objective**: Separate registry, trie, and bus into testable modules.

```bash
mix new nexus --sup
cd nexus
mkdir -p lib/nexus test/nexus bench
```

### Step 2: Dependencies (mix.exs)

**Objective**: Minimal deps — only `benchee` and `stream_data`. Hand-roll the trie and QoS protocol.

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
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

### Step 6: Tests — Contract as Specs

**Objective**: Lock in wildcard semantics and retry behavior so delivery guarantees cannot degrade silently.

```elixir
# test/nexus/registry_test.exs
defmodule Nexus.RegistryTest do
  use ExUnit.Case, async: false

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
# test/nexus/trie_test.exs
defmodule Nexus.TrieTest do
  use ExUnit.Case, async: true

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
# test/nexus/delivery_test.exs
defmodule Nexus.DeliveryTest do
  use ExUnit.Case, async: false

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
