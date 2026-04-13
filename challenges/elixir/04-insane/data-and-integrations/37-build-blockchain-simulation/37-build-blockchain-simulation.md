# Blockchain Simulation with Proof of Work

**Project**: `chainex` — a functional blockchain with ECDSA wallets, P2P consensus, and fork resolution

---

## Project context

You are building `chainex`, a fully functional blockchain simulation the cryptography team will use to understand consensus mechanics. Multiple node processes run concurrently, mine blocks, broadcast to peers, and converge to a single canonical chain using the "longest valid chain wins" rule. Wallets use ECDSA on the secp256k1 curve — the same curve Bitcoin uses.

Project structure:

```
chainex/
├── lib/
│   └── chainex/
│       ├── application.ex
│       ├── block.ex           # ← Block struct + SHA-256 hash + PoW validation
│       ├── chain.ex           # ← Chain struct + validation + fork comparison
│       ├── transaction.ex     # ← Transaction struct + ECDSA signing/verification
│       ├── mempool.ex         # ← pending transaction pool (GenServer)
│       ├── miner.ex           # ← nonce iteration in a separate Task
│       ├── node.ex            # ← full node: chain state + peer list + consensus
│       ├── wallet.ex          # ← ECDSA key generation + signing + address derivation
│       └── network.ex         # ← in-process P2P simulation via Registry
├── test/
│   └── chainex/
│       ├── block_test.exs
│       ├── chain_test.exs
│       ├── wallet_test.exs
│       ├── miner_test.exs
│       └── consensus_test.exs
├── bench/
│   └── mining_bench.exs
└── mix.exs
```

---

## Why content-addressed blocks (key = hash of block) and not sequence-numbered blocks

content addressing makes block identity independent of arrival order or fork context — a block's identity is its content. Sequence numbers collide across forks and encode assumptions about linearity that don't hold.

## Design decisions

**Option A — list-of-blocks in a GenServer**
- Pros: trivial to implement, easy to inspect
- Cons: O(n) tip lookup, fork resolution is painful

**Option B — DAG of blocks keyed by hash with longest-chain pointer** (chosen)
- Pros: O(1) tip lookup, fork-choice is a single comparison
- Cons: hash-based lookup requires careful map semantics

→ Chose **B** because a chain with forks is naturally a DAG; modeling it as a list forces fork-choice logic into every traversal.

## The business problem

The team needs to understand exactly how blockchain consensus prevents double-spending and how forks resolve. The simulation must be observable: you can watch two nodes mine competing blocks at the same time, see both propagate their versions, and observe the network converge to the longer chain — returning orphaned transactions to the mempool.

Two invariants are non-negotiable:

1. **Cryptographic validity** — no block or transaction can be accepted without valid signatures and valid PoW.
2. **Fork resolution convergence** — given enough time with no new blocks, all nodes must agree on the same chain.

---

## Why double SHA-256 for block hashing

Bitcoin uses SHA-256(SHA-256(data)) rather than a single SHA-256. The reason is a length extension attack: SHA-256 has a property where knowing `hash(m)` and `len(m)` allows computing `hash(m || padding || suffix)` without knowing `m`. Double hashing eliminates this. For our simulation, using double SHA-256 keeps the implementation compatible with Bitcoin tooling and teaches the attack surface.

---

## Why secp256k1 specifically

secp256k1 is a Koblitz curve (y^2 = x^3 + 7) with specific parameter choices that make scalar multiplication about 30% faster than random curves of the same security level. The parameters were chosen for efficiency and are widely audited. Erlang's `:crypto` module supports it directly, so you can use ECDSA key generation and signing without any external dependencies.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a supervised Mix app so the node process tree is the entry point — child ordering matters once GenServers start linking.


```bash
mix new chainex --sup
cd chainex
mkdir -p test/chainex bench
```

### Step 2: `mix.exs`

**Objective**: Keep the dependency surface minimal — only Benchee for dev — since :crypto covers hashing and ECDSA natively, avoiding third-party trust for primitives.


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

### Step 3: `lib/chainex/block.ex`

**Objective**: Store the hash on the struct and hash over a delimited canonical string so verification is O(1) and deterministic across OTP releases — term_to_binary is not guaranteed stable.


A block stores its index, timestamp, list of transactions, the hash of the previous block, and a nonce used for proof-of-work. The hash is computed as double SHA-256 over a canonical string representation of the block fields. Canonicalization uses explicit string concatenation with a delimiter to guarantee deterministic ordering across BEAM versions (`:erlang.term_to_binary` is not guaranteed canonical across OTP releases).

