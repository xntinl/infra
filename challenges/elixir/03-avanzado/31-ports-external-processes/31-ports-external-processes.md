# Ports and External Processes

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` needs two integrations with external processes: a Python-based ML scoring
service that cannot be rewritten in Elixir, and a live log tailer that streams gateway
access logs to monitoring subscribers. Both require bidirectional, long-lived communication
with OS processes.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       └── middleware/
│           ├── ml_scorer.ex        # ← you implement this
│           └── log_tailer.ex       # ← and this
├── test/
│   └── api_gateway/
│       └── middleware/
│           ├── ml_scorer_test.exs
│           └── log_tailer_test.exs
├── priv/
│   └── scorer.py                   # given — do not modify
└── mix.exs
```

---

## The business problem

**ML scorer**: the fraud detection team trained a Python model. They deliver it as a script
that reads JSON requests from stdin and writes JSON responses to stdout. The gateway must call
it on every payment request. The model takes ~30ms per call, so the Port must stay alive
across requests — spawning a new Python process per request would be 200ms+ of startup overhead.

**Log tailer**: the ops team subscribes to gateway access logs in real time. Multiple
monitoring processes (Prometheus exporter, alerting agent) must receive every new log line.
The simplest source is `tail -f` on the access log file, wrapped in a Port with fanout.

---

## Why Ports and not `System.cmd`

```
System.cmd: spawns a process, waits for it to finish, returns {output, exit_code}
             Blocking. One-shot. No streaming. Simple.

Port.open:  creates a long-lived bridge to an OS process.
             Messages flow in both directions while both sides are alive.
             The Port dies when the OS process dies, and vice versa.
```

A Port behaves like a process: you send it messages and receive messages from it. You can
monitor it with `Port.monitor/1`. When the GenServer that owns the Port dies, the Port
closes and the OS process receives EOF on stdin.

```
GenServer  ---send({self(), {:command, data}})----->  Port  ---stdin----->  OS process
           <---{port, {:data, response}}-----------         <---stdout--
```

---

## Given Python script — `priv/scorer.py`

```python
#!/usr/bin/env python3
import sys, json

for line in sys.stdin:
    req = json.loads(line.strip())
    action = req.get("action", "")
    if action == "score":
        amount = req.get("amount", 0)
        # Fake model: flag amounts > 1000 as high risk
        score = min(1.0, amount / 1000.0)
        print(json.dumps({"score": score, "risk": "high" if score > 0.7 else "low"}),
              flush=True)
    else:
        print(json.dumps({"error": f"unknown action: {action}"}), flush=True)
```

---

## Implementation

### Step 1: `lib/api_gateway/middleware/ml_scorer.ex`

The MlScorer GenServer opens a Python process via a Port at startup and keeps it alive across
requests. Each scoring request is serialized as a JSON line to the Port's stdin; the response
arrives as a JSON line on stdout. The GenServer stores the caller's `from` reference and replies
asynchronously when the Port sends back data.

The restart logic uses exponential backoff: if the Python process crashes, the GenServer waits
progressively longer before re-opening the Port, up to `@max_retries` attempts.

