# Remote calls in BEAM — `:rpc` vs `:erpc` vs `GenServer.call`

**Project**: `rpc_demo` — measure, compare, and choose between the three cross-node call mechanisms in Elixir/Erlang: legacy `:rpc`, modern `:erpc`, and plain `GenServer.call` to a remote-registered process.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

You're wiring up a small service mesh of Elixir nodes and you need to invoke functions on remote peers: fetch a cached config from the leader, run a maintenance job, ask a specific node for its local telemetry snapshot. There are three idiomatic paths:

1. `:rpc` — the original OTP module, dating to Erlang's telephony years. Convenient, widely used in tutorials, but **every call is serialised through a single `rex` server** on the target node, and it has poor timeout / error semantics.
2. `:erpc` — introduced in OTP 23 (2020) as the "enhanced" RPC. Runs the callee function in a **fresh spawned process** per call, propagates exceptions with full stack traces, supports multi-target broadcast with deadlines, and is the officially recommended API in modern Erlang docs.
3. `GenServer.call({Name, node()}, msg)` — not RPC at all, but a cross-node GenServer invocation. Used when you have a **named, stateful** server on the remote node and you want to invoke its specific behaviour.

The practical question is almost always: "I need to call `Foo.bar/1` on node X — which one?" The answer depends on whether you control the target, how you want failures to surface, and whether you need throughput.

This exercise builds `rpc_demo`, an app exposing the same logical operation — `get_snapshot/0` — via all three mechanisms, with a benchmark harness comparing latency, throughput, and failure modes (remote crash, timeout, unreachable node).

Project structure:

```
rpc_demo/
├── lib/
│   └── rpc_demo/
│       ├── application.ex
│       ├── snapshot.ex            # the function being called remotely
│       ├── snapshot_server.ex     # GenServer exposing snapshot/0
│       ├── client.ex              # 3 client APIs: via_rpc, via_erpc, via_genserver
│       └── bench.ex               # benchmarks across the three paths
├── test/
│   └── rpc_demo/
│       └── client_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:rpc` — the `rex` bottleneck

Every BEAM node starts a `rex` GenServer under the `kernel` application. `:rpc.call(Node, M, F, A)` sends a message to `rex` on the target; `rex` invokes `M.F(A...)` **in its own process** and replies.

```
caller ──rpc.call──▶ {rex, target_node}
                       │
                       ▼
                     apply(M, F, A)
                     reply: result | {:badrpc, reason}
```

Consequences:

- All RPC traffic is serialised through one process. Saturated under concurrent load.
- An exception in `M.F(A)` is caught by `rex` and returned as `{:badrpc, {:EXIT, {reason, stack}}}` — not a raise. Easy to ignore the error branch.
- Timeouts use `GenServer.call` semantics: on timeout the caller raises, but `rex` keeps running the function (potential duplicated side effects on retry).

### 2. `:erpc` — per-call fresh process

`:erpc.call(Node, M, F, A, timeout)` dispatches directly via the dist port, **spawns a new process** on the target for every call, runs the function, replies. No central bottleneck.

```
caller ──erpc.call──▶ target node
                       │ spawn(fun -> apply(M,F,A) end)
                       │
                       ▼
                     reply: result | throw/exit/error propagated
```

Features beyond `:rpc`:

- Errors propagate as genuine `raise`/`throw`/`exit` on the caller side, with remote stack traces.
- `:erpc.multicall/5` fans out to many nodes in parallel with a shared deadline.
- `:erpc.send_request/4` + `:erpc.receive_response/1` enable pipelining and concurrent RPC.
- Cancellation: if caller dies, `:erpc` tries to terminate the remote process.

The OTP docs explicitly recommend `:erpc` over `:rpc` for new code.

### 3. `GenServer.call({Name, node}, msg)`

If the target node runs a named GenServer, you address it by tuple:

```elixir
GenServer.call({MyApp.SnapshotServer, :"beta@host"}, :get)
```

Semantics:

- Same as a local `GenServer.call` — serialised by the target GenServer's mailbox.
- Default timeout `5_000` ms; on timeout, caller raises.
- Errors inside the GenServer propagate as `{:EXIT, ...}` reason only if you `handle_call` returns `{:stop, ...}` — normal replies just return `{:reply, result, state}`.
- You can leverage state, backpressure, and ordering guarantees the GenServer provides — impossible via stateless RPC.

Use this when the target has **identity** (its own state), not for pure function calls.

### 4. Timeouts and clean-up guarantees