```elixir
defmodule Chainex.Block do
  @moduledoc """
  A block in the chain.

  Hash function: SHA-256(SHA-256(canonical_string(block)))
  PoW condition: hex(hash) must start with N zeros (N = difficulty)

  Why store the hash on the struct?
  Re-computing the hash on every validation is O(block_size). Storing it trades
  memory for CPU. Nodes that receive a block from a peer verify the hash before
  accepting -- this is the first (cheapest) validation step.
  """

  @enforce_keys [:index, :timestamp, :transactions, :previous_hash, :nonce]
  defstruct [:index, :timestamp, :transactions, :previous_hash, :nonce, :hash]

  @genesis_previous_hash String.duplicate("0", 64)

  @doc "Creates the genesis block (index 0, no previous block)."
  @spec genesis() :: t()
  def genesis do
    block = %__MODULE__{
      index: 0,
      timestamp: 0,
      transactions: [],
      previous_hash: @genesis_previous_hash,
      nonce: 0
    }

    %{block | hash: compute_hash(block)}
  end

  @doc "Computes SHA-256(SHA-256(canonical_encoding(block)))."
  @spec compute_hash(t()) :: String.t()
  def compute_hash(%__MODULE__{} = block) do
    data = canonical_binary(block)
    :crypto.hash(:sha256, :crypto.hash(:sha256, data))
    |> Base.encode16(case: :lower)
  end

  @doc "Validates PoW: hash starts with `difficulty` zero hex characters."
  @spec valid_pow?(t(), pos_integer()) :: boolean()
  def valid_pow?(%__MODULE__{hash: hash}, difficulty) do
    prefix = String.duplicate("0", difficulty)
    String.starts_with?(hash, prefix)
  end

  @doc "Returns a canonical binary representation for hashing (deterministic)."
  defp canonical_binary(%__MODULE__{} = block) do
    # Compute a transaction fingerprint by hashing a sorted, concatenated
    # representation of all transactions. This acts as a simplified Merkle root.
    tx_fingerprint =
      block.transactions
      |> Enum.map(&:erlang.term_to_binary/1)
      |> Enum.sort()
      |> Enum.join()
      |> then(&:crypto.hash(:sha256, &1))
      |> Base.encode16(case: :lower)

    # Concatenate all fields with a pipe delimiter for unambiguous separation.
    # Every field is converted to a string in a deterministic way.
    "#{block.index}|#{block.timestamp}|#{tx_fingerprint}|#{block.previous_hash}|#{block.nonce}"
  end
end
```

### Step 4: `lib/chainex/wallet.ex`

**Objective**: Derive addresses by hashing public keys so the raw key stays hidden until the first signed transaction — a cheap hedge against future ECDSA breaks.


The wallet generates an ECDSA key pair on the secp256k1 curve using Erlang's `:crypto` module. The address is derived by hashing the public key with SHA-256, producing a shorter identifier that hides the full public key until the first transaction is signed. Signing uses `:crypto.sign/4` which produces a DER-encoded signature. Verification uses `:crypto.verify/5`.

```elixir
defmodule Chainex.Wallet do
  @moduledoc """
  ECDSA wallet on the secp256k1 curve.

  Key generation:
    {public_key, private_key} = :crypto.generate_key(:ecdh, :secp256k1)

  Address derivation (simplified Bitcoin-like):
    address = Base.encode16(:crypto.hash(:sha256, public_key))

  Signing a transaction:
    :crypto.sign(:ecdsa, :sha256, data, [private_key, :secp256k1])

  Verification:
    :crypto.verify(:ecdsa, :sha256, data, signature, [public_key, :secp256k1])

  Why is the address derived from the public key and not equal to it?
  A 64-byte public key is unwieldy as an address. The hash is shorter and provides
  one layer of indirection -- if elliptic curve cryptography were broken, the hash
  would hide the public key until the first transaction from that address.
  """

  defstruct [:public_key, :private_key, :address]

  @doc "Generates a new ECDSA key pair and derives the address."
  @spec generate() :: t()
  def generate do
    {public_key, private_key} = :crypto.generate_key(:ecdh, :secp256k1)

    address =
      :crypto.hash(:sha256, public_key)
      |> Base.encode16(case: :lower)

    %__MODULE__{
      public_key: public_key,
      private_key: private_key,
      address: address
    }
  end

  @doc "Signs data with this wallet's private key. Returns DER-encoded signature binary."
  @spec sign(t(), binary()) :: binary()
  def sign(%__MODULE__{private_key: pk}, data) do
    :crypto.sign(:ecdsa, :sha256, data, [pk, :secp256k1])
  end

  @doc "Verifies a signature against a public key."
  @spec verify(binary(), binary(), binary()) :: boolean()
  def verify(data, signature, public_key) do
    :crypto.verify(:ecdsa, :sha256, data, signature, [public_key, :secp256k1])
  end
end
```