```elixir
defmodule ApiGateway.Middleware.MlScorer do
  @moduledoc """
  GenServer that keeps a Python scoring process alive via a Port.

  The Port is opened once at startup and reused across all requests.
  Requests are sent as JSON lines; responses arrive as JSON lines.

  Single-request-at-a-time semantics: the GenServer holds the caller's
  `from` reference and replies when the Port sends back data.
  For concurrent requests, a correlation-ID scheme would be needed.
  """
  use GenServer

  defstruct [:port, :pending, :script_path, :retries, :status]

  @max_retries 3

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    script_path = Keyword.fetch!(opts, :script_path)
    GenServer.start_link(__MODULE__, script_path, opts)
  end

  @spec score(GenServer.server(), map(), timeout()) ::
          {:ok, map()} | {:error, term()}
  def score(pid, request, timeout \\ 5_000) do
    GenServer.call(pid, {:score, request}, timeout)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(script_path) do
    state = %__MODULE__{
      script_path: script_path,
      retries: 0,
      status: :starting,
      pending: nil
    }

    case open_port(script_path) do
      {:ok, port} ->
        {:ok, %{state | port: port, status: :running}}

      {:error, reason} ->
        {:stop, {:port_open_failed, reason}}
    end
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:score, _request}, _from, %{status: :restarting} = state) do
    {:reply, {:error, :restarting}, state}
  end

  @impl true
  def handle_call({:score, request}, from, state) do
    json_line = Jason.encode!(request) <> "\n"
    send(state.port, {self(), {:command, json_line}})
    {:noreply, %{state | pending: from}}
  end

  @impl true
  def handle_info({port, {:data, {:eol, line}}}, %{port: port} = state) do
    case Jason.decode(line) do
      {:ok, decoded} ->
        if state.pending, do: GenServer.reply(state.pending, {:ok, decoded})

      {:error, reason} ->
        if state.pending, do: GenServer.reply(state.pending, {:error, {:decode_failed, reason}})
    end

    {:noreply, %{state | pending: nil}}
  end

  @impl true
  def handle_info({port, {:exit_status, _code}}, %{port: port} = state) do
    if state.pending do
      GenServer.reply(state.pending, {:error, :scorer_crashed})
    end

    attempt_restart(%{state | pending: nil, status: :restarting})
  end

  @impl true
  def handle_info({:DOWN, _ref, :port, _port, reason}, state) do
    if state.pending do
      GenServer.reply(state.pending, {:error, {:port_down, reason}})
    end

    attempt_restart(%{state | pending: nil, status: :restarting})
  end

  @impl true
  def handle_info(:do_restart, state) do
    case open_port(state.script_path) do
      {:ok, port} ->
        Process.send_after(self(), :reset_retries, 30_000)
        {:noreply, %{state | port: port, status: :running, retries: state.retries}}

      {:error, _reason} ->
        attempt_restart(state)
    end
  end

  @impl true
  def handle_info(:reset_retries, state) do
    {:noreply, %{state | retries: 0}}
  end

  @impl true
  def terminate(_reason, state) do
    if state.port && Port.info(state.port) != nil do
      Port.close(state.port)
    end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp open_port(script_path) do
    python = System.find_executable("python3") || System.find_executable("python")

    if is_nil(python) do
      {:error, :python_not_found}
    else
      port =
        Port.open({:spawn_executable, python}, [
          :binary,
          :exit_status,
          {:line, 4096},
          args: [script_path]
        ])

      Port.monitor(port)
      {:ok, port}
    end
  end

  defp attempt_restart(%{retries: n}) when n >= @max_retries do
    {:stop, {:exhausted_retries, @max_retries}, %__MODULE__{}}
  end

  defp attempt_restart(%{retries: n} = state) do
    delay = trunc(100 * :math.pow(2, n))
    Process.send_after(self(), :do_restart, delay)
    {:noreply, %{state | retries: n + 1}}
  end
end
```

### Step 2: `lib/api_gateway/middleware/log_tailer.ex`

The LogTailer opens `tail -f` via a Port and fans out each new line to all subscribed processes.
Subscribers are monitored with `Process.monitor/1` so dead subscribers are automatically
removed from the fanout list without affecting the others.

