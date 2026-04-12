# Graceful shutdown and drain

**Project**: `drain_shutdown` — drain in-flight requests before `terminate/2` completes.

**Difficulty**: ★★★★☆
**Estimated time**: 4–5 hours

---

## Project context

You run an HTTP API that handles 800 rps with a p95 of 120 ms. Deploys happen 8 times a day. Each deploy kills the old VM — historically by sending `SIGKILL` after Kubernetes' 30 s grace period. Reality: the 30 s was being wasted because the app was not wired for graceful shutdown. Every deploy dropped ~100 in-flight requests, which returned `502 Bad Gateway` to clients. That was acceptable a year ago; now it's a user-facing SLO violation.

You need to implement a four-stage shutdown for your request-handling GenServer:

1. **Stop accepting new work** (reject fast, flip an "unavailable" flag).
2. **Wait for in-flight work to complete** (bounded by a drain timeout).
3. **Flush side effects** (commit pending DB writes, ship telemetry buffer).
4. **Exit cleanly** so the supervisor reports `:shutdown` not `:killed`.

Pattern: `Process.flag(:trap_exit, true)` in `init/1` to intercept the `:shutdown` signal from the supervisor, then run the drain in `terminate/2`. The parent supervisor must specify a `shutdown:` timeout ≥ your drain budget, otherwise the supervisor sends `:brutal_kill` before drain completes.

```
drain_shutdown/
├── lib/
│   └── drain_shutdown/
│       ├── application.ex
│       ├── server.ex          # the drainable GenServer
│       └── dispatcher.ex      # sends jobs, mimics the request layer
└── test/
    └── drain_shutdown/
        └── drain_test.exs
```

---

## Core concepts

### 1. The shutdown signal path

```
Supervisor.terminate_child(sup, pid)
       │
       ├── sends {:EXIT, sup_pid, :shutdown} to the child
       │
       ├── waits up to `shutdown:` ms for child to exit
       │
       └── if still alive → Process.exit(pid, :kill)  # brutal
```

Without `trap_exit: true`, a `GenServer` receiving `{:EXIT, sup_pid, :shutdown}` exits immediately, skipping `terminate/2`. With `trap_exit`, the EXIT becomes a regular message, the `GenServer` runs `terminate/2`, and you have the full `shutdown:` budget to drain.

### 2. `shutdown:` values and their meaning

```elixir
%{id: MyServer,
  start: {MyServer, :start_link, []},
  shutdown: 10_000,      # ms
  restart: :permanent}
```

| Value | Behavior |
|---|---|
| `:brutal_kill` | `Process.exit(pid, :kill)` immediately. No `terminate/2`. |
| `:infinity` | Wait forever. Dangerous — hangs shutdown if drain hangs. |
| integer N (ms) | Wait N ms; then `Process.exit(pid, :kill)`. |

Pick N = (typical drain) + (safety margin). Never `:infinity` for workers; reserve it for nested supervisors only.

### 3. Application-wide shutdown timeout

`Application.stop/1` has its own timer (default `:infinity`). The OS supervisor (systemd, K8s) gives you a fixed window (K8s `terminationGracePeriodSeconds`, default 30 s). If your app's drain takes 60 s, K8s sends `SIGKILL` at 30 s regardless.

Align these three:

```
K8s terminationGracePeriodSeconds (30s)
    ≥  Application drain budget (25s)
        ≥  Root supervisor shutdown (20s)
            ≥  Leaf server shutdown (15s)
```

### 4. The four-stage drain pattern

```elixir
def terminate(reason, state) do
  # Stage 1: flip gate to reject new work.
  :ets.insert(:gate, {:accepting, false})

  # Stage 2: wait for in-flight work to drain.
  wait_for_drain(state, _deadline_ms = 10_000)

  # Stage 3: flush side effects.
  flush_buffer(state)

  # Stage 4: return — Supervisor will log reason.
  :ok
end
```

### 5. The gate pattern

A public "accepting" flag (ETS or `:persistent_term`) that the entry points check BEFORE doing work:

```elixir
def handle(req) do
  if accepting?() do
    GenServer.call(Server, {:handle, req})
  else
    {:error, :draining}
  end
end
```

The flag is set by the GenServer's `terminate/2` in stage 1. Readers do NOT go through the GenServer, so the flip is effective even if the GenServer is already handling 50 queued messages.

---

## Implementation

### Step 1: Application

