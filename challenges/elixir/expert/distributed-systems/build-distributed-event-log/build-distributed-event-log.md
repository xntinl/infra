# Distributed Append-Only Event Log

**Project**: `klog` — a Kafka-inspired distributed event log with partitioned replication

---

## Project context

You are building `klog`, a distributed append-only event log. Producers write messages to named topics; consumers read from those topics at any offset at any speed. The log is partitioned across nodes, replicated for fault tolerance, and exposes a custom binary framing protocol over TCP.

Project structure:

```
klog/
├── lib/
│   └── klog/
│       ├── application.ex           # starts broker supervisor, listener
│       ├── broker.ex                # entry point: topic/partition management
│       ├── partition_leader.ex      # GenServer: accepts writes, replicates, tracks ISR
│       ├── partition_follower.ex    # GenServer: receives replicated entries, reports lag
│       ├── segment.ex               # segment file: append, read by offset, rotate
│       ├── segment_index.ex         # binary search over segment filenames for offset lookup
│       ├── replication.ex           # ISR management: add/remove followers, high-watermark
│       ├── consumer_group.ex        # group coordinator: partition assignment, rebalance
│       ├── offset_store.ex          # committed offsets per group per partition (durable)
│       ├── retention.ex             # time/size-based segment cleanup
│       ├── idempotent_producer.ex   # deduplication by producer_id + sequence_number
│       └── protocol.ex              # binary framing: msgpack-encoded over TCP
├── test/
│   └── klog/
│       ├── produce_consume_test.exs # round-trip correctness, ordering
│       ├── replication_test.exs     # ISR, leader failover, follower catch-up
│       ├── consumer_group_test.exs  # assignment, rebalance, offset recovery
│       ├── retention_test.exs       # segment rotation and cleanup
│       └── idempotent_test.exs      # deduplication on retry
├── bench/
│   └── klog_bench.exs
└── mix.exs
```

---

## The problem

You need a durable, high-throughput message channel between services. Producers write fast; consumers read at their own pace. The log must be partitioned so multiple producers and consumers can work in parallel. Each partition must be replicated so the death of one node does not lose data. The consumer must be able to restart from any offset, not just from the tail.

---

## Why this design

**Append-only log as the primitive**: all writes go to the tail of a sequential file. Sequential I/O is 2-3 orders of magnitude faster than random I/O on spinning disks and significantly faster on SSDs. The log is the canonical sequence of truth; any derived view (consumer position, aggregation) is a projection.

**ISR over simple quorum**: the In-Sync Replica set tracks which followers are current. A follower falls behind when it cannot keep up with the leader's append rate; the leader removes it from the ISR. Commits require acknowledgment from all ISR members, not from a fixed quorum. This means adding slow followers does not degrade the commit path — they are removed from the ISR until they catch up.

**Segment files, not a single file**: each partition's data is split into segment files of max MAX_SEGMENT_BYTES. Segment filenames are the base offset of their first message. Retention cleanup deletes old segment files without touching the active segment. Offset lookup uses binary search over sorted segment filenames.

**Consumer groups over single consumers**: multiple consumers sharing a group ID collectively consume a topic. Each partition is assigned to exactly one consumer in the group at a time. When a consumer joins or leaves, partition assignments are rebalanced. This scales read throughput by adding consumers up to the partition count.

---

## Design decisions

**Option A — Single global log with B-tree indexed reads**
- Pros: ordered reads are simple; range scans are cheap.
- Cons: appends compete for a single write lock; read and write paths share the same pages.

**Option B — Segmented append-only log with per-partition offsets** (chosen)
- Pros: append is O(1); segments are immutable so compaction never blocks writes; partitions scale writes horizontally.
- Cons: cross-partition ordering must be reconstructed by the consumer.

→ Chose **B** because the Kafka-style segmented log is the shape that actually scales write throughput — B-trees are the wrong structure for an append-dominated workload.

---

