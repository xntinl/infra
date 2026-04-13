# TCP and HTTP Load Balancer

**Project**: `balancer` — a Layer 4 + Layer 7 load balancer with health monitoring and connection draining

---

## Project context

You are building `balancer`, which the infrastructure team will use to distribute traffic across pools of backend services. It must operate at TCP (Layer 4) for arbitrary protocols and at HTTP (Layer 7) for virtual hosting. Backends can fail and recover at any time. The balancer must detect failures, remove unhealthy backends, and allow graceful removal of backends without dropping active connections.

## Full Project Directory Tree

```
balancer/
├── lib/
│   ├── balancer.ex                  # main application module
│   └── balancer/
│       ├── application.ex           # OTP supervisor
│       ├── listener.ex              # TCP accept loop, spawns relay per client
│       ├── proxy.ex                 # bidirectional TCP relay: forward + reverse tasks
│       ├── http_classifier.ex       # HTTP Host header extraction (minimal parser)
│       ├── pool.ex                  # backend pool: ETS storage + selection logic
│       ├── algorithms/
│       │   ├── round_robin.ex       # stateless sequential selection
│       │   ├── least_connections.ex # lock-free atomics counter per backend
│       │   ├── ip_hash.ex           # consistent hash for sticky sessions
│       │   └── weighted.ex          # current-weight algorithm (Nginx style)
│       ├── health/
│       │   ├── active.ex            # periodic HTTP probe per backend
│       │   └── passive.ex           # error rate tracking on real traffic
│       ├── drain.ex                 # graceful backend removal (no new conns)
│       └── stats.ex                 # per-backend P50/P99 latency + throughput
├── test/
│   ├── balancer_test.exs            # integration smoke tests
│   └── balancer/
│       ├── proxy_test.exs           # TCP relay bidirectional flow
│       ├── pool_test.exs            # selection algorithms + health management
│       ├── health_test.exs          # active probe hysteresis
│       └── drain_test.exs           # graceful shutdown semantics
├── bench/
│   └── throughput_bench.exs         # selection algorithm performance
├── mix.exs                          # dependencies and config
└── README.md
```

---

## Why power-of-two-choices and not least-connections globally

global least-connections needs a shared counter across all LB instances — either a contended lock or an eventually-consistent view that's wrong under load. P2C achieves ~95% of its benefit with zero cross-instance coordination.

## Design decisions

**Option A — random selection**
- Pros: stateless, trivially parallel
- Cons: tail latency ignored, hot backends overloaded

**Option B — power-of-two-choices with active health check feedback** (chosen)
- Pros: near-optimal load distribution with minimal state
- Cons: requires per-backend state, slightly more code

→ Chose **B** because P2C achieves load imbalance ratios close to optimal while remaining O(1) per decision.

## The business problem

The deployment team needs to rotate backend instances during deployments. The old pattern — take down the backend and update DNS — causes request failures during the propagation window. With a load balancer, the workflow is:

1. Start the new backend instance.
2. Register it in the balancer pool.
3. Drain the old backend (`PUT /backends/:id/drain`).
4. Wait for active connections to complete.
5. Shut down the old backend.

Zero requests are dropped. No DNS propagation required.

---

## Why bidirectional TCP relay requires two concurrent tasks

A single process reading from socket A and writing to socket B blocks while waiting for data from A. Meanwhile, B may be trying to send data back (full-duplex). This deadlocks if the send buffer fills.

The solution is two tasks per proxied connection:

```
Task A: read from client socket → write to backend socket
Task B: read from backend socket → write to client socket
```

When either task finishes (connection closed), it signals the other to stop. Implement this with a monitor: each task monitors the other; when one exits, the other exits too.

---

## Why weighted round-robin needs a current-weight algorithm

Naive weighted round-robin with `List.duplicate(backend, weight)` is correct but memory-intensive for large weights. If backend A has weight 5 and B has weight 1, you need a 6-element list.

The current-weight algorithm (used by Nginx) avoids this:

1. Each backend starts with `current_weight = 0`.
2. On each selection, add each backend's `weight` to its `current_weight`.
3. Select the backend with the highest `current_weight`.
4. Subtract the total weight from the selected backend's `current_weight`.

