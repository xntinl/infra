# Ports — Wrapping External Processes Safely

**Project**: `port_demo` — a long-lived wrapper around an external CLI (`ffmpeg`, `imagemagick`, a Python ML model) exposed as an OTP GenServer to the rest of the system.

**Difficulty**: ★★★★☆

**Estimated time**: 3–6 hours

---

## Project context

Sooner or later every Elixir system needs to run a program the BEAM didn't write — `ffmpeg`, `ocrmypdf`, a Python tokenizer, a Go binary, a shell pipeline. The naive reach is `System.cmd/3`, but it has three fatal limits: it blocks the calling process, spawns a fresh OS process per call, and has no way to stream stdout as it arrives. For anything heavier than `echo`, you need a **Port**.

Ports are the BEAM's mechanism to own an external OS process as a linked "port" object. The VM sends bytes to the program's stdin and receives messages when bytes arrive on stdout. Link semantics let you crash the owning Elixir process if the port dies, and vice versa — with one enormous caveat: **an Elixir process killed without closing its port leaks the OS process**.

In this exercise you wrap `cat -u` (line-buffered) as a stand-in for a real subprocess and build `port_demo` — a GenServer that owns the port, serializes requests, streams responses by line, and handles crashes deterministically.

```
port_demo/
├── lib/
│   └── port_demo/
│       ├── application.ex
│       ├── worker.ex           # GenServer owning the port
│       └── line_buffer.ex      # stdin/stdout framing helper
├── test/
│   └── port_demo/
│       └── worker_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Port anatomy

```
 Elixir process (owner)
       │
       │  Port.command(port, data)           # send bytes to stdin
       │
       ▼
     Port ──────────────────────────────────▶ external OS program
       ▲                                           (stdin/stdout pipe)
       │
       │  {port, {:data, bytes}}             # stdout message
       │  {port, {:exit_status, n}}          # process exit
       │
 Elixir process (owner mailbox)
```

A Port is referenced by a `port()` BEAM term. Only the owning process can send commands to it. Messages are delivered to the owner's mailbox.

### 2. Port options — the ones that matter

| Option | Effect |
|---|---|
| `:binary` | deliver data as binaries (not charlists) — **always use this** |
| `{:packet, N}` | frame messages with an N-byte length prefix (1, 2, or 4) |
| `:line` (N) | deliver one line at a time, buffered up to N bytes |
| `:stream` | raw stream, no framing — caller handles partials |
| `:exit_status` | send `{:exit_status, N}` when program exits |
| `:use_stdio` | communicate via stdin/stdout (default) |
| `:stderr_to_stdout` | merge stderr into stdout |
| `{:args, [...]}` | argv (safe — no shell interpretation) |
| `{:cd, path}` | working directory |
| `{:env, [{k, v}]}` | environment |

For most wrappers, `[:binary, :exit_status, {:args, argv}, :stderr_to_stdout]` is the safe baseline.

### 3. Framing modes — why `:packet` matters

With `:stream` you receive arbitrary byte chunks — a single `puts "hello"` may arrive as two `{:data, _}` messages. You must reassemble. Options:

- **`:line` mode** — BEAM buffers until `\n`. Good for line-oriented tools.
- **`{:packet, 4}`** — BEAM prepends/expects a 4-byte big-endian length header. The other side must do the same. Use this when you control the program (e.g., a Rust or Go helper you wrote).
- **`:stream`** — for binary protocols where you implement your own framing.

### 4. The zombie process problem

If the Elixir owner crashes without closing the port, BEAM closes the port — but the external program **may survive** if it doesn't watch for stdin EOF. Classic zombie: `Port.open({:spawn_executable, path}, ...)` and the owner crashes while the external is in a `sleep` loop. The process keeps running, detached.

Mitigations:
1. The external program must exit when it reads EOF on stdin.
2. Use `erlexec` / `:exec` library for proper SIGKILL on port close.
3. Wrap spawn with a shell script that traps parent death (`prctl(PR_SET_PDEATHSIG)` on Linux).

### 5. `System.cmd/3` vs `Port.open/2` vs `:os.cmd/1`

| API | Use when |
|---|---|
| `System.cmd/3` | one-shot, small output, don't care about streaming |
| `:os.cmd/1` | quick debug only — **shell interpolation risk** |
| `Port.open/2` | long-lived subprocess, streaming, bidirectional |
| `:exec.run/2` (erlexec) | need proper signal handling, kill on orphan |

### 6. Backpressure via port_command

`Port.command/2` (and `send/2` under the hood) will block the owner if the OS pipe buffer fills. This is BEAM-level backpressure: your GenServer's `handle_call` will stall until the external drains stdin. Use `Port.command(port, data, [:nosuspend])` to fail fast instead if you prefer.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule PortDemo.MixProject do
  use Mix.Project

  def project, do: [app: :port_demo, version: "0.1.0", elixir: "~> 1.15", deps: []]
  def application, do: [extra_applications: [:logger], mod: {PortDemo.Application, []}]
end
```

