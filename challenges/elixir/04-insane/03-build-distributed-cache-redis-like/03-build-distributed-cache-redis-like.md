# Distributed Cache with Redis-Compatible Protocol

**Project**: `krebs` -- a distributed, multi-node in-memory cache that speaks RESP2 over TCP

---

## Project context

You are building `krebs`, a distributed in-memory cache with a subset of the Redis protocol. A standard `redis-cli` binary connects and issues commands without knowing it is talking to Elixir. Data is distributed across nodes using consistent hashing and replicated for fault tolerance.

Project structure:

```
krebs/
├── lib/
│   └── krebs/
│       ├── application.ex           # starts TCP listener, ring supervisor, pub/sub
│       ├── listener.ex              # :gen_tcp accept loop, spawns connection handlers
│       ├── connection.ex            # GenServer per TCP connection, RESP parser state machine
│       ├── resp.ex                  # RESP2 encoder and decoder
│       ├── command.ex               # command dispatch: SET, GET, DEL, TTL, SUBSCRIBE, PUBLISH
│       ├── ring.ex                  # consistent hashing ring with virtual nodes
│       ├── shard.ex                 # GenServer per shard: ETS-backed KV store with LRU
│       ├── replication.ex           # quorum writes, quorum reads across R replicas
│       ├── pubsub.ex                # pub/sub: subscribe, publish, cross-node routing
│       ├── ttl_sweeper.ex           # background process: active TTL expiration sweep
│       ├── aof.ex                   # append-only file: write before ack, replay on start
│       └── hinted_handoff.ex        # sloppy quorum: hinted writes, forwarding on recovery
├── test/
│   └── krebs/
│       ├── resp_test.exs            # RESP2 encoding/decoding correctness
│       ├── ring_test.exs            # consistent hashing distribution
│       ├── replication_test.exs     # quorum reads/writes, failure tolerance
│       ├── ttl_test.exs             # TTL expiration and lazy cleanup
│       ├── pubsub_test.exs          # cross-node pub/sub delivery
│       └── aof_test.exs             # persistence and replay
├── bench/
│   └── krebs_bench.exs
└── mix.exs
```

---

## The problem

You need a cache that multiple services connect to over TCP using the Redis protocol so existing tooling (redis-cli, redis-benchmark) works out of the box. The cache must be distributed: no single node holds all data, and the death of one node does not lose data. The protocol parser is the foundation -- every byte matters when redis-cli is your integration test.

---

## Why this design

**RESP2 first, distribution second**: start with the protocol. A complete RESP2 encoder and decoder is the prerequisite for every other feature. Only once redis-cli works end-to-end do you add distribution.

**Consistent hashing over modular hashing**: with modular hashing, adding a node requires rehashing nearly all keys. Consistent hashing with virtual nodes moves only `1/N` of keys when a node joins or leaves. This is the difference between a 10-second migration and a 10-minute migration on a live system.

**Sloppy quorum with hinted handoff**: strict quorum requires `R` live replicas to serve a write. Sloppy quorum (Dynamo-style) allows writes to go to any available node with a "hint" annotation, then forward to the target replica when it recovers. This trades strict consistency for availability during partial failures.