For backends `[{A, 5}, {B, 1}]`, the selection sequence is: A, A, A, A, A, B — exactly the 5:1 ratio. Memory usage is proportional to the number of backends, not the sum of their weights.

---

## Implementation

### Step 1: Create the project

**Objective**: Use `--sup` so the pool, probers, and per-connection relays boot under a tree that restarts without dropping peers.


```bash
mix new balancer --sup
cd balancer
mkdir -p lib/balancer/{algorithms,health}
mkdir -p test/balancer bench
```

### Step 2: `mix.exs`

**Objective**: Pick Cowboy for L7 framing but keep the relay on `:gen_tcp` — any HTTP client would hide the backpressure we need.


```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/balancer/proxy.ex`

**Objective**: Split the relay into two tasks plus a monitor — a single-process relay deadlocks when one side's send buffer fills.


The bidirectional TCP relay spawns two processes: one reads from the client socket and writes to the backend socket, the other reads from the backend and writes to the client. A third "monitor" process watches both relay processes. When either relay exits (because its read socket closed), the monitor kills the other and closes both sockets.

This architecture avoids deadlocks that occur with a single-process relay: if the client sends data but the backend's send buffer is full, the single process blocks on `:gen_tcp.send` and cannot read from the backend to drain its buffer.

```elixir
defmodule Balancer.Proxy do
  @moduledoc """
  Bidirectional TCP relay for a single client connection.

  Spawns two tasks:
  - forward: client -> backend
  - reverse: backend -> client

  When either direction closes, sends :close to the other task.
  When both are done, closes both sockets and decrements the backend connection counter.

  The process structure:
                  +--------------------+
  client_socket -> |  forward task    | -> backend_socket
                  +--------------------+
  client_socket <- |  reverse task    | <- backend_socket
                  +--------------------+
  """

  @doc """
  Starts a bidirectional relay between client_socket and backend_socket.
  Returns immediately -- relay runs in background tasks under a DynamicSupervisor.
  Calls on_close/0 when both directions have closed (for connection counting).
  """
  @spec start(port(), port(), (-> :ok)) :: :ok
  def start(client_socket, backend_socket, on_close) do
    parent = self()

    forward_pid = spawn(fn ->
      relay(client_socket, backend_socket)
      send(parent, {:closed, :forward})
    end)

    reverse_pid = spawn(fn ->
      relay(backend_socket, client_socket)
      send(parent, {:closed, :reverse})
    end)

    # Monitor both; when one closes, signal the other
    spawn(fn ->
      ref_f = Process.monitor(forward_pid)
      ref_r = Process.monitor(reverse_pid)

      receive do
        {:DOWN, ^ref_f, :process, _, _} ->
          Process.exit(reverse_pid, :shutdown)
        {:DOWN, ^ref_r, :process, _, _} ->
          Process.exit(forward_pid, :shutdown)
      end

      # Wait for the remaining one
      receive do
        {:DOWN, _, :process, _, _} -> :ok
      end

      :gen_tcp.close(client_socket)
      :gen_tcp.close(backend_socket)
      on_close.()
    end)

    :ok
  end

  defp relay(from, to) do
    case :gen_tcp.recv(from, 0, 30_000) do
      {:ok, data} ->
        case :gen_tcp.send(to, data) do
          :ok -> relay(from, to)
          {:error, _} -> :closed
        end

      {:error, _} ->
        :closed
    end
  end
end
```

### Step 4: `lib/balancer/algorithms/least_connections.ex`

**Objective**: Keep counters in `:atomics` — routing through a GenServer on every connect/disconnect serializes all traffic.


Least-connections routing selects the backend with the fewest active connections. Connection counts are stored in `:atomics` arrays, which allow lock-free concurrent increment and decrement from any process. This is critical because connection counts change on every request start/end, and routing through a GenServer for each counter update would serialize all traffic.

The `decrement/1` function uses `:atomics.sub/3` directly. In production you would guard against negative counts (which wrap to max unsigned int) using a compare-exchange loop, but for typical usage where increment and decrement are always paired via `try/after`, this is sufficient.

