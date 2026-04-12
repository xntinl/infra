# libcluster with EPMD and EPMD-less DNS Strategy

**Project**: `cluster_bootstrap` — service discovery and node connectivity for a multi-node BEAM application.

## Project context

You operate a real-time notification service deployed on Kubernetes. The service is built in Elixir and needs all replicas to form a connected Erlang cluster so that `Phoenix.PubSub` can broadcast across pods without an external broker. In development and bare-metal production the operations team still uses EPMD (Erlang Port Mapper Daemon). In the new Kubernetes stack they want to drop EPMD because it requires an extra port (4369) and extra firewall rules per pod, and because pods resolve each other through a headless service (DNS SRV records).

You need one codebase that works in both environments: classic `epmd` strategy in development, and EPMD-less DNS strategy in Kubernetes. libcluster is the de facto library for this, but the configuration differs drastically between strategies, and the EPMD-less mode has subtle requirements (release vm.args flags, `:erl_epmd` module replacement, unique node names that survive restarts).

```
cluster_bootstrap/
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── prod.exs
│   └── runtime.exs
├── lib/
│   └── cluster_bootstrap/
│       ├── application.ex
│       ├── topology.ex
│       └── node_namer.ex
├── rel/
│   ├── env.sh.eex
│   └── vm.args.eex
├── test/
│   └── cluster_bootstrap/
│       └── node_namer_test.exs
├── bench/
│   └── connect_bench.exs
└── mix.exs
```

## Why libcluster and not hand-rolled `Node.connect/1`

The naive approach is to hardcode a list of node names and call `Node.connect/1` at boot. Three problems:

1. **Reconnection**: if a node crashes and restarts with a new IP, nothing reconnects.
2. **Bootstrap races**: pod A starts 200 ms before pod B; A's initial connect fails silently.
3. **Dynamic topology**: adding a replica to Kubernetes requires redeploying every other replica with an updated list.

libcluster provides pluggable strategies (`Epmd`, `Kubernetes`, `DNSPoll`, `Gossip`) that run a supervised process which polls for candidates and issues `Node.connect/1` on state changes. Reconnection is automatic; topology is declarative.

## Why EPMD-less in Kubernetes

EPMD listens on TCP 4369 and resolves `node@host` → port. In Kubernetes, every pod already has a stable DNS name via the headless service, and the distribution port is fixed per release (e.g. 9100). EPMD becomes redundant. Removing it:

- drops one listening port per pod,
- avoids the EPMD-gets-stuck-after-crash class of bugs,
- simplifies NetworkPolicy rules,
- makes distribution work with IPv6 clusters where EPMD has historical quirks.

The price: you must replace the default `:erl_epmd` module with `Elixir.ErlEpmdWrapper` (shipped by libcluster) and pin the distribution port via vm.args.

## Core concepts

### 1. EPMD vs EPMD-less

| Aspect                 | EPMD (`Cluster.Strategy.Epmd`)                 | EPMD-less (`Cluster.Strategy.DNSPoll` + `erl_epmd` replacement) |
| ---------------------- | ---------------------------------------------- | ------------------------------------------------------------- |
| Port                   | 4369 + dynamic distribution port               | Only a fixed distribution port                                |
| Node name resolution   | EPMD maps name → port                          | Distribution port is hardcoded, DNS maps name → IP            |
| Suited for             | Local dev, bare metal, static clusters         | Kubernetes, Nomad, any orchestrator with stable DNS           |
| Requires vm.args flags | No                                             | Yes (`-start_epmd false -epmd_module Elixir.ErlEpmdWrapper`)  |

### 2. Node naming strategies

Long names (`app@pod-0.headless.namespace.svc.cluster.local`) are required whenever nodes live across subnets or containers. Short names (`app@host`) only work on the same host or broadcast domain. In Kubernetes you MUST use long names.

### 3. Stable node names across restarts

If a pod restarts and gets a new node name, monitors on the old name fire a `:nodedown` but nobody reconnects the new name until libcluster polls. Use the pod's stable DNS name (`POD_IP`-based) so the same pod keeps the same node name across crashes.

## Design decisions