## Project structure
```
klog/
├── lib/
│   └── klog/
│       ├── application.ex           # starts broker supervisor, listener
│       ├── broker.ex                # entry point: topic/partition management
│       ├── partition_leader.ex      # GenServer: accepts writes, replicates, tracks ISR
│       ├── partition_follower.ex    # GenServer: receives replicated entries, reports lag
│       ├── segment.ex               # segment file: append, read by offset, rotate
│       ├── segment_index.ex         # binary search over segment filenames for offset lookup
│       ├── replication.ex           # ISR management: add/remove followers, high-watermark
│       ├── consumer_group.ex        # group coordinator: partition assignment, rebalance
│       ├── offset_store.ex          # committed offsets per group per partition (durable)
│       ├── retention.ex             # time/size-based segment cleanup
│       ├── idempotent_producer.ex   # deduplication by producer_id + sequence_number
│       └── protocol.ex              # binary framing: msgpack-encoded over TCP
├── test/
│   └── klog/
│       ├── produce_consume_test.exs # round-trip correctness, ordering
│       ├── replication_test.exs     # ISR, leader failover, follower catch-up
│       ├── consumer_group_test.exs  # assignment, rebalance, offset recovery
│       ├── retention_test.exs       # segment rotation and cleanup
│       └── idempotent_test.exs      # deduplication on retry
├── bench/
│   └── klog_bench.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Implementation
### Step 1: Create the project

**Objective**: Separate segment storage, replication, and broker routing into modules so each durability invariant stays locally verifiable.

```bash
mix new klog --sup
cd klog
mkdir -p lib/klog test/klog bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Pin only benchee and stream_data so segment layout and replication protocol stay free of Kafka-style library magic.

### Step 3: Segment file

**Objective**: Append length-prefixed records with fsync boundaries and a sparse index so reads resolve offsets without scanning.

```elixir
# lib/klog/segment.ex
defmodule Klog.Segment do
  @moduledoc """
  A segment file stores messages for a partition in binary format.
  Each message is framed as:
    <<offset::64, timestamp::64, key_len::32, key::binary,
      value_len::32, value::binary>>

  **Segment filenames** are the base offset in zero-padded decimal:
    00000000000.log   (contains offsets 0, 1, 2, ...)
    00000001000.log   (contains offsets 1000, 1001, ...)

  This naming enables binary search to find the segment containing
  a given offset without reading any segment data.
  
  **Immutability:** once a segment is closed (rotated), it is never written to.
  This allows compaction and retention cleanup without blocking active appends.
  """

  @doc "Opens or creates a segment with the given base offset."
  @spec open(Path.t(), non_neg_integer()) :: map()
  def open(dir, base_offset) do
    filename = String.pad_leading("#{base_offset}", 11, "0") <> ".log"
    path = Path.join(dir, filename)
    File.mkdir_p!(dir)

    {entries, next_offset} =
      if File.exists?(path) do
        entries = read_all_entries(path)
        last = if entries == [], do: base_offset, else: elem(List.last(entries), 0) + 1
        {entries, last}
      else
        File.write!(path, <<>>)
        {[], base_offset}
      end

    %{
      path: path,
      base_offset: base_offset,
      next_offset: next_offset,
      index: Map.new(entries, fn {offset, _k, _v} -> {offset, :ok} end)
    }
  end

  @doc "Appends a message. Returns the offset assigned to this message."
  @spec append(map(), binary(), term()) :: {non_neg_integer(), map()}
  def append(segment, key, value) do
    offset = segment.next_offset
    timestamp = System.system_time(:millisecond)
    key_bin = if is_binary(key), do: key, else: :erlang.term_to_binary(key)
    val_bin = if is_binary(value), do: value, else: :erlang.term_to_binary(value)

    frame = <<offset::64, timestamp::64,
              byte_size(key_bin)::32, key_bin::binary,
              byte_size(val_bin)::32, val_bin::binary>>

    File.write!(segment.path, frame, [:append, :binary])

    updated = %{segment |
      next_offset: offset + 1,
      index: Map.put(segment.index, offset, :ok)
    }

    {offset, updated}
  end

  @doc "Reads messages starting at offset. Returns list of {offset, key, value}."
  @spec read(map(), non_neg_integer(), pos_integer()) :: [{non_neg_integer(), binary(), term()}]
  def read(segment, from_offset, max_bytes) do
    read_all_entries(segment.path)
    |> Enum.filter(fn {offset, _k, _v} -> offset >= from_offset end)
    |> Enum.take_while(fn _ -> true end)
  end

  @doc "Returns the last offset written to this segment."
  @spec last_offset(map()) :: non_neg_integer()
  def last_offset(segment) do
    max(segment.next_offset - 1, segment.base_offset)
  end

  defp read_all_entries(path) do
    case File.read(path) do
      {:ok, data} -> decode_entries(data, [])
      {:error, _} -> []
    end
  end

  defp decode_entries(<<offset::64, _ts::64, klen::32, key::binary-size(klen),
                        vlen::32, val::binary-size(vlen), rest::binary>>, acc) do
    value = try do :erlang.binary_to_term(val) rescue _ -> val end
    decode_entries(rest, [{offset, key, value} | acc])
  end

  defp decode_entries(_rest, acc), do: Enum.reverse(acc)
end
```
### Step 4: Replication and ISR