```elixir
defmodule Balancer.Algorithms.LeastConnections do
  @moduledoc """
  Selects the backend with the fewest active connections.

  Connection counts are stored in :atomics arrays -- one atomic integer per backend.
  This allows concurrent increment/decrement without going through a GenServer.

  The trade-off vs. round-robin:
  - Round-robin: O(1) but ignores actual backend load
  - Least-connections: O(n backends) to find minimum, but routes to the least loaded backend
  - Use least-connections when backends have heterogeneous capacity or when request
    processing time varies significantly (e.g., some requests are 10x heavier than others)
  """

  @doc """
  Selects the backend with the fewest active connections from the pool.
  Returns {:ok, backend} or {:error, :no_healthy_backends}.
  """
  @spec select([map()]) :: {:ok, map()} | {:error, :no_healthy_backends}
  def select([]), do: {:error, :no_healthy_backends}

  def select(backends) do
    healthy = Enum.filter(backends, &(&1.healthy and not &1.draining))

    case healthy do
      [] ->
        {:error, :no_healthy_backends}

      backends ->
        # Read the current connection count for each backend from its atomics ref.
        # :atomics.get/2 is a lock-free read -- safe to call from any process.
        # Enum.min_by selects the backend with the lowest count; on ties, the
        # first one encountered wins, which provides a stable selection order.
        selected = Enum.min_by(backends, fn b -> :atomics.get(b.atomics_ref, 1) end)
        {:ok, selected}
    end
  end

  @doc "Increments the connection counter for the given backend."
  @spec increment(map()) :: :ok
  def increment(backend) do
    :atomics.add(backend.atomics_ref, 1, 1)
    :ok
  end

  @doc "Decrements the connection counter for the given backend."
  @spec decrement(map()) :: :ok
  def decrement(backend) do
    :atomics.sub(backend.atomics_ref, 1, 1)
    :ok
  end
end
```

### Step 5: `lib/balancer/pool.ex`

**Objective**: Store backends in ETS with an `:atomics` round-robin cursor — selection must stay lock-free on the hot path.


The pool is a GenServer holding all backend state in an ETS table. It supports multiple selection algorithms (round-robin, weighted, least-connections, ip-hash) and provides mark_healthy/mark_unhealthy/drain/restore operations. The round-robin index is stored as an `:atomics` counter so selection can be lock-free.