**LRU via doubly-linked list + hash map**: a true O(1) LRU cache requires both O(1) access (hash map) and O(1) eviction (doubly-linked list that tracks access order). ETS `:ordered_set` gives you sorted access but not access-order tracking. You must maintain the order yourself.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new krebs --sup
cd krebs
mkdir -p lib/krebs test/krebs bench
```

### Step 2: `mix.exs` -- dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: RESP2 protocol

The entire client-facing API depends on this being correct.

```elixir
# lib/krebs/resp.ex
defmodule Krebs.RESP do
  @moduledoc """
  RESP2 wire protocol encoder and decoder.

  Types:
    Simple strings: "+OK\\r\\n"
    Errors:         "-ERR message\\r\\n"
    Integers:       ":42\\r\\n"
    Bulk strings:   "$5\\r\\nhello\\r\\n"   (or "$-1\\r\\n" for nil)
    Arrays:         "*2\\r\\n$3\\r\\nfoo\\r\\n$3\\r\\nbar\\r\\n"
  """

  # --- Encoder ---

  @doc "Encodes an Elixir term into RESP2 binary."
  @spec encode(term()) :: binary()
  def encode(:ok), do: "+OK\r\n"
  def encode(nil), do: "$-1\r\n"
  def encode(n) when is_integer(n), do: ":#{n}\r\n"

  def encode(s) when is_binary(s) do
    "$#{byte_size(s)}\r\n#{s}\r\n"
  end

  def encode(list) when is_list(list) do
    elements = Enum.map(list, &encode/1) |> Enum.join()
    "*#{length(list)}\r\n#{elements}"
  end

  def encode({:error, msg}), do: "-ERR #{msg}\r\n"

  # --- Decoder ---

  @doc """
  Parses RESP2 bytes from a TCP stream. Returns {:ok, value, rest} when
  a complete message is available, or {:more, partial_state} when more
  bytes are needed.

  The connection handler maintains partial_state across TCP recv calls.
  """
  @spec parse(binary()) :: {:ok, term(), binary()} | {:more, binary()}
  def parse("+" <> rest) do
    case String.split(rest, "\r\n", parts: 2) do
      [str, remaining] ->
        value = if str == "OK", do: :ok, else: str
        {:ok, value, remaining}
      [_incomplete] ->
        {:more, "+" <> rest}
    end
  end

  def parse("-" <> rest) do
    case String.split(rest, "\r\n", parts: 2) do
      [msg, remaining] -> {:ok, {:error, msg}, remaining}
      [_incomplete] -> {:more, "-" <> rest}
    end
  end

  def parse(":" <> rest) do
    case String.split(rest, "\r\n", parts: 2) do
      [num_str, remaining] -> {:ok, String.to_integer(num_str), remaining}
      [_incomplete] -> {:more, ":" <> rest}
    end
  end

  def parse("$" <> rest) do
    case String.split(rest, "\r\n", parts: 2) do
      [len_str, remaining] ->
        len = String.to_integer(len_str)
        if len == -1 do
          {:ok, nil, remaining}
        else
          if byte_size(remaining) >= len + 2 do
            <<data::binary-size(len), "\r\n", final_rest::binary>> = remaining
            {:ok, data, final_rest}
          else
            {:more, "$" <> rest}
          end
        end
      [_incomplete] ->
        {:more, "$" <> rest}
    end
  end

  def parse("*" <> rest) do
    case String.split(rest, "\r\n", parts: 2) do
      [count_str, remaining] ->
        count = String.to_integer(count_str)
        if count == -1 do
          {:ok, nil, remaining}
        else
          parse_array_elements(remaining, count, [])
        end
      [_incomplete] ->
        {:more, "*" <> rest}
    end
  end

  def parse(buffer) when byte_size(buffer) == 0, do: {:more, buffer}
  def parse(buffer), do: {:more, buffer}

  defp parse_array_elements(rest, 0, acc), do: {:ok, Enum.reverse(acc), rest}

  defp parse_array_elements(rest, count, acc) do
    case parse(rest) do
      {:ok, element, remaining} ->
        parse_array_elements(remaining, count - 1, [element | acc])
      {:more, _} ->
        {:more, rest}
    end
  end
