# PartitionSupervisor: scaling GenServer contention

**Project**: `partition_sup_demo` — use `PartitionSupervisor` (Elixir 1.14+) to remove a single-process bottleneck.

---

## The business problem

Your team runs a multi-tenant SaaS. Every HTTP request touches a `UsageCounter` process that increments a per-tenant counter used for billing and soft quotas. The counter is a single named `GenServer` because originally there were five tenants and 20 rps. Today there are 8 000 tenants and 12 000 rps peak. The `GenServer.call` latency distribution now looks like this: p50 1 ms, p95 40 ms, p99 180 ms. A mailbox inspection (`Process.info(pid, :message_queue_len)`) shows 400–2 000 messages during peaks. The single counter process is scheduled on one core; the other 15 cores are idle for this workload.

You can refactor to ETS, but you also want strong per-tenant ordering (monotonic increment semantics) for auditability — some counters participate in rate-limit decisions that require a sequential view. ETS `:update_counter` gives you atomicity but spreads the work across many schedulers in a way that makes it hard to attach per-tenant side effects (flush-to-DB, telemetry aggregation). The right answer is `PartitionSupervisor`: keep the GenServer, but run N of them, each owning a shard of tenant IDs.

`PartitionSupervisor` shipped in Elixir 1.14. Before it, you had to hand-roll a registry of N copies and hash manually. Now you declare `{PartitionSupervisor, child_spec: ..., name: ..., partitions: N}` and route via `{:via, PartitionSupervisor, {name, key}}`.

## Project structure

