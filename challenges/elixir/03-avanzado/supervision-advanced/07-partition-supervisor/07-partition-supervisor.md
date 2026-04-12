# PartitionSupervisor: scaling GenServer contention

**Project**: `partition_sup_demo` — use `PartitionSupervisor` (Elixir 1.14+) to remove a single-process bottleneck.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–4 hours

---

## Project context

Your team runs a multi-tenant SaaS. Every HTTP request touches a `UsageCounter` process that increments a per-tenant counter used for billing and soft quotas. The counter is a single named `GenServer` because originally there were five tenants and 20 rps. Today there are 8 000 tenants and 12 000 rps peak. The `GenServer.call` latency distribution now looks like this: p50 1 ms, p95 40 ms, p99 180 ms. A mailbox inspection (`Process.info(pid, :message_queue_len)`) shows 400–2 000 messages during peaks. The single counter process is scheduled on one core; the other 15 cores are idle for this workload.

You can refactor to ETS, but you also want strong per-tenant ordering (monotonic increment semantics) for auditability — some counters participate in rate-limit decisions that require a sequential view. ETS `:update_counter` gives you atomicity but spreads the work across many schedulers in a way that makes it hard to attach per-tenant side effects (flush-to-DB, telemetry aggregation). The right answer is `PartitionSupervisor`: keep the GenServer, but run N of them, each owning a shard of tenant IDs.

`PartitionSupervisor` shipped in Elixir 1.14. Before it, you had to hand-roll a registry of N copies and hash manually. Now you declare `{PartitionSupervisor, child_spec: ..., name: ..., partitions: N}` and route via `{:via, PartitionSupervisor, {name, key}}`.

```
partition_sup_demo/
├── lib/
│   └── partition_sup_demo/
│       ├── application.ex
│       ├── usage_counter.ex
│       └── billing.ex
├── bench/
│   └── contention_bench.exs
└── test/
    └── partition_sup_demo/
        └── usage_counter_test.exs
```

---

## Core concepts

### 1. What `PartitionSupervisor` is (and is not)

It is a `Supervisor` that starts **N copies** of the same child spec, each identified by an integer partition `0..N-1`. That is all. It does NOT do:

- consistent hashing (uses `:erlang.phash2/2` by default — simple and fast, not ring-based)
- dynamic child creation (use `DynamicSupervisor` for that)
- sharding across nodes (single-node; pair with Horde or libcluster for multi-node)
- rebalancing when a partition dies (restarts in place, same partition index)

### 2. Routing a call

```elixir
# Old: single process
GenServer.call(UsageCounter, {:incr, tenant_id})

# New: one of N partitions
GenServer.call(
  {:via, PartitionSupervisor, {UsageCounter.Partitions, tenant_id}},
  {:incr, tenant_id}
)
```

The `{:via, PartitionSupervisor, {name, key}}` tuple resolves to the pid of the partition responsible for `key`. Resolution is `partition_index = :erlang.phash2(key, partitions)` — deterministic, stateless, no lookup.

### 3. Choosing the partition count

The sweet spot is **`System.schedulers_online/0`** or a small multiple (2×, 4×). More partitions do NOT help if there's only one scheduler to run them. Fewer partitions than cores wastes capacity.

```elixir
partitions: System.schedulers_online()   # sensible default
```

For workloads with long sync I/O blocking each partition (DB calls), go higher — 4×–8× schedulers — so other partitions can run while peers wait.

### 4. `:erlang.phash2/2` and hot keys

Routing is per key. If 80 % of your traffic hits `tenant_id = "acme"`, 80 % of your load lands on ONE partition regardless of how many you configured. `PartitionSupervisor` does NOT solve hotspots; it solves *spread*. For skewed workloads, shard by `{tenant_id, request_id}` or by `:rand.uniform(partitions)` for a read-only path.

### 5. Partition death semantics

When partition 3 crashes, `Supervisor` restarts it. During the restart window (typically <1 ms), calls routed to partition 3 fail with `:noproc` or timeout. This is identical to the classic GenServer-dies-during-call race. Keep idempotent ops or add a retry wrapper.

