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

secp256k1 is a Koblitz curve (y² = x³ + 7) with specific parameter choices that make scalar multiplication about 30% faster than random curves of the same security level. The parameters were chosen for efficiency and are widely audited. Erlang's `:crypto` module supports it directly, so you can use ECDSA key generation and signing without any external dependencies.

---

## Implementation

### Step 1: Create the project

```bash
mix new chainex --sup
cd chainex
mkdir -p test/chainex bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/chainex/block.ex`

```elixir
defmodule Chainex.Block do
  @moduledoc """
  A block in the chain.

  Hash function: SHA-256(SHA-256(canonical_binary(block)))
  PoW condition: hex(hash) must start with N zeros (N = difficulty)

  Why store the hash on the struct?
  Re-computing the hash on every validation is O(block_size). Storing it trades
  memory for CPU. Nodes that receive a block from a peer verify the hash before
  accepting — this is the first (cheapest) validation step.
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
    # TODO: String.starts_with?(hash, String.duplicate("0", difficulty))
    false
  end

  @doc "Returns a canonical binary representation for hashing (deterministic)."
  defp canonical_binary(%__MODULE__{} = block) do
    # TODO: encode all fields in a deterministic order
    # HINT: :erlang.term_to_binary/1 is NOT canonical across BEAM versions
    # Use explicit concatenation: "#{index}#{timestamp}#{tx_hash}#{previous_hash}#{nonce}"
    # where tx_hash is the Merkle root of transactions (or simple hash of sorted tx list)
    ""
  end
end
```

### Step 4: `lib/chainex/wallet.ex`

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
  one layer of indirection — if elliptic curve cryptography were broken, the hash
  would hide the public key until the first transaction from that address.
  """

  defstruct [:public_key, :private_key, :address]

  @doc "Generates a new ECDSA key pair and derives the address."
  @spec generate() :: t()
  def generate do
    # TODO: :crypto.generate_key(:ecdh, :secp256k1)
    # TODO: derive address from public key
    %__MODULE__{public_key: nil, private_key: nil, address: ""}
  end

  @doc "Signs data with this wallet's private key. Returns DER-encoded signature binary."
  @spec sign(t(), binary()) :: binary()
  def sign(%__MODULE__{private_key: pk}, data) do
    # TODO: :crypto.sign(:ecdsa, :sha256, data, [pk, :secp256k1])
    ""
  end

  @doc "Verifies a signature against a public key."
  @spec verify(binary(), binary(), binary()) :: boolean()
  def verify(data, signature, public_key) do
    # TODO: :crypto.verify(:ecdsa, :sha256, data, signature, [public_key, :secp256k1])
    false
  end
end
```

### Step 5: `lib/chainex/node.ex`

```elixir
defmodule Chainex.Node do
  use GenServer

  @moduledoc """
  A full blockchain node.

  State:
  - chain: current canonical chain
  - peers: list of peer node pids
  - mempool: reference to Mempool GenServer

  On receiving a new block from a peer:
  1. Verify PoW
  2. Verify all transactions in the block
  3. Verify previous_hash links to our chain tip
  4. If longer than our chain → adopt it (fork resolution)
  5. If same height → keep ours (first seen wins)
  6. Announce to peers if accepted

  Fork resolution algorithm:
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
  def handle_cast({:receive_block, block}, state) do
    new_state =
      cond do
        not Chainex.Block.valid_pow?(block, state.difficulty) ->
          # Reject: invalid PoW
          state

        not valid_previous_hash?(block, state.chain) ->
          # TODO: handle possible fork — request full chain from sender?
          state

        length(state.chain) < length([block | state.chain]) ->
          # New block extends our chain
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

  defp valid_previous_hash?(block, chain) do
    chain_tip = List.last(chain)
    block.previous_hash == chain_tip.hash
  end

  defp broadcast_block(peers, block) do
    Enum.each(peers, &Chainex.Node.receive_block(&1, block))
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/chainex/block_test.exs
defmodule Chainex.BlockTest do
  use ExUnit.Case, async: true

  alias Chainex.Block

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
```

```elixir
# test/chainex/wallet_test.exs
defmodule Chainex.WalletTest do
  use ExUnit.Case, async: true

  alias Chainex.Wallet

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
```

```elixir
# test/chainex/consensus_test.exs
defmodule Chainex.ConsensusTest do
  use ExUnit.Case, async: false

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
```

### Step 7: Run the tests

```bash
mix test test/chainex/ --trace
```

### Step 8: Mining benchmark

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

Expected: difficulty=2 (hash starts with "00") requires on average ~256 nonce iterations. At SHA-256 speeds on Erlang, this should be well under 1ms. Difficulty=4 requires ~65536 iterations, typically 10–100ms.

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

## Resources

- [Bitcoin Whitepaper](https://bitcoin.org/bitcoin.pdf) — Nakamoto (2008) — sections 4–11 cover PoW, the blockchain data structure, and the fork resolution rule directly
- ["Mastering Bitcoin"](https://github.com/bitcoinbook/bitcoinbook) — Antonopoulos — chapters 6–10 on mining, consensus, and the network; free on GitHub
- [Erlang `:crypto` module](https://www.erlang.org/doc/man/crypto.html) — read the ECDH and ECDSA sections; the `generate_key/2`, `sign/4`, and `verify/5` functions are your entire cryptography layer
- [RFC 5480 — Elliptic Curve Cryptography Subject Public Key Information](https://www.rfc-editor.org/rfc/rfc5480) — the DER encoding format for ECDSA signatures and keys
- [Ethereum Yellow Paper](https://ethereum.github.io/yellowpaper/paper.pdf) — for comparison: the account-based model vs. the UTXO model your simulation uses
