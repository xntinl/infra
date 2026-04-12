# LSM-Tree Storage Engine

**Project**: `lsmex` — a Log-Structured Merge-tree storage engine in pure Elixir

---

## Project context

You are building `lsmex`, a production-grade LSM-tree storage engine. The engine provides key-value storage with durability, crash recovery, compaction, Bloom filters, and MVCC snapshot reads. All storage structures are implemented from scratch in Elixir.

Project structure:

```
lsmex/
├── lib/
│   └── lsmex/
│       ├── application.ex           # engine supervisor
│       ├── engine.ex                # public API: put, get, delete, scan, snapshot
│       ├── memtable.ex              # in-memory sorted ETS table; accepts put/delete
│       ├── wal.ex                   # write-ahead log: append, fsync, replay
│       ├── sstable.ex               # immutable on-disk sorted file: write, read, binary search
│       ├── bloom.ex                 # Bloom filter: build from key set, check membership
│       ├── compaction.ex            # GenServer: merge SSTables, discard tombstones
│       ├── snapshot.ex              # snapshot isolation: sequence numbers, version fencing
│       ├── level_manager.ex         # level metadata: SSTable list per level, trigger thresholds
│       └── checksum.ex              # CRC32: compute and verify per block
├── test/
│   └── lsmex/
│       ├── engine_test.exs          # put/get/delete/scan correctness
│       ├── wal_test.exs             # crash recovery via WAL replay
│       ├── compaction_test.exs      # merge, tombstone discard, level growth
│       ├── bloom_test.exs           # false positive rate at configured capacity
│       ├── snapshot_test.exs        # snapshot isolation under concurrent compaction
│       └── checksum_test.exs        # corruption detection
├── bench/
│   └── lsmex_bench.exs
└── mix.exs
```

---

## The problem

Traditional B-tree storage engines perform random writes: updating a value requires seeking to the key's position in the tree and writing in place. On SSDs, random writes cause write amplification (one 512-byte update may trigger a full page write). On spinning disks, random write latency is orders of magnitude higher than sequential write latency.

LSM-trees invert this: all writes are sequential. Data is first written to a WAL (durability) and an in-memory MemTable (fast access). When the MemTable reaches a threshold, it is flushed to disk as an immutable SSTable file in sorted key order. Periodic compaction merges SSTables, removes deleted keys, and reorganizes data into levels. The read path is slower (must check multiple levels) but write throughput is maximized.

---

## Why this design

**WAL before MemTable**: the WAL is written and fsynced before the MemTable is updated. This means on crash, replaying the WAL reconstructs the exact MemTable state at the moment of crash. Without the fsync, a crash after the MemTable update but before the WAL write loses data.

**Bloom filters to skip disk reads**: on a read, you must check the MemTable (O(log N) in ETS) and then, if not found, potentially check every SSTable. Bloom filters eliminate most unnecessary SSTable reads. If the filter says "key not in this SSTable," you skip the disk read entirely. At 1% false positive rate, you skip 99% of unnecessary reads.

**Compaction to bound read amplification**: without compaction, the number of SSTables grows without bound, and every read must check all of them. Compaction merges SSTables at each level into larger SSTables at the next level, bounding the number of levels to check. LevelDB-style leveled compaction keeps at most `threshold` files per level.

**MVCC snapshot isolation**: readers need a consistent snapshot of the data at a point in time, even if compaction runs concurrently and deletes old versions. Assign a monotonic sequence number to each MemTable flush. A snapshot holds the sequence number at creation time. During compaction, only discard versions that are below the lowest active snapshot sequence number.

---

## Design decisions

**Option A — B-tree (or B+tree) on disk**
- Pros: great for read-heavy workloads; balanced tree guarantees O(log N) everything.
- Cons: in-place writes require random I/O and WAL; write amplification under high insert rates.

**Option B — LSM tree with memtable + sorted string tables** (chosen)
- Pros: all writes are sequential appends; compaction batches disk work; memtable absorbs bursts.
- Cons: reads may touch multiple SSTables; compaction adds background write amplification.

→ Chose **B** because the workload we're targeting (ingest-heavy, analytical reads) is exactly the one LSM was designed for; B-tree's in-place update cost is the wrong trade-off.

## Implementation milestones

### Step 1: Create the project