```elixir
defmodule Balancer.Pool do
  use GenServer

  @table :balancer_pool

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Selects a backend using the given algorithm."
  @spec select(atom()) :: {:ok, map()} | {:error, :no_healthy_backends}
  def select(algorithm \\ :round_robin) do
    backends = all_backends()
    healthy = Enum.filter(backends, &(&1.healthy and not &1.draining))

    case {algorithm, healthy} do
      {_, []} ->
        {:error, :no_healthy_backends}

      {:round_robin, backends} ->
        index = :atomics.add_get(:persistent_term.get(:balancer_rr_index), 1, 1)
        selected = Enum.at(backends, rem(abs(index), length(backends)))
        {:ok, selected}

      {:weighted, backends} ->
        select_weighted(backends)

      {:least_connections, backends} ->
        Balancer.Algorithms.LeastConnections.select(backends)

      {:ip_hash, backends} ->
        {:ok, hd(backends)}
    end
  end

  @doc "Adds a backend to the pool."
  def add_backend(backend) do
    GenServer.call(__MODULE__, {:add, backend})
  end

  @doc "Marks a backend as unhealthy (removes from rotation)."
  def mark_unhealthy(id), do: GenServer.call(__MODULE__, {:set_health, id, false})

  @doc "Marks a backend as healthy (returns to rotation)."
  def mark_healthy(id), do: GenServer.call(__MODULE__, {:set_health, id, true})

  @doc "Sets a backend to draining mode (no new connections)."
  def drain(id), do: GenServer.call(__MODULE__, {:set_drain, id, true})

  @doc "Restores a drained backend to active rotation."
  def restore(id), do: GenServer.call(__MODULE__, {:set_drain, id, false})

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    :ets.new(@table, [:named_table, :public, :set])

    # Atomic counter for round-robin index
    rr_index = :atomics.new(1, signed: true)
    :persistent_term.put(:balancer_rr_index, rr_index)

    # Weighted round-robin state: current weights per backend
    :persistent_term.put(:balancer_weights, %{})

    backends = Keyword.get(opts, :backends, [])
    Enum.each(backends, fn b ->
      atomics_ref = :atomics.new(1, signed: true)
      backend = Map.put(b, :atomics_ref, atomics_ref)
      :ets.insert(@table, {b.id, backend})
    end)

    {:ok, %{}}
  end

  @impl true
  def handle_call({:add, backend}, _from, state) do
    atomics_ref = :atomics.new(1, signed: true)
    backend = Map.put(backend, :atomics_ref, atomics_ref)
    :ets.insert(@table, {backend.id, backend})
    {:reply, :ok, state}
  end

  @impl true
  def handle_call({:set_health, id, healthy}, _from, state) do
    case :ets.lookup(@table, id) do
      [{^id, backend}] ->
        :ets.insert(@table, {id, %{backend | healthy: healthy}})
        {:reply, :ok, state}

      [] ->
        {:reply, {:error, :not_found}, state}
    end
  end

  @impl true
  def handle_call({:set_drain, id, draining}, _from, state) do
    case :ets.lookup(@table, id) do
      [{^id, backend}] ->
        :ets.insert(@table, {id, %{backend | draining: draining}})
        {:reply, :ok, state}

      [] ->
        {:reply, {:error, :not_found}, state}
    end
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp all_backends do
    :ets.tab2list(@table) |> Enum.map(fn {_id, b} -> b end)
  end

  # Nginx current-weight algorithm for weighted round-robin.
  # Each call adds each backend's configured weight to its current_weight,
  # selects the backend with the highest current_weight, then subtracts
  # the total weight from the selected backend's current_weight.
  defp select_weighted(backends) do
    current_weights = :persistent_term.get(:balancer_weights)
    total_weight = Enum.reduce(backends, 0, fn b, acc -> acc + b.weight end)

    # Add configured weight to current weight for each backend
    updated =
      Enum.map(backends, fn b ->
        cw = Map.get(current_weights, b.id, 0) + b.weight
        {b, cw}
      end)

    # Select the backend with the highest current weight
    {selected, selected_cw} = Enum.max_by(updated, fn {_b, cw} -> cw end)

    # Subtract total weight from the selected backend
    new_weights =
      Enum.reduce(updated, %{}, fn {b, cw}, acc ->
        if b.id == selected.id do
          Map.put(acc, b.id, selected_cw - total_weight)
        else
          Map.put(acc, b.id, cw)
        end
      end)

    :persistent_term.put(:balancer_weights, new_weights)
    {:ok, selected}
  end
end
```

### Step 6: `lib/balancer/health/active.ex`

**Objective**: Apply hysteresis on consecutive failures and recoveries — one flapping probe must not evict a healthy backend.


Active health checking sends periodic HTTP probes to each backend's health endpoint. It tracks consecutive failures and successes to implement hysteresis: a backend must fail `threshold` consecutive probes to be marked unhealthy, and must pass `recovery` consecutive probes to be restored. This prevents a single failed probe (network blip) from removing a backend.

The probe uses Erlang's built-in `:httpc` module (part of the `inets` application) to avoid external dependencies. The `:httpc.request/4` call with a timeout ensures the probe does not hang indefinitely on unresponsive backends.

