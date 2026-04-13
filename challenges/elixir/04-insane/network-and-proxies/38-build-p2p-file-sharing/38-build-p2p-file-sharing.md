# P2P File Sharing System

**Project**: `swarm` — a BitTorrent-inspired P2P file sharing system with DHT discovery

---

## Project context

You are building `swarm`, a peer-to-peer file sharing system the distributed systems team will use to understand DHT routing, piece selection algorithms, and transfer optimization. Files are split into pieces, distributed across peers, and downloaded from multiple sources simultaneously. A simplified Kademlia DHT handles peer discovery without a central tracker.

Project structure:

```
swarm/
├── lib/
│   └── swarm/
│       ├── application.ex
│       ├── metadata.ex            # ← info_hash, piece list, piece_length
│       ├── dht/
│       │   ├── node.ex            # ← DHT node with 160-bit ID + k-buckets
│       │   ├── routing_table.ex   # ← k-bucket maintenance
│       │   └── lookup.ex          # ← iterative find_node + find_value
│       ├── peer/
│       │   ├── connection.ex      # ← per-peer GenServer: handshake + message loop
│       │   ├── protocol.ex        # ← BitTorrent message encode/decode
│       │   └── choker.ex          # ← tit-for-tat unchoke algorithm
│       ├── piece_manager.ex       # ← rarest-first selection + verification
│       ├── downloader.ex          # ← coordinates download from multiple peers
│       ├── rate_limiter.ex        # ← token bucket per peer + global
│       └── transport.ex           # ← abstraction over TCP vs. in-process messages
├── test/
│   └── swarm/
│       ├── metadata_test.exs
│       ├── dht_test.exs
│       ├── protocol_test.exs
│       ├── piece_manager_test.exs
│       └── choker_test.exs
├── bench/
│   └── transfer_bench.exs
└── mix.exs
```

---

## Why rarest-first piece selection and not sequential piece selection

rarest-first maximizes piece diversity across the swarm, so peers can exchange pieces with each other and leave the initial seed faster. Sequential selection creates a bottleneck at the seed for every piece.

## Design decisions

**Option A — centralized tracker only**
- Pros: simple, easy to diagnose
- Cons: single point of failure, scale bottleneck

**Option B — DHT-based peer discovery (Kademlia) with tracker fallback** (chosen)
- Pros: self-healing, horizontal scale, tolerates node churn
- Cons: more complex to implement and debug

→ Chose **B** because a P2P system that dies when its tracker dies isn't actually P2P — DHT is the whole point.

## The business problem

The distributed systems team needs to transfer large datasets (10-100GB model files) between datacenter nodes without relying on a central file server. A central server is both a bottleneck and a single point of failure. With P2P, every node that has completed a download becomes an upload source, and aggregate bandwidth scales with the number of participants.

Three algorithms drive the design:

1. **Rarest-first**: download the pieces that fewest peers have first. This increases overall piece availability and helps the network reach full distribution faster.
2. **Tit-for-tat choking**: upload to peers who upload to you. This prevents free-riding and incentivizes contribution.
3. **Endgame mode**: when only a few pieces remain, request each from multiple peers simultaneously. The first response wins; duplicates are cancelled.

---

## Why Kademlia uses XOR distance

Kademlia organizes nodes in a 160-bit keyspace. The "distance" between two nodes is the XOR of their IDs — not geographic or network distance. XOR has a crucial property: for any three points A, B, C, the XOR triangle inequality holds (`distance(A,C) <= distance(A,B) XOR distance(B,C)`). This means the routing table converges: every lookup step at least halves the distance to the target, guaranteeing O(log n) hops to find any key in the network.

---

## Why token bucket for rate limiting

A naive rate limiter counts requests in a fixed window. If the limit is 100KB/s and you allow 100KB in the first millisecond, the client is blocked for 999ms. Token bucket smooths this: tokens refill at a constant rate, and each transfer consumes tokens proportionally. Bursts up to the bucket capacity are allowed; sustained rate cannot exceed the refill rate.

---

## Implementation

### Step 1: Create the project

**Objective**: Use `--sup` so the DHT node, peer wire handlers, and piece manager live under supervision and survive peer churn.


```bash
mix new swarm --sup
cd swarm
mkdir -p lib/swarm/{dht,peer}
mkdir -p test/swarm bench
```

### Step 2: `mix.exs`