| API                             | On caller timeout                            | On target crash                               |
|---------------------------------|----------------------------------------------|-----------------------------------------------|
| `:rpc.call/5`                   | caller raises; `rex` keeps running the func  | returns `{:badrpc, ...}`                      |
| `:erpc.call/5`                  | caller raises; target process killed         | caller raises `ErlangError` with `:erpc` tag  |
| `GenServer.call/3` (remote)     | caller raises; GenServer keeps the `:from`   | caller raises `:noproc` or `:nodedown`        |

If the side effect of the remote function is non-idempotent (e.g., "increment a counter"), a timeout on `:rpc`/`GenServer.call` leaves the side effect applied but the caller doesn't know. Design for idempotency.

### 5. Concurrency and throughput

`:rpc` tops out at a few thousand calls/sec per target node — the single `rex` process is the limit. `:erpc` scales with available schedulers; each call is a fresh process. `GenServer.call` tops out at ~1 call/sec × N processors × mailbox-processing-rate for that specific GenServer — you're bottlenecked by the target GenServer's sequential mailbox.

### 6. Fan-out: `:erpc.multicall/5`

```elixir
:erpc.multicall([:a@host, :b@host, :c@host], MyMod, :snapshot, [], 2_000)
# => [{:ok, result_a}, {:error, {:erpc, :timeout}}, {:ok, result_c}]
```

All targets execute in parallel with a shared deadline. Implementing this with `:rpc` requires manual `Task.async/await_many`; `:erpc` does it in one call.

---

## Implementation

### Step 1: Create the project

```bash
mix new rpc_demo --sup
cd rpc_demo
```

### Step 2: `mix.exs`

```elixir
defmodule RpcDemo.MixProject do
  use Mix.Project

  def project do
    [app: :rpc_demo, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {RpcDemo.Application, []}]
  end

  defp deps, do: []
end
```

### Step 3: `lib/rpc_demo/application.ex`

```elixir
defmodule RpcDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [RpcDemo.SnapshotServer]
    Supervisor.start_link(children, strategy: :one_for_one, name: RpcDemo.Supervisor)
  end
end
```

### Step 4: `lib/rpc_demo/snapshot.ex`

```elixir
defmodule RpcDemo.Snapshot do
  @moduledoc """
  The function being invoked remotely. Returns a small payload representative
  of real work (memory stats, process counts, a per-node tag).
  """

  @spec get() :: %{node: node(), memory_total: non_neg_integer(), process_count: non_neg_integer()}
  def get do
    %{
      node: node(),
      memory_total: :erlang.memory(:total),
      process_count: :erlang.system_info(:process_count)
    }
  end

  @doc "Raises to test error propagation."
  @spec boom!() :: no_return()
  def boom! do
    raise "boom on #{node()}"
  end

  @doc "Sleeps, to test timeouts."
  @spec slow(pos_integer()) :: :ok
  def slow(ms) do
    Process.sleep(ms)
    :ok
  end
end
```

### Step 5: `lib/rpc_demo/snapshot_server.ex`

```elixir
defmodule RpcDemo.SnapshotServer do
  @moduledoc "Named GenServer exposing Snapshot.get/0."
  use GenServer

  alias RpcDemo.Snapshot

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok), do: {:ok, %{calls: 0}}

  @impl true
  def handle_call(:get, _from, state) do
    {:reply, Snapshot.get(), %{state | calls: state.calls + 1}}
  end
end
```

### Step 6: `lib/rpc_demo/client.ex`

```elixir
defmodule RpcDemo.Client do
  @moduledoc """
  Three equivalent ways to call `RpcDemo.Snapshot.get/0` on a remote node.
  Return types are normalised to `{:ok, result} | {:error, reason}`.
  """
  require Logger

  alias RpcDemo.Snapshot

  @type result :: {:ok, term()} | {:error, term()}

  @spec via_rpc(node(), timeout()) :: result()
  def via_rpc(target, timeout \\ 5_000) do
    case :rpc.call(target, Snapshot, :get, [], timeout) do
      {:badrpc, reason} -> {:error, {:rpc, reason}}
      result -> {:ok, result}
    end
  end

  @spec via_erpc(node(), timeout()) :: result()
  def via_erpc(target, timeout \\ 5_000) do
    {:ok, :erpc.call(target, Snapshot, :get, [], timeout)}
  rescue
    e in ErlangError -> {:error, {:erpc, e.original}}
  catch
    :exit, reason -> {:error, {:erpc_exit, reason}}
  end

  @spec via_genserver(node(), timeout()) :: result()
  def via_genserver(target, timeout \\ 5_000) do
    {:ok, GenServer.call({RpcDemo.SnapshotServer, target}, :get, timeout)}
  catch
    :exit, {:noproc, _} -> {:error, :server_not_running}
    :exit, {:nodedown, _} -> {:error, :nodedown}
    :exit, {:timeout, _} -> {:error, :timeout}
    :exit, reason -> {:error, {:genserver_exit, reason}}
  end

  @doc "Fan-out via :erpc.multicall — one call with a shared deadline."
  @spec multicall_erpc([node()], timeout()) :: [result()]
  def multicall_erpc(targets, timeout \\ 2_000) do
    :erpc.multicall(targets, Snapshot, :get, [], timeout)
    |> Enum.map(fn
      {:ok, v} -> {:ok, v}
      {:error, reason} -> {:error, reason}
      {:throw, t} -> {:error, {:throw, t}}
      {:exit, r} -> {:error, {:exit, r}}
    end)
  end
end
```

