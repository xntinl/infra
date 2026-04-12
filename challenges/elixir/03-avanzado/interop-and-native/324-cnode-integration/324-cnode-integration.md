# C Node Integration over Erlang Distribution

**Project**: `hw_bridge` — a C program that talks to a specialized hardware SDK appears as an Erlang node on the cluster, receives RPC calls from Elixir, and returns telemetry.

## Project context

An industrial controller ships a proprietary C SDK for an RF spectrum analyzer. The SDK is
synchronous, allocates its own threads, and cannot be linked into the BEAM (known memory
corruption under load) — so NIFs and port drivers are off the table. The team already runs
Erlang distribution for cluster coordination; the cleanest integration is for the C program
to act as an **Erlang node** itself.

A C node uses `erl_interface` / `ei` to speak the distribution protocol over TCP. From the
BEAM's point of view it is a node like any other: `Node.list/0` shows it, `:rpc.call/4`
works, monitors work. The C side never has to understand the BEAM's scheduler or memory
model — it just reads and writes external terms on a socket.

```
hw_bridge/
├── lib/
│   └── hw_bridge/
│       ├── application.ex
│       └── client.ex
├── c_src/
│   └── hw_node.c
├── priv/                          # compiled c node binary lands here
├── Makefile
├── test/hw_bridge/client_test.exs
└── mix.exs
```

## Why a C node and not a NIF / driver / port

| Concern | C node | NIF | Driver | Port |
|---|---|---|---|---|
| Crash isolation | perfect (own OS process) | none | none | perfect |
| Integration cost | epmd, cookie, TCP | lowest | medium | low |
| Per-call latency | ~50µs (TCP local) | ~1µs | ~1µs | ~5ms spawn or persistent pipe |
| Vendor SDK threading safe | yes | risky | risky | yes |
| Fits cluster model | yes (a node) | no | no | no |

For a vendor SDK that cannot be trusted inside the BEAM address space, a C node is the
safe path. The ~50µs per-call cost is irrelevant for hardware telemetry sampled at 1-100
Hz.

## Why `ei` and not `erl_interface`

Two C libraries ship with Erlang: `erl_interface` (older, deprecated for new code) and `ei`
(current, low-level). `ei` is smaller, avoids the legacy connection API, and is maintained.
All new C nodes should use `ei` + either `ei_connect` (builtin) or a small helper.

## Core concepts

### 1. epmd discovery

Nodes register with `epmd` (Erlang Port Mapper Daemon) on port 4369. A C node calls
`ei_publish` to announce itself. Peer nodes lookup via epmd to find the TCP port.

### 2. The magic cookie

Every node on a distribution ring shares a cookie (a shared secret). Mismatched cookies
reject connections at handshake. A C node passes the cookie to `ei_connect_init`.

### 3. The loop

```c
while (true) {
    erlang_msg msg;
    char *buf;
    int result = ei_xreceive_msg(fd, &msg, &buf);
    // dispatch by msg.msgtype and content
}
```

A C node is a message loop. Each inbound message is a term — encoded pid, tuple, atom —
and the handler decodes with `ei_decode_*`, processes, and responds with `ei_send`.

### 4. Process naming vs anonymous

A C node process cannot register itself as `:global`. To be callable, either:
- Register a name with `erl_interface`'s local registry inside the C node, or
- Have Elixir send a first "hello" message containing the reply pid, which the C node
  stores and uses for future replies (our approach, simpler).

## Design decisions

- **Option A — one request-reply loop per connection**: simple, one client at a time.
- **Option B — per-request async with concurrent handlers**: needs pthread management in C.

→ **Option A**. Telemetry polling is low frequency; a single loop is enough and eliminates
  threading bugs in C.

- **Option A — Elixir always initiates**: the C node is a passive responder.
- **Option B — C node pushes unsolicited messages**: needs known reply pid.