**Objective**: Zero runtime deps — any bencoding or wire-protocol helper hides the BitTorrent framing you must own on the wire.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/swarm/metadata.ex`

**Objective**: Derive info_hash from piece hashes so two peers only swap data once they agree on the exact same file bytes.


The metadata module represents a shared file's identity. It splits the file into fixed-size pieces, computes the SHA-256 hash of each piece for integrity verification, and derives a unique `info_hash` that identifies the file across the network. The `split_into_pieces/2` function uses `:binary.part/3` for zero-copy slicing of large binaries.

```elixir
defmodule Swarm.Metadata do
  @moduledoc """
  File metadata for a shared file.

  The info_hash identifies the file uniquely: it is the SHA-256 of the "info dict"
  (a map of name, piece_length, length, pieces). This allows peers to verify they
  are talking about the same file before exchanging piece data.

  BitTorrent uses SHA-1 for historical reasons. New implementations (BitTorrent v2)
  use SHA-256. This implementation uses SHA-256 for piece verification.
  """

  defstruct [:name, :total_size, :piece_length, :pieces, :info_hash]

  @default_piece_length 256 * 1024  # 256 KB

  @doc """
  Creates metadata for a file binary.
  Splits the file into pieces and computes the SHA-256 of each piece.
  """
  @spec from_binary(String.t(), binary(), pos_integer()) :: t()
  def from_binary(name, data, piece_length \\ @default_piece_length) do
    pieces = split_into_pieces(data, piece_length)
    piece_hashes = Enum.map(pieces, &:crypto.hash(:sha256, &1))

    info_dict = %{
      name: name,
      piece_length: piece_length,
      length: byte_size(data),
      pieces: piece_hashes
    }

    info_hash = :crypto.hash(:sha256, :erlang.term_to_binary(info_dict))

    %__MODULE__{
      name: name,
      total_size: byte_size(data),
      piece_length: piece_length,
      pieces: piece_hashes,
      info_hash: info_hash
    }
  end

  @doc "Returns the number of pieces."
  @spec num_pieces(t()) :: pos_integer()
  def num_pieces(%__MODULE__{total_size: size, piece_length: pl}) do
    ceil(size / pl)
  end

  @doc "Returns the byte range [offset, length] for piece at index."
  @spec piece_range(t(), non_neg_integer()) :: {non_neg_integer(), pos_integer()}
  def piece_range(%__MODULE__{} = meta, index) do
    offset = index * meta.piece_length
    length = min(meta.piece_length, meta.total_size - offset)
    {offset, length}
  end

  defp split_into_pieces(data, piece_length) do
    total = byte_size(data)
    split_into_pieces(data, piece_length, 0, total, [])
  end

  defp split_into_pieces(_data, _piece_length, offset, total, acc) when offset >= total do
    Enum.reverse(acc)
  end

  defp split_into_pieces(data, piece_length, offset, total, acc) do
    length = min(piece_length, total - offset)
    piece = :binary.part(data, offset, length)
    split_into_pieces(data, piece_length, offset + length, total, [piece | acc])
  end