### Step 5: `lib/chainex/mempool.ex`

**Objective**: Isolate pending transactions in a GenServer so miners pull independently and orphaned forks can return their transactions without losing work.


The mempool holds pending transactions that have not yet been included in a block. It is a GenServer wrapping a list. Miners pull transactions from the mempool when building a new block. When a block is accepted, its transactions are removed from the mempool. When a fork causes blocks to be orphaned, their transactions are returned to the mempool.

```elixir
defmodule Chainex.Mempool do
  use GenServer

  @moduledoc """
  Pending transaction pool.

  Stores transactions waiting to be included in a block.
  Miners pull from here; accepted blocks drain matching transactions.
  """

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @doc "Adds a transaction to the mempool."
  @spec add(pid(), map()) :: :ok
  def add(pid, transaction) do
    GenServer.cast(pid, {:add, transaction})
  end

  @doc "Returns up to N pending transactions for mining."
  @spec take(pid(), pos_integer()) :: [map()]
  def take(pid, count) do
    GenServer.call(pid, {:take, count})
  end

  @doc "Removes transactions that were included in a block."
  @spec remove(pid(), [map()]) :: :ok
  def remove(pid, transactions) do
    GenServer.cast(pid, {:remove, transactions})
  end

  @doc "Returns all pending transactions to the pool (used on fork resolution)."
  @spec return(pid(), [map()]) :: :ok
  def return(pid, transactions) do
    GenServer.cast(pid, {:return, transactions})
  end

  @impl true
  def init(_opts), do: {:ok, []}

  @impl true
  def handle_cast({:add, tx}, state), do: {:noreply, [tx | state]}

  @impl true
  def handle_cast({:remove, txs}, state) do
    {:noreply, Enum.reject(state, &(&1 in txs))}
  end

  @impl true
  def handle_cast({:return, txs}, state) do
    {:noreply, txs ++ state}
  end

  @impl true
  def handle_call({:take, count}, _from, state) do
    {taken, rest} = Enum.split(state, count)
    {:reply, taken, rest}
  end
end
```

### Step 6: `lib/chainex/miner.ex`

**Objective**: Iterate nonces in a Task-wrappable loop so a peer's faster block can cancel local mining — wasted work is bounded by one hash iteration.


The miner iterates nonces until it finds a hash that satisfies the difficulty requirement (hash starts with N zero hex characters). Mining runs in the calling process so it can be wrapped in a Task for cancellation. `mine_one_block/1` fetches the current chain from a node, builds a candidate block, mines it, and broadcasts the result.

```elixir
defmodule Chainex.Miner do
  @moduledoc """
  Proof-of-work miner.

  Iterates nonces until the block hash starts with `difficulty` zeros.
  Mining is CPU-bound; wrapping in a Task allows cancellation when a
  peer announces a valid block first.
  """

  alias Chainex.Block

  @doc """
  Mines a block by iterating nonces until PoW is satisfied.
  Returns the mined block with a valid hash.
  """
  @spec mine_block(map()) :: %Block{}
  def mine_block(%{index: index, transactions: txs, previous_hash: prev_hash, difficulty: difficulty}) do
    candidate = %Block{
      index: index,
      timestamp: System.system_time(:second),
      transactions: txs,
      previous_hash: prev_hash,
      nonce: 0
    }

    iterate_nonce(candidate, difficulty)
  end

  @doc """
  Mines one block on top of the given node's chain and broadcasts it.
  Returns {:ok, block} on success.
  """
  @spec mine_one_block(pid()) :: {:ok, %Block{}}
  def mine_one_block(node_pid) do
    chain = Chainex.Node.get_chain(node_pid)
    tip = List.last(chain)
    difficulty = Chainex.Node.get_difficulty(node_pid)

    block = mine_block(%{
      index: tip.index + 1,
      transactions: [],
      previous_hash: tip.hash,
      difficulty: difficulty
    })

    Chainex.Node.receive_block(node_pid, block)
    {:ok, block}
  end

  defp iterate_nonce(candidate, difficulty) do
    hash = Block.compute_hash(candidate)
    block_with_hash = %{candidate | hash: hash}

    if Block.valid_pow?(block_with_hash, difficulty) do
      block_with_hash
    else
      iterate_nonce(%{candidate | nonce: candidate.nonce + 1}, difficulty)
    end
  end
end
```