→ We support **both**. Elixir sends a `{:subscribe, self()}` once; C node can then push
  telemetry proactively.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule HwBridge.MixProject do
  use Mix.Project

  def project do
    [
      app: :hw_bridge,
      version: "0.1.0",
      elixir: "~> 1.17",
      compilers: [:elixir_make] ++ Mix.compilers(),
      make_makefile: "Makefile",
      deps: [{:elixir_make, "~> 0.8", runtime: false}]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {HwBridge.Application, []}]
end
```

### Step 1: `Makefile`

```make
EI_INCLUDE := $(shell erl -eval 'io:format("~s", [code:lib_dir(erl_interface, include)])' -s init stop -noshell)
EI_LIB     := $(shell erl -eval 'io:format("~s", [code:lib_dir(erl_interface, lib)])' -s init stop -noshell)

CFLAGS  := -O2 -Wall -I$(EI_INCLUDE)
LDFLAGS := -L$(EI_LIB) -lei -lpthread

all: priv/hw_node

priv/hw_node: c_src/hw_node.c
	mkdir -p priv
	$(CC) $(CFLAGS) $< -o $@ $(LDFLAGS)

clean:
	rm -f priv/hw_node
```

### Step 2: The C node (`c_src/hw_node.c`)

```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <ei.h>

/*
 * Minimal C node:
 *   - publishes as `hw_node@<host>` with cookie from argv[1]
 *   - accepts one connection
 *   - loops: decodes {pid, {:get_reading, Channel}} or {:stop}
 *   - replies {:reading, Channel, Value} where Value is a synthetic float
 *
 * In production this is where the vendor SDK call goes.
 */

static int loop(int fd) {
    while (1) {
        ei_x_buff x;
        ei_x_new(&x);
        erlang_msg msg;
        int r = ei_xreceive_msg(fd, &msg, &x);
        if (r == ERL_TICK) { ei_x_free(&x); continue; }
        if (r == ERL_ERROR) { ei_x_free(&x); return -1; }

        int idx = 0, version;
        ei_decode_version(x.buff, &idx, &version);

        int arity;
        if (ei_decode_tuple_header(x.buff, &idx, &arity) != 0 || arity != 2) {
            ei_x_free(&x);
            continue;
        }

        erlang_pid from;
        if (ei_decode_pid(x.buff, &idx, &from) != 0) {
            ei_x_free(&x);
            continue;
        }

        // Second element: a tuple {:get_reading, N} or atom :stop
        int type, size;
        ei_get_type(x.buff, &idx, &type, &size);

        if (type == ERL_ATOM_EXT) {
            char atom[MAXATOMLEN];
            ei_decode_atom(x.buff, &idx, atom);
            if (strcmp(atom, "stop") == 0) {
                ei_x_free(&x);
                return 0;
            }
        } else if (type == ERL_SMALL_TUPLE_EXT || type == ERL_LARGE_TUPLE_EXT) {
            int inner_arity;
            ei_decode_tuple_header(x.buff, &idx, &inner_arity);
            char op[MAXATOMLEN];
            ei_decode_atom(x.buff, &idx, op);

            if (strcmp(op, "get_reading") == 0 && inner_arity == 2) {
                long channel;
                ei_decode_long(x.buff, &idx, &channel);

                // Synthetic reading — replace with vendor SDK call.
                double value = -42.5 + (double)channel;

                ei_x_buff reply;
                ei_x_new_with_version(&reply);
                ei_x_encode_tuple_header(&reply, 3);
                ei_x_encode_atom(&reply, "reading");
                ei_x_encode_long(&reply, channel);
                ei_x_encode_double(&reply, value);

                ei_send(fd, &from, reply.buff, reply.index);
                ei_x_free(&reply);
            }
        }

        ei_x_free(&x);
    }
}