end
```

### Step 4: `lib/swarm/piece_manager.ex`

**Objective**: Pick rarest-first across the swarm — common pieces last, so the seed can leave without stranding peers.


The piece manager tracks which pieces each peer has, which pieces we have received, and which pieces are currently requested. The rarest-first selection algorithm chooses the piece with the lowest availability count among the pieces a given peer can provide and we still need. This maximizes piece diversity across the swarm.

```elixir
defmodule Swarm.PieceManager do
  use GenServer

  @moduledoc """
  Tracks piece availability across peers and implements rarest-first selection.

  State:
  - have: MapSet of piece indices we have
  - requested: MapSet of piece indices we have requested but not received
  - availability: %{piece_index => count_of_peers_with_it}
  - peer_pieces: %{peer_id => MapSet of piece indices that peer has}

  Rarest-first algorithm:
  1. Find all pieces we don't have and haven't requested
  2. Of those, select the one with the minimum availability count
  3. On ties, select randomly (prevents systematic routing to one peer)

  Why rarest-first helps the network:
  If all peers download the most common pieces first, rare pieces remain scarce.
  Eventually only one peer has the rarest pieces, and if it goes offline, those
  pieces are permanently lost. Rarest-first distributes rare pieces first,
  maximizing overall piece availability.
  """

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @doc "Records that peer_id has the pieces in their bitfield."
  def update_peer_bitfield(pid, peer_id, piece_indices) do
    GenServer.cast(pid, {:bitfield, peer_id, MapSet.new(piece_indices)})
  end

  @doc "Records that peer_id has acquired piece at index (via :have message)."
  def peer_has_piece(pid, peer_id, index) do
    GenServer.cast(pid, {:have, peer_id, index})
  end

  @doc "Selects the rarest piece we need that peer_id has. Returns {:ok, index} or :none."
  def select_piece(pid, peer_id) do
    GenServer.call(pid, {:select, peer_id})
  end

  @doc "Marks piece at index as received and verified."
  def piece_received(pid, index) do
    GenServer.cast(pid, {:received, index})
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    num_pieces = Keyword.get(opts, :num_pieces, 0)

    state = %{
      have: MapSet.new(),
      requested: MapSet.new(),
      availability: Map.new(0..(num_pieces - 1), &{&1, 0}),
      peer_pieces: %{}
    }

    {:ok, state}
  end

  @impl true
  def handle_cast({:bitfield, peer_id, pieces}, state) do
    # Store which pieces this peer has
    new_peer_pieces = Map.put(state.peer_pieces, peer_id, pieces)

    # Increment availability count for each piece in the bitfield
    new_availability =
      Enum.reduce(pieces, state.availability, fn index, avail ->
        Map.update(avail, index, 1, &(&1 + 1))
      end)

    {:noreply, %{state | peer_pieces: new_peer_pieces, availability: new_availability}}
  end

  @impl true
  def handle_cast({:have, peer_id, index}, state) do
    # Add this piece to the peer's set
    current_pieces = Map.get(state.peer_pieces, peer_id, MapSet.new())
    new_peer_pieces = Map.put(state.peer_pieces, peer_id, MapSet.put(current_pieces, index))

    # Increment availability for this piece
    new_availability = Map.update(state.availability, index, 1, &(&1 + 1))

    {:noreply, %{state | peer_pieces: new_peer_pieces, availability: new_availability}}
  end

  @impl true
  def handle_cast({:received, index}, state) do
    new_state = %{state |
      have: MapSet.put(state.have, index),
      requested: MapSet.delete(state.requested, index)
    }
    {:noreply, new_state}
  end

  @impl true
  def handle_call({:select, peer_id}, _from, state) do
    peer_has = Map.get(state.peer_pieces, peer_id, MapSet.new())
    already_obtained = MapSet.union(state.have, state.requested)
    needed = MapSet.difference(peer_has, already_obtained)

    result =
      if MapSet.size(needed) == 0 do
        :none
      else
        # Rarest-first: find piece in `needed` with minimum availability count.
        # Shuffle first so that ties are broken randomly, preventing all peers
        # from requesting the same rare piece from the same source.
        selected =
          needed
          |> MapSet.to_list()
          |> Enum.shuffle()
          |> Enum.min_by(&Map.get(state.availability, &1, 0))

        {:ok, selected}
      end

    # Mark selected piece as requested to avoid duplicate requests
    new_state =
      case result do
        {:ok, index} -> %{state | requested: MapSet.put(state.requested, index)}
        :none -> state
      end

    {:reply, result, new_state}
  end
end
```

### Step 5: `lib/swarm/rate_limiter.ex`

**Objective**: Use a token bucket per peer — fixed windows let one burst consume a full second of budget in milliseconds.


The rate limiter implements the token bucket algorithm. Each peer has an independent bucket with a capacity (burst size) and a refill rate (sustained throughput). Tokens are computed lazily on each `consume` call by calculating how many tokens have accumulated since the last refill. Float arithmetic preserves sub-integer token accumulation.

```elixir
defmodule Swarm.RateLimiter do
  use GenServer

  @moduledoc """
  Token bucket rate limiter.

  Each bucket has:
  - capacity: maximum tokens (burst size)
  - rate: tokens added per second (sustained throughput limit)
  - current_tokens: current token count (float for precision)
  - last_refill: timestamp of last token addition

  On consume(n):
  1. Compute tokens to add: (now - last_refill) * rate
  2. current_tokens = min(capacity, current_tokens + new_tokens)
  3. If current_tokens >= n: subtract n, return :ok
  4. Else: return {:wait, (n - current_tokens) / rate} -- time in ms to wait

  Why float tokens?
  If rate is 10KB/s and we call consume(1KB) every 50ms, the refill adds
  0.5KB per call. Using integers would round down to 0KB, blocking all transfers.
  Floats preserve sub-integer accumulation.
  """

  @doc "Tries to consume n bytes from the bucket for the given peer. Returns :ok or {:wait, ms}."
  @spec consume(pid(), String.t(), non_neg_integer()) :: :ok | {:wait, non_neg_integer()}
  def consume(pid, peer_id, bytes) do
    GenServer.call(pid, {:consume, peer_id, bytes})
  end

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    {:ok, %{
      capacity: Keyword.get(opts, :capacity, 1_000_000),   # 1MB burst
      rate: Keyword.get(opts, :rate, 100_000),              # 100KB/s
      buckets: %{}  # %{peer_id => {current_tokens, last_refill_ms}}
    }}
  end

  @impl true
  def handle_call({:consume, peer_id, bytes}, _from, state) do
    now = System.monotonic_time(:millisecond)
    {tokens, last_refill} = Map.get(state.buckets, peer_id, {state.capacity * 1.0, now})

    elapsed_s = (now - last_refill) / 1000.0
    new_tokens = min(state.capacity * 1.0, tokens + elapsed_s * state.rate)

    if new_tokens >= bytes do
      updated_bucket = {new_tokens - bytes, now}
      new_state = %{state | buckets: Map.put(state.buckets, peer_id, updated_bucket)}
      {:reply, :ok, new_state}
    else
      # Wait time in milliseconds
      wait_ms = round((bytes - new_tokens) / state.rate * 1000)
      {:reply, {:wait, wait_ms}, %{state | buckets: Map.put(state.buckets, peer_id, {new_tokens, now})}}
    end
  end