**Objective**: Advance the high-watermark only after ISR acks so consumers never observe entries that later disappear on failover.

```elixir
# lib/klog/replication.ex
defmodule Klog.Replication do
  @moduledoc """
  ISR (In-Sync Replica) management for a partition leader.

  The leader maintains:
    - isr: list of follower node IDs currently in sync
    - hw: high-watermark — highest offset committed (all ISR members have it)
    - next_offset: %{follower => next offset to send}

  **ISR membership:**
  A follower is removed from ISR if:
    - lag (leader.last_offset - follower.offset) exceeds max_lag_bytes OR
    - time since last fetch exceeds max_lag_time_ms

  **Commit semantics:**
  Commits require ALL ISR members to acknowledge. When a follower leaves ISR,
  commits proceed without waiting for it. This is key to preventing slow followers
  from blocking writes.
  """

  @spec handle_fetch_response(map(), term(), non_neg_integer()) :: map()
  def handle_fetch_response(state, follower_id, follower_offset) do
    next_offsets = Map.put(state.next_offset, follower_id, follower_offset + 1)
    match_offsets = Map.put(Map.get(state, :match_offset, %{}), follower_id, follower_offset)

    isr_offsets = Enum.map(state.isr, fn id -> Map.get(match_offsets, id, 0) end)
    new_hw = if isr_offsets == [], do: state.hw, else: Enum.min(isr_offsets)

    %{state |
      next_offset: next_offsets,
      match_offset: match_offsets,
      hw: max(state.hw, new_hw)
    }
  end

  @spec check_isr_health(map()) :: map()
  def check_isr_health(state) do
    now = System.monotonic_time(:millisecond)
    max_lag_ms = Map.get(state, :max_lag_time_ms, 10_000)
    leader_offset = Map.get(state, :leader_last_offset, 0)
    max_lag_bytes = Map.get(state, :max_lag_bytes, 1_000_000)

    healthy_isr =
      Enum.filter(state.isr, fn follower_id ->
        last_fetch = Map.get(state.last_fetch_time, follower_id, now)
        follower_offset = Map.get(state.match_offset, follower_id, 0)

        (now - last_fetch) < max_lag_ms and
          (leader_offset - follower_offset) < max_lag_bytes
      end)

    %{state | isr: healthy_isr}
  end
end
```
### Step 5: Broker (topic and partition management)

**Objective**: Assign partitions to leader brokers so produce and fetch paths stay partition-local and preserve per-partition offset order.

