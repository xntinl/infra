# Distributed Testing with `:peer` Nodes

**Project**: `cluster_registry` — a distributed registry whose multi-node behaviour is tested in-process using the `:peer` module introduced in OTP 25+.

## Project context

`cluster_registry` uses `:global` to register processes across a cluster. The production
cluster is 5 nodes; a misregistration or netsplit bug could take down the whole service.
Manual cluster tests (`iex --sname a` + `iex --sname b` + `Node.connect/1`) are not
repeatable. Before OTP 25, the alternative was `:slave` (deprecated, fragile). Since
OTP 25, `:peer` is the canonical multi-node test tool.

`:peer` starts child Erlang VMs as linked processes. Each has its own scheduler, code
server, and process tree. You connect them, execute code on them via `:peer.call/4`,
and shut them down cleanly — from a single test process. No external `epmd` setup,
no OS-level coordination.

```
cluster_registry/
├── lib/
│   └── cluster_registry/
│       └── registry.ex
├── test/
│   ├── cluster_registry/
│   │   └── multi_node_test.exs
│   ├── support/
│   │   └── cluster_case.ex
│   └── test_helper.exs
└── mix.exs
```

## Why `:peer` and not `:slave`

- `:slave` is deprecated since OTP 25. Scheduled for removal.
- `:peer` supports multiple distribution modes (standard, alternative), proper cleanup,
  and explicit control over arguments.
- `:peer.call/4` provides RPC without the quirks of `:rpc.call/4`.

## Why in-process peer nodes, not docker-compose

- Docker-compose multi-node is slow (multi-second startup), requires Docker in CI, and
  loses the in-process determinism of a crash or message trace.
- `:peer` starts a node in ~500ms, shuts it down cleanly, and leaves no state.

## Core concepts

### 1. Starting a peer
`{:ok, pid, node} = :peer.start_link(%{name: :alpha, args: ["-setcookie", "cookie"]})`

### 2. Connecting and calling
After `Node.connect(node)`, use `:peer.call(pid, module, fun, args)` to execute code on the peer.

### 3. Lifecycle
`:peer.stop(pid)` gracefully terminates the node. Always pair with `on_exit/1`.

### 4. Distribution is required on the calling node too
`Node.start/1` (or `--sname`/`--name` on the main VM) must run before peers can connect.

## Design decisions

- **Option A — `:slave`**: deprecated. Don't start new code with it.
- **Option B — external docker-compose / CI cluster**: heavy. Reserved for full E2E.
- **Option C — `:peer` in-process**: fast, repeatable, easy to reason about.

Chosen: **Option C**.

## Implementation

### Dependencies (`mix.exs`)

```elixir
# stdlib only
```

### Step 1: the registry

```elixir
# lib/cluster_registry/registry.ex
defmodule ClusterRegistry.Registry do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec register(term(), pid()) :: :ok | {:error, :already_registered}
  def register(key, pid) do
    case :global.register_name({__MODULE__, key}, pid) do
      :yes -> :ok
      :no  -> {:error, :already_registered}
    end
  end

  @spec whereis(term()) :: pid() | :undefined
  def whereis(key), do: :global.whereis_name({__MODULE__, key})

  @impl true
  def init(_opts), do: {:ok, %{}}
end
```

### Step 2: the ClusterCase helper

```elixir
# test/support/cluster_case.ex
defmodule ClusterRegistry.ClusterCase do
  use ExUnit.CaseTemplate

  @cookie :cluster_registry_cookie

  using do
    quote do
      import ClusterRegistry.ClusterCase
    end
  end

  setup_all do
    # Ensure the current VM is distributed. If not, start it as :nonode@nohost → short name.
    unless Node.alive?() do
      {:ok, _} = Node.start(:"primary@127.0.0.1", :longnames)
      Node.set_cookie(@cookie)
    end

    :ok
  end

  @doc """
  Starts a peer node with the current project's code loaded. Returns {peer_pid, node_name}.
  Registered via on_exit to stop cleanly.
  """
  def start_peer(short_name) do
    {:ok, peer, node} =
      :peer.start_link(%{
        name: short_name,
        host: ~c"127.0.0.1",
        longnames: true,
        args: [~c"-setcookie", Atom.to_charlist(@cookie)]
      })

    # Load current project's code paths onto the peer
    :ok = :peer.call(peer, :code, :add_paths, [:code.get_path()])

    # Ensure our app is started on the peer
    {:ok, _} = :peer.call(peer, Application, :ensure_all_started, [:cluster_registry])

    ExUnit.Callbacks.on_exit(fn -> :peer.stop(peer) end)

    {peer, node}
  end
end
```

### Step 3: tests