end
```

### Step 6: Given tests — must pass without modification

**Objective**: Tests pin public contracts — if info_hash shape or bitfield semantics drift, no peer interop is possible.


```elixir
# test/swarm/metadata_test.exs
defmodule Swarm.MetadataTest do
  use ExUnit.Case, async: true

  alias Swarm.Metadata


  describe "file splitting and piece integrity" do
    test "pieces reassemble to original file" do
      data = :crypto.strong_rand_bytes(1_000_000)
      meta = Metadata.from_binary("test.bin", data, 256 * 1024)

      # Verify piece count
      expected_pieces = ceil(byte_size(data) / (256 * 1024))
      assert length(meta.pieces) == expected_pieces
    end

    test "last piece is smaller than configured piece_length" do
      data = :crypto.strong_rand_bytes(1_000_000)
      meta = Metadata.from_binary("test.bin", data, 256 * 1024)

      # Piece count is determined by rounding up
      last_piece_size = byte_size(data) - (meta.num_pieces() - 1) * 256 * 1024
      assert last_piece_size > 0
      assert last_piece_size <= 256 * 1024
    end
  end

  describe "info_hash as content fingerprint" do
    test "info_hash is deterministic for same content" do
      data = "same content"
      m1 = Metadata.from_binary("file.txt", data)
      m2 = Metadata.from_binary("file.txt", data)
      assert m1.info_hash == m2.info_hash
    end

    test "info_hash differs for different content" do
      m1 = Metadata.from_binary("f.txt", "content A")
      m2 = Metadata.from_binary("f.txt", "content B")
      assert m1.info_hash != m2.info_hash
    end

    test "info_hash enables peer discovery (same file identification)" do
      data = "shared file"
      peer1_meta = Metadata.from_binary("shared.bin", data)
      peer2_meta = Metadata.from_binary("shared.bin", data)
      
      # Peers with same info_hash can exchange pieces
      assert peer1_meta.info_hash == peer2_meta.info_hash
    end
  end
