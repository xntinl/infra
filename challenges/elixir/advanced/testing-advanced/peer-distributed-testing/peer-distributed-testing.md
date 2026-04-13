# Distributed Testing with `:peer` Nodes

**Project**: `cluster_registry` — a distributed registry whose multi-node behaviour is tested in-process using the `:peer` module introduced in OTP 25+.

---

## Why `:peer` distributed testing matters

`cluster_registry` uses `:global` to register processes across a cluster. The production
cluster is 5 nodes; a misregistration or netsplit bug could take down the whole service.
Manual cluster tests (`iex --sname a` + `iex --sname b` + `Node.connect/1`) are not
repeatable. Before OTP 25, the alternative was `:slave` (deprecated, fragile). Since
OTP 25, `:peer` is the canonical multi-node test tool.

`:peer` starts child Erlang VMs as linked processes. Each has its own scheduler, code
server, and process tree. You connect them, execute code on them via `:peer.call/4`,
and shut them down cleanly — from a single test process.

---

## The business problem

- `:slave` is deprecated since OTP 25. Scheduled for removal.
- `:peer` supports multiple distribution modes, proper cleanup, and explicit control over arguments.
- Docker-compose multi-node is slow (multi-second startup), requires Docker in CI, and
  loses the in-process determinism of a crash or message trace.
- `:peer` starts a node in ~500ms, shuts it down cleanly, and leaves no state.

Reliable distributed tests prevent the class of bugs that only manifest across nodes:
stale `:global` views, duplicate registrations during joins, and pids left dangling after
netsplits.

---

## Project structure

```
cluster_registry/
├── lib/
│   └── cluster_registry/
│       └── registry.ex
├── script/
│   └── main.exs
├── test/
│   ├── cluster_registry/
│   │   └── multi_node_test.exs
│   ├── support/
│   │   └── cluster_case.ex
│   └── test_helper.exs
└── mix.exs
```

---

## Design decisions

- **Option A — `:slave`**: deprecated. Don't start new code with it.
- **Option B — external docker-compose / CI cluster**: heavy. Reserved for full E2E.
- **Option C — `:peer` in-process** (chosen): fast, repeatable, easy to reason about.

---

## Implementation

### `mix.exs`

```elixir
defmodule ClusterRegistry.MixProject do
  use Mix.Project

  def project do
    [
      app: :cluster_registry,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_),     do: ["lib"]
end
```

### `lib/cluster_registry/registry.ex`

```elixir
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

### `test/cluster_registry_test.exs`

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
    unless Node.alive?() do
      {:ok, _} = Node.start(:"primary@127.0.0.1", :longnames)
      Node.set_cookie(@cookie)
    end
    :ok
  end

  def start_peer(short_name) do
    {:ok, peer, node} =
      :peer.start_link(%{
        name: short_name,
        host: ~c"127.0.0.1",
        longnames: true,
        args: [~c"-setcookie", Atom.to_charlist(@cookie)]
      })

    :ok = :peer.call(peer, :code, :add_paths, [:code.get_path()])
    {:ok, _} = :peer.call(peer, Application, :ensure_all_started, [:cluster_registry])

    ExUnit.Callbacks.on_exit(fn -> :peer.stop(peer) end)
    {peer, node}
  end
end

# test/cluster_registry/multi_node_test.exs
defmodule ClusterRegistry.MultiNodeTest do
  use ClusterRegistry.ClusterCase, async: false

  alias ClusterRegistry.Registry

  setup do
    start_supervised!(Registry)
    :ok
  end

  describe ":global registration across nodes" do
    test "a pid registered on node A is visible from node B" do
      {peer_b, node_b} = start_peer(:node_b)
      true = Node.connect(node_b)
      :ok = :global.sync()

      {:ok, agent} = Agent.start_link(fn -> :hello end)
      :ok = Registry.register(:greeter, agent)

      peer_pid = :peer.call(peer_b, :global, :whereis_name, [{Registry, :greeter}])
      assert peer_pid == agent

      result = :peer.call(peer_b, Agent, :get, [agent, & &1])
      assert result == :hello
    end

    test "stopping a peer removes its processes from the cluster view" do
      {peer_b, node_b} = start_peer(:node_d)
      true = Node.connect(node_b)
      :ok = :global.sync()

      {:ok, peer_agent} = :peer.call(peer_b, Agent, :start_link, [fn -> :on_peer end])
      :ok = :peer.call(peer_b, Registry, :start_link, [[]])
      :ok = :peer.call(peer_b, Registry, :register, [:only_on_b, peer_agent])
      :ok = :global.sync()

      assert Registry.whereis(:only_on_b) == peer_agent

      :peer.stop(peer_b)

      wait_until(fn -> Registry.whereis(:only_on_b) == :undefined end, 2_000)
      assert Registry.whereis(:only_on_b) == :undefined
    end
  end

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

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== Peer Distributed Testing Demo ===")
    IO.puts("This example uses :peer, which requires a distributed VM.")
    IO.puts("Run `mix test` to execute the multi-node suite.")
    IO.puts("=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs` (or `mix test` for the real suite).

---

## Key concepts

### 1. Starting a peer
`{:ok, pid, node} = :peer.start_link(%{name: :alpha, args: ["-setcookie", "cookie"]})`

### 2. Connecting and calling
After `Node.connect(node)`, use `:peer.call(pid, module, fun, args)` to execute code on the peer.

### 3. Lifecycle
`:peer.stop(pid)` gracefully terminates the node. Always pair with `on_exit/1`.

### 4. Distribution is required on the calling node too
`Node.start/1` (or `--sname`/`--name` on the main VM) must run before peers can connect.

### 5. Production gotchas

- Forgetting `Node.start/1` on the primary — peers cannot connect.
- Not loading `:code.get_path/0` onto the peer — your app's modules are not available.
- `:global.sync/0` omitted — tests race the distribution protocol after a topology change.
- Running peer tests `async: true` — `:global` state is a single logical namespace; always `async: false`.
- Short-names vs long-names mismatch between primary and peer — produces `:nodedown` errors.
- For single-node logic (even if the code uses `:global` APIs), `:peer` is overkill.

---

## Resources

- [`:peer` module — Erlang docs](https://www.erlang.org/doc/man/peer.html)
- [`:global` — Erlang stdlib](https://www.erlang.org/doc/man/global.html)
- [OTP 25 release notes](https://www.erlang.org/news/157)
- [Distributed Elixir](https://hexdocs.pm/elixir/Node.html)