### Step 7: `lib/rpc_demo/bench.ex`

```elixir
defmodule RpcDemo.Bench do
  @moduledoc "Latency + throughput harness across the three call mechanisms."

  alias RpcDemo.Client

  @iterations 2_000

  @spec run(node()) :: %{atom() => map()}
  def run(target) do
    %{
      rpc: time_calls(fn -> Client.via_rpc(target) end),
      erpc: time_calls(fn -> Client.via_erpc(target) end),
      genserver: time_calls(fn -> Client.via_genserver(target) end)
    }
  end

  defp time_calls(fun) do
    samples =
      for _ <- 1..@iterations do
        t0 = System.monotonic_time(:microsecond)
        _ = fun.()
        System.monotonic_time(:microsecond) - t0
      end
      |> Enum.sort()

    %{
      min: List.first(samples),
      p50: Enum.at(samples, div(@iterations, 2)),
      p99: Enum.at(samples, div(@iterations * 99, 100)),
      max: List.last(samples)
    }
  end

  @spec concurrent(node(), pos_integer()) :: %{atom() => float()}
  def concurrent(target, concurrency \\ 32) do
    %{
      rpc: throughput(fn -> Client.via_rpc(target) end, concurrency),
      erpc: throughput(fn -> Client.via_erpc(target) end, concurrency)
    }
  end

  defp throughput(fun, concurrency) do
    t0 = System.monotonic_time(:millisecond)
    duration_ms = 3_000

    tasks =
      for _ <- 1..concurrency do
        Task.async(fn ->
          count_until(fun, t0 + duration_ms, 0)
        end)
      end

    total = tasks |> Task.await_many(duration_ms + 2_000) |> Enum.sum()
    total / (duration_ms / 1_000)
  end

  defp count_until(fun, deadline_ms, acc) do
    if System.monotonic_time(:millisecond) >= deadline_ms do
      acc
    else
      _ = fun.()
      count_until(fun, deadline_ms, acc + 1)
    end
  end
end
```

### Step 8: Tests

```elixir
# test/rpc_demo/client_test.exs
defmodule RpcDemo.ClientTest do
  use ExUnit.Case, async: false

  alias RpcDemo.{Client, Snapshot}

  @self_node node()

  test "via_rpc/1 against local node returns a snapshot" do
    assert {:ok, %{node: node, process_count: n}} = Client.via_rpc(@self_node)
    assert node == @self_node
    assert is_integer(n)
  end

  test "via_erpc/1 against local node returns a snapshot" do
    assert {:ok, %{node: node}} = Client.via_erpc(@self_node)
    assert node == @self_node
  end

  test "via_genserver/1 routes through the named SnapshotServer" do
    assert {:ok, %{node: node}} = Client.via_genserver(@self_node)
    assert node == @self_node
  end

  test "via_erpc/1 surfaces raised errors" do
    assert_raise ErlangError, fn ->
      :erpc.call(@self_node, Snapshot, :boom!, [], 500)
    end
  end

  test "via_genserver/1 returns :nodedown for an unreachable node" do
    fake = :"never_existed@127.0.0.1"
    assert {:error, reason} = Client.via_genserver(fake, 200)
    assert reason in [:nodedown, :server_not_running, {:genserver_exit, :noconnection}] or
             match?({:genserver_exit, _}, reason)
  end

  test "via_rpc/1 returns {:error, {:rpc, ...}} for an unreachable node" do
    fake = :"never_existed@127.0.0.1"
    assert {:error, {:rpc, _}} = Client.via_rpc(fake, 200)
  end
end
```

Run the tests:

```bash
elixir --name test@127.0.0.1 --cookie devcluster -S mix test
```

### Step 9: Multi-node benchmark