```bash
mix new lsmex --sup
cd lsmex
mkdir -p lib/lsmex test/lsmex bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Bloom filter

```elixir
# lib/lsmex/bloom.ex
defmodule Lsmex.Bloom do
  @moduledoc """
  Bloom filter backed by a bitstring.

  A Bloom filter is a probabilistic data structure that can tell you
  "definitely not in set" or "probably in set." It uses multiple hash
  functions to set bits in a bit array. The false positive rate is
  controlled by the ratio of bits to elements and the number of hash
  functions.

  Optimal parameters for target false positive rate `fp`:
    bits_per_element = -1.44 * log2(fp)
    num_hashes       = round(bits_per_element * ln(2))
  """

  defstruct [:bits, :size, :num_hashes]

  @doc "Builds a Bloom filter from a list of keys with the given target false positive rate."
  @spec build([binary()], keyword()) :: %__MODULE__{}
  def build(keys, opts \\ []) do
    target_fp = Keyword.get(opts, :target_fp, 0.01)
    n = max(length(keys), 1)

    bits_per_element = -1.44 * :math.log2(target_fp)
    total_bits = trunc(n * bits_per_element) |> max(64)
    num_hashes = round(bits_per_element * :math.log(2)) |> max(1)

    bit_array = :atomics.new(div(total_bits, 64) + 1, signed: false)

    Enum.each(keys, fn key ->
      for i <- 0..(num_hashes - 1) do
        bit_index = hash(key, i, total_bits)
        word_index = div(bit_index, 64) + 1
        bit_offset = rem(bit_index, 64)
        current = :atomics.get(bit_array, word_index)
        :atomics.put(bit_array, word_index, Bitwise.bor(current, Bitwise.bsl(1, bit_offset)))
      end
    end)

    %__MODULE__{bits: bit_array, size: total_bits, num_hashes: num_hashes}
  end

  @doc "Checks if a key is probably in the set. False means definitely not."
  @spec member?(%__MODULE__{}, binary()) :: boolean()
  def member?(%__MODULE__{bits: bit_array, size: total_bits, num_hashes: num_hashes}, key) do
    Enum.all?(0..(num_hashes - 1), fn i ->
      bit_index = hash(key, i, total_bits)
      word_index = div(bit_index, 64) + 1
      bit_offset = rem(bit_index, 64)
      current = :atomics.get(bit_array, word_index)
      Bitwise.band(current, Bitwise.bsl(1, bit_offset)) != 0
    end)
  end

  defp hash(key, seed, total_bits) do
    h1 = :erlang.phash2({key, 0}, 1_000_000_000)
    h2 = :erlang.phash2({key, 1}, 1_000_000_000)
    rem(abs(h1 + seed * h2), total_bits)
  end
end
```

### Step 4: Write-ahead log

```elixir
# lib/lsmex/wal.ex
defmodule Lsmex.WAL do
  @moduledoc """
  Append-only write-ahead log. Each record is framed as:
    <<crc32::32, key_len::32, val_len::32, key::binary, value::binary>>

  value is nil for deletes (represented as a tombstone marker).

  On crash, replay reads records sequentially, verifying CRC32 on each.
  A CRC32 mismatch indicates a partial write — stop replay at that point.
  """

  @tombstone_marker <<0xFF, 0xFF, 0xFF, 0xFF>>

  @doc "Appends a record to the WAL and fsyncs. Returns :ok."
  @spec append(Path.t(), binary(), binary() | nil) :: :ok
  def append(path, key, value) do
    key_bin = if is_binary(key), do: key, else: :erlang.term_to_binary(key)
    val_bin = if is_nil(value), do: @tombstone_marker, else: (if is_binary(value), do: value, else: :erlang.term_to_binary(value))

    payload = <<byte_size(key_bin)::32, byte_size(val_bin)::32, key_bin::binary, val_bin::binary>>
    crc = :erlang.crc32(payload)
    frame = <<crc::32, payload::binary>>

    {:ok, fd} = :file.open(path, [:append, :binary, :raw])
    :ok = :file.write(fd, frame)
    :ok = :file.sync(fd)
    :ok = :file.close(fd)
    :ok
  end

  @doc "Replays the WAL. Calls callback({key, value}) for each valid record."
  @spec replay(Path.t(), (tuple() -> any())) :: :ok
  def replay(path, callback) do
    case File.read(path) do
      {:ok, data} -> replay_frames(data, callback)
      {:error, :enoent} -> :ok
    end
  end

  defp replay_frames(<<crc::32, klen::32, vlen::32, key::binary-size(klen),
                       val::binary-size(vlen), rest::binary>>, callback) do
    payload = <<klen::32, vlen::32, key::binary, val::binary>>
    if :erlang.crc32(payload) == crc do
      value = if val == @tombstone_marker, do: nil, else: val
      callback.({key, value})
      replay_frames(rest, callback)
    else
      :ok
    end
  end

  defp replay_frames(_rest, _callback), do: :ok