### Step 2: `lib/port_demo/application.ex`

```elixir
defmodule PortDemo.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {PortDemo.Worker, cmd: ~w(cat -u)}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PortDemo.Supervisor)
  end
end
```

### Step 3: `lib/port_demo/worker.ex`

```elixir
defmodule PortDemo.Worker do
  @moduledoc """
  GenServer wrapping an external process via `Port.open/2` in `:line` mode.
  Serializes `send/2` requests and returns the next full line from stdout.
  """
  use GenServer
  require Logger

  @type opt :: {:cmd, [String.t()]} | {:name, GenServer.name()}

  @spec start_link([opt()]) :: GenServer.on_start()
  def start_link(opts) do
    {name, opts} = Keyword.pop(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  @spec send(GenServer.server(), binary(), timeout()) :: {:ok, binary()} | {:error, term()}
  def send(server \\ __MODULE__, payload, timeout \\ 5_000) do
    GenServer.call(server, {:send, payload}, timeout)
  end

  @impl true
  def init(opts) do
    [exe | args] = Keyword.fetch!(opts, :cmd)
    path = System.find_executable(exe) || raise "executable not found: #{exe}"

    port =
      Port.open(
        {:spawn_executable, path},
        [
          :binary,
          :exit_status,
          :stderr_to_stdout,
          {:line, 8192},
          {:args, args}
        ]
      )

    Process.flag(:trap_exit, true)
    {:ok, %{port: port, waiting: :queue.new()}}
  end

  @impl true
  def handle_call({:send, payload}, from, %{port: port, waiting: q} = state) do
    Port.command(port, payload <> "\n")
    {:noreply, %{state | waiting: :queue.in(from, q)}}
  end

  @impl true
  def handle_info({port, {:data, {:eol, line}}}, %{port: port, waiting: q} = state) do
    case :queue.out(q) do
      {{:value, from}, q2} ->
        GenServer.reply(from, {:ok, line})
        {:noreply, %{state | waiting: q2}}

      {:empty, _} ->
        Logger.warning("unexpected stdout line, no waiter: #{inspect(line)}")
        {:noreply, state}
    end
  end

  @impl true
  def handle_info({port, {:exit_status, status}}, %{port: port, waiting: q} = state) do
    Logger.error("external process exited with status #{status}")
    for from <- :queue.to_list(q), do: GenServer.reply(from, {:error, {:exit, status}})
    {:stop, {:port_exited, status}, state}
  end

  @impl true
  def handle_info({:EXIT, port, reason}, %{port: port} = state) do
    {:stop, {:port_exit, reason}, state}
  end

  @impl true
  def terminate(_reason, %{port: port}) do
    if Port.info(port), do: Port.close(port)
    :ok
  end
end
```

### Step 4: `test/port_demo/worker_test.exs`