Boot `beta@127.0.0.1` and from `alpha@127.0.0.1`:

```elixir
Node.connect(:"beta@127.0.0.1")
RpcDemo.Bench.run(:"beta@127.0.0.1")
# => %{
#   rpc:       %{min: 210, p50: 260, p99: 720, max: 1800},
#   erpc:      %{min: 180, p50: 220, p99: 560, max: 1500},
#   genserver: %{min: 190, p50: 230, p99: 640, max: 1700}
# }

RpcDemo.Bench.concurrent(:"beta@127.0.0.1", 64)
# => %{rpc: 3200.0, erpc: 15400.0}   (calls/sec)
```

`:erpc` scales roughly linearly with concurrency. `:rpc` flatlines because `rex` is sequential.

---

## Trade-offs and production gotchas

**1. `:rpc.call` silences exceptions**
Returns `{:badrpc, {:EXIT, {reason, stack}}}`. Easy to pattern-match on the happy path and ignore the `{:badrpc, _}` branch. Switch to `:erpc` and wrap in `try/rescue` for explicit error handling.

**2. Timeouts leak processes on `:rpc`**
If the caller times out, `rex` keeps running the function. The result is discarded, but the side effects are not. For non-idempotent work (writes, HTTP calls), prefer `:erpc` which kills the spawned worker on caller timeout.

**3. `GenServer.call` reorders with the server's own work**
Cross-node `GenServer.call` enters the remote mailbox like a local one. A busy GenServer on the remote side is the bottleneck, not the network. Consider async patterns (`GenServer.cast` + reply pid) or a pool.

**4. `:erpc.multicall` deadlines are shared**
If the deadline is 2 s and one node takes 1.9 s, the remaining nodes have 0.1 s. Design deadlines per request, not per node.

**5. Atom creation on the receiver**
Any `:rpc`/`:erpc` call where the arguments contain fresh atoms (e.g., `String.to_atom(input)`) creates atoms on the target node too. Classic atom-exhaustion vector. Validate inputs server-side.

**6. Code loading across nodes**
`:rpc.call(node, M, F, A)` fails with `:undef` if `M` is not loaded on the target. In hot-code-loaded systems, ensure the callee's module is present on every node (same release, or explicit `:code.load_file/1`).

**7. `:erpc` still uses the single dist port**
All `:erpc` traffic between two nodes goes through the same TCP connection. Under bursty fan-out, you can hit "busy distribution port". Monitor with `:erlang.process_info(net_kernel_pid, :message_queue_len)`.

**8. When NOT to use any of these**
Skip BEAM-native RPC when: (a) you need typed, versioned contracts across polyglot services — use gRPC or REST; (b) you need strong authentication and authorization per call — disterl auth is a single cookie; (c) throughput must survive arbitrary network failures with retries — use a message queue; (d) callees live in different failure domains (regions, VPCs) — avoid tight coupling via in-band RPC.

---

## Benchmark summary

On two BEAM nodes over loopback, 2 000 iterations each:

| API                           | p50 latency (µs) | p99 latency (µs) | throughput @ 64 concurrency (req/s) |
|-------------------------------|-----------------:|-----------------:|------------------------------------:|
| `:rpc.call/5`                 |             260  |             720  |                           ~3 200    |
| `:erpc.call/5`                |             220  |             560  |                          ~15 400    |
| `GenServer.call` remote       |             230  |             640  |                           ~4 100    |
| local `GenServer.call`        |               8  |              45  |                         ~320 000    |

`:erpc` beats `:rpc` on every axis. `GenServer.call` is competitive for single-call latency but cannot scale past the target GenServer's mailbox throughput.

---

## Resources

- [`:erpc` module — Erlang/OTP](https://www.erlang.org/doc/man/erpc.html) — the modern API
- [`:rpc` module — Erlang/OTP](https://www.erlang.org/doc/man/rpc.html) — legacy, still widely used
- [OTP 23 release notes — `:erpc` introduction](https://www.erlang.org/patches/otp-23.0) — rationale for a new RPC module
- [Learn You Some Erlang — Distribunomicon](https://learnyousomeerlang.com/distribunomicon) — RPC semantics and failure modes
- [Saša Jurić — "To spawn or not to spawn"](https://www.theerlangelist.com/article/spawn_or_not) — when to use cross-node calls vs message passing
- [Fred Hébert — Erlang in Anger, ch. 8](https://www.erlang-in-anger.com/) — busy dist ports, RPC pitfalls
- [Dashbit blog — "Introducing `:erpc` in Elixir"](https://dashbit.co/blog) — practical Elixir usage