end
```

### Step 4: Consistent hashing ring

```elixir
# lib/krebs/ring.ex
defmodule Krebs.Ring do
  @moduledoc """
  Consistent hashing ring with virtual nodes.

  Each physical node owns V virtual token positions on the ring.
  A key is routed to the physical node whose first virtual token
  is encountered walking the ring clockwise from the key's hash.

  The ring is stored as a sorted list of {token, physical_node} pairs.
  Key lookup uses binary search: O(log(N * V)) per lookup.
  """

  defstruct [:tokens, :node_count]

  @doc "Creates a new ring with the given nodes and virtual node count V."
  @spec new([atom()], pos_integer()) :: %__MODULE__{}
  def new(nodes, v \\ 150) do
    tokens =
      for node <- nodes, i <- 1..v do
        token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
        {token, node}
      end
      |> Enum.uniq_by(fn {token, _} -> token end)
      |> Enum.sort_by(fn {token, _} -> token end)

    %__MODULE__{tokens: tokens, node_count: length(nodes)}
  end

  @doc "Returns the primary physical node for a key."
  @spec lookup(%__MODULE__{}, binary()) :: atom()
  def lookup(%__MODULE__{tokens: tokens}, key) do
    hash = :erlang.phash2(key, 0xFFFFFFFF)

    case Enum.find(tokens, fn {token, _node} -> token >= hash end) do
      {_token, node} -> node
      nil ->
        {_token, node} = List.first(tokens)
        node
    end
  end

  @doc "Returns the R replica nodes for a key (primary + R-1 successors)."
  @spec replicas(%__MODULE__{}, binary(), pos_integer()) :: [atom()]
  def replicas(%__MODULE__{tokens: tokens}, key, r) do
    hash = :erlang.phash2(key, 0xFFFFFFFF)

    start_idx =
      Enum.find_index(tokens, fn {token, _} -> token >= hash end) || 0

    ring_size = length(tokens)

    Stream.iterate(start_idx, fn i -> rem(i + 1, ring_size) end)
    |> Stream.map(fn i -> elem(Enum.at(tokens, i), 1) end)
    |> Stream.uniq()
    |> Enum.take(r)
  end

  @doc "Returns a new ring with the node added."
  @spec add_node(%__MODULE__{}, atom(), pos_integer()) :: %__MODULE__{}
  def add_node(%__MODULE__{tokens: existing_tokens, node_count: nc}, node, v \\ 150) do
    new_tokens =
      for i <- 1..v do
        token = :erlang.phash2("#{node}:#{i}", 0xFFFFFFFF)
        {token, node}
      end

    merged =
      (existing_tokens ++ new_tokens)
      |> Enum.uniq_by(fn {token, _} -> token end)
      |> Enum.sort_by(fn {token, _} -> token end)

    %__MODULE__{tokens: merged, node_count: nc + 1}
  end

  @doc "Returns a new ring with the node removed."
  @spec remove_node(%__MODULE__{}, atom()) :: %__MODULE__{}
  def remove_node(%__MODULE__{tokens: tokens, node_count: nc}, node) do
    filtered = Enum.reject(tokens, fn {_token, n} -> n == node end)
    %__MODULE__{tokens: filtered, node_count: max(nc - 1, 0)}
  end
end
```

### Step 5: Given tests -- must pass without modification

```elixir
# test/krebs/resp_test.exs
defmodule Krebs.RESPTest do
  use ExUnit.Case, async: true

  alias Krebs.RESP

  test "encodes simple string" do
    assert RESP.encode(:ok) == "+OK\r\n"
  end

  test "encodes bulk string" do
    assert RESP.encode("hello") == "$5\r\nhello\r\n"
  end

  test "encodes nil as null bulk string" do
    assert RESP.encode(nil) == "$-1\r\n"
  end

  test "encodes integer" do
    assert RESP.encode(42) == ":42\r\n"
  end

  test "encodes array" do
    assert RESP.encode(["SET", "foo", "bar"]) == "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
  end

  test "parses inline command" do
    assert {:ok, ["SET", "foo", "bar"], ""} =
      RESP.parse("*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n")
  end

  test "returns :more when buffer is incomplete" do
    assert {:more, _} = RESP.parse("*3\r\n$3\r\nSET\r\n")
  end

  test "handles pipelined commands in one buffer" do
    buf = "+OK\r\n:1\r\n"
    assert {:ok, :ok, ":1\r\n"} = RESP.parse(buf)
  end