```
                 PartitionSupervisor(name: UsageCounter.Partitions, n=8)
                 /      |      |      |      |      |      |      \
          UC#0   UC#1   UC#2   UC#3   UC#4   UC#5   UC#6   UC#7
          (pids registered internally, resolved by phash2(key, 8))
```

---

## Implementation

### Step 1: Application supervisor

```elixir
# lib/partition_sup_demo/application.ex
defmodule PartitionSupDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {PartitionSupervisor,
       child_spec: PartitionSupDemo.UsageCounter,
       name: PartitionSupDemo.UsageCounter.Partitions,
       partitions: System.schedulers_online()}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PartitionSupDemo.Supervisor)
  end
end
```

### Step 2: The counter

```elixir
# lib/partition_sup_demo/usage_counter.ex
defmodule PartitionSupDemo.UsageCounter do
  @moduledoc """
  A partitioned per-tenant counter. Each partition owns ~1/N of the tenant
  keyspace.
  """
  use GenServer

  @type tenant_id :: String.t()

  @spec incr(tenant_id(), pos_integer()) :: pos_integer()
  def incr(tenant_id, by \\ 1) do
    GenServer.call(partition(tenant_id), {:incr, tenant_id, by})
  end

  @spec get(tenant_id()) :: non_neg_integer()
  def get(tenant_id) do
    GenServer.call(partition(tenant_id), {:get, tenant_id})
  end

  @doc "Sum across all partitions. O(N_partitions)."
  @spec total() :: non_neg_integer()
  def total do
    PartitionSupDemo.UsageCounter.Partitions
    |> PartitionSupervisor.which_children()
    |> Enum.map(fn {_id, pid, _type, _modules} -> GenServer.call(pid, :dump_total) end)
    |> Enum.sum()
  end

  defp partition(key) do
    {:via, PartitionSupervisor, {PartitionSupDemo.UsageCounter.Partitions, key}}
  end

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(_opts), do: {:ok, %{counts: %{}}}

  @impl true
  def handle_call({:incr, tenant_id, by}, _from, %{counts: counts} = state) do
    new = Map.update(counts, tenant_id, by, &(&1 + by))
    {:reply, Map.fetch!(new, tenant_id), %{state | counts: new}}
  end

  def handle_call({:get, tenant_id}, _from, state) do
    {:reply, Map.get(state.counts, tenant_id, 0), state}
  end

  def handle_call(:dump_total, _from, state) do
    {:reply, state.counts |> Map.values() |> Enum.sum(), state}
  end
end
```

### Step 3: Tests

```elixir
# test/partition_sup_demo/usage_counter_test.exs
defmodule PartitionSupDemo.UsageCounterTest do
  use ExUnit.Case, async: false

  alias PartitionSupDemo.UsageCounter

  test "counter per tenant is independent" do
    UsageCounter.incr("alice", 3)
    UsageCounter.incr("bob", 5)
    assert UsageCounter.get("alice") == 3
    assert UsageCounter.get("bob") == 5
  end

  test "same tenant always routes to the same partition (stable under hash)" do
    p1 = GenServer.whereis({:via, PartitionSupervisor, {UsageCounter.Partitions, "acme"}})
    p2 = GenServer.whereis({:via, PartitionSupervisor, {UsageCounter.Partitions, "acme"}})
    assert p1 == p2 and is_pid(p1)
  end

  test "total aggregates across partitions" do
    UsageCounter.incr("t-#{System.unique_integer()}", 1)
    UsageCounter.incr("t-#{System.unique_integer()}", 1)
    UsageCounter.incr("t-#{System.unique_integer()}", 1)
    assert UsageCounter.total() >= 3
  end

  test "concurrent writers to different tenants do not serialize" do
    tasks =
      for i <- 1..1_000 do
        Task.async(fn -> UsageCounter.incr("tenant-#{i}", 1) end)
      end

    assert Enum.all?(Task.await_many(tasks, 5_000), &is_integer/1)
  end
end
```

### Step 4: Benchmark — single vs partitioned

