# TCP and HTTP Load Balancer

**Project**: `balancer` — a Layer 4 + Layer 7 load balancer with health monitoring and connection draining

---

## Project context

You are building `balancer`, which the infrastructure team will use to distribute traffic across pools of backend services. It must operate at TCP (Layer 4) for arbitrary protocols and at HTTP (Layer 7) for virtual hosting. Backends can fail and recover at any time. The balancer must detect failures, remove unhealthy backends, and allow graceful removal of backends without dropping active connections.

Project structure:

```
balancer/
├── lib/
│   └── balancer/
│       ├── application.ex
│       ├── listener.ex               # ← TCP accept loop
│       ├── proxy.ex                  # ← bidirectional TCP relay per connection
│       ├── http_classifier.ex        # ← HTTP Host header extraction (minimal parser)
│       ├── pool.ex                   # ← backend pool state + selection
│       ├── algorithms/
│       │   ├── round_robin.ex
│       │   ├── least_connections.ex  # ← :atomics connection counter
│       │   ├── ip_hash.ex            # ← consistent hash for sticky sessions
│       │   └── weighted.ex           # ← current-weight algorithm
│       ├── health/
│       │   ├── active.ex             # ← periodic HTTP probe per backend
│       │   └── passive.ex            # ← error rate tracking on real traffic
│       ├── drain.ex                  # ← PUT /backends/:id/drain + restore
│       └── stats.ex                  # ← per-backend P50/P99 latency + rates
├── test/
│   └── balancer/
│       ├── proxy_test.exs
│       ├── pool_test.exs
│       ├── health_test.exs
│       └── drain_test.exs
├── bench/
│   └── throughput_bench.exs
└── mix.exs
```

---

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

```bash
mix new balancer --sup
cd balancer
mkdir -p lib/balancer/{algorithms,health}
mkdir -p test/balancer bench
```

### Step 2: `mix.exs`

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

```elixir
defmodule Balancer.Proxy do
  @moduledoc """
  Bidirectional TCP relay for a single client connection.

  Spawns two tasks:
  - forward: client → backend
  - reverse: backend → client

  When either direction closes, sends :close to the other task.
  When both are done, closes both sockets and decrements the backend connection counter.

  The process structure:
                  ┌──────────────────┐
  client_socket → │  forward task    │ → backend_socket
                  └──────────────────┘
  client_socket ← │  reverse task    │ ← backend_socket
                  └──────────────────┘
  """

  @buf_size 65_536

  @doc """
  Starts a bidirectional relay between client_socket and backend_socket.
  Returns immediately — relay runs in background tasks under a DynamicSupervisor.
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

```elixir
defmodule Balancer.Algorithms.LeastConnections do
  @moduledoc """
  Selects the backend with the fewest active connections.

  Connection counts are stored in :atomics arrays — one atomic integer per backend.
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
        # TODO: read connection count for each backend via :atomics.get/2
        # TODO: select the backend with minimum count
        # HINT: Enum.min_by(backends, fn b -> :atomics.get(b.atomics_ref, 1) end)
        {:ok, hd(backends)}
    end
  end

  @doc "Increments the connection counter for the given backend."
  @spec increment(map()) :: :ok
  def increment(backend) do
    # TODO: :atomics.add(backend.atomics_ref, 1, 1)
    :ok
  end

  @doc "Decrements the connection counter for the given backend."
  @spec decrement(map()) :: :ok
  def decrement(backend) do
    # TODO: :atomics.sub(backend.atomics_ref, 1, 1)
    # Design question: what happens if decrement is called more times than increment?
    # :atomics integers are unsigned — they would wrap around to max_int.
    # Guard: max(0, current - 1) via compare_exchange.
    :ok
  end
end
```

### Step 5: `lib/balancer/health/active.ex`

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
    url = "http://#{backend.host}:#{backend.port}#{backend.health_path}"
    # TODO: make HTTP GET request with timeout
    # TODO: return :ok if status 200-299, :error otherwise
    # HINT: :httpc.request/4 is available in OTP stdlib
    :error
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

### Step 6: Given tests — must pass without modification

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

  test "round_robin skips unhealthy backends" do
    selections = for _ <- 1..10, do: Pool.select(:round_robin)
    backends_selected = Enum.map(selections, fn {:ok, b} -> b.id end)
    refute "b3" in backends_selected
  end

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

  test "weighted distribution approximates configured weights" do
    # b1: weight 1, b2: weight 2 → expect ~33%/66% distribution
    counts = for _ <- 1..300, reduce: %{"b1" => 0, "b2" => 0} do
      acc ->
        {:ok, b} = Pool.select(:weighted)
        Map.update!(acc, b.id, &(&1 + 1))
    end

    ratio = counts["b2"] / counts["b1"]
    assert ratio > 1.5 and ratio < 2.5, "Expected ~2.0, got #{ratio}"
  end
end
```

```elixir
# test/balancer/drain_test.exs
defmodule Balancer.DrainTest do
  use ExUnit.Case, async: false

  test "draining backend receives no new connections" do
    Pool.add_backend(%{id: "drain-test", host: "localhost", port: 9001, weight: 1, healthy: true, draining: false})
    Pool.drain("drain-test")

    for _ <- 1..20 do
      {:ok, backend} = Pool.select(:round_robin)
      assert backend.id != "drain-test"
    end
  end

  test "restore returns drained backend to rotation" do
    Pool.drain("drain-test")
    Pool.restore("drain-test")

    ids = for _ <- 1..20, do: elem(Pool.select(:round_robin), 1) |> Map.get(:id)
    assert "drain-test" in ids
  end
end
```

### Step 7: Run the tests

```bash
mix test test/balancer/ --trace
```

### Step 8: Throughput benchmark

```bash
# Install wrk (https://github.com/wg/wrk) or vegeta, then:
wrk -t4 -c100 -d30s http://localhost:4000/
```

Expected baseline: a pure TCP relay should forward at least 50k req/s on modern hardware for small responses. HTTP parsing overhead adds ~5–10%. If you see < 10k req/s, verify you are not allocating a new binary for each forwarded chunk — use the raw `data` binary from `:gen_tcp.recv/3` directly.

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

## Resources

- [HAProxy documentation: Load Balancing Algorithms](https://www.haproxy.com/documentation/hapee/latest/load-balancing/algorithms/) — the reference for round-robin, least-connections, and weighted variants
- [Maglev: A Fast and Reliable Software Network Load Balancer](https://research.google/pubs/pub44824/) — Google's consistent hashing paper; relevant for `ip_hash` at scale
- [Nginx upstream module documentation](https://nginx.org/en/docs/http/ngx_http_upstream_module.html) — the source for the current-weight algorithm description
- [RFC 7230 — HTTP/1.1](https://www.rfc-editor.org/rfc/rfc7230) — sections 5.4 (Host header) and 6 (Connection management / keep-alive) are directly relevant
- [`wrk` HTTP benchmarking tool](https://github.com/wg/wrk) — your primary load testing tool for the benchmark step
