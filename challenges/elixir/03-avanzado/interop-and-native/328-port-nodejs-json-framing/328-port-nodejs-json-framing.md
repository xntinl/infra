# BEAM ↔ Node.js over a Port with JSON Framing

**Project**: `frontend_render` — server-side render React components by keeping a Node.js process alive and exchanging JSON messages with a 4-byte length prefix.

## Project context

A content platform renders marketing pages server-side for SEO. React components live in
a Next.js-like JavaScript codebase maintained by the frontend team. Rewriting them in
Elixir (e.g. with `eex`) is politically impossible and technically suboptimal — React
Server Components and hydration rely on the JavaScript runtime.

The integration pattern: start `node render.js` once as a port subprocess, keep it alive,
and send render requests as length-prefixed JSON frames. Each frame is one JSON object.
Node.js renders, writes back a JSON response with the same frame protocol, and waits for
the next.

```
frontend_render/
├── lib/
│   └── frontend_render/
│       ├── application.ex
│       └── node_port.ex
├── priv/
│   └── node/
│       └── render.js
├── test/frontend_render/node_port_test.exs
└── mix.exs
```

## Why length-prefixed JSON and not newline-delimited

Newline-delimited JSON (NDJSON) works for small messages but breaks when the payload
contains a literal `\n` (rare but possible in HTML strings). Length prefixing is robust:
you know exactly how many bytes to read.

The BEAM Port type `{:packet, 4}` handles this natively on the Elixir side — the VM reads
a 4-byte big-endian length, then the payload, and delivers the payload as one message.
On the Node.js side you do the same manually.

## Why Node.js subprocess and not a service

For this workload (server-side rendering) the rate is low (~100 req/sec per node), the
request is ephemeral, and the JS runtime must stay local to exploit file-level caches of
compiled bundles. A subprocess is perfect: one per Elixir node, started at boot, restarted
on crash.

## Core concepts

### 1. `Port.open({:spawn_executable, ...}, [{:packet, 4}, :binary])`

- `:packet, 4` — framing: 4-byte big-endian length prefix on every packet.
- `:binary` — payload arrives as a binary.
- Elixir sends/receives complete frames; partial-read handling is kernel-done.

### 2. Node.js-side framing

Node's `process.stdin` gives you a byte stream. You must:
1. Maintain a rolling buffer.
2. Read the 4-byte length prefix.
3. Read `length` more bytes.
4. Repeat.

Use `process.stdout.write(Buffer.concat([lengthBuf, payload]))` for responses.

### 3. Correlation IDs

Requests may overlap if you use cast patterns. Include a monotonic ID per request; the
response echoes the same ID. The Elixir side keeps a `Map.new()` of pending IDs → callers.

### 4. Backpressure

`Port.command/2` blocks when the pipe buffer fills. For large payloads this is automatic
backpressure. For small ones, the caller returns instantly; Node.js queues internally.

## Design decisions

- **Option A — one request-at-a-time synchronous call**: no IDs needed, simpler Node code.
  Serialized, low throughput.
- **Option B — async with correlation IDs**: concurrent requests to the same Node process.
  Node.js is single-threaded but async, so multiple in-flight I/O-bound requests overlap.