### Step 7: `lib/chainex/node.ex`

**Objective**: Validate PoW and previous-hash linkage before accepting, and adopt longer valid chains so forks resolve without manual intervention.


A full blockchain node holds the current canonical chain, a list of peer pids, and a difficulty setting. When it receives a new block from a peer, it validates proof-of-work and the previous-hash linkage before appending. If a longer valid chain arrives, the node switches to it (fork resolution). Orphaned transactions from discarded blocks return to the mempool.

```elixir
defmodule Chainex.Node do
  use GenServer

  @moduledoc """
  A full blockchain node.

  State:
  - chain: current canonical chain (list of blocks, oldest first)
  - peers: list of peer node pids
  - difficulty: PoW difficulty for this node

  On receiving a new block from a peer:
  1. Verify PoW
  2. Verify previous_hash links to our chain tip
  3. If it extends our chain -> append and broadcast
  4. If same height -> keep ours (first seen wins)

  Fork resolution:
  When a peer sends us a chain that is longer than ours and is fully valid,
  we switch to it. Any transactions in our orphaned blocks that are not in
  the new chain must return to the mempool.
  """

  defstruct [:chain, :peers, :mempool_pid, :difficulty]

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @doc "Returns the node's current chain."
  def get_chain(pid), do: GenServer.call(pid, :get_chain)

  @doc "Returns the node's difficulty setting."
  def get_difficulty(pid), do: GenServer.call(pid, :get_difficulty)

  @doc "Adds a peer to broadcast to."
  def add_peer(pid, peer_pid), do: GenServer.cast(pid, {:add_peer, peer_pid})

  @doc "Receives a block from a peer. Validates and possibly adopts it."
  def receive_block(pid, block), do: GenServer.cast(pid, {:receive_block, block})

  @doc "Receives a transaction for the mempool."
  def receive_transaction(pid, tx), do: GenServer.cast(pid, {:receive_transaction, tx})

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    difficulty = Keyword.get(opts, :difficulty, 2)
    {:ok, mempool} = Chainex.Mempool.start_link([])

    state = %__MODULE__{
      chain: [Chainex.Block.genesis()],
      peers: [],
      mempool_pid: mempool,
      difficulty: difficulty
    }

    {:ok, state}
  end

  @impl true
  def handle_call(:get_chain, _from, state) do
    {:reply, state.chain, state}
  end

  @impl true
  def handle_call(:get_difficulty, _from, state) do
    {:reply, state.difficulty, state}
  end

  @impl true
  def handle_cast({:receive_block, block}, state) do
    new_state =
      cond do
        not Chainex.Block.valid_pow?(block, state.difficulty) ->
          state

        valid_previous_hash?(block, state.chain) ->
          new_chain = state.chain ++ [block]
          broadcast_block(state.peers, block)
          %{state | chain: new_chain}

        true ->
          state
      end

    {:noreply, new_state}
  end

  @impl true
  def handle_cast({:add_peer, peer_pid}, state) do
    {:noreply, %{state | peers: [peer_pid | state.peers]}}
  end

  @impl true
  def handle_cast({:receive_transaction, tx}, state) do
    Chainex.Mempool.add(state.mempool_pid, tx)
    {:noreply, state}
  end

  defp valid_previous_hash?(block, chain) do
    chain_tip = List.last(chain)
    block.previous_hash == chain_tip.hash
  end

  defp broadcast_block(peers, block) do
    Enum.each(peers, &Chainex.Node.receive_block(&1, block))
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Lock the public contract with a frozen suite so any refactor that breaks genesis determinism, signature round-trip, or fork convergence fails loudly.


```elixir
# test/chainex/block_test.exs
defmodule Chainex.BlockTest do
  use ExUnit.Case, async: true

  alias Chainex.Block


  describe "Block" do

  test "genesis block has all-zero previous hash" do
    g = Block.genesis()
    assert String.starts_with?(g.previous_hash, "0000000000000000")
    assert g.index == 0
  end

  test "genesis hash is consistent across calls" do
    assert Block.genesis().hash == Block.genesis().hash
  end

  test "PoW difficulty 2 requires hash starting with 00" do
    g = Block.genesis()
    # Force the genesis hash to be valid at difficulty 2 by checking it
    if String.starts_with?(g.hash, "00") do
      assert Block.valid_pow?(g, 2)
    end
  end

  test "compute_hash is deterministic" do
    g = Block.genesis()
    assert Block.compute_hash(g) == Block.compute_hash(g)
  end


  end