```elixir
defmodule ApiGateway.Middleware.LogTailer do
  @moduledoc """
  Follows a log file with `tail -f` and broadcasts each new line to subscribers.

  Subscribers are monitored. When a subscriber process dies, it is removed
  automatically from the fanout list without affecting the others.
  """
  use GenServer

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    file_path  = Keyword.fetch!(opts, :file_path)
    subscriber = Keyword.fetch!(opts, :subscriber)
    GenServer.start_link(__MODULE__, {file_path, subscriber}, opts)
  end

  @spec subscribe(GenServer.server(), pid()) :: :ok
  def subscribe(pid, subscriber), do: GenServer.call(pid, {:subscribe, subscriber})

  @spec unsubscribe(GenServer.server(), pid()) :: :ok | {:error, :not_subscribed}
  def unsubscribe(pid, subscriber), do: GenServer.call(pid, {:unsubscribe, subscriber})

  @spec stop(GenServer.server()) :: :ok
  def stop(pid), do: GenServer.stop(pid)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init({file_path, subscriber}) do
    tail = System.find_executable("tail")

    port =
      Port.open({:spawn_executable, tail}, [
        :binary,
        {:line, 4096},
        args: ["-f", file_path]
      ])

    ref = Process.monitor(subscriber)
    {:ok, %{port: port, subscribers: %{subscriber => ref}}}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:subscribe, pid}, _from, state) do
    if Map.has_key?(state.subscribers, pid) do
      {:reply, :ok, state}
    else
      ref = Process.monitor(pid)
      {:reply, :ok, %{state | subscribers: Map.put(state.subscribers, pid, ref)}}
    end
  end

  @impl true
  def handle_call({:unsubscribe, pid}, _from, state) do
    case Map.pop(state.subscribers, pid) do
      {nil, _subs} ->
        {:reply, {:error, :not_subscribed}, state}

      {ref, new_subs} ->
        Process.demonitor(ref, [:flush])
        {:reply, :ok, %{state | subscribers: new_subs}}
    end
  end

  @impl true
  def handle_info({port, {:data, {:eol, line}}}, %{port: port} = state) do
    Enum.each(state.subscribers, fn {pid, _ref} ->
      send(pid, {:new_line, line})
    end)

    {:noreply, state}
  end

  @impl true
  def handle_info({:DOWN, ref, :process, pid, _reason}, state) do
    new_subs = Map.reject(state.subscribers, fn {p, r} -> p == pid and r == ref end)
    {:noreply, %{state | subscribers: new_subs}}
  end

  @impl true
  def terminate(_reason, %{port: port}) do
    Port.close(port)
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/ml_scorer_test.exs
defmodule ApiGateway.Middleware.MlScorerTest do
  use ExUnit.Case, async: false

  @script Path.expand("../../../priv/scorer.py", __DIR__)

  setup do
    skip_if_no_python()
    {:ok, pid} = ApiGateway.Middleware.MlScorer.start_link(script_path: @script)
    {:ok, scorer: pid}
  end

  test "scores a low-risk request", %{scorer: scorer} do
    assert {:ok, %{"score" => score, "risk" => "low"}} =
             ApiGateway.Middleware.MlScorer.score(scorer, %{"action" => "score", "amount" => 50})

    assert score < 0.7
  end

  test "scores a high-risk request", %{scorer: scorer} do
    assert {:ok, %{"score" => score, "risk" => "high"}} =
             ApiGateway.Middleware.MlScorer.score(scorer, %{"action" => "score", "amount" => 900})

    assert score > 0.7
  end

  test "returns error for unknown action", %{scorer: scorer} do
    assert {:ok, %{"error" => _}} =
             ApiGateway.Middleware.MlScorer.score(scorer, %{"action" => "unknown"})
  end

  defp skip_if_no_python do
    unless System.find_executable("python3") || System.find_executable("python") do
      ExUnit.skip("python3 not found")
    end
  end
end
```

### Step 4: `log_tailer_test.exs` — given tests