```elixir
defmodule Balancer.Health.Active do
  use GenServer

  @default_interval_ms 10_000
  @default_timeout_ms 5_000
  @default_threshold 3
  @default_recovery 2

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Starts monitoring backend with active HTTP probes.
  """
  @spec monitor(map(), keyword()) :: {:ok, pid()}
  def monitor(backend, opts \\ []) do
    GenServer.start_link(__MODULE__, {backend, opts})
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  @impl true
  def init({backend, opts}) do
    # Ensure :inets and :ssl are started for :httpc to work
    :inets.start()
    :ssl.start()

    state = %{
      backend: backend,
      interval_ms: Keyword.get(opts, :interval_ms, @default_interval_ms),
      timeout_ms: Keyword.get(opts, :timeout_ms, @default_timeout_ms),
      threshold: Keyword.get(opts, :threshold, @default_threshold),
      recovery: Keyword.get(opts, :recovery, @default_recovery),
      consecutive_failures: 0,
      consecutive_successes: 0
    }

    schedule_probe(state.interval_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:probe, state) do
    result = probe_backend(state.backend, state.timeout_ms)
    new_state = update_health(state, result)
    schedule_probe(state.interval_ms)
    {:noreply, new_state}
  end

  defp probe_backend(backend, timeout_ms) do
    url = ~c"http://#{backend.host}:#{backend.port}#{backend.health_path}"

    # :httpc.request/4 sends an HTTP GET and returns {status_line, headers, body}.
    # The timeout tuple controls both connection timeout and receive timeout.
    case :httpc.request(:get, {url, []}, [{:timeout, timeout_ms}, {:connect_timeout, timeout_ms}], []) do
      {:ok, {{_http_version, status_code, _reason}, _headers, _body}}
        when status_code >= 200 and status_code < 300 ->
        :ok

      _ ->
        :error
    end
  end

  defp update_health(state, :ok) do
    new_state = %{state | consecutive_failures: 0, consecutive_successes: state.consecutive_successes + 1}

    if not state.backend.healthy and new_state.consecutive_successes >= state.recovery do
      Balancer.Pool.mark_healthy(state.backend.id)
      %{new_state | consecutive_successes: 0}
    else
      new_state
    end
  end

  defp update_health(state, :error) do
    new_state = %{state | consecutive_successes: 0, consecutive_failures: state.consecutive_failures + 1}

    if state.backend.healthy and new_state.consecutive_failures >= state.threshold do
      Balancer.Pool.mark_unhealthy(state.backend.id)
      %{new_state | consecutive_failures: 0}
    else
      new_state
    end
  end

  defp schedule_probe(interval_ms) do
    Process.send_after(self(), :probe, interval_ms)
  end
end
```

### Step 7: Given tests — must pass without modification

**Objective**: Tests pin pool invariants and health transitions — deviations break weighted fairness and health-check hysteresis.


```elixir
# test/balancer/pool_test.exs
defmodule Balancer.PoolTest do
  use ExUnit.Case, async: false

  alias Balancer.Pool

  setup do
    backends = [
      %{id: "b1", host: "localhost", port: 8001, weight: 1, healthy: true, draining: false},
      %{id: "b2", host: "localhost", port: 8002, weight: 2, healthy: true, draining: false},
      %{id: "b3", host: "localhost", port: 8003, weight: 1, healthy: false, draining: false}
    ]
    {:ok, pool} = Pool.start_link(backends: backends, algorithm: :round_robin)
    %{pool: pool}
  end


  describe "algorithm: round-robin" do
    test "distributes sequentially across healthy backends" do
      selections = for _ <- 1..20, do: Pool.select(:round_robin)
      ids = Enum.map(selections, fn {:ok, b} -> b.id end)
      # Should see b1 and b2 alternating (b3 is unhealthy)
      assert Enum.count(ids, &(&1 == "b1")) > 0
      assert Enum.count(ids, &(&1 == "b2")) > 0
      assert Enum.count(ids, &(&1 == "b3")) == 0
    end

    test "skips unhealthy backends" do
      selections = for _ <- 1..10, do: Pool.select(:round_robin)
      backends_selected = Enum.map(selections, fn {:ok, b} -> b.id end)
      refute "b3" in backends_selected
    end
  end

  describe "health management" do
    test "mark_unhealthy removes backend from rotation" do
      Pool.mark_unhealthy("b1")
      # b1 should no longer be selected
      for _ <- 1..20 do
        {:ok, backend} = Pool.select(:round_robin)
        assert backend.id != "b1"
      end
    end

    test "mark_healthy returns backend to rotation" do
      Pool.mark_unhealthy("b1")
      Pool.mark_healthy("b1")
      ids = for _ <- 1..20, do: elem(Pool.select(:round_robin), 1) |> Map.get(:id)
      assert "b1" in ids
    end

    test "transitions between healthy and unhealthy are atomic" do
      for _ <- 1..5 do
        Pool.mark_unhealthy("b1")
        {:ok, b} = Pool.select(:round_robin)
        assert b.id != "b1"
        
        Pool.mark_healthy("b1")
        {:ok, b2} = Pool.select(:round_robin)
        assert b2.id in ["b1", "b2"]
      end
    end
  end

  describe "algorithm: weighted round-robin" do
    test "distribution approximates configured weights" do
      # b1: weight 1, b2: weight 2 → expect ~33%/66% distribution
      counts = for _ <- 1..300, reduce: %{"b1" => 0, "b2" => 0} do
        acc ->
          {:ok, b} = Pool.select(:weighted)
          Map.update!(acc, b.id, &(&1 + 1))
      end

      ratio = counts["b2"] / counts["b1"]
      assert ratio > 1.5 and ratio < 2.5, "Expected ~2.0, got #{ratio}"
    end

    test "single backend with weight 1 always selected" do
      {:ok, b1} = Pool.select(:weighted)
      {:ok, b2} = Pool.select(:weighted)
      assert b1.id in ["b1", "b2"]
      assert b2.id in ["b1", "b2"]
    end
  end
end
```