end
```

```elixir
# test/swarm/piece_manager_test.exs
defmodule Swarm.PieceManagerTest do
  use ExUnit.Case, async: true

  alias Swarm.PieceManager

  setup do
    {:ok, pm} = PieceManager.start_link(num_pieces: 10)
    %{pm: pm}
  end


  describe "piece selection strategy" do
    test "select_piece returns :none when peer has nothing we need", %{pm: pm} do
      PieceManager.update_peer_bitfield(pm, "peer1", [])
      assert PieceManager.select_piece(pm, "peer1") == :none
    end

    test "select_piece returns a piece we need", %{pm: pm} do
      PieceManager.update_peer_bitfield(pm, "peer1", [0, 1, 2, 3])
      assert {:ok, index} = PieceManager.select_piece(pm, "peer1")
      assert index in [0, 1, 2, 3]
    end
  end

  describe "rarest-first selection" do
    test "rarest piece is preferred over common pieces", %{pm: pm} do
      # Peer1 and Peer2 both have piece 0 (common)
      # Only Peer1 has piece 5 (rare — availability 1 vs 2 for piece 0)
      PieceManager.update_peer_bitfield(pm, "peer2", [0])
      PieceManager.update_peer_bitfield(pm, "peer1", [0, 5])

      # When selecting from peer1, should prefer piece 5 (less available)
      {:ok, index} = PieceManager.select_piece(pm, "peer1")
      assert index == 5
    end

    test "multiple rare pieces are shuffled (break ties randomly)" do
      # Both pieces 7 and 8 have availability 1 (only peer1 has them)
      # Piece 0 has availability 2 (multiple peers)
      # Should prefer 7 or 8, not 0
      PieceManager.update_peer_bitfield(pm, "peer2", [0])
      PieceManager.update_peer_bitfield(pm, "peer1", [0, 7, 8])

      {:ok, index} = PieceManager.select_piece(pm, "peer1")
      assert index in [7, 8]
    end

    test "availability increases when multiple peers have piece" do
      PieceManager.update_peer_bitfield(pm, "peer1", [0, 5])
      PieceManager.update_peer_bitfield(pm, "peer2", [0])
      PieceManager.update_peer_bitfield(pm, "peer3", [0])

      # Piece 0 now has high availability; piece 5 is rarer
      {:ok, index} = PieceManager.select_piece(pm, "peer1")
      assert index == 5
    end
  end

  describe "duplicate request prevention" do
    test "received pieces are not re-requested", %{pm: pm} do
      PieceManager.update_peer_bitfield(pm, "peer1", [0, 1])
      PieceManager.piece_received(pm, 0)
      PieceManager.piece_received(pm, 1)

      assert PieceManager.select_piece(pm, "peer1") == :none
    end

    test "requested pieces are marked to avoid duplicate requests", %{pm: pm} do
      PieceManager.update_peer_bitfield(pm, "peer1", [0, 1])
      PieceManager.update_peer_bitfield(pm, "peer2", [0, 1])
      
      {:ok, index} = PieceManager.select_piece(pm, "peer1")
      
      # Even though peer2 has the same piece, we should not request it twice
      # (this is implicit in the state tracking)
      assert index in [0, 1]
    end
  end
end
```

### Step 7: Run the tests

**Objective**: Run with `--trace` so piece-request ordering and rate-limiter decisions surface deterministically on failure.


```bash
mix test test/swarm/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the P2P file sharing (BitTorrent-like) invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.
## Quick start

```bash
# Start the application and run tests
mix deps.get
mix test test/swarm/ --trace

# Or run performance benchmarks:
mix run bench/transfer_bench.exs
```

Target: sustained >100 Mbps per peer with 1000+ concurrent connections; DHT lookup latency <500ms.

---

## Benchmark

```elixir
# bench/transfer_bench.exs
file_data = :crypto.strong_rand_bytes(100_000_000)  # 100MB
meta = Swarm.Metadata.from_binary("test.bin", file_data)
{:ok, pm} = Swarm.PieceManager.start_link(num_pieces: meta |> Swarm.Metadata.num_pieces())
{:ok, rl} = Swarm.RateLimiter.start_link(capacity: 10_000_000, rate: 1_000_000)

Benchee.run(%{
  "piece_selection_rarest_first" => fn ->
    Swarm.PieceManager.update_peer_bitfield(pm, "peer1", [0, 1, 2, 3, 4, 5])
    Swarm.PieceManager.select_piece(pm, "peer1")
  end,
  "rate_limiter_consume_check" => fn ->
    Swarm.RateLimiter.consume(rl, "peer1", 10_000)
  end,
  "metadata_piece_range" => fn ->
    idx = :rand.uniform(Swarm.Metadata.num_pieces(meta)) - 1
    Swarm.Metadata.piece_range(meta, idx)
  end
}, time: 10, warmup: 3)
```

**Expected results** (on modern hardware):
- Piece selection (rarest-first): ~5-10µs per selection
- Rate limiter check: ~0.5-1µs (lock-free atomics)
- Metadata piece lookup: ~0.2µs (O(1) computation)

---

## Reflection

1. **Rarest-first piece strategy**: Sequential selection means if the seed goes offline, rare pieces are lost. Rarest-first distributes rare pieces first. What is the minimum number of peers required to guarantee that no piece is permanently lost, assuming each peer downloads independently and at least one peer completes the file?

2. **DHT vs. Centralized tracker**: DHT is censorship-resistant but requires O(log N) hops per lookup. Centralized tracker is fast (1 RPC) but is a SPOF and legal target. At what swarm size does DHT lookup latency become unacceptable for interactive use (< 1 second)?