int main(int argc, char **argv) {
    if (argc < 3) {
        fprintf(stderr, "usage: hw_node <node_name> <cookie>\n");
        return 1;
    }
    const char *node_name = argv[1];
    const char *cookie    = argv[2];

    char hostname[256];
    gethostname(hostname, sizeof(hostname));
    char full[512];
    snprintf(full, sizeof(full), "%s@%s", node_name, hostname);

    ei_cnode ec;
    if (ei_connect_init(&ec, node_name, cookie, 0) < 0) {
        fprintf(stderr, "ei_connect_init failed\n");
        return 1;
    }

    int listen_fd, port = 0;
    if ((listen_fd = ei_listen(&ec, &port, 5)) < 0) {
        fprintf(stderr, "ei_listen failed\n");
        return 1;
    }

    if (ei_publish(&ec, port) < 0) {
        fprintf(stderr, "ei_publish failed — is epmd running?\n");
        return 1;
    }

    ErlConnect conn;
    int fd = ei_accept(&ec, listen_fd, &conn);
    if (fd < 0) {
        fprintf(stderr, "ei_accept failed\n");
        return 1;
    }
    fprintf(stderr, "hw_node: connected to %s\n", conn.nodename);

    return loop(fd);
}
```

### Step 3: Elixir client (`lib/hw_bridge/client.ex`)

```elixir
defmodule HwBridge.Client do
  @moduledoc """
  Sends requests to the C node `hw_node@<host>` and receives typed replies.

  The Elixir side owns the C node lifecycle: a Port.open subprocess running
  `priv/hw_node`. The C node publishes itself on epmd and accepts one
  distribution connection from us.
  """
  use GenServer
  require Logger

  @c_node_shortname "hw_node"

  # ---- Public API ---------------------------------------------------------

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec get_reading(non_neg_integer(), timeout()) :: {:ok, float()} | {:error, term()}
  def get_reading(channel, timeout \\ 2_000) do
    GenServer.call(__MODULE__, {:get_reading, channel}, timeout)
  end

  # ---- Callbacks ---------------------------------------------------------

  @impl true
  def init(opts) do
    cookie = Keyword.get(opts, :cookie, to_string(Node.get_cookie()))
    host = :net_adm.localhost() |> to_string()
    c_node = :"#{@c_node_shortname}@#{host}"

    priv = :code.priv_dir(:hw_bridge) |> List.to_string()
    bin = Path.join(priv, "hw_node")

    port = Port.open({:spawn_executable, bin}, [
      :binary, :exit_status, :stderr_to_stdout,
      args: [@c_node_shortname, cookie]
    ])

    # Wait for the C node to publish and become reachable.
    :ok = wait_until_reachable(c_node, 50)

    {:ok, %{port: port, c_node: c_node}}
  end

  @impl true
  def handle_call({:get_reading, channel}, from, state) do
    # Spawn a short-lived middleman to receive the reply. This keeps the
    # GenServer off the hot path and lets multiple in-flight calls coexist.
    me = self()
    spawn_link(fn ->
      send({nil, state.c_node}, {self(), {:get_reading, channel}})
      # ↑ the "nil" process name here is a placeholder — the C node uses
      # the sender's pid encoded in the tuple, not a registered name.
      receive do
        {:reading, ^channel, value} ->
          GenServer.reply(from, {:ok, value})
      after
        2_000 ->
          GenServer.reply(from, {:error, :timeout})
      end
    end)
    {:noreply, state}
  end

  @impl true
  def handle_info({port, {:data, bytes}}, %{port: port} = state) do
    Logger.debug("hw_node stderr: #{bytes}")
    {:noreply, state}
  end

  def handle_info({port, {:exit_status, s}}, %{port: port} = state) do
    {:stop, {:c_node_exit, s}, state}
  end

  # ---- Helpers -----------------------------------------------------------

  defp wait_until_reachable(_node, 0), do: {:error, :c_node_unreachable}
  defp wait_until_reachable(node, n) do
    case :net_kernel.connect_node(node) do
      true -> :ok
      _    ->
        Process.sleep(100)
        wait_until_reachable(node, n - 1)
    end
  end
end
```

### Step 4: Application

```elixir
defmodule HwBridge.Application do
  use Application

  @impl true
  def start(_, _) do
    # The BEAM must be started with `--sname some_name --cookie COOKIE` for
    # distribution to work. Otherwise connect_node/1 always fails.
    unless Node.alive?() do
      raise "BEAM must run distributed — start with: iex --sname elixir_node --cookie foo -S mix"
    end
    Supervisor.start_link([HwBridge.Client],
      strategy: :one_for_one, name: HwBridge.Supervisor)
  end