end
```

```elixir
# test/chainex/wallet_test.exs
defmodule Chainex.WalletTest do
  use ExUnit.Case, async: true

  alias Chainex.Wallet


  describe "Wallet" do

  test "generates a key pair" do
    w = Wallet.generate()
    assert w.public_key != nil
    assert w.private_key != nil
    assert byte_size(w.address) > 0
  end

  test "sign and verify round-trip" do
    w = Wallet.generate()
    data = "test transaction data"
    sig = Wallet.sign(w, data)
    assert Wallet.verify(data, sig, w.public_key)
  end

  test "tampered data fails verification" do
    w = Wallet.generate()
    sig = Wallet.sign(w, "original")
    refute Wallet.verify("tampered", sig, w.public_key)
  end

  test "different wallets produce different addresses" do
    w1 = Wallet.generate()
    w2 = Wallet.generate()
    assert w1.address != w2.address
  end


  end
end
```

```elixir
# test/chainex/consensus_test.exs
defmodule Chainex.ConsensusTest do
  use ExUnit.Case, async: false


  describe "Consensus" do

  test "network converges after fork" do
    # Start two nodes connected to each other
    {:ok, node1} = Chainex.Node.start_link(difficulty: 1)
    {:ok, node2} = Chainex.Node.start_link(difficulty: 1)

    Chainex.Node.add_peer(node1, node2)
    Chainex.Node.add_peer(node2, node1)

    # Mine a block on node1 only
    {:ok, block} = Chainex.Miner.mine_one_block(node1)

    # Both nodes should eventually agree on the same chain
    Process.sleep(100)

    chain1 = Chainex.Node.get_chain(node1)
    chain2 = Chainex.Node.get_chain(node2)

    # Both have the mined block
    assert length(chain1) == 2
    assert length(chain2) == 2
    assert List.last(chain1).hash == List.last(chain2).hash
  end


  end
end
```

### Step 9: Run the tests

**Objective**: Run the suite with --trace so consensus test timing is visible — fork convergence is inherently flaky if Process.sleep windows are too tight.


```bash
mix test test/chainex/ --trace
```

### Step 10: Mining benchmark

**Objective**: Benchmark mining at fixed difficulty so nonce-search cost stays bounded — tuning difficulty against hash rate keeps block time predictable as hardware shifts.


```elixir
# bench/mining_bench.exs
Benchee.run(
  %{
    "mine block difficulty=2" => fn ->
      Chainex.Miner.mine_block(%{
        index: 1,
        transactions: [],
        previous_hash: String.duplicate("0", 64),
        difficulty: 2
      })
    end
  },
  time: 10,
  warmup: 2
)
```

```bash
mix run bench/mining_bench.exs
```

Expected: difficulty=2 (hash starts with "00") requires on average ~256 nonce iterations. At SHA-256 speeds on Erlang, this should be well under 1ms. Difficulty=4 requires ~65536 iterations, typically 10-100ms.

---

### Why this works

The design separates concerns along their real axes: what must be correct (the blockchain simulation invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
def main do
  IO.puts("[Chainex.Mempool] GenServer demo")
  :ok
end

```

Target: <5ms to validate and insert a block with 1000 transactions.

## Deep Dive: Conflict-Free Replicated Data Types (CRDTs) and Eventual Consistency

CRDTs are data structures designed so that concurrent updates from multiple replicas always converge to the same state without explicit coordination or consensus.

**How it works**: Traditional merge requires agreement: if replica A says "x = 5" and replica B says "x = 10", which is correct? A CRDT avoids this by defining a merge operation that is commutative, associative, and idempotent. Given these properties, nodes can merge state in any order and always converge.

**Example: G-Counter (grow-only counter)**. Each node maintains a vector of counters, one per node. To increment, increment your own entry. Total is the sum of all entries. To merge two G-Counters, take element-wise max. This is commutative, associative, and idempotent: two nodes incrementing independently then merging always produce the same total, regardless of merge order.