- **Option A — single static topology for all envs**: wrong. Dev does not have Kubernetes DNS; prod does not have localhost.
- **Option B — separate releases per env**: too much maintenance overhead.
- **Option C — one codebase, topology resolved at runtime from `config/runtime.exs`** (chosen). Environment variables select strategy; release config stays identical.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ClusterBootstrap.MixProject do
  use Mix.Project

  def project do
    [
      app: :cluster_bootstrap,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: releases()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {ClusterBootstrap.Application, []}
    ]
  end

  defp deps do
    [
      {:libcluster, "~> 3.3"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end

  defp releases do
    [
      cluster_bootstrap: [
        include_executables_for: [:unix],
        applications: [runtime_tools: :permanent]
      ]
    ]
  end
end
```

### Step 1: Node naming helper

Derives a deterministic node name from the pod's IP and release name.

```elixir
# lib/cluster_bootstrap/node_namer.ex
defmodule ClusterBootstrap.NodeNamer do
  @moduledoc """
  Builds the long node name used when starting distribution.

  In Kubernetes the pod exposes its IP via the `POD_IP` env var
  (downward API). Using the IP instead of the pod hostname avoids
  issues when headless DNS propagation is slow at boot.
  """

  @spec build(String.t(), map()) :: String.t()
  def build(release_name, env) when is_binary(release_name) and is_map(env) do
    host = Map.get(env, "POD_IP") || Map.get(env, "HOSTNAME") || "127.0.0.1"
    "#{release_name}@#{host}"
  end
end
```

### Step 2: Topology module resolved at runtime

```elixir
# lib/cluster_bootstrap/topology.ex
defmodule ClusterBootstrap.Topology do
  @moduledoc """
  Returns the libcluster topology list for the current environment.

  Two modes, selected via `CLUSTER_MODE`:
    * `epmd`    → `Cluster.Strategy.Epmd` with a static hosts list
    * `dns`     → `Cluster.Strategy.DNSPoll` against a headless service
  """

  @spec build(map()) :: keyword()
  def build(env \\ System.get_env()) do
    case Map.get(env, "CLUSTER_MODE", "epmd") do
      "epmd" -> [notifications: [strategy: Cluster.Strategy.Epmd, config: epmd_config(env)]]
      "dns" -> [notifications: [strategy: Cluster.Strategy.DNSPoll, config: dns_config(env)]]
    end
  end

  defp epmd_config(env) do
    hosts =
      env
      |> Map.get("CLUSTER_HOSTS", "")
      |> String.split(",", trim: true)
      |> Enum.map(&String.to_atom/1)

    [hosts: hosts]
  end

  defp dns_config(env) do
    [
      polling_interval: 5_000,
      query: Map.fetch!(env, "CLUSTER_DNS_QUERY"),
      node_basename: Map.fetch!(env, "CLUSTER_NODE_BASENAME")
    ]
  end
end
```

### Step 3: Application supervision tree

```elixir
# lib/cluster_bootstrap/application.ex
defmodule ClusterBootstrap.Application do
  use Application

  @impl true
  def start(_type, _args) do
    topologies = Application.get_env(:libcluster, :topologies, [])

    children = [
      {Cluster.Supervisor, [topologies, [name: ClusterBootstrap.ClusterSupervisor]]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ClusterBootstrap.Supervisor)
  end
end
```

### Step 4: Runtime config

```elixir
# config/runtime.exs
import Config

if config_env() == :prod do
  config :libcluster,
    topologies: ClusterBootstrap.Topology.build(System.get_env())
end
```

```elixir
# config/dev.exs
import Config

config :libcluster,
  topologies: [
    notifications: [
      strategy: Cluster.Strategy.Epmd,
      config: [hosts: [:"app1@127.0.0.1", :"app2@127.0.0.1"]]
    ]
  ]
```

### Step 5: Release vm.args for EPMD-less mode

```
# rel/vm.args.eex
-name <%= System.get_env("RELEASE_NODE") %>
-setcookie <%= System.get_env("RELEASE_COOKIE") %>

## EPMD-less: pin the distribution port and disable EPMD startup
-start_epmd false
-epmd_module Elixir.Cluster.EpmdCache
-kernel inet_dist_listen_min 9100 inet_dist_listen_max 9100
```

```bash
# rel/env.sh.eex — invoked before release start
#!/bin/sh
export RELEASE_NODE="cluster_bootstrap@${POD_IP}"
```

### Step 6: Data flow diagram

```
                    ┌───────────────────────────────────┐
                    │  Kubernetes headless Service      │
                    │  notifications-headless            │
                    └──────────────┬────────────────────┘
                                   │ DNS A records
                                   ▼
  Pod A (10.0.1.4)          Pod B (10.0.1.5)          Pod C (10.0.1.6)
  RELEASE_NODE=             RELEASE_NODE=              RELEASE_NODE=
  cb@10.0.1.4               cb@10.0.1.5                cb@10.0.1.6
        │                         │                          │
        └──── libcluster DNSPoll ──┴── Node.connect ──────────┘
                                   │
                                   ▼
                         Phoenix.PubSub broadcasts
                         Horde.Registry sync
                         :pg groups
```

## Why this works

libcluster's `DNSPoll` strategy performs an `:inet_res.getbyname/2` every `polling_interval` ms, builds the list of candidate nodes as `#{basename}@#{ip}`, and calls `Node.connect/1` only for nodes not yet connected. The polling interval covers the gap between pod start and DNS propagation; once connected, the BEAM maintains the TCP link and libcluster simply keeps the observed set in sync.

The EPMD-less setup works because we pin the distribution port to 9100 (matching min and max in `vm.args`), so any node can reach any other node directly via `IP:9100` without consulting EPMD. The `erl_epmd` module replacement answers name-lookups locally with the static port.

## Tests

```elixir
# test/cluster_bootstrap/node_namer_test.exs
defmodule ClusterBootstrap.NodeNamerTest do
  use ExUnit.Case, async: true

  alias ClusterBootstrap.NodeNamer

  describe "build/2" do
    test "uses POD_IP when available" do
      assert NodeNamer.build("app", %{"POD_IP" => "10.0.0.1"}) == "app@10.0.0.1"
    end

    test "falls back to HOSTNAME when POD_IP missing" do
      assert NodeNamer.build("app", %{"HOSTNAME" => "pod-0"}) == "app@pod-0"
    end

    test "falls back to 127.0.0.1 when nothing is set" do
      assert NodeNamer.build("app", %{}) == "app@127.0.0.1"
    end
  end

  describe "topology build/1" do
    alias ClusterBootstrap.Topology

    test "returns EPMD topology when CLUSTER_MODE=epmd" do
      env = %{"CLUSTER_MODE" => "epmd", "CLUSTER_HOSTS" => "a@h,b@h"}
      topo = Topology.build(env)

      assert [{:notifications, cfg}] = topo
      assert cfg[:strategy] == Cluster.Strategy.Epmd
      assert cfg[:config][:hosts] == [:"a@h", :"b@h"]
    end

    test "returns DNS topology when CLUSTER_MODE=dns" do
      env = %{
        "CLUSTER_MODE" => "dns",
        "CLUSTER_DNS_QUERY" => "notifications-headless",
        "CLUSTER_NODE_BASENAME" => "cb"
      }

      topo = Topology.build(env)
      assert [{:notifications, cfg}] = topo
      assert cfg[:strategy] == Cluster.Strategy.DNSPoll
      assert cfg[:config][:query] == "notifications-headless"
      assert cfg[:config][:node_basename] == "cb"
    end
  end
end
```

## Benchmark

Measures time to form a 3-node cluster from a cold start. Run with 3 terminals.

```elixir
# bench/connect_bench.exs
:timer.tc(fn ->
  # Wait until at least 2 peers are visible
  Stream.repeatedly(fn -> Node.list() end)
  |> Stream.each(fn _ -> Process.sleep(50) end)
  |> Enum.find(fn list -> length(list) >= 2 end)
end)
|> then(fn {micros, _} -> IO.puts("Cluster formed in #{micros / 1000} ms") end)
```

Target: < 6 seconds with `polling_interval: 5_000`. On a LAN EPMD strategy converges under 500 ms; DNSPoll is bounded by the polling interval plus DNS TTL.

## Trade-offs and production gotchas

1. **Polling interval vs convergence**: lower interval = faster recovery, higher DNS pressure. 5 s is the sweet spot for most teams. Under 1 s you risk rate-limiting your DNS resolver.
2. **Cookie leakage**: `RELEASE_COOKIE` in a ConfigMap is world-readable to anyone with cluster-read RBAC. Use a Secret mounted as env, never hardcode in `vm.args`.
3. **Node name reuse after crash**: if a pod crashes and the same IP is reassigned to a fresh pod before the cluster notices, the new pod inherits all inbound monitors — usually benign, occasionally surprising.
4. **Long name vs short name mismatch**: mixing `-name` and `-sname` in the same cluster silently refuses connection. Pick one per cluster.
5. **Firewall forgot port 9100**: EPMD-less requires the pinned distribution port to be reachable in both directions. NetworkPolicies that only allow 4369 will fail to form the cluster.
6. **When NOT to use libcluster**: if you operate a single-node service, distribution buys you nothing and adds attack surface. Turn off distribution entirely (`-dist_listen false` is not a thing; use `elixir` without `--sname`/`--name`).

## Reflection

If your cluster needs to span two Kubernetes clusters in different regions with 60 ms RTT, what breaks first: `Node.monitor` timeouts, `:global` locks, or `Phoenix.PubSub` broadcast fan-out? Which libcluster strategy would you choose, and would you still use `:global` at all?

## Resources

- [libcluster hexdocs](https://hexdocs.pm/libcluster/readme.html)
- [`Cluster.Strategy.DNSPoll` source](https://github.com/bitwalker/libcluster/blob/main/lib/strategy/dns_poll.ex)
- [Fly.io — Running Elixir Clusters](https://fly.io/docs/elixir/the-basics/clustering/)
- [Erlang distribution protocol](https://www.erlang.org/doc/apps/erts/erl_dist_protocol.html)
- [EPMD-less release guide](https://github.com/bitwalker/libcluster#epmdless)