```elixir
# test/api_gateway/middleware/log_tailer_test.exs
defmodule ApiGateway.Middleware.LogTailerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Middleware.LogTailer

  @log_path "/tmp/test_gateway_#{:erlang.unique_integer([:positive])}.log"

  setup do
    File.write!(@log_path, "")

    on_exit(fn -> File.rm(@log_path) end)
    :ok
  end

  defp start_tailer do
    {:ok, pid} =
      LogTailer.start_link(file_path: @log_path, subscriber: self())

    pid
  end

  test "delivers new log lines to the subscriber" do
    skip_if_no_tail()
    tailer = start_tailer()

    Process.sleep(200)

    File.write!(@log_path, "line one\n", [:append])
    assert_receive {:new_line, "line one"}, 2_000

    File.write!(@log_path, "line two\n", [:append])
    assert_receive {:new_line, "line two"}, 2_000

    LogTailer.stop(tailer)
  end

  test "additional subscriber receives lines after subscribe/2" do
    skip_if_no_tail()
    tailer = start_tailer()
    other  = self()

    Process.sleep(100)
    LogTailer.subscribe(tailer, other)
    Process.sleep(100)

    File.write!(@log_path, "shared line\n", [:append])
    assert_receive {:new_line, "shared line"}, 2_000

    LogTailer.stop(tailer)
  end

  test "dead subscriber is removed automatically" do
    skip_if_no_tail()
    tailer = start_tailer()

    dead_sub = spawn(fn -> receive do: (:stop -> :ok) end)
    LogTailer.subscribe(tailer, dead_sub)
    Process.exit(dead_sub, :kill)
    Process.sleep(100)

    # Writing to the file must not crash the tailer
    File.write!(@log_path, "after death\n", [:append])
    assert_receive {:new_line, "after death"}, 2_000

    LogTailer.stop(tailer)
  end

  defp skip_if_no_tail do
    unless System.find_executable("tail") do
      ExUnit.skip("tail not found on this system")
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/middleware/ml_scorer_test.exs --trace
mix test test/api_gateway/middleware/log_tailer_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Port (line mode) | Port (packet N) | `System.cmd` |
|--------|-----------------|-----------------|--------------|
| Communication | Text lines | Framed binary | One-shot |
| Suitable for | JSON-over-stdio protocols | Binary protocols | Quick commands |
| Streaming | Yes | Yes | No |
| Backpressure | None (OS pipe buffer) | None | N/A |
| Latency per call | ~0.1ms IPC | ~0.1ms IPC | 100-300ms (spawn) |
| Crash handling | Monitor Port, restart | Monitor Port, restart | Check exit code |

Reflection: the ML scorer currently processes one request at a time. If you needed 50
concurrent scoring requests, what would you change? Consider a pool of Ports vs. correlation
IDs in a single Port vs. a `ConsumerSupervisor` pattern.

---

## Common production mistakes

**1. Spawning a new Port per request**
OS process startup is 50-300ms. Opening a Port once and keeping it alive is the reason
`MlScorer` is a GenServer. Never `Port.open` inside a request handler.

**2. Not handling `{:exit_status, code}` in `handle_info`**
When the OS process dies, the Port sends `{port, {:exit_status, code}}`. Without this
clause, the GenServer does not know the process is gone and the next `send` to the port
raises a `Port not open` error.

**3. Not flushing stdout in the external process**
If the Python script buffers its output (the default when stdout is not a TTY), the Port
never receives data. Always call `flush=True` in `print()` or use `sys.stdout.flush()`.
Node.js scripts need `process.stdout.write(...)` followed by synchronous drain.

**4. Using `{:spawn, "cmd arg"}` instead of `{:spawn_executable, path}`**
The first form invokes a shell (unsafe with user-controlled arguments). The second bypasses
the shell and passes `args` as a list — safer and avoids shell injection.

**5. Not cleaning up dead subscribers in the log tailer**
Without `Process.monitor/1`, dead subscribers stay in the fanout list forever. Every
`send/2` to a dead pid silently succeeds (messages go to the dead process's mailbox, which
no longer exists), but the map grows without bound.

---

## Resources

- [Port — Elixir docs](https://hexdocs.pm/elixir/Port.html)
- [`:erlang.open_port/2` — Erlang docs](https://www.erlang.org/doc/man/erlang.html#open_port-2)
- [Erlang in Anger — Ports and external processes](https://www.erlang-in-anger.com/)
- [Porcelain — higher-level Port wrapper](https://github.com/alco/porcelain)