end
```

### Step 5: SSTable

```elixir
# lib/lsmex/sstable.ex
defmodule Lsmex.SSTable do
  @moduledoc """
  Immutable sorted-string table file.

  File layout:
    [data block 1] [data block 2] ... [footer]

  Each data block:
    <<block_len::32, crc32::32, N x (key_len::32, val_len::32, key::binary, val::binary)>>

  Footer (at end of file):
    <<index_offset::64>> followed by:
    index: N x (key::binary, byte_offset::64) for binary search

  Bloom filter is stored in a separate .bloom file alongside the .sst file.
  """

  @doc "Writes a sorted list of {key, value} pairs to a new SSTable file."
  @spec write(Path.t(), [{binary(), binary()}]) :: :ok
  def write(path, entries) do
    {:ok, fd} = :file.open(path, [:write, :binary, :raw])

    {_offset, index} =
      Enum.reduce(entries, {0, []}, fn {key, value}, {offset, idx} ->
        key_bin = if is_binary(key), do: key, else: :erlang.term_to_binary(key)
        val_bin = if is_binary(value), do: value, else: :erlang.term_to_binary(value)
        record = <<byte_size(key_bin)::32, byte_size(val_bin)::32, key_bin::binary, val_bin::binary>>
        crc = :erlang.crc32(record)
        block = <<byte_size(record)::32, crc::32, record::binary>>
        :file.write(fd, block)
        new_offset = offset + byte_size(block)
        {new_offset, [{key_bin, offset} | idx]}
      end)

    index_data = :erlang.term_to_binary(Enum.reverse(index))
    index_offset = :file.position(fd, :cur) |> elem(1)
    :file.write(fd, index_data)
    :file.write(fd, <<index_offset::64>>)
    :file.close(fd)

    keys = Enum.map(entries, fn {k, _} -> if is_binary(k), do: k, else: :erlang.term_to_binary(k) end)
    bloom = Lsmex.Bloom.build(keys, target_fp: 0.01)
    File.write!(path <> ".bloom", :erlang.term_to_binary(bloom))
    :ok
  end

  @doc "Reads the value for key. Returns {:ok, value} | {:error, :not_found}."
  @spec get(Path.t(), binary()) :: {:ok, binary()} | {:error, :not_found}
  def get(path, key) do
    key_bin = if is_binary(key), do: key, else: :erlang.term_to_binary(key)

    bloom =
      case File.read(path <> ".bloom") do
        {:ok, data} -> :erlang.binary_to_term(data)
        _ -> nil
      end

    if bloom && not Lsmex.Bloom.member?(bloom, key_bin) do
      {:error, :not_found}
    else
      {:ok, data} = File.read(path)
      data_size = byte_size(data) - 8
      <<content::binary-size(data_size), index_offset::64>> = data
      index_data = binary_part(content, index_offset, data_size - index_offset)
      index = :erlang.binary_to_term(index_data)

      case Enum.find(index, fn {k, _off} -> k == key_bin end) do
        nil -> {:error, :not_found}
        {_k, offset} ->
          <<_before::binary-size(offset), _block_len::32, _crc::32,
            klen::32, vlen::32, _key::binary-size(klen), val::binary-size(vlen),
            _rest::binary>> = content
          {:ok, val}
      end
    end
  end
