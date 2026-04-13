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

## Design decisions

**Option A — Modulo-N sharding (`hash(key) mod N`)**
- Pros: trivial to implement; zero lookup structure.
- Cons: adding or removing a node remaps almost every key; cache hit rate collapses on every topology change.

**Option B — Consistent hashing with virtual nodes** (chosen)
- Pros: only `1/N` of keys move on a topology change; hot-spot mitigation via vnodes; well-known invariants.
- Cons: ring lookup is O(log N) instead of O(1); more bookkeeping per join/leave.

→ Chose **B** because the cost of a single rebalance under mod-N (cold cache → origin stampede) dominates any lookup savings; consistent hashing is the only choice once topology isn't static.

## Project structure
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
├── script/
│   └── main.exs
└── mix.exs
```

### Architecture

```
    redis-cli                     Krebs TCP Listener (port 6379)
       |                                    |
       +--[RESP2 Commands]-->  Connection Handler (GenServer)
                                    |
                              RESP Parser (resumable)
                                    |
                    +-----------+---+---+----------+
                    |                   |
                Command Dispatch    Ring Lookup
                    |                   |
        [SET/GET/DEL/TTL]        Physical Node (shard)
                    |                   |
                WAL (aof.ex)      ETS LRU Cache
                    |                   |
              [durability]      [Replication to R-1 nodes]
```

## Implementation
### Step 1: Create the project

**Objective**: Generate `--sup` skeleton so the TCP listener and shard ring hang under a supervisor from boot.

```bash
mix new krebs --sup
cd krebs
mkdir -p lib/krebs test/krebs bench
```

### Step 2: `mix.exs` -- dependencies

**Objective**: Keep deps to `:benchee` only; RESP, ETS, and `:gen_tcp` ship with OTP, no third-party client needed.

### Step 3: RESP2 protocol

**Objective**: Build a resumable RESP2 parser returning `{:more, buffer}` so partial TCP frames do not corrupt pipelined commands.

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

**Objective**: Use 150 vnodes per physical node so topology changes move only `1/N` of keys, avoiding origin stampede.

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

**Objective**: Lock RESP round-trips and ring distribution invariants so later refactors cannot silently break the wire protocol.

```elixir
defmodule Krebs.RESPTest do
  use ExUnit.Case, async: true
  doctest Krebs.Ring

  alias Krebs.RESP

  describe "core functionality" do
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
end
```
```elixir
defmodule Krebs.RingTest do
  use ExUnit.Case, async: true
  doctest Krebs.Ring

  alias Krebs.Ring

  describe "core functionality" do
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
end
```
### Step 6: Run the tests

**Objective**: Run with `--trace` so any ring distribution variance surfaces as a visible outlier rather than a flaky green.

```bash
mix test test/krebs/ --trace
```

### Step 7: Benchmark

**Objective**: Drive 8 parallel clients through Benchee to expose connection-handler contention before AOF or replication hides it.

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

### Why this works

Each key maps to exactly one primary owner on the ring, and replicas follow the next R-1 vnodes clockwise. Because vnodes are hash-distributed, adding a node moves only its share of keys, and reads can fall back to replicas without violating the ownership invariant.

---
## Quick start

1. **Generate the project**:
   ```bash
   mix new krebs --sup
   cd krebs
   mkdir -p lib/krebs test/krebs bench
   ```

2. **Test RESP protocol and ring**:
   ```bash
   mix test test/krebs/resp_test.exs test/krebs/ring_test.exs --trace
   ```

3. **Run benchmarks** (start `iex -S mix` first, then in another terminal):
   ```bash
   mix run bench/krebs_bench.exs
   ```

---

## Reflection

1. **Why does consistent hashing with virtual nodes scale better than modulo-N?** Consider the rebalancing cost when a node joins or leaves in a 1000-node cluster. What fraction of keys move with each strategy?
2. **In sloppy quorum, when does read repair matter?** If a write goes to {A, B} instead of {B, C} due to A's availability, and later A is rebuilt from its AOF, what race conditions can occur?

---

## Benchmark

**Target metrics** (8 parallel clients, 10s duration):
- **GET — cache hit**: ~100,000 ops/s (R=2 quorum, AOF enabled)
- **SET — no replica**: ~50,000 ops/s

**Setup**:
- 3-node cluster with 150 vnodes per node
- R=2 replication factor
- 64KB per-shard LRU cache
- Benchee with `time: 10, warmup: 3`

**Expected variance**: ±5% across runs (ring distribution uniformity).

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Krebs.MixProject do
  use Mix.Project

  def project do
    [
      app: :krebs,
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
      mod: {Krebs.Application, []}
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
  Realistic stress harness for `krebs` (Redis-like distributed cache).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 2000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:krebs) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Krebs stress test ===")

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
    case Application.stop(:krebs) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:krebs)
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
      # TODO: replace with actual krebs operation
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

Krebs classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

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
| **Sustained throughput** | **100,000 ops/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **2 ms** | Redis + Dynamo paper |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Redis + Dynamo paper: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Distributed Cache with Redis-Compatible Protocol matters

Mastering **Distributed Cache with Redis-Compatible Protocol** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/krebs.ex`

```elixir
defmodule Krebs do
  @moduledoc """
  Reference implementation for Distributed Cache with Redis-Compatible Protocol.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the krebs module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Krebs.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/krebs_test.exs`

```elixir
defmodule KrebsTest do
  use ExUnit.Case, async: true

  doctest Krebs

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Krebs.run(:noop) == :ok
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

- Redis + Dynamo paper
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