end
```

```elixir
# test/krebs/ring_test.exs
defmodule Krebs.RingTest do
  use ExUnit.Case, async: true

  alias Krebs.Ring

  test "uniform distribution: no node holds more than 40% of keys" do
    nodes = [:node1, :node2, :node3]
    ring = Ring.new(nodes, 150)

    distribution =
      for _ <- 1..100_000, reduce: %{} do
        acc ->
          key = :crypto.strong_rand_bytes(16) |> Base.encode16()
          node = Ring.lookup(ring, key)
          Map.update(acc, node, 1, &(&1 + 1))
      end

    for {node, count} <- distribution do
      pct = count / 100_000
      assert pct < 0.40, "#{node} holds #{Float.round(pct * 100, 1)}% of keys (max 40%)"
    end
  end

  test "adding a node moves at most 1/N + 5% keys" do
    ring4 = Ring.new([:n1, :n2, :n3, :n4], 150)
    ring5 = Ring.add_node(ring4, :n5, 150)

    keys = for _ <- 1..10_000, do: :crypto.strong_rand_bytes(8) |> Base.encode16()

    moved =
      keys
      |> Enum.count(fn k -> Ring.lookup(ring4, k) != Ring.lookup(ring5, k) end)

    moved_pct = moved / 10_000
    assert moved_pct < 0.25, "expected ~20% movement, got #{Float.round(moved_pct * 100, 1)}%"
  end
end
```

### Step 6: Run the tests

```bash
mix test test/krebs/ --trace
```

### Step 7: Benchmark

```elixir
# bench/krebs_bench.exs
# Requires krebs to be running: iex -S mix
# Then: mix run bench/krebs_bench.exs

Benchee.run(
  %{
    "GET — cache hit"  => fn -> Krebs.get("bench_key") end,
    "SET — no replica" => fn -> Krebs.set("bench_key", "v", ttl: nil) end
  },
  parallel: 8,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 100,000 reads/second and 50,000 writes/second with AOF enabled and R=2 quorum.

---

## Trade-off analysis

| Aspect | Strict quorum (R + W > N) | Sloppy quorum + hinted handoff | Single-node (no replication) |
|--------|--------------------------|-------------------------------|------------------------------|
| Write availability | requires R live replicas | always writes to any node | always available |
| Read consistency | strong | eventual (until handoff completes) | strong |
| Failure tolerance | minority partition | sloppy -- survives any minority | none |
| Handoff complexity | none | must track hints, forward on recovery | none |
| Consistency model | linearizable reads | read-your-writes eventually | linearizable |

Reflection: in what scenarios does sloppy quorum return a stale value even after hinted handoff completes?

---

## Common production mistakes

**1. Parsing RESP inline vs multibulk incorrectly**
redis-cli sends commands as multibulk arrays (`*N\r\n$len\r\nword\r\n...`). Some clients send inline commands (`PING\r\n`). Both must parse correctly. A parser that only handles one will fail silently on the other.

**2. LRU eviction using `:ordered_set` access time**
ETS `:ordered_set` orders by key, not by access time. To implement LRU, you must maintain a separate doubly-linked structure that tracks access order. Sorting all entries to find the LRU is O(n) and will not meet the benchmark.

**3. One timer per key for TTL**
`Process.send_after` per key does not scale to millions of entries. Use a clock wheel or bucket expiration: group keys by their expiration second into ETS buckets. A single sweeper wakes up each second and evicts all keys in the current bucket.

**4. Forgetting to handle partial TCP writes**
`:gen_tcp.send/2` may not send the full binary in one call. Accumulate bytes in the connection process state until a complete RESP message is parsed.

**5. Blocking the accept loop**
The accept loop must only call `:gen_tcp.accept/1` and spawn a handler process. Any work beyond that blocks new connections. Each connection runs in its own process.

---

## Resources

- DeCandia, G. et al. (2007). *Dynamo: Amazon's Highly Available Key-Value Store* -- sections on consistent hashing, quorum, and hinted handoff
- [Redis RESP2 protocol specification](https://redis.io/docs/reference/protocol-spec) -- study the wire encoding in full detail
- [Redis `dict.c`](https://github.com/redis/redis/blob/unstable/src/dict.c), [`aof.c`](https://github.com/redis/redis/blob/unstable/src/aof.c) -- reference C implementations
- Karger, D. et al. (1997). *Consistent Hashing and Random Trees* -- the original MIT paper
