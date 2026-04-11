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

### Step 3: Write-ahead log

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

  @doc "Appends a record to the WAL and fsyncs. Returns :ok."
  def append(path, key, value) do
    # TODO
    # HINT: :file.open(path, [:append, :binary, :raw]) for low-level IO
    # HINT: crc = :erlang.crc32(<<key_len::32, val_len::32, key::binary, value_or_tombstone::binary>>)
    # HINT: :file.sync(fd) before returning
  end

  @doc "Replays the WAL. Calls callback({key, value}) for each valid record."
  def replay(path, callback) do
    # TODO: read binary, pattern match frame-by-frame
    # HINT: stop at first CRC32 mismatch (partial write at crash point)
  end
end
```

### Step 4: SSTable

```elixir
# lib/lsmex/sstable.ex
defmodule Lsmex.SSTable do
  @moduledoc """
  Immutable sorted-string table file.

  File layout:
    [data block 1] [data block 2] ... [footer]

  Each data block:
    <<block_len::32, crc32::32, N × (key_len::32, val_len::32, key::binary, val::binary)>>

  Footer (at end of file):
    <<index_offset::64>> followed by:
    index: N × (key::binary, byte_offset::64) for binary search

  Bloom filter is stored in a separate .bloom file alongside the .sst file.
  """

  @doc "Writes a sorted list of {key, value} pairs to a new SSTable file."
  def write(path, entries) do
    # TODO: write data blocks, build index, write footer
    # TODO: build Bloom filter from keys, write to path <> ".bloom"
  end

  @doc "Reads the value for key. Returns {:ok, value} | {:error, :not_found}."
  def get(path, key) do
    # TODO: check Bloom filter first — if absent, return {:error, :not_found}
    # TODO: binary search the index for key's block offset
    # TODO: read and verify CRC32 on the block
    # TODO: linear search within the block for the key
  end
end
```

### Step 5: Given tests — must pass without modification

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

### Step 6: Run the tests

```bash
mix test test/lsmex/ --trace
```

### Step 7: Benchmark

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

---

## Resources

- O'Neil, P. et al. (1996). *The Log-Structured Merge-Tree* — Acta Informatica
- [LevelDB implementation notes](https://github.com/google/leveldb/blob/main/doc/impl.md) — the reference implementation
- [RocksDB tuning guide](https://github.com/facebook/rocksdb/wiki/RocksDB-Tuning-Guide) — production compaction strategies
- Kleppmann, M. — *Designing Data-Intensive Applications* — Chapter 3 (Storage Engines)