```elixir
defmodule PortDemo.WorkerTest do
  use ExUnit.Case, async: false

  alias PortDemo.Worker

  setup do
    name = :"worker_#{System.unique_integer([:positive])}"
    pid = start_supervised!({Worker, cmd: ~w(cat -u), name: name})
    %{worker: name, pid: pid}
  end

  test "echoes back the payload", %{worker: w} do
    assert {:ok, "hello"} = Worker.send(w, "hello")
    assert {:ok, "world"} = Worker.send(w, "world")
  end

  test "handles many rapid sends", %{worker: w} do
    replies = for i <- 1..100, do: Worker.send(w, "msg-#{i}")
    expected = for i <- 1..100, do: {:ok, "msg-#{i}"}
    assert replies == expected
  end

  test "crashes cleanly when external dies", %{worker: w, pid: pid} do
    Process.monitor(pid)
    {:os_pid, os_pid} = Port.info(:sys.get_state(pid).port, :os_pid)
    System.cmd("kill", ["-9", Integer.to_string(os_pid)])
    assert_receive {:DOWN, _ref, :process, ^pid, {:port_exited, _}}, 2_000
  end
end
```

---

## Trade-offs and production gotchas

**1. Zombie external processes.** If the GenServer dies and the external doesn't read stdin EOF, the process survives detached. Either write the helper to exit on EOF, or use `erlexec`.

**2. Ordering assumption.** The line-buffered wrapper assumes the external responds in the same order requests arrived. True for `cat`, not for a multi-threaded Python service — you'd need per-request correlation IDs.

**3. Partial lines.** `:line, N` truncates if a single line exceeds N bytes and delivers `{:noeol, data}` instead of `{:eol, data}`. Test with huge lines or raise N.

**4. `:stderr_to_stdout` is lossy.** Interleaving stderr into stdout corrupts structured output. For JSON-over-stdout, keep stderr separate and log it — use `:stderr_to_stdout` only for human-readable CLIs.

**5. Shell injection via `:spawn`.** `Port.open({:spawn, cmd_string}, ...)` goes through `/bin/sh -c`. **Never** use `:spawn` with user input. Always `{:spawn_executable, path}` + `{:args, list}`.

**6. Backpressure hangs your GenServer.** If the external stalls reading stdin, `Port.command/2` blocks the GenServer, backing up `handle_call`. Consider `:nosuspend` + a send queue + timeout.

**7. Restart storms.** If the external flaps (init → crash in 100ms), a Supervisor with `:permanent` will respawn indefinitely. Set `max_restarts: 3, max_seconds: 60` and tag the child.

**8. When NOT to use this.** Short-lived, one-shot invocations — `System.cmd/3` is simpler and fine. Heavy binary protocols with complex framing — consider a NIF or Rustler instead. Sub-millisecond latency — the OS process boundary kills you.

---

## Performance notes

Round-trip latency for a line-mode `cat -u`:

```elixir
:timer.tc(fn ->
  for _ <- 1..10_000, do: Worker.send(:worker, "x")
end)
```

Typical: ~0.3–0.8 ms per round-trip on localhost Linux — dominated by pipe syscall overhead, not BEAM scheduling. Batching (write 1000 lines, read 1000 replies) drops per-op cost by 10×.

For comparison, `System.cmd/3` spawning a fresh process takes 3–10 ms each due to `fork+exec`.

---

## Resources

- https://hexdocs.pm/elixir/Port.html — Port module reference
- https://www.erlang.org/doc/man/erlang.html#open_port-2 — `erlang:open_port/2`
- https://hexdocs.pm/erlexec/ — `:exec` library with proper signal handling
- https://hexdocs.pm/porcelain/ — high-level wrapper (deprecated but pedagogically useful)
- https://stuff-things.net/2016/05/24/elixir-ports/ — long-form Ports writeup
- https://theerlangelist.com/article/outside_elixir — Saša Jurić on external processes
- https://dashbit.co/blog/running-port-drivers-on-elixir — Dashbit on port lifecycle
