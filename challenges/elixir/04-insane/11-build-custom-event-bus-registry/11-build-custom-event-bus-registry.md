# Custom Distributed Event Bus and Registry

**Project**: `nexus` — a distributed process registry and hierarchical event bus across BEAM nodes

---

## Project context

You are building `nexus`, a distributed process registry and hierarchical event bus that operates across a multi-node cluster without any external dependencies. No Redis, no RabbitMQ, no libcluster. The registry maps names to PIDs; the event bus routes events using AMQP-style topic wildcards.

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

## The problem

Services on different BEAM nodes need to discover each other by name and receive events from each other. A naive approach uses `:global` for registration and `GenServer.cast` for events. The problem: `:global` does not scale to fast-changing registrations, and direct PID messaging does not support topic routing or delivery guarantees.

The registry must have O(1) lookup and automatic cleanup when processes die. The event bus must support topic hierarchies with wildcards so consumers can subscribe to broad patterns without enumerating every publisher. Cross-node delivery must work without manual routing.

---

## Why this design

**ETS for the registry**: `:ets.lookup/2` is O(1) average time with concurrent reads. `Process.monitor/1` triggers cleanup automatically when a process dies — no polling, no TTL. This is the same pattern used by Elixir's built-in `Registry`.

**Trie for wildcard routing**: a trie where each path segment is a node key makes wildcard matching O(S) where S is the number of active subscriptions, not O(T) where T is the total length of all topic strings. `"*"` is a special trie node that matches any single segment; `"#"` matches zero or more segments. Matching walks the trie, branching at wildcards.

**QoS levels as delivery semantics**: `:at_most_once` is fire-and-forget — no retry, no confirmation. `:at_least_once` retries until the subscriber acknowledges with `{:ack, event_id}`. `:exactly_once` is two-phase: publisher sends PREPARE, subscriber acks, publisher sends COMMIT. These map directly to AMQP's QoS levels.

**Cross-node delivery via `:pg`-inspired gossip**: when a node subscribes, its subscription is gossiped to all cluster members. When an event is published, the publisher routes it to every node that has a matching subscriber. This avoids a central routing node.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new nexus --sup
cd nexus
mkdir -p lib/nexus test/nexus bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: Process registry

```elixir
# lib/nexus/registry.ex
defmodule Nexus.Registry do
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

### Step 4: Wildcard trie

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

### Step 5: Given tests — must pass without modification

```elixir
# test/nexus/registry_test.exs
defmodule Nexus.RegistryTest do
  use ExUnit.Case, async: false

  setup do
    :ok = Nexus.Registry.clear()
    :ok
  end

  test "register and lookup" do
    pid = spawn(fn -> Process.sleep(:infinity) end)
    :ok = Nexus.Registry.register(:my_service, pid)
    assert {:ok, ^pid} = Nexus.Registry.lookup(:my_service)
  end

  test "lookup returns :not_found for unknown name" do
    assert {:error, :not_found} = Nexus.Registry.lookup(:unknown)
  end

  test "entry is removed within one monitor cycle after process dies" do
    pid = spawn(fn -> :ok end)
    :ok = Nexus.Registry.register(:dying_service, pid)
    Process.sleep(50)  # let the process die and monitor fire
    assert {:error, :not_found} = Nexus.Registry.lookup(:dying_service)
  end
end
```

```elixir
# test/nexus/trie_test.exs
defmodule Nexus.TrieTest do
  use ExUnit.Case, async: true

  alias Nexus.Trie

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
```

```elixir
# test/nexus/delivery_test.exs
defmodule Nexus.DeliveryTest do
  use ExUnit.Case, async: false

  test ":at_least_once retries until ack received" do
    {:ok, _bus} = Nexus.EventBus.start_link()
    Nexus.EventBus.subscribe("test.event", self(), qos: :at_least_once)

    Nexus.EventBus.publish("test.event", %{data: "hello"})

    assert_receive {:event, event_id, %{data: "hello"}}, 1_000

    # Don't ack yet — should receive retry
    assert_receive {:event, ^event_id, %{data: "hello"}}, 2_000

    # Ack — should stop retrying
    Nexus.EventBus.ack(event_id)
    refute_receive {:event, ^event_id, _}, 1_500
  end
end
```

### Step 6: Run the tests

```bash
mix test test/nexus/ --trace
```

### Step 7: Benchmark

```elixir
# bench/nexus_bench.exs
{:ok, _} = Nexus.EventBus.start_link()
Nexus.EventBus.subscribe("bench.topic.a", self(), qos: :at_most_once)

Benchee.run(
  %{
    "registry lookup — O(1)" => fn ->
      Nexus.Registry.lookup(:nonexistent)
    end,
    "publish — single exact match" => fn ->
      Nexus.EventBus.publish("bench.topic.a", %{ts: :erlang.monotonic_time()})
    end,
    "trie match — 1000 subscriptions" => fn ->
      Nexus.Trie.match(Nexus.EventBus.trie(), "bench.topic.a")
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | `:at_most_once` | `:at_least_once` | `:exactly_once` |
|--------|----------------|-----------------|----------------|
| Delivery guarantee | may drop | no drops (retries) | no duplicates, no drops |
| Publisher complexity | fire and forget | retry loop + ack wait | prepare → ack → commit |
| Subscriber complexity | none | must ack | must be idempotent + ack both phases |
| Latency | minimum | higher (round trip) | highest (2 round trips) |
| Use case | metrics, logs | orders, payments | financial ledger |

Reflection: `:exactly_once` delivery requires idempotency on the subscriber side AND the two-phase publisher protocol. What happens if the subscriber crashes between PREPARE and COMMIT? Who cleans up, and what is the recovery protocol?

---

## Common production mistakes

**1. Looking up in ETS from outside the owning process in a write-heavy scenario**
The ETS table is `:public` for concurrent reads. But writes to the registry go through the GenServer to guarantee atomicity of "check-and-insert + start-monitor". If you insert directly from the caller and monitor from the server, there is a window where the process can die between insert and monitor start, leaving a stale entry.

**2. Wildcard matching via string comparison**
Iterating all subscription patterns and doing string comparison for every publish is O(P × T) where P is patterns and T is topic length. The trie reduces this to O(S) where S is the number of matching subscribers.

**3. Retrying `:at_least_once` without exponential backoff**
A subscriber that is slow or crashed causes the publisher to retry at the configured interval, flooding the subscriber's mailbox. Use exponential backoff with a maximum retry limit before giving up and dead-lettering.

**4. Cross-node subscriptions not cleaned up on node disconnect**
When a remote node disconnects, subscriptions registered by processes on that node must be removed. Monitor the node with `Node.monitor/2` and clean up on `:nodedown`.

---

## Resources

- [AMQP 0-9-1 Model Explained](https://www.rabbitmq.com/tutorials/amqp-concepts) — topic exchange routing semantics
- [Erlang `:pg` source](https://github.com/erlang/otp/blob/master/lib/kernel/src/pg.erl) — the reference implementation of distributed process groups
- van Steen, M. & Tanenbaum, A. — *Distributed Systems* — Chapter 6 (Naming)
- Narkhede, N. et al. (2017). *Exactly-once semantics in Apache Kafka*