```elixir
# test/cluster_registry/multi_node_test.exs
defmodule ClusterRegistry.MultiNodeTest do
  # async: false — peer setup and :global are cluster-wide, serialization is safer
  use ClusterRegistry.ClusterCase, async: false

  alias ClusterRegistry.Registry

  setup do
    # Make sure our registry is running on the primary
    start_supervised!(Registry)
    :ok
  end

  describe ":global registration across nodes" do
    test "a pid registered on node A is visible from node B" do
      {peer_b, node_b} = start_peer(:node_b)

      # Connect primary <-> peer
      true = Node.connect(node_b)

      # Ensure :global has synchronized
      :ok = :global.sync()

      # Register a pid on the primary
      {:ok, agent} = Agent.start_link(fn -> :hello end)
      :ok = Registry.register(:greeter, agent)

      # Ask the peer: is the pid visible?
      peer_pid =
        :peer.call(peer_b, :global, :whereis_name, [{Registry, :greeter}])

      assert peer_pid == agent

      # The peer can even message it across the cluster
      result = :peer.call(peer_b, Agent, :get, [agent, & &1])
      assert result == :hello
    end

    test "duplicate registration across nodes is rejected" do
      {peer_b, node_b} = start_peer(:node_c)
      true = Node.connect(node_b)
      :ok = :global.sync()

      # Spawn the registry on the peer as well
      {:ok, _} = :peer.call(peer_b, Registry, :start_link, [[]])

      # Register from primary
      {:ok, agent_a} = Agent.start_link(fn -> :a end)
      :ok = Registry.register(:dup, agent_a)

      # Attempt duplicate from peer — :global returns :no → our wrapper returns {:error, :already_registered}
      {:ok, agent_b} = :peer.call(peer_b, Agent, :start_link, [fn -> :b end])

      result = :peer.call(peer_b, Registry, :register, [:dup, agent_b])
      assert result == {:error, :already_registered}
    end
  end

  describe "netsplit simulation" do
    test "stopping a peer removes its processes from the cluster view" do
      {peer_b, node_b} = start_peer(:node_d)
      true = Node.connect(node_b)
      :ok = :global.sync()

      # Register a pid that lives on the peer
      {:ok, peer_agent} = :peer.call(peer_b, Agent, :start_link, [fn -> :on_peer end])
      :ok = :peer.call(peer_b, Registry, :start_link, [[]])
      :ok = :peer.call(peer_b, Registry, :register, [:only_on_b, peer_agent])
      :ok = :global.sync()

      assert Registry.whereis(:only_on_b) == peer_agent

      # Kill the peer
      :peer.stop(peer_b)

      # :global will eventually notice — wait up to 2s
      wait_until(fn -> Registry.whereis(:only_on_b) == :undefined end, 2_000)

      assert Registry.whereis(:only_on_b) == :undefined
    end
  end

  # ---------------------------------------------------------------------------
  # Helpers — polling without sleep
  # ---------------------------------------------------------------------------

  defp wait_until(fun, timeout) when timeout > 0 do
    if fun.() do
      :ok
    else
      Process.sleep(20)
      wait_until(fun, timeout - 20)
    end
  end

  defp wait_until(_, _), do: {:error, :timeout}
end
```

## Why this works

- `:peer.start_link/1` spawns a real Erlang VM as a linked process. When the test exits
  (or the linking process dies), the peer is reaped. `on_exit/1` is a belt-and-braces
  for clean teardown.
- `:peer.call/4` is synchronous RPC; the return value is the peer's execution result.
  Arguments must be serializable, which means no closures that capture local pids (unless
  the pid remains valid on the peer — which works in a connected cluster).
- Loading `:code.get_path/0` onto the peer is what makes the current project's modules
  available. Without it, you get `{:undef, ...}` the first time you call into your code.
- `:global.sync/0` forces the global registry to reach consensus across connected nodes.
  Without it, a just-connected peer may briefly see a stale view.

## Tests

See Step 3 — three tests cover cross-node registration, conflict detection, and
cluster departure.

## Benchmark

Starting a peer takes ~500ms on a modern laptop. Running 3 tests with peers in a single
module takes ~2 seconds wall clock.

```elixir
{t, _} = :timer.tc(fn ->
  {peer, _} = ClusterRegistry.ClusterCase.start_peer(:bench_peer)
  :peer.stop(peer)
end)
IO.puts("peer lifecycle: #{t / 1000}ms")
```

Target: peer start+stop < 1s.

## Trade-offs and production gotchas

**1. Forgetting `Node.start/1` on the primary**
If the test-runner VM is not distributed, peers cannot connect. Always guard with
`unless Node.alive?()` in setup.

**2. Not loading `:code.get_path/0` onto the peer**
The peer has a pristine OTP code server. Your app's modules are not available. Always
`:peer.call(peer, :code, :add_paths, [:code.get_path()])`.

**3. `:global.sync/0` omitted**
Right after `Node.connect/1`, `:global` state may not have propagated. Without `sync`,
tests race the distribution protocol. Always sync after a topology change.

**4. Running peer tests `async: true`**
`:global` state is a single logical namespace across all connected nodes. Two async
tests connecting peers see each other's registrations. Always `async: false`.

**5. Test host with short-names vs long-names mismatch**
Peers specified with `longnames: true` require the primary to be started with
`longnames`. Mixing modes produces `:nodedown` errors. Be consistent.

**6. When NOT to use this**
For single-node logic (even if the code uses `:global` APIs, if the test only needs
"one node"), `:peer` is overkill. Start multi-node testing once you are actually
testing distribution concerns — not before.

## Reflection

`:peer` starts a real VM. Yet many "distributed" bugs (netsplits, delayed messages,
clock skew) still require more than two connected nodes in a trustworthy network to
reproduce. What classes of distributed failure remain unreachable by `:peer`-based
tests, and what tooling (Jepsen, Concuerror, fault injection) would close the gap?

## Resources

- [`:peer` on erlang.org](https://www.erlang.org/doc/man/peer.html)
- [OTP 25 release notes — peer](https://www.erlang.org/blog/my-otp-25-highlights/)
- [`:global` module](https://www.erlang.org/doc/man/global.html)
- [Elixir Forum — migrating from :slave to :peer](https://elixirforum.com/t/migrating-from-slave-to-peer/)