```elixir
# lib/drain_shutdown/application.ex
defmodule DrainShutdown.Application do
  use Application

  @impl true
  def start(_type, _args) do
    :ets.new(:drain_gate, [:named_table, :public, read_concurrency: true])
    :ets.insert(:drain_gate, {:accepting, true})

    children = [
      %{
        id: DrainShutdown.Server,
        start: {DrainShutdown.Server, :start_link, []},
        shutdown: 15_000,
        restart: :permanent
      }
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: DrainShutdown.Supervisor
    )
  end
end
```

### Step 2: The drainable server

```elixir
# lib/drain_shutdown/server.ex
defmodule DrainShutdown.Server do
  @moduledoc """
  Request-handling GenServer with four-stage graceful shutdown.
  """
  use GenServer
  require Logger

  @drain_deadline_ms 10_000

  # ---------------------------------------------------------------------------
  # Public API — the gate is on the CLIENT side for fast rejection.
  # ---------------------------------------------------------------------------

  @spec handle(term()) :: {:ok, term()} | {:error, :draining}
  def handle(req) do
    if accepting?() do
      GenServer.call(__MODULE__, {:handle, req}, 30_000)
    else
      {:error, :draining}
    end
  end

  @spec in_flight() :: non_neg_integer()
  def in_flight, do: GenServer.call(__MODULE__, :in_flight)

  defp accepting? do
    case :ets.lookup(:drain_gate, :accepting) do
      [{:accepting, true}] -> true
      _ -> false
    end
  end

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    # Without trap_exit, terminate/2 is NOT called on :shutdown from supervisor.
    Process.flag(:trap_exit, true)
    {:ok, %{in_flight: 0, buffer: []}}
  end

  @impl true
  def handle_call({:handle, req}, from, state) do
    # Simulate async work: spawn a task, reply when done.
    parent = self()
    ref = make_ref()

    Task.start(fn ->
      Process.sleep(100)
      send(parent, {:work_done, ref, from, {:ok, {:handled, req}}})
    end)

    {:noreply, %{state | in_flight: state.in_flight + 1}}
  end

  def handle_call(:in_flight, _from, state), do: {:reply, state.in_flight, state}

  @impl true
  def handle_info({:work_done, _ref, from, reply}, state) do
    GenServer.reply(from, reply)
    {:noreply, %{state | in_flight: state.in_flight - 1, buffer: [reply | state.buffer]}}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info("[drain] starting, reason=#{inspect(reason)}, in_flight=#{state.in_flight}")

    # Stage 1: stop accepting new requests.
    :ets.insert(:drain_gate, {:accepting, false})

    # Stage 2: wait for in-flight work to complete.
    final_state = wait_for_drain(state, System.monotonic_time(:millisecond) + @drain_deadline_ms)

    # Stage 3: flush buffer.
    flushed = flush_buffer(final_state.buffer)
    Logger.info("[drain] flushed #{flushed} buffered replies")

    # Stage 4: return :ok so Supervisor logs :shutdown cleanly.
    :ok
  end

  # ---------------------------------------------------------------------------
  # Drain loop — process messages ourselves while draining.
  # ---------------------------------------------------------------------------

  defp wait_for_drain(%{in_flight: 0} = state, _deadline), do: state

  defp wait_for_drain(state, deadline) do
    now = System.monotonic_time(:millisecond)

    if now >= deadline do
      Logger.warning("[drain] deadline exceeded with #{state.in_flight} in flight")
      state
    else
      remaining = deadline - now

      receive do
        {:work_done, _ref, from, reply} ->
          GenServer.reply(from, reply)
          new_state = %{state | in_flight: state.in_flight - 1, buffer: [reply | state.buffer]}
          wait_for_drain(new_state, deadline)
      after
        remaining -> state
      end
    end
  end

  defp flush_buffer(buffer), do: length(buffer)
end
```

### Step 3: Tests