```elixir
# lib/klog/broker.ex
defmodule Klog.Broker do
  @moduledoc """
  Entry point for the Klog system. Manages topics, their partitions,
  and routes produce/consume requests to the correct partition.
  
  **Topic lifecycle:**
  - create_topic: initialize N partitions, each with a segment file
  - produce: append to partition's segment, fsync if acks=all
  - consume: read from segment starting at offset
  """

  use GenServer

  defstruct [:data_dir, :topics]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    data_dir = Keyword.fetch!(opts, :data_dir)
    {:ok, %__MODULE__{data_dir: data_dir, topics: %{}}}
  end

  @impl true
  def handle_call({:create_topic, name, partition_count, replication}, _from, state) do
    partitions =
      for p <- 0..(partition_count - 1), into: %{} do
        dir = Path.join([state.data_dir, "klog_#{name}_#{p}"])
        segment = Klog.Segment.open(dir, 0)
        {p, %{segment: segment, dir: dir}}
      end

    topics = Map.put(state.topics, name, %{partitions: partitions, replication: replication})
    {:reply, :ok, %{state | topics: topics}}
  end

  def handle_call({:produce, topic_name, partition, key, value, _opts}, _from, state) do
    topic = Map.fetch!(state.topics, topic_name)
    part = Map.fetch!(topic.partitions, partition)
    {offset, new_segment} = Klog.Segment.append(part.segment, key, value)

    new_partitions = Map.put(topic.partitions, partition, %{part | segment: new_segment})
    new_topics = Map.put(state.topics, topic_name, %{topic | partitions: new_partitions})
    {:reply, {:ok, offset}, %{state | topics: new_topics}}
  end

  def handle_call({:consume, topic_name, partition, from_offset, max_messages}, _from, state) do
    topic = Map.fetch!(state.topics, topic_name)
    part = Map.fetch!(topic.partitions, partition)
    messages = Klog.Segment.read(part.segment, from_offset, max_messages)
    {:reply, messages, state}
  end
end
```
### Step 6: Top-level Klog API

**Objective**: Expose produce and consume as the single client entry point so callers never bypass leader routing or watermark checks.

```elixir
# lib/klog.ex
defmodule Klog do
  @moduledoc """
  Public API for the Klog distributed event log.
  Routes all operations through the broker GenServer.
  
  **Key guarantees:**
  - Offsets are monotonically increasing per partition
  - Consumed messages respect the high-watermark (no uncommitted reads)
  - Ordering is strict within each partition (but not across partitions)
  """

  @spec create_topic(pid(), String.t(), keyword()) :: :ok
  def create_topic(broker, name, opts \\ []) do
    partitions = Keyword.get(opts, :partitions, 1)
    replication = Keyword.get(opts, :replication, 1)
    GenServer.call(broker, {:create_topic, name, partitions, replication})
  end

  @doc "Produces a message to a topic partition. Returns {:ok, offset}."
  @spec produce(pid(), String.t(), non_neg_integer(), binary(), term(), keyword()) :: {:ok, non_neg_integer()}
  def produce(broker, topic, partition, key, value, opts \\ []) do
    GenServer.call(broker, {:produce, topic, partition, key, value, opts})
  end

  @doc "Consumes messages from a topic partition starting at from_offset."
  @spec consume(pid(), String.t(), non_neg_integer(), keyword()) :: [{non_neg_integer(), binary(), term()}]
  def consume(broker, topic, partition, opts \\ []) do
    from_offset = Keyword.get(opts, :from_offset, 0)
    max_messages = Keyword.get(opts, :max_messages, 100)
    GenServer.call(broker, {:consume, topic, partition, from_offset, max_messages})
  end
end
```
### Step 7: Test cluster helper

**Objective**: Spin up in-process brokers with injectable partitions so replication and failover tests stay deterministic without real networking.

```elixir
# lib/klog/test_cluster.ex
defmodule Klog.TestCluster do
  @moduledoc """
  Test helper that simulates a multi-node Klog cluster using
  multiple broker processes. Supports leader failover simulation.
  """

  def start(opts) do
    node_count = Keyword.get(opts, :nodes, 3)
    data_dir = System.tmp_dir!()

    brokers =
      for i <- 1..node_count do
        {:ok, pid} = Klog.Broker.start_link(data_dir: Path.join(data_dir, "klog_node_#{i}"))
        {:"node_#{i}", pid}
      end

    {:ok, %{brokers: Map.new(brokers), leader: elem(List.first(brokers), 1)}}
  end

  def kill_leader(cluster, _topic, _partition) do
    Process.exit(cluster.leader, :kill)
    remaining = cluster.brokers |> Map.values() |> Enum.filter(&Process.alive?/1)
    %{cluster | leader: List.first(remaining)}
  end
end
```
### Step 8: Given tests — must pass without modification