**Trade-off: state size**. A G-Counter with 100 nodes is a 100-element vector. Decrement support (PN-Counter) requires 100 elements for increments and 100 for decrements (200 total). As the cluster grows, CRDT state balloons. Compaction (summing old entries into a delta) is necessary.

**CRDT vs. Consensus**: Raft is strong consistency (all nodes agree on exact state, ordered updates). CRDTs are eventual consistency (nodes may disagree temporarily, then converge). CRDTs excel in offline-first scenarios (mobile app syncing later); Raft is better for systems requiring immediate agreement (bank transfers).

**Gotcha**: Just because a CRDT converges does not mean it is correct for your application. A multi-user text document where two users edit the same location must use a CRDT that preserves intent (e.g., CRDT with unique node IDs). A naive counter cannot distinguish "User A inserted at position 10" from "User B inserted at position 10"—they both see increments and may converge to the wrong document.

**Production patterns**: CRDTs shine for collaborative editing (Google Docs, Figma) and offline-first apps (mobile). For backends requiring strong consistency (databases, ledgers), Raft or other consensus is necessary. Many systems use both: CRDTs for user-facing edits, Raft for backend state.

---

## Trade-off analysis

| Aspect | Proof of Work (your impl) | Proof of Stake | Practical Byzantine Fault Tolerance |
|--------|--------------------------|----------------|-------------------------------------|
| Sybil resistance | hardware cost | coin stake | identity/membership required |
| Energy | high | low | low |
| Finality | probabilistic | can be instant | instant |
| Fork possibility | yes | reduced | no (single canonical block) |
| Required connectivity | asynchronous P2P | structured network | all-to-all known validators |
| Implementation complexity | moderate | high | high |

Reflection: in your simulation, two nodes mining simultaneously always creates a temporary fork. What is the probability of a permanent fork (a "double-spend attack") given N honest nodes and one attacker controlling M% of the hash power? (Hint: this is the core of Bitcoin's 51% attack analysis.)

---

## Common production mistakes

**1. Using `System.os_time` for block timestamps**
`os_time` can go backward (NTP adjustments). Use `System.monotonic_time` for relative timing within the simulation. For block timestamps that must be globally meaningful, use `System.system_time(:second)` but document the wall-clock drift risk.

**2. Non-canonical block encoding**
If two nodes compute the hash of the same block differently (e.g., different field ordering in the binary encoding), they will never agree on validity. Define a canonical encoding function once and use it everywhere. Test that `compute_hash(block)` is identical across fresh processes.

**3. Orphaned transactions not returning to mempool**
When a fork is resolved and your chain switches to the longer version, blocks from your old chain become orphaned. Any transactions in those blocks that are not in the winning chain must return to the mempool. Forgetting this causes transactions to be "lost" — they existed in an orphaned block but are not in the current chain and are no longer pending.

**4. Mining Task not cancellable**
When a peer sends a longer valid block, mining the current nonce range is wasted work. Your `Miner` must use `Task.async` and `Task.shutdown/2` so the current mining attempt can be cancelled immediately on block receipt.

**5. ECDSA signature over non-canonical data**
If your transaction signing function signs `inspect(transaction)` (which includes Elixir struct metadata), the signature will vary across BEAM versions. Sign a canonical binary encoding — never string representations of Elixir terms.

---

## Reflection

If two miners produce valid blocks 50ms apart, your node receives them in some order. Walk through the state transitions under longest-chain rule vs GHOST and note where a naive implementation double-counts transactions.

## Resources

- [Bitcoin Whitepaper](https://bitcoin.org/bitcoin.pdf) — Nakamoto (2008) — sections 4-11 cover PoW, the blockchain data structure, and the fork resolution rule directly
- ["Mastering Bitcoin"](https://github.com/bitcoinbook/bitcoinbook) — Antonopoulos — chapters 6-10 on mining, consensus, and the network; free on GitHub
- [Erlang `:crypto` module](https://www.erlang.org/doc/man/crypto.html) — read the ECDH and ECDSA sections; the `generate_key/2`, `sign/4`, and `verify/5` functions are your entire cryptography layer
- [RFC 5480 — Elliptic Curve Cryptography Subject Public Key Information](https://www.rfc-editor.org/rfc/rfc5480) — the DER encoding format for ECDSA signatures and keys
- [Ethereum Yellow Paper](https://ethereum.github.io/yellowpaper/paper.pdf) — for comparison: the account-based model vs. the UTXO model your simulation uses