```elixir
# test/drain_shutdown/drain_test.exs
defmodule DrainShutdown.DrainTest do
  use ExUnit.Case, async: false

  alias DrainShutdown.Server

  setup do
    :ets.insert(:drain_gate, {:accepting, true})
    :ok
  end

  test "accepts requests under normal conditions" do
    assert {:ok, {:handled, :ping}} = Server.handle(:ping)
  end

  test "terminate drains in-flight work before returning" do
    # Kick off 5 in-flight requests.
    tasks = for i <- 1..5, do: Task.async(fn -> Server.handle({:req, i}) end)
    Process.sleep(20)

    # Manually invoke terminate under controlled conditions.
    pid = Process.whereis(Server)
    ref = Process.monitor(pid)

    # Send a :shutdown exit like the supervisor would.
    Process.exit(pid, :shutdown)

    assert_receive {:DOWN, ^ref, :process, ^pid, :shutdown}, 15_000

    # All in-flight tasks should have received a reply (not a timeout/exit).
    results = Task.await_many(tasks, 15_000)
    assert Enum.all?(results, &match?({:ok, {:handled, _}}, &1))
  end

  test "rejects new work after gate flips" do
    :ets.insert(:drain_gate, {:accepting, false})
    assert {:error, :draining} = Server.handle(:new_req)
  end
end
```

---

## Trade-offs and production gotchas

**1. `trap_exit` + slow `terminate/2` + short `shutdown:` = brutal kill.** The supervisor enforces `shutdown:`. If your drain takes 20 s but `shutdown: 5_000`, at 5 s the supervisor sends `:kill` and `terminate/2` is interrupted mid-flush. Always: `shutdown:` ≥ drain budget + 2s safety.

**2. `terminate/2` runs in the GenServer's process — it still receives messages.** Casts and calls keep arriving during drain. The `receive` loop above only handles `:work_done`; other messages pile up in the mailbox. Either selectively receive (as shown) or explicitly drain the mailbox.

**3. The gate must be accessible without going through the GenServer.** If `accepting?/0` calls `GenServer.call(Server, :accepting?)`, and the GenServer is busy draining, new callers block on the call. ETS or `:persistent_term` with O(1) read is the correct primitive.

**4. `:persistent_term.put/2` is NOT fast on hot paths.** Each put triggers a global GC scan. Use it ONCE at shutdown, not per-request. ETS is the better fit for flipping state during normal operation.

**5. K8s `preStop` hook races with `SIGTERM`.** K8s sends `preStop` THEN `SIGTERM` simultaneously. If your app starts draining on `SIGTERM` but your Service endpoint still routes traffic for 2 s (endpoint propagation delay), your "unavailable" window accepts traffic. Solution: `preStop` sleep(3s) + separate readiness probe that fails as soon as drain starts.

**6. `terminate/2` does NOT run on `:brutal_kill`, VM crash, or `Process.exit(pid, :kill)`.** It is a best-effort hook. Durability invariants belong in the DB, not `terminate/2`. Idempotent writes + DB transactions are the real guarantee.

**7. Monitored callers see `:noproc` not `:draining`.** If a request is in `GenServer.call` when the GenServer dies, the caller gets `{:noproc, _}` or `:timeout`. Wrap all calls with a try/catch or make them idempotent at the client layer.

**8. When NOT to use this.** For stateless workers whose loss is harmless (pure in-memory caches, telemetry aggregators that re-read on startup), `:brutal_kill` is simpler and faster. Drain is for processes with external side effects or user-visible replies.

---

## Performance notes

`Process.flag(:trap_exit, true)` has no measurable runtime cost — it's a single bit on the PCB. The drain itself is bounded by the slowest in-flight job; measure it with `:timer.tc/1` wrapped around the supervisor's terminate call.

ETS lookups for the gate are ~50 ns. `:persistent_term.get/1` is ~20 ns but with global GC cost on writes — the trade-off favors ETS for frequently-flipped flags.

---

## Resources

- [`Process.flag(:trap_exit, true)` — hexdocs](https://hexdocs.pm/elixir/Process.html#flag/2) — the EXIT interception semantics.
- [`GenServer.terminate/2` — hexdocs](https://hexdocs.pm/elixir/GenServer.html#c:terminate/2) — when it runs and when it doesn't.
- [OTP Design Principles — sys and proc_lib](https://www.erlang.org/doc/design_principles/spec_proc.html) — under-the-hood shutdown protocol.
- [Fred Hébert — Handling Overload](https://ferd.ca/handling-overload.html) — load shedding patterns that pair with drain.
- [K8s pod termination lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination) — how `preStop`, `SIGTERM`, `terminationGracePeriodSeconds` interact.
- [Phoenix.Endpoint draining (ranch)](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/endpoint/cowboy2_adapter.ex) — real HTTP drain with ranch's protocol.
- [Plug.Cowboy drain](https://hexdocs.pm/plug_cowboy/Plug.Cowboy.html#module-options) — production drain hooks.