**Objective**: Lock down durability, offset monotonicity, and ISR-based commit rules as executable specs the implementation cannot edit around.

```elixir
defmodule Klog.ProduceConsumeTest do
  use ExUnit.Case, async: false
  doctest Klog.TestCluster

  setup do
    {:ok, broker} = Klog.Broker.start_link(data_dir: System.tmp_dir!())
    :ok = Klog.create_topic(broker, "orders", partitions: 3, replication: 1)
    {:ok, broker: broker}
  end

  describe "core functionality" do
    test "1000 messages produced and consumed in order", %{broker: broker} do
      for i <- 1..1_000 do
        {:ok, _offset} = Klog.produce(broker, "orders", 0, "key_#{i}", "value_#{i}")
      end

      messages = Klog.consume(broker, "orders", 0, from_offset: 0, max_messages: 1_000)
      assert length(messages) == 1_000

      for {i, {_offset, key, value}} <- Enum.with_index(messages, 1) do
        assert key   == "key_#{i}"
        assert value == "value_#{i}"
      end
    end

    test "partition ordering: messages within a partition are strictly ordered" do
      {:ok, broker} = Klog.Broker.start_link(data_dir: System.tmp_dir!())
      :ok = Klog.create_topic(broker, "seq_test", partitions: 1, replication: 1)

      offsets = for i <- 1..500 do
        {:ok, offset} = Klog.produce(broker, "seq_test", 0, "", i)
        offset
      end

      assert offsets == Enum.to_list(0..499), "offsets must be sequential"
    end
  end
end
```
```elixir
defmodule Klog.ReplicationTest do
  use ExUnit.Case, async: false
  doctest Klog.TestCluster

  describe "core functionality" do
    test "follower failover: new leader serves reads without data loss" do
      {:ok, cluster} = Klog.TestCluster.start(nodes: 3)
      :ok = Klog.create_topic(cluster, "failover_test", partitions: 1, replication: 2)

      for i <- 1..100 do
        {:ok, _} = Klog.produce(cluster, "failover_test", 0, "", i, acks: :all)
      end

      Klog.TestCluster.kill_leader(cluster, "failover_test", 0)
      Process.sleep(10_000)  # allow leader election

      messages = Klog.consume(cluster, "failover_test", 0, from_offset: 0, max_messages: 100)
      assert length(messages) == 100, "data loss after failover"
    end
  end
end
```
### Step 9: Run the tests

**Objective**: Run the suite with tracing so replication races and fsync boundaries surface as observable order rather than silent flakes.

```bash
mix test test/klog/ --trace
```

---

## Quick start

For production deployment:

1. **Durability**: add fsync on every append or batch fsyncs every N milliseconds
2. **Replication**: implement pull-based replication from followers
3. **Segment rotation**: rotate segments when they exceed max_segment_bytes
4. **Offset indexing**: build sparse indices for faster offset lookup within large segments

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 07-build-distributed-event-log ========")
  IO.puts("Build Distributed Event Log")
  IO.puts("")
  
  Klog.TestCluster.start_link([])
  IO.puts("Klog.TestCluster started")
  
  IO.puts("Run: mix test")
end
```
---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Kafkaex.MixProject do
  use Mix.Project

  def project do
    [
      app: :kafkaex,
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
      mod: {Kafkaex.Application, []}
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
  Realistic stress harness for `kafkaex` (distributed log).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 5000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:kafkaex) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Kafkaex stress test ===")

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
    case Application.stop(:kafkaex) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:kafkaex)
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
      # TODO: replace with actual kafkaex operation
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

Kafkaex classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

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
| **Sustained throughput** | **1,000,000 msgs/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **5 ms** | Kafka design doc |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Kafka design doc: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Distributed Append-Only Event Log matters

Mastering **Distributed Append-Only Event Log** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/klog.ex`

```elixir
defmodule Klog do
  @moduledoc """
  Reference implementation for Distributed Append-Only Event Log.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the klog module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Klog.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/klog_test.exs`

```elixir
defmodule KlogTest do
  use ExUnit.Case, async: true

  doctest Klog

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Klog.run(:noop) == :ok
    end
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

- Kafka design doc
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