→ **Option A** first for clarity. For higher throughput you spin up a pool of Node workers;
  within each worker, serialization is fine because Node itself is single-threaded.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule FrontendRender.MixProject do
  use Mix.Project

  def project do
    [
      app: :frontend_render,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [
        {:jason, "~> 1.4"}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {FrontendRender.Application, []}]
end
```

### Step 1: The Node.js side (`priv/node/render.js`)

```javascript
// render.js — length-prefixed JSON frame handler.
// Reads 4-byte big-endian length, then a JSON payload of that length.
// Writes responses in the same format.
//
// This file is intentionally dependency-free so the exercise runs with
// vanilla Node.js. A real renderer would import React's renderToString.

'use strict';

const LENGTH_BYTES = 4;
let buffer = Buffer.alloc(0);

function writeFrame(obj) {
    const payload = Buffer.from(JSON.stringify(obj), 'utf8');
    const len = Buffer.alloc(LENGTH_BYTES);
    len.writeUInt32BE(payload.length, 0);
    process.stdout.write(Buffer.concat([len, payload]));
}

function handle(req) {
    try {
        // Trivial render: wrap the input in <h1>.
        // In production, this calls ReactDOMServer.renderToString(...).
        if (req.op === 'render') {
            const html = `<h1>${escapeHtml(req.title)}</h1>`;
            return { id: req.id, ok: true, html };
        }
        if (req.op === 'echo') {
            return { id: req.id, ok: true, echo: req.payload };
        }
        return { id: req.id, ok: false, error: `unknown op: ${req.op}` };
    } catch (e) {
        return { id: req.id, ok: false, error: String(e) };
    }
}

function escapeHtml(s) {
    return String(s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

process.stdin.on('data', (chunk) => {
    buffer = Buffer.concat([buffer, chunk]);
    while (buffer.length >= LENGTH_BYTES) {
        const len = buffer.readUInt32BE(0);
        if (buffer.length < LENGTH_BYTES + len) break;
        const payload = buffer.subarray(LENGTH_BYTES, LENGTH_BYTES + len);
        buffer = buffer.subarray(LENGTH_BYTES + len);

        let req;
        try {
            req = JSON.parse(payload.toString('utf8'));
        } catch {
            writeFrame({ ok: false, error: 'invalid JSON' });
            continue;
        }
        writeFrame(handle(req));
    }
});

process.stdin.on('end', () => process.exit(0));
process.on('SIGTERM', () => process.exit(0));
```

### Step 2: Elixir port wrapper (`lib/frontend_render/node_port.ex`)

```elixir
defmodule FrontendRender.NodePort do
  @moduledoc """
  Manages one persistent Node.js subprocess. Requests and responses are
  JSON frames with a 4-byte big-endian length prefix; the BEAM enforces
  framing via the Port option `{:packet, 4}`.

  The GenServer holds a map of `correlation_id => GenServer.from` so
  multiple callers can have requests in flight simultaneously.
  """
  use GenServer
  require Logger

  defstruct [:port, :pending, :next_id]

  # ---- Public API ---------------------------------------------------------

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec render(String.t(), timeout()) :: {:ok, String.t()} | {:error, term()}
  def render(title, timeout \\ 5_000),
    do: request(%{op: "render", title: title}, timeout)

  @spec echo(term(), timeout()) :: {:ok, term()} | {:error, term()}
  def echo(payload, timeout \\ 5_000),
    do: request(%{op: "echo", payload: payload}, timeout)

  defp request(body, timeout),
    do: GenServer.call(__MODULE__, {:request, body}, timeout)

  # ---- GenServer ---------------------------------------------------------

  @impl true
  def init(_) do
    priv = :code.priv_dir(:frontend_render) |> List.to_string()
    script = Path.join([priv, "node", "render.js"])

    node_bin =
      System.find_executable("node") ||
        raise "node is not on PATH — install Node.js to run frontend_render"

    port =
      Port.open({:spawn_executable, node_bin}, [
        :binary,
        :exit_status,
        {:packet, 4},
        args: [script]
      ])

    {:ok, %__MODULE__{port: port, pending: %{}, next_id: 1}}
  end

  @impl true
  def handle_call({:request, body}, from, %{port: port, pending: p, next_id: id} = state) do
    frame = Map.put(body, :id, id) |> Jason.encode!()
    Port.command(port, frame)
    {:noreply, %{state | pending: Map.put(p, id, from), next_id: id + 1}}
  end

  @impl true
  def handle_info({port, {:data, frame}}, %{port: port, pending: p} = state) do
    case Jason.decode(frame) do
      {:ok, %{"id" => id} = resp} ->
        case Map.pop(p, id) do
          {nil, _} ->
            Logger.warning("unknown correlation id #{id} in response")
            {:noreply, state}
          {from, rest} ->
            GenServer.reply(from, decode_result(resp))
            {:noreply, %{state | pending: rest}}
        end
      {:error, reason} ->
        Logger.error("invalid JSON from node: #{inspect(reason)}")
        {:noreply, state}
    end
  end

  def handle_info({port, {:exit_status, s}}, %{port: port, pending: p} = state) do
    # Node.js died — fail all pending callers.
    for {_id, from} <- p, do: GenServer.reply(from, {:error, {:node_exited, s}})
    {:stop, :node_exited, state}
  end

  defp decode_result(%{"ok" => true, "html" => html}), do: {:ok, html}
  defp decode_result(%{"ok" => true, "echo" => payload}), do: {:ok, payload}
  defp decode_result(%{"ok" => false, "error" => err}), do: {:error, err}
end
```

### Step 3: Supervision

```elixir
defmodule FrontendRender.Application do
  use Application

  @impl true
  def start(_, _) do
    Supervisor.start_link([FrontendRender.NodePort],
      strategy: :one_for_one, name: __MODULE__)
  end
end
```

## Why this works

```
┌── Elixir ────────────────────┐     ┌── Node.js ─────────────────┐
│                              │     │                            │
│ render(title)                │     │ process.stdin.on('data')   │
│   ↓ Jason.encode! + id       │     │   buffer accumulation      │
│   ↓ Port.command             │     │   ↓ readUInt32BE + slice   │
│                              │     │   ↓ JSON.parse             │
│   ── {:packet,4} frame ───▶  │     │   ↓ handle(req)            │
│                              │     │   ↓ writeFrame(resp)       │
│   ◀── {:packet,4} frame ──   │     │                            │
│                              │     │                            │
│ handle_info → reply to caller│     │                            │
└──────────────────────────────┘     └────────────────────────────┘
```

- The BEAM's `{:packet, 4}` removes ambiguity: Elixir always gets complete JSON objects.
- Correlation IDs let N callers have parallel in-flight requests.
- If Node.js crashes, every pending `GenServer.from` gets an error reply — no caller
  hangs forever.
- On supervisor restart, a new Node.js interpreter boots in ~50ms; the GenServer's
  `init` blocks until the port is open.

## Tests (`test/frontend_render/node_port_test.exs`)

```elixir
defmodule FrontendRender.NodePortTest do
  use ExUnit.Case, async: false

  setup_all do
    unless System.find_executable("node"), do: {:skip, "node not on PATH"}
    {:ok, _} = start_supervised(FrontendRender.NodePort)
    :ok
  end

  describe "render/2" do
    test "wraps title in h1" do
      assert {:ok, "<h1>Hello</h1>"} = FrontendRender.NodePort.render("Hello")
    end

    test "escapes HTML-sensitive characters" do
      assert {:ok, html} = FrontendRender.NodePort.render("<script>alert(1)</script>")
      assert html =~ "&lt;script&gt;"
    end
  end

  describe "echo/2" do
    test "round-trips a string" do
      assert {:ok, "ping"} = FrontendRender.NodePort.echo("ping")
    end

    test "round-trips a map" do
      assert {:ok, %{"a" => 1, "b" => [2, 3]}} =
               FrontendRender.NodePort.echo(%{a: 1, b: [2, 3]})
    end
  end

  describe "concurrent requests" do
    test "100 concurrent renders all succeed" do
      tasks =
        for i <- 1..100 do
          Task.async(fn -> FrontendRender.NodePort.render("item #{i}") end)
        end
      results = Task.await_many(tasks, 10_000)
      assert Enum.all?(results, &match?({:ok, _}, &1))
    end
  end
end
```

## Trade-offs and production gotchas

**1. Head-of-line blocking inside Node.**  Node.js is single-threaded. If one render takes
500ms, requests queued behind it wait. Run a pool of N Node.js subprocesses and round-robin,
or use Node's worker_threads for CPU-heavy renders.

**2. JSON is the wrong format for binary data.** Base64-encoding a 2MB image inflates it by
33% and costs CPU both sides. For binary payloads, switch to MessagePack or keep binaries
out of the frame (pass a file path).

**3. Node's unhandled promise rejection kills the process.** A runaway `await` with no
`catch` aborts. Wrap top-level handlers in try/catch and log; never let a request crash
the whole interpreter.

**4. Restart loop on deterministic crashes.** If the first frame after boot always crashes
the JS, the supervisor restarts forever. Use an exponential-backoff restart policy or a
one-shot circuit breaker.

**5. Port command buffer limits.** `Port.command` can block if node's stdin buffer is full
(> 64KB). For huge payloads (> 100KB), test under load; consider chunking or base64-encoding
into a separate channel.

**6. When NOT to use this pattern.** If you can express the workload in Elixir (EEx,
Phoenix.LiveView, plain string interpolation), do that. Use the Node bridge only when
you genuinely need JavaScript's ecosystem (React, specific npm packages).

## Reflection

The current design has one Node.js subprocess per Elixir VM. Under load you want N, each
handling a slice of traffic. Should the pool be round-robin (this exercise's approach in
ml_inference) or should it be "free-first" (next available worker takes the request)?
Reason about the mailbox-vs-checkout trade-off when the subprocesses have non-uniform
response times.

## Resources

- [`Port` packet protocols — Erlang erts](https://www.erlang.org/doc/man/erlang.html#open_port-2)
- [Jason — Elixir JSON library](https://hexdocs.pm/jason/)
- [Node.js process.stdin](https://nodejs.org/api/process.html#process_process_stdin)
- [`renderToString` — React docs](https://react.dev/reference/react-dom/server/renderToString)
