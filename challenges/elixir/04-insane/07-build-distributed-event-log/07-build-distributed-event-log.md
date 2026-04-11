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

## Implementation milestones

### Step 1: Create the project

```bash
mix new klog --sup
cd klog
mkdir -p lib/klog test/klog bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:msgpax, "~> 2.4"},     # msgpack encoding for the binary protocol
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Segment file

```elixir
# lib/klog/segment.ex
defmodule Klog.Segment do
  @moduledoc """
  A segment file stores messages for a partition in binary format.
  Each message is framed as:
    <<offset::64, timestamp::64, key_len::32, key::binary,
      value_len::32, value::binary>>

  Segment filenames are the base offset in zero-padded decimal:
    00000000000.log   (contains offsets 0, 1, 2, ...)
    00000001000.log   (contains offsets 1000, 1001, ...)

  This naming enables binary search to find the segment containing
  a given offset without reading any segment data.
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

```elixir
# lib/klog/replication.ex
defmodule Klog.Replication do
  @moduledoc """
  ISR (In-Sync Replica) management for a partition leader.

  The leader maintains:
    - isr: list of follower node IDs currently in sync
    - hw: high-watermark — highest offset committed (all ISR members have it)
    - next_offset: %{follower => next offset to send}

  A follower is removed from ISR if its lag (leader.last_offset - follower.offset)
  exceeds max_lag_bytes OR if it misses max_lag_time_ms since last fetch.

  Commits require ALL ISR members to acknowledge. When a follower leaves ISR,
  commits proceed without waiting for it.
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

```elixir
# lib/klog/broker.ex
defmodule Klog.Broker do
  @moduledoc """
  Entry point for the Klog system. Manages topics, their partitions,
  and routes produce/consume requests to the correct partition.
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

```elixir
# lib/klog.ex
defmodule Klog do
  @moduledoc """
  Public API for the Klog distributed event log.
  Routes all operations through the broker GenServer.
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

  @spec consume(pid(), String.t(), non_neg_integer(), keyword()) :: [{non_neg_integer(), binary(), term()}]
  def consume(broker, topic, partition, opts \\ []) do
    from_offset = Keyword.get(opts, :from_offset, 0)
    max_messages = Keyword.get(opts, :max_messages, 100)
    GenServer.call(broker, {:consume, topic, partition, from_offset, max_messages})
  end
end
```

### Step 7: Test cluster helper

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

```elixir
# test/klog/produce_consume_test.exs
defmodule Klog.ProduceConsumeTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, broker} = Klog.Broker.start_link(data_dir: System.tmp_dir!())
    :ok = Klog.create_topic(broker, "orders", partitions: 3, replication: 1)
    {:ok, broker: broker}
  end

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
```

```elixir
# test/klog/replication_test.exs
defmodule Klog.ReplicationTest do
  use ExUnit.Case, async: false

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
```

### Step 9: Run the tests

```bash
mix test test/klog/ --trace
```

### Step 10: Benchmark

```elixir
# bench/klog_bench.exs
{:ok, broker} = Klog.Broker.start_link(data_dir: System.tmp_dir!())
:ok = Klog.create_topic(broker, "bench", partitions: 1, replication: 1)

Benchee.run(
  %{
    "produce — acks=1" => fn ->
      Klog.produce(broker, "bench", 0, "", :rand.uniform(1_000_000), acks: 1)
    end,
    "consume — sequential read" => fn ->
      Klog.consume(broker, "bench", 0, from_offset: 0, max_messages: 1_000)
    end
  },
  parallel: 1,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 500,000 messages/second write throughput (`acks=1`), 1M messages/second read throughput.

---

## Trade-off analysis

| Aspect | ISR quorum (your impl) | Simple majority quorum | No replication |
|--------|----------------------|----------------------|----------------|
| Throughput with slow follower | unaffected (slow follower removed from ISR) | degraded until majority responds | maximum |
| Durability guarantee | all ISR members have the data | majority have the data | none |
| ISR shrink on failure | no wait; ISR shrinks immediately | must wait for timeout | n/a |
| Min replicas for commit | 1 (ISR can shrink to leader only) | always majority | 1 |
| Recovery after follower restart | follower catches up, re-joins ISR | automatic | n/a |

Reflection: when ISR shrinks to just the leader (all followers are lagging), commits succeed but durability is zero — the leader holds the only copy. What is the correct production setting to prevent this?

---

## Common production mistakes

**1. High-watermark not updated on ISR membership change**
When a follower is removed from the ISR, the high-watermark must be recalculated using only the remaining ISR members' offsets. Failing to do this can advance the HW past what the new ISR can guarantee.

**2. Consumer group rebalance without a generation counter**
Two rebalances can overlap: consumer A is acting on generation 2 assignment while consumer B has already moved to generation 3. Without a generation counter, partition A is double-consumed. Every assignment carries a generation; consumers must reject assignments from stale generations.

**3. Segment rotation without index update**
When a segment is rotated (a new segment file is created), the in-memory list of segment filenames must be updated atomically. A reader that picks up the old list will miss newly written segments.

**4. Idempotent deduplication window too small**
Deduplication by (producer_id, sequence) is only effective within a session. If the producer_id is recycled across restarts and the sequence resets, a retry that looks like a new message will be applied. Use a persistent producer_id (UUID at startup, stored to disk) rather than a generated PID.

---

## Resources

- Kreps, J. (2013). *The Log: What every software engineer should know about unification* — LinkedIn Engineering Blog — read the full post, not summaries
- Kreps, J., Narkhede, N. & Rao, J. (2011). *Kafka: a Distributed Messaging System for Log Processing*
- [Apache Kafka Replication documentation](https://kafka.apache.org/documentation/#replication)
- [Kafka binary protocol reference](https://kafka.apache.org/protocol.html)