```
partition_sup_demo/
├── lib/
│   └── partition_sup_demo/
│       ├── application.ex
│       ├── usage_counter.ex
│       └── billing.ex
├── bench/
│   └── contention_bench.exs
├── test/
│   └── partition_sup_demo/
│       └── usage_counter_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why `PartitionSupervisor` and not ETS `:update_counter`

ETS with `:update_counter` gives O(1) atomic increments and beats any GenServer on raw throughput — but it is *just* a number. The counter participates in rate-limit decisions that need per-tenant ordering, telemetry hooks on each increment, and an eventual flush-to-DB. ETS forces you to bolt on a separate process for those concerns, recreating the serialization point you were trying to avoid. `PartitionSupervisor` keeps the GenServer contract (serial, stateful, attachable side effects) while spreading load across schedulers. You lose ~2× raw throughput vs. ETS, but you keep the abstraction.

---

## Design decisions

**Option A — hand-rolled N-copy Registry with manual hashing**
- Pros: arbitrary routing (consistent hashing, jump hash, custom load metric); fine-grained control over rebalance semantics.
- Cons: every call site rewrites the routing boilerplate; Registry-lookup cost per call; more moving parts.

**Option B — `PartitionSupervisor` with `{:via, PartitionSupervisor, ...}`** (chosen)
- Pros: stock OTP primitive since Elixir 1.14; routing is a `phash2` + named lookup; child specs stay vanilla; consistent behaviour across teams.
- Cons: no rebalance on partition count change (reshard = full migration); no built-in hot-key mitigation; single-node only.

→ Chose **B** because the workload is single-node, partition count is chosen at boot, and the uniform ergonomics win against the flexibility that a hand-rolled registry would provide.

---

## Implementation

### Dependencies (`mix.exs`)

### `mix.exs`
```elixir
defmodule PartitionSupervisor.MixProject do
  use Mix.Project

  def project do
    [
      app: :partition_supervisor,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```elixir
GenServer.call(UsageCounter, {:incr, tenant_id})

GenServer.call(
  {:via, PartitionSupervisor, {UsageCounter.Partitions, tenant_id}},
  {:incr, tenant_id}
)
```
The `{:via, PartitionSupervisor, {name, key}}` tuple resolves to the pid of the partition responsible for `key`. Resolution is `partition_index = :erlang.phash2(key, partitions)` — deterministic, stateless, no lookup.

The sweet spot is **`System.schedulers_online/0`** or a small multiple (2×, 4×). More partitions do NOT help if there's only one scheduler to run them. Fewer partitions than cores wastes capacity.

```elixir
partitions: System.schedulers_online()   # sensible default
```
For workloads with long sync I/O blocking each partition (DB calls), go higher — 4×–8× schedulers — so other partitions can run while peers wait.

Routing is per key. If 80 % of your traffic hits `tenant_id = "acme"`, 80 % of your load lands on ONE partition regardless of how many you configured. `PartitionSupervisor` does NOT solve hotspots; it solves *spread*. For skewed workloads, shard by `{tenant_id, request_id}` or by `:rand.uniform(partitions)` for a read-only path.

When partition 3 crashes, `Supervisor` restarts it. During the restart window (typically <1 ms), calls routed to partition 3 fail with `:noproc` or timeout. This is identical to the classic GenServer-dies-during-call race. Keep idempotent ops or add a retry wrapper.

```
                 PartitionSupervisor(name: UsageCounter.Partitions, n=8)
                 /      |      |      |      |      |      |      \
          UC#0   UC#1   UC#2   UC#3   UC#4   UC#5   UC#6   UC#7
          (pids registered internally, resolved by phash2(key, 8))
```

---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---

## Deep Dive: Supervisor Patterns and Production Implications

Supervisor trees define fault tolerance at the application level. Testing supervisor restart strategies (one_for_one, rest_for_one, one_for_all) requires reasoning about side effects of crashes across multiple children. The insight is that your test should verify not just that a child restarts, but that dependent state (ETS tables, connections, message queues) is properly initialized after restart. Production incidents often involve restart loops under load—a supervisor that works fine in quiet tests can spin wildly when children fail faster than they recover.

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

## Benchmark

Measure the mailbox before and after:

```elixir
for {_, pid, _, _} <- PartitionSupervisor.which_children(UsageCounter.Partitions) do
  {pid, Process.info(pid, :message_queue_len)}
end
```
On a saturated single counter you'll see queue lengths in the hundreds. With partitions equal to schedulers you should see single-digit queues across all partitions under the same load — that's your signal the bottleneck moved.

The cost of `{:via, PartitionSupervisor, ...}` resolution is a single `phash2` + an ETS lookup, sub-microsecond.

Target: partition mailbox ≤ 20 under peak load; p99 call latency < 5 ms; via-routing overhead ≤ 1 µs per call.

---

## Reflection

1. One tenant (`"acme"`) sends 80% of your traffic and keeps hashing onto partition 3. You've ruled out adding a second partition tier. Would you compound the key with a request counter, detect hot tenants and route them to a dedicated pool, or migrate `"acme"`-only traffic to ETS? What observability do you need to decide?
2. You need to go from 8 partitions to 16 for a bigger host. The state is in-memory only. Describe a zero-downtime reshard procedure that preserves per-tenant counts, and identify the exact window during which double-counts or missed increments can occur.

---

### `script/main.exs`
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

defmodule Main do
  def main do
      # Demonstrate PartitionSupervisor scaling GenServer contention

      # Start PartitionSupervisor with N partitions equal to schedulers
      {:ok, sup_pid} = PartitionSupervisor.start_link(
        child_spec: PartitionSupDemo.UsageCounter.child_spec([]),
        name: PartitionSupDemo.UsageCounter.Partitions,
        partitions: System.schedulers_online()
      )

      assert is_pid(sup_pid), "PartitionSupervisor must start"
      IO.inspect(System.schedulers_online(), label: "Schedulers (partitions)")

      # Test monotonic per-tenant increments across partitions
      incr_result_1 = PartitionSupDemo.UsageCounter.incr("tenant_1", 10)
      assert incr_result_1 == 10, "First incr should return 10"

      incr_result_2 = PartitionSupDemo.UsageCounter.incr("tenant_1", 5)
      assert incr_result_2 == 15, "Second incr should return 15"

      # Different tenants should not affect each other
      incr_result_3 = PartitionSupDemo.UsageCounter.incr("tenant_2", 7)
      assert incr_result_3 == 7, "Different tenant should start at 0"

      # Get per-tenant counters
      count_1 = PartitionSupDemo.UsageCounter.get("tenant_1")
      count_2 = PartitionSupDemo.UsageCounter.get("tenant_2")

      assert count_1 == 15, "Tenant 1 counter should be 15"
      assert count_2 == 7, "Tenant 2 counter should be 7"

      IO.inspect(count_1, label: "Tenant 1 counter")
      IO.inspect(count_2, label: "Tenant 2 counter")

      # Aggregate across all partitions
      total = PartitionSupDemo.UsageCounter.total()
      assert total == 22, "Total should be 15 + 7"
      IO.inspect(total, label: "Total across all partitions")

      # Verify partition distribution via phash2
      partitions_count = System.schedulers_online()
      partition_idx = :erlang.phash2("tenant_1", partitions_count)
      assert partition_idx >= 0 and partition_idx < partitions_count, "Partition index should be valid"
      IO.inspect(partition_idx, label: "Tenant 1 partition index")

      IO.puts("✓ PartitionSupervisor initialized with #{partitions_count} partitions")
      IO.puts("✓ Per-tenant monotonic increments verified")
      IO.puts("✓ Cross-partition aggregation working")
      IO.puts("✓ Load distribution via phash2 demonstrated")

      PartitionSupervisor.stop(sup_pid)
      IO.puts("✓ PartitionSupervisor shutdown complete")
  end
end

Main.main()
```
---

## Why PartitionSupervisor matters

Mastering **PartitionSupervisor** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/partition_sup_demo.ex`

```elixir
defmodule PartitionSupDemo do
  @moduledoc """
  Reference implementation for PartitionSupervisor: scaling GenServer contention.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the partition_sup_demo module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> PartitionSupDemo.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/partition_sup_demo_test.exs`

```elixir
defmodule PartitionSupDemoTest do
  use ExUnit.Case, async: true

  doctest PartitionSupDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert PartitionSupDemo.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

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