```elixir
# bench/contention_bench.exs
defmodule Bench.SingleCounter do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def incr(k), do: GenServer.call(__MODULE__, {:incr, k})

  @impl true
  def init(:ok), do: {:ok, %{}}
  @impl true
  def handle_call({:incr, k}, _f, s), do: {:reply, :ok, Map.update(s, k, 1, &(&1 + 1))}
end

{:ok, _} = Bench.SingleCounter.start_link([])

tenants = for i <- 1..500, do: "tenant-#{i}"

Benchee.run(
  %{
    "single GenServer" => fn -> Enum.each(tenants, &Bench.SingleCounter.incr/1) end,
    "partitioned (N schedulers)" => fn ->
      Enum.each(tenants, &PartitionSupDemo.UsageCounter.incr(&1, 1))
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
```

Expected on an 8-core machine with `parallel: 8`:

| Path | ips | p99 |
|---|---|---|
| Single GenServer | ~4–8 k ips | ~25 ms |
| Partitioned (×8) | ~45–70 k ips | ~2 ms |

---

## Trade-offs and production gotchas

**1. `:erlang.phash2/2` is stable but NOT cryptographic.** Adversarial keys can be crafted to land on the same partition. For untrusted keys (public API) this is a DoS vector — hash with `:crypto.hash(:blake2s, key)` before `phash2` or cap concurrent work per partition.

**2. `PartitionSupervisor.which_children/1` is O(partitions).** Calling it on the hot path defeats the purpose. Use it only for administrative ops (metrics dump, graceful shutdown) — never per request.

**3. Cross-partition operations require a fan-out.** `total/0` above calls every partition. A cross-tenant report that touches K keys calls K partitions sequentially — do it concurrently with `Task.async_stream/3` or keep a separate, eventually-consistent aggregate process.

**4. Changing `partitions:` is a reshard.** If you go from 8 partitions to 12, every key now hashes to a different partition. Existing in-memory state is on the "old" partition. Plan a migration: drain, snapshot to ETS/disk, restart with new N, restore.

**5. Same process name conflict on restart.** If you pass `name: __MODULE__` in the child's `start_link`, you can't start N copies — they collide. Remove names from partitioned child specs; route via the `{:via, PartitionSupervisor, ...}` tuple instead.

**6. Hot-key hotspots.** If 80 % of traffic targets one tenant, one partition handles 80 % of load. Two mitigations: (a) compound sharding key `{tenant_id, req_seq}` if ordering per-tenant is not required; (b) detect hot keys via telemetry and route them to a separate dedicated process pool.

**7. Telemetry per partition is noisy.** With 8 partitions you get 8 metrics streams per measurement. Aggregate at emission (tag `{:partition, idx}`) and roll up in your TSDB, or emit only aggregate counters from a separate rollup process.

**8. When NOT to use this.** If your bottleneck is the work done *inside* each call (DB latency, external API), partitioning doesn't help — you're I/O bound, not mailbox bound. Measure `Process.info(pid, :message_queue_len)` under load; if it's always <10, partitioning is overkill and ETS is simpler.

---

## Performance notes

Measure the mailbox before and after:

```elixir
for {_, pid, _, _} <- PartitionSupervisor.which_children(UsageCounter.Partitions) do
  {pid, Process.info(pid, :message_queue_len)}
end
```

On a saturated single counter you'll see queue lengths in the hundreds. With partitions equal to schedulers you should see single-digit queues across all partitions under the same load — that's your signal the bottleneck moved.

The cost of `{:via, PartitionSupervisor, ...}` resolution is a single `phash2` + an ETS lookup, sub-microsecond.

---

## Resources

- [`PartitionSupervisor` — hexdocs](https://hexdocs.pm/elixir/PartitionSupervisor.html) — official API.
- [Elixir v1.14 CHANGELOG — PartitionSupervisor](https://github.com/elixir-lang/elixir/blob/main/CHANGELOG.md#v1140) — the introduction rationale by José Valim.
- [`:erlang.phash2/2` internals](https://www.erlang.org/doc/man/erlang.html#phash2-2) — hash function, distribution characteristics.
- [Dashbit blog — Elixir 1.14 highlights](https://dashbit.co/blog/welcome-to-elixir-1-14) — design motivations from the team that shipped it.
- [Oban's peer/queue sharding](https://github.com/sorentwo/oban/tree/main/lib/oban/peers) — real-world example of sharded supervision in a library.
- [Phoenix PubSub sharded registry](https://github.com/phoenixframework/phoenix_pubsub) — another production-grade sharded pattern.