```elixir
# test/balancer/drain_test.exs
defmodule Balancer.DrainTest do
  use ExUnit.Case, async: false


  describe "connection draining" do
    test "draining backend receives no new connections" do
      Pool.add_backend(%{id: "drain-test", host: "localhost", port: 9001, weight: 1, healthy: true, draining: false})
      Pool.drain("drain-test")

      for _ <- 1..20 do
        {:ok, backend} = Pool.select(:round_robin)
        assert backend.id != "drain-test"
      end
    end

    test "restore returns drained backend to rotation" do
      Pool.add_backend(%{id: "drain-test2", host: "localhost", port: 9002, weight: 1, healthy: true, draining: false})
      Pool.drain("drain-test2")
      Pool.restore("drain-test2")

      ids = for _ <- 1..20, do: elem(Pool.select(:round_robin), 1) |> Map.get(:id)
      assert "drain-test2" in ids
    end

    test "draining does not affect other backends" do
      Pool.add_backend(%{id: "other-1", host: "localhost", port: 9003, weight: 1, healthy: true, draining: false})
      Pool.drain("drain-test")
      
      {:ok, b} = Pool.select(:round_robin)
      assert b.id in ["b1", "b2", "other-1"]
      assert b.id != "drain-test"
    end
  end
end
```

### Step 8: Run the tests

**Objective**: Run with `--trace` so selection order across algorithms and health-state flips remain deterministic per run.


```bash
mix test test/balancer/ --trace
```

### Step 9: Throughput benchmark

**Objective**: Verify you forward raw binaries without reallocation — anything under 10k req/s points to a per-chunk copy.


```bash
# Install wrk (https://github.com/wg/wrk) or vegeta, then:
wrk -t4 -c100 -d30s http://localhost:4000/
```

Expected baseline: a pure TCP relay should forward at least 50k req/s on modern hardware for small responses. HTTP parsing overhead adds ~5-10%. If you see < 10k req/s, verify you are not allocating a new binary for each forwarded chunk — use the raw `data` binary from `:gen_tcp.recv/3` directly.

---

### Why this works