end
```

### Step 6: MemTable

```elixir
# lib/lsmex/memtable.ex
defmodule Lsmex.Memtable do
  @moduledoc """
  In-memory sorted table backed by ETS ordered_set.

  Stores {key, value} pairs where value is nil for tombstones (deletes).
  The ordered_set guarantees iteration in sorted key order, which is
  required for flushing to an SSTable.
  """

  @doc "Creates a new MemTable ETS table. Returns the table reference."
  @spec new() :: :ets.tid()
  def new do
    :ets.new(:memtable, [:ordered_set, :public])
  end

  @doc "Inserts or updates a key-value pair in the MemTable."
  @spec put(:ets.tid(), binary(), binary() | nil) :: true
  def put(table, key, value) do
    :ets.insert(table, {key, value})
  end

  @doc "Retrieves a value by key. Returns {:ok, value} | {:error, :not_found}."
  @spec get(:ets.tid(), binary()) :: {:ok, binary() | nil} | {:error, :not_found}
  def get(table, key) do
    case :ets.lookup(table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> {:error, :not_found}
    end
  end

  @doc "Returns all entries as a sorted list of {key, value} tuples."
  @spec to_sorted_list(:ets.tid()) :: [{binary(), binary() | nil}]
  def to_sorted_list(table) do
    :ets.tab2list(table)
  end

  @doc "Returns the number of entries in the MemTable."
  @spec size(:ets.tid()) :: non_neg_integer()
  def size(table) do
    :ets.info(table, :size)
  end

  @doc "Deletes the MemTable ETS table."
  @spec destroy(:ets.tid()) :: true
  def destroy(table) do
    :ets.delete(table)
  end
end
```

### Step 7: Engine — public API

```elixir
# lib/lsmex/engine.ex
defmodule Lsmex.Engine do
  use GenServer

  @moduledoc """
  Public API for the LSM-tree storage engine.

  Provides put/get/delete/scan operations. Writes go to the WAL first
  (for durability), then to the in-memory MemTable. When the MemTable
  exceeds a size threshold, it is flushed to disk as an SSTable.

  Reads check the MemTable first, then SSTables in reverse chronological
  order (newest first). The first match wins.
  """

  @memtable_flush_threshold 1_000
  @tombstone :__tombstone__

  defstruct [:data_dir, :wal_path, :memtable, :sstables, :seq]

  @doc "Starts the engine with the given data directory."
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @doc "Stores a key-value pair."
  @spec put(GenServer.server(), binary(), binary()) :: :ok
  def put(engine, key, value), do: GenServer.call(engine, {:put, key, value})

  @doc "Retrieves a value by key."
  @spec get(GenServer.server(), binary()) :: {:ok, binary()} | {:error, :not_found}
  def get(engine, key), do: GenServer.call(engine, {:get, key})

  @doc "Deletes a key by inserting a tombstone."
  @spec delete(GenServer.server(), binary()) :: :ok
  def delete(engine, key), do: GenServer.call(engine, {:delete, key})

  @doc "Scans keys in [from, to) range. Returns a list of {key, value} pairs."
  @spec scan(GenServer.server(), binary(), binary()) :: Enumerable.t()
  def scan(engine, from, to), do: GenServer.call(engine, {:scan, from, to})

  @impl true
  def init(opts) do
    data_dir = Keyword.fetch!(opts, :data_dir)
    File.mkdir_p!(data_dir)
    wal_path = Path.join(data_dir, "wal.log")
    memtable = Lsmex.Memtable.new()

    sstables = discover_sstables(data_dir)

    Lsmex.WAL.replay(wal_path, fn {key, value} ->
      Lsmex.Memtable.put(memtable, key, value)
    end)

    seq = length(sstables)

    {:ok, %__MODULE__{
      data_dir: data_dir,
      wal_path: wal_path,
      memtable: memtable,
      sstables: sstables,
      seq: seq
    }}
  end

  @impl true
  def handle_call({:put, key, value}, _from, state) do
    key_bin = ensure_binary(key)
    val_bin = ensure_binary(value)

    :ok = Lsmex.WAL.append(state.wal_path, key_bin, val_bin)
    Lsmex.Memtable.put(state.memtable, key_bin, val_bin)

    state = maybe_flush(state)
    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:get, key}, _from, state) do
    key_bin = ensure_binary(key)

    result =
      case Lsmex.Memtable.get(state.memtable, key_bin) do
        {:ok, nil} -> {:error, :not_found}
        {:ok, value} -> {:ok, value}
        {:error, :not_found} -> search_sstables(state.sstables, key_bin)
      end

    {:reply, result, state}
  end

  @impl true
  def handle_call({:delete, key}, _from, state) do
    key_bin = ensure_binary(key)

    :ok = Lsmex.WAL.append(state.wal_path, key_bin, nil)
    Lsmex.Memtable.put(state.memtable, key_bin, nil)

    state = maybe_flush(state)
    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:scan, from, to}, _from, state) do
    mem_entries =
      state.memtable
      |> Lsmex.Memtable.to_sorted_list()
      |> Enum.filter(fn {k, v} -> k >= from and k < to and v != nil end)

    {:reply, mem_entries, state}
  end

  defp maybe_flush(state) do
    if Lsmex.Memtable.size(state.memtable) >= @memtable_flush_threshold do
      flush_memtable(state)
    else
      state
    end
  end

  defp flush_memtable(state) do
    entries =
      state.memtable
      |> Lsmex.Memtable.to_sorted_list()
      |> Enum.reject(fn {_k, v} -> is_nil(v) end)

    sst_path = Path.join(state.data_dir, "sst_#{state.seq}.sst")
    Lsmex.SSTable.write(sst_path, entries)

    Lsmex.Memtable.destroy(state.memtable)
    new_memtable = Lsmex.Memtable.new()

    File.rm(state.wal_path)

    %{state |
      memtable: new_memtable,
      sstables: [sst_path | state.sstables],
      seq: state.seq + 1
    }
  end

  defp search_sstables([], _key), do: {:error, :not_found}
  defp search_sstables([sst_path | rest], key) do
    case Lsmex.SSTable.get(sst_path, key) do
      {:ok, value} -> {:ok, value}
      {:error, :not_found} -> search_sstables(rest, key)
    end
  end

  defp discover_sstables(data_dir) do
    data_dir
    |> File.ls!()
    |> Enum.filter(&String.ends_with?(&1, ".sst"))
    |> Enum.sort(:desc)
    |> Enum.map(&Path.join(data_dir, &1))
  end

  defp ensure_binary(val) when is_binary(val), do: val
  defp ensure_binary(val), do: :erlang.term_to_binary(val)