end
```

## Why this works

```
BEAM node                                                     C node
─────────                                                     ──────
ei_listen (embedded)                                          ei_publish → epmd
            ◀────── epmd resolve "hw_node@host" ──────────▶
            ──────── distribution handshake (cookie) ────────▶
            ────────── {pid, {:get_reading, 3}} ────────────▶
            ◀──────────── {:reading, 3, -39.5} ─────────────
```

- All transport is Erlang distribution. No JSON, no manual framing — `ei_decode_*` and
  `ei_encode_*` handle term serialization.
- The C program never holds BEAM state; it is a pure message responder. Crash isolation
  is at the OS level.
- Cookie check prevents unauthorized connections — run the C node with the same cookie
  you pass to `iex --cookie`.

## Tests (`test/hw_bridge/client_test.exs`)

```elixir
defmodule HwBridge.ClientTest do
  use ExUnit.Case, async: false

  @moduletag :distributed
  @moduletag timeout: 30_000

  setup_all do
    unless Node.alive?() do
      {:ok, _} = Node.start(:"hw_bridge_test@127.0.0.1", :shortnames)
      Node.set_cookie(:hw_test_cookie)
    end

    {:ok, _} = start_supervised({HwBridge.Client, cookie: "hw_test_cookie"})
    :ok
  end

  describe "get_reading/2" do
    test "returns a float reading for a valid channel" do
      assert {:ok, value} = HwBridge.Client.get_reading(1)
      assert is_float(value)
    end

    test "reading depends on channel number" do
      {:ok, v0} = HwBridge.Client.get_reading(0)
      {:ok, v3} = HwBridge.Client.get_reading(3)
      refute v0 == v3
    end
  end

  describe "C node lifecycle" do
    test "hw_node appears in Node.list after client starts" do
      nodes = Node.list()
      assert Enum.any?(nodes, fn n -> String.starts_with?(to_string(n), "hw_node@") end)
    end
  end
end
```

Run with:
```bash
iex --sname elixir_node --cookie hw_test_cookie -S mix test
```

## Trade-offs and production gotchas

**1. EPMD dependency.** The C node registers with epmd. If epmd is not running, `ei_publish`
fails. Production deploys must start epmd (`epmd -daemon`) before the C node.

**2. Cookie leakage.** The cookie appears as a command-line argument, visible in `ps`.
Use `ERL_COOKIE_FILE` or read from an env var instead, and chmod the file 0600.

**3. Single-connection loop.** Our C node accepts exactly one connection. A second Elixir
node attempting to connect gets rejected. For multi-tenant scenarios, spawn a new pthread
per connection — and accept the added complexity.

**4. Term size limits.** `ei_x_buff` grows dynamically but very large replies (> 64MB) can
exceed TCP buffer tuning. Chunk large responses or stream them as separate messages.

**5. Graceful shutdown.** If the BEAM dies, the C node's `ei_xreceive_msg` returns
`ERL_ERROR`. Handle it: close resources, exit cleanly. Otherwise systemd or your process
supervisor keeps restarting a dead connection.

**6. When NOT to use a C node.** For < 100µs latency or > 10k calls/sec, a NIF or driver
wins. C nodes are the right choice when isolation and operational simplicity matter more
than throughput.

## Reflection

The C node here runs as a subprocess launched by Elixir via `Port.open`. An equally valid
deployment is a standalone binary started by `systemd`, connecting to the BEAM cluster at
its own pace. What operational differences (restart behavior, observability, rolling
upgrades) make you prefer one deployment topology over the other for a vendor-controlled
piece of C code that you cannot audit?

## Resources

- [`ei` reference manual — Erlang/OTP](https://www.erlang.org/doc/man/ei.html)
- [C Node tutorial — learnyousomeerlang.com](https://learnyousomeerlang.com/distribunomicon)
- [epmd — Erlang Port Mapper Daemon](https://www.erlang.org/doc/man/epmd.html)
- [Erlang distribution protocol spec](https://www.erlang.org/doc/apps/erts/erl_dist_protocol.html)