The design separates concerns along their real axes: what must be correct (the load balancer invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.


## Quick start

```bash
# Start the application and run tests
mix deps.get
mix test test/balancer/ --trace

# Or run performance benchmarks:
mix run bench/throughput_bench.exs
```

Target: <1µs per backend selection at 100 backends; sustained >50k req/s at p99 tail latency <10ms.



## Benchmark

```elixir
# bench/balancer_bench.exs
{:ok, pool} = Balancer.Pool.start_link(
  backends: for i <- 1..100 do
    %{
      id: "b#{i}",
      host: "localhost",
      port: 8000 + i,
      healthy: true,
      draining: false,
      weight: if(rem(i, 10) == 0, do: 2, else: 1)
    }
  end,
  algorithm: :round_robin
)

Benchee.run(%{
  "round_robin_100_backends" => fn ->
    {:ok, _} = Balancer.Pool.select(:round_robin)
  end,
  "weighted_distribution_100_backends" => fn ->
    {:ok, _} = Balancer.Pool.select(:weighted)
  end,
  "least_connections_100_backends" => fn ->
    {:ok, _} = Balancer.Pool.select(:least_connections)
  end
}, time: 10, warmup: 3)
```

## Quick start

```bash
# Start the application
mix deps.get
mix test

# Or run the benchmark:
mix run bench/balancer_bench.exs
```

Target: <1µs per backend selection at 100 backends.

## Key Concepts: Load Balancing Algorithms and Connection Draining

Load balancers distribute traffic across backends using algorithms that balance latency, fairness, and operational simplicity.

**Round-robin**: Sequential selection (backend i % N). Simple, fair on average, but blind to backend state. If one backend GC-pauses, that backend's clients stall. No state per backend, minimal CPU cost.

**Least connections**: Select the backend with fewest active connections. Better than round-robin because it accounts for actual load. Cost: O(N) per selection to find the minimum. With 100 backends, finding the minimum is still fast, but the cost is 100x higher than round-robin.

**Power-of-two choices (P2C)**: Pick two random backends, select the one with fewer connections. Statistically (via the coupon collector problem), this achieves ~95% of the benefit of global least-connections with O(1) cost. The key insight: with randomness, tail latency improves dramatically. Even with 100 backends, picking two random ones and comparing is faster and more effective than sequential scan.

**Weighted round-robin**: Some backends have more capacity. Assign weights (e.g., backend A=5, B=1) so A gets 5x more traffic. Naive implementation: `List.duplicate(a, 5) ++ [b]` creates a 6-element list per selection. Current-weight algorithm: maintain `current_weight[i]` per backend; each round, increment by configured weight, select max, subtract total_weight. This avoids list allocation.

**Connection draining**: During deployment, old backends must release active connections gracefully. Mark as "draining" so no new connections route to it. Existing connections complete normally. Active health checks continue to test the backend. Once all connections close, shut it down. This avoids request failures during rolling deploys.

**Production insight**: Load balancer algorithms on paper are not what matters. What matters is tail latency under realistic failure modes: one slow backend, cascading failures where recovery produces spikes, connection pools at different stages of lifecycle.

---

## Trade-off analysis

| Algorithm | Best use case | Latency overhead | Stickiness |
|-----------|--------------|-----------------|-----------|
| Round-robin | Uniform requests, uniform backends | O(1) | none |
| Least-connections | Variable request duration | O(n backends) | none |
| Weighted | Heterogeneous backend capacity | O(1) with current-weight | none |
| IP hash | Session-stateful backends | O(1) hash | client IP |

Reflection: `ip_hash` breaks when your load balancer sits behind a NAT or another proxy — all clients appear to have the same IP. How would you implement sticky sessions without relying on client IP? (Hint: cookies.)

---

## Common production mistakes

**1. Race condition in "zero active connections" check during drain**
A backend reports 0 connections at time T. Between the check and the removal, a new connection arrives (it passed the "not draining" check before the drain flag was set). The fix: use an atomic `compare_and_swap` — remove only if count is 0 AND draining flag is set in a single atomic operation.

**2. Passive health check that never recovers**
A passive check that marks a backend unhealthy does nothing to re-probe it. The backend could recover but stay excluded forever. Always pair passive checks with active probes: passive removes the backend, active probes re-admit it after `recovery_threshold` successes.

**3. `ip_hash` using the X-Forwarded-For header naively**
`X-Forwarded-For: client_ip, proxy1, proxy2` — the first IP is the real client. But this header is attacker-controlled. A client can send `X-Forwarded-For: 1.2.3.4` to route to a specific backend. Only trust this header when you control the upstream proxy chain.

**4. Not handling partial writes to backend sockets**
`:gen_tcp.send/2` may return `{:error, :eagain}` if the send buffer is full (backpressure). A naive relay that ignores this error drops data silently. Retry the send or buffer in the relay task.

**5. Active health check that counts redirects as failures**
A health endpoint that returns HTTP 301 is technically not a 2xx success. Your probe must follow redirects or explicitly allow 3xx responses as healthy, depending on your backend's convention.

---

## Reflection

Under a traffic surge where one backend is 3x slower than the rest, how long until P2C routes around it, and what happens if you also turn on active health-check ejection?

## Resources

- [HAProxy documentation: Load Balancing Algorithms](https://www.haproxy.com/documentation/hapee/latest/load-balancing/algorithms/) — the reference for round-robin, least-connections, and weighted variants
- [Maglev: A Fast and Reliable Software Network Load Balancer](https://research.google/pubs/pub44824/) — Google's consistent hashing paper; relevant for `ip_hash` at scale
- [Nginx upstream module documentation](https://nginx.org/en/docs/http/ngx_http_upstream_module.html) — the source for the current-weight algorithm description
- [RFC 7230 — HTTP/1.1](https://www.rfc-editor.org/rfc/rfc7230) — sections 5.4 (Host header) and 6 (Connection management / keep-alive) are directly relevant
- [`wrk` HTTP benchmarking tool](https://github.com/wg/wrk) — your primary load testing tool for the benchmark step