end
```

### Step 8: Application supervisor

```elixir
# lib/lsmex/application.ex
defmodule Lsmex.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = []
    opts = [strategy: :one_for_one, name: Lsmex.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 9: Given tests — must pass without modification

```elixir
# test/lsmex/engine_test.exs
defmodule Lsmex.EngineTest do
  use ExUnit.Case, async: false

  setup do
    dir = System.tmp_dir!() <> "/lsmex_test_#{:erlang.unique_integer([:positive])}"
    File.mkdir_p!(dir)
    {:ok, engine} = Lsmex.Engine.start_link(data_dir: dir)
    on_exit(fn -> File.rm_rf!(dir) end)
    {:ok, engine: engine, dir: dir}
  end

  test "put and get round-trip", %{engine: engine} do
    :ok = Lsmex.Engine.put(engine, "k1", "v1")
    assert {:ok, "v1"} = Lsmex.Engine.get(engine, "k1")
  end

  test "delete inserts tombstone; get returns :not_found", %{engine: engine} do
    :ok = Lsmex.Engine.put(engine, "del_key", "value")
    :ok = Lsmex.Engine.delete(engine, "del_key")
    assert {:error, :not_found} = Lsmex.Engine.get(engine, "del_key")
  end

  test "WAL replay after crash recovers all writes", %{engine: engine, dir: dir} do
    for i <- 1..100, do: :ok = Lsmex.Engine.put(engine, "key_#{i}", "val_#{i}")

    # Simulate crash by killing the engine process
    Process.exit(engine, :kill)
    Process.sleep(100)

    # Restart
    {:ok, engine2} = Lsmex.Engine.start_link(data_dir: dir)

    for i <- 1..100 do
      assert {:ok, "val_#{i}"} = Lsmex.Engine.get(engine2, "key_#{i}")
    end
  end

  test "scan returns sorted keys in range", %{engine: engine} do
    for i <- 1..10, do: :ok = Lsmex.Engine.put(engine, "key_#{String.pad_leading("#{i}", 3, "0")}", i)

    results = Lsmex.Engine.scan(engine, "key_001", "key_005") |> Enum.to_list()
    keys = Enum.map(results, fn {k, _} -> k end)

    assert keys == ["key_001", "key_002", "key_003", "key_004"]
  end
end
```

```elixir
# test/lsmex/bloom_test.exs
defmodule Lsmex.BloomTest do
  use ExUnit.Case, async: true

  test "false positive rate at 1% for 10,000 keys" do
    keys = for i <- 1..10_000, do: "key_#{i}"
    filter = Lsmex.Bloom.build(keys, target_fp: 0.01)

    # Absent keys (not in the filter)
    absent = for i <- 10_001..20_000, do: "key_#{i}"
    false_positives = Enum.count(absent, fn k -> Lsmex.Bloom.member?(filter, k) end)

    fp_rate = false_positives / 10_000
    assert fp_rate <= 0.02, "FP rate #{Float.round(fp_rate * 100, 2)}% exceeds 2% (target 1%)"
  end

  test "no false negatives: all inserted keys are found" do
    keys = for i <- 1..1_000, do: "k_#{i}"
    filter = Lsmex.Bloom.build(keys, target_fp: 0.01)
    assert Enum.all?(keys, fn k -> Lsmex.Bloom.member?(filter, k) end)
  end
end
```

### Step 10: Run the tests

```bash
mix test test/lsmex/ --trace
```

### Step 11: Benchmark

```elixir
# bench/lsmex_bench.exs
dir = System.tmp_dir!() <> "/lsmex_bench"
File.mkdir_p!(dir)
{:ok, engine} = Lsmex.Engine.start_link(data_dir: dir)

# Pre-populate for read benchmark
for i <- 1..100_000, do: Lsmex.Engine.put(engine, "key_#{i}", "value_#{i}")

Benchee.run(
  %{
    "sequential write" => fn ->
      Lsmex.Engine.put(engine, :crypto.strong_rand_bytes(8) |> Base.encode16(), "v")
    end,
    "random read — existing key" => fn ->
      k = :rand.uniform(100_000)
      Lsmex.Engine.get(engine, "key_#{k}")
    end,
    "random read — nonexistent key" => fn ->
      Lsmex.Engine.get(engine, "absent_#{:rand.uniform(1_000_000)}")
    end
  },
  parallel: 1,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Targets: 100k sequential writes/second, 200k random reads/second (warm Bloom filter).

### Why this works

Writes hit an in-memory skiplist; when it fills, it's flushed as an immutable SSTable. Compaction merges overlapping SSTables into larger levels, which bounds the number of files a read must consult and amortizes disk work.

---

## Benchmark

```elixir
# bench/lsm_bench.exs
Benchee.run(%{"put" => fn -> Lsm.put(db, k(), v()) end, "get" => fn -> Lsm.get(db, k()) end}, time: 10)
```

Target: 500,000 puts/second (memtable) and 10,000 gets/second at a 10 GB working set; p99 get < 5 ms.

---

## Trade-off analysis

| Aspect | LSM-tree (your impl) | B-tree | Hash map (pure in-memory) |
|--------|---------------------|--------|--------------------------|
| Write throughput | high (sequential) | moderate (random) | maximum |
| Read throughput | moderate (multi-level) | high (O(log N)) | maximum |
| Disk space amplification | moderate (tombstones until compaction) | low | none |
| Write amplification | high (compaction rewrites) | low | none |
| Range scan | efficient (sorted) | efficient (sorted) | depends on ordering |
| Crash recovery | WAL replay | B-tree journaling | none (in-memory) |

Fill in measured throughput from your benchmark.

Reflection: the compaction process rewrites every key that was ever deleted. A workload with 80% deletes will have very high write amplification. What compaction strategy would you use to minimize write amplification for delete-heavy workloads?

---

## Common production mistakes

**1. Not fsyncing the WAL before updating the MemTable**
The fsync guarantees that the WAL record is on disk before the in-memory state changes. Without it, a crash after the MemTable update but before the WAL disk write leaves the WAL behind the MemTable, and replay will miss the write.

**2. Reading from multiple SSTables without merging correctly**
The same key may appear in multiple SSTables (different write versions). When scanning, you must merge all SSTable iterators with the MemTable iterator and return the latest version for each key. Returning the first match without checking all levels gives stale data.

**3. Bloom filter not persisted with the SSTable**
If the Bloom filter is built only in memory, a process restart requires reading every SSTable to rebuild it. Persist the Bloom filter in a sidecar file (`.bloom`) alongside each SSTable. Load it on startup.

**4. Compaction running while snapshots are active**
Compaction must not discard a tombstone if any active snapshot could still see the key before the tombstone. Track the minimum snapshot sequence number and only compact below it.

## Reflection

- Under uniform-random reads vs hot-key reads, which compaction policy (size-tiered vs leveled) wins, and why?
- How would you tune the memtable size and compaction triggers if disk I/O were 10x slower (e.g., spinning disks)?

---

## Resources

- O'Neil, P. et al. (1996). *The Log-Structured Merge-Tree* — Acta Informatica
- [LevelDB implementation notes](https://github.com/google/leveldb/blob/main/doc/impl.md) — the reference implementation
- [RocksDB tuning guide](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide) — production compaction strategies
- Kleppmann, M. — *Designing Data-Intensive Applications* — Chapter 3 (Storage Engines)
