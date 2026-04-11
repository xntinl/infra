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

## The business problem

The distributed systems team needs to transfer large datasets (10–100GB model files) between datacenter nodes without relying on a central file server. A central server is both a bottleneck and a single point of failure. With P2P, every node that has completed a download becomes an upload source, and aggregate bandwidth scales with the number of participants.

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

```bash
mix new swarm --sup
cd swarm
mkdir -p lib/swarm/{dht,peer}
mkdir -p test/swarm bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/swarm/metadata.ex`

```elixir
defmodule Swarm.Metadata do
  @moduledoc """
  File metadata for a shared file.

  The info_hash identifies the file uniquely: it is the SHA-1 of the "info dict"
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
    # TODO: split data into chunks of piece_length bytes
    # HINT: use recursive :binary.part/3 — do not use binary matching that copies
    []
  end
end
```

### Step 4: `lib/swarm/piece_manager.ex`

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
      # Start with 0 availability for all pieces
      availability: Map.new(0..(num_pieces - 1), &{&1, 0}),
      peer_pieces: %{}
    }

    {:ok, state}
  end

  @impl true
  def handle_cast({:bitfield, peer_id, pieces}, state) do
    # TODO: store peer_pieces[peer_id] = pieces
    # TODO: increment availability count for each piece in the bitfield
    {:noreply, state}
  end

  @impl true
  def handle_cast({:have, peer_id, index}, state) do
    # TODO: add index to peer_pieces[peer_id]
    # TODO: increment availability[index]
    {:noreply, state}
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
    needed = MapSet.difference(peer_has, MapSet.union(state.have, state.requested))

    result =
      if MapSet.size(needed) == 0 do
        :none
      else
        # TODO: find piece in `needed` with minimum availability count
        # HINT: Enum.min_by(MapSet.to_list(needed), &Map.get(state.availability, &1, 0))
        # On tie: Enum.shuffle first, then min_by
        {:ok, MapSet.to_list(needed) |> hd()}
      end

    # TODO: mark selected piece as requested to avoid duplicate requests
    {:reply, result, state}
  end
end
```

### Step 5: `lib/swarm/rate_limiter.ex`

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
  4. Else: return {:wait, (n - current_tokens) / rate} — time in ms to wait

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

```elixir
# test/swarm/metadata_test.exs
defmodule Swarm.MetadataTest do
  use ExUnit.Case, async: true

  alias Swarm.Metadata

  test "pieces reassemble to original file" do
    data = :crypto.strong_rand_bytes(1_000_000)
    meta = Metadata.from_binary("test.bin", data, 256 * 1024)

    # Verify piece count
    expected_pieces = ceil(byte_size(data) / (256 * 1024))
    assert length(meta.pieces) == expected_pieces
  end

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

  test "select_piece returns :none when peer has nothing we need", %{pm: pm} do
    PieceManager.update_peer_bitfield(pm, "peer1", [])
    assert PieceManager.select_piece(pm, "peer1") == :none
  end

  test "select_piece returns a piece we need", %{pm: pm} do
    PieceManager.update_peer_bitfield(pm, "peer1", [0, 1, 2, 3])
    assert {:ok, index} = PieceManager.select_piece(pm, "peer1")
    assert index in [0, 1, 2, 3]
  end

  test "rarest piece is preferred when one peer has unique piece", %{pm: pm} do
    # Peer1 and Peer2 both have piece 0 (common)
    # Only Peer1 has piece 5 (rare — availability 1 vs 2 for piece 0)
    PieceManager.update_peer_bitfield(pm, "peer2", [0])
    PieceManager.update_peer_bitfield(pm, "peer1", [0, 5])

    # When selecting from peer1, should prefer piece 5 (less available)
    {:ok, index} = PieceManager.select_piece(pm, "peer1")
    assert index == 5
  end

  test "received pieces are not re-requested", %{pm: pm} do
    PieceManager.update_peer_bitfield(pm, "peer1", [0, 1])
    PieceManager.piece_received(pm, 0)
    PieceManager.piece_received(pm, 1)

    assert PieceManager.select_piece(pm, "peer1") == :none
  end
end
```

### Step 7: Run the tests

```bash
mix test test/swarm/ --trace
```

---

## Trade-off analysis

| Aspect | Kademlia DHT | Central tracker | mDNS/local discovery |
|--------|-------------|-----------------|---------------------|
| Single point of failure | none | tracker is critical | none |
| Discovery latency | O(log n) hops | O(1) with tracker | broadcast (LAN only) |
| Network size | millions of nodes | limited by tracker | LAN only |
| Implementation complexity | high | low | low |
| Privacy | pseudonymous (node ID) | tracker logs IPs | LAN visible |

Reflection: Kademlia routes through O(log n) nodes to find a value. With 1000 nodes in your simulation, that is about 10 hops. What is the main failure mode when nodes join and leave frequently (churn), and how does Kademlia's k-bucket structure mitigate it?

---

## Common production mistakes

**1. Requesting the same piece from two peers simultaneously (outside endgame)**
Without the `requested` set in `PieceManager`, two peers both receive a request for piece 5, both transfer it, and one transfer is wasted. Track `requested` separately from `have` and only request a piece from one peer at a time (except in endgame mode).

**2. Not verifying piece integrity after download**
If a peer sends corrupted data and you write it to the assembled file without verifying the SHA-256, the final file hash will not match. Always verify each piece against `meta.pieces[index]` before marking it as received. Corrupt peers should be disconnected and the piece re-requested.

**3. Blocking the peer connection process in `consume/3`**
If `rate_limiter.consume/3` uses `Process.sleep` to wait for tokens, the peer connection GenServer is blocked and cannot process incoming messages (including `have` announcements from the peer). Instead, return `{:wait, ms}` and use `Process.send_after/3` to schedule a retry.

**4. XOR distance computed on raw binaries vs. integers**
Kademlia XOR distance is defined over integers, not over binary strings. `<<a::160>> XOR <<b::160>>` in Elixir gives the binary XOR, which is correct only if you then interpret the result as a 160-bit integer for comparison. Verify that your distance comparison orders nodes correctly.

**5. Choking interval using `Process.sleep` instead of `send_after`**
The choker re-evaluates every 10 seconds which peers to unchoke. Using `Process.sleep(10_000)` in a loop blocks the entire process, preventing it from handling incoming upload speed updates. Use `Process.send_after(self(), :rechoke, 10_000)` and handle it in `handle_info`.

---

## Resources

- [BitTorrent Protocol Specification BEP-3](http://www.bittorrent.org/beps/bep_0003.html) — the handshake, messages (`bitfield`, `have`, `request`, `piece`, `choke`, `unchoke`), and endgame algorithm
- [BEP-5 — DHT Protocol](http://www.bittorrent.org/beps/bep_0005.html) — Kademlia implementation details including k-bucket structure and iterative lookup
- [Kademlia: A Peer-to-peer Information System Based on the XOR Metric](https://pdos.csail.mit.edu/~petar/papers/maymounkov-kademlia-lncs.pdf) — Maymounkov & Mazières (2002) — the original paper; short and readable
- [BitTorrent Economics Paper](http://bittorrent.org/bittorrentecon.pdf) — Bram Cohen's tit-for-tat analysis; explains why unchoke incentivizes uploading
- [Erlang `:crypto` documentation](https://www.erlang.org/doc/man/crypto.html) — for SHA-1 and SHA-256 piece hashing
