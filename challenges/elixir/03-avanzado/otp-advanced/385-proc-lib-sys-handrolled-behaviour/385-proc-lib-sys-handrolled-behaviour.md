# Hand-Rolled Behaviour with `:proc_lib` and `:sys`

**Project**: `tiny_server` — a special process implemented from scratch with `:proc_lib`, `:sys`, and a custom receive loop, so you can handle `:sys` debug commands, system messages, and supervision-tree hand-off without wrapping `GenServer`.

## Project context

Every GenServer you write is built on top of `:proc_lib` + `:sys` + a hand-written receive loop. The OTP team wrote those abstractions once, and now 99% of us just `use GenServer`. But `GenServer` makes opinionated choices: every call is a synchronous request-reply, every cast is fire-and-forget, every handler gets the full mailbox in order, and you can't bypass the dispatcher to peek at messages first.

Sometimes those opinions are wrong. A network-protocol server may want to receive TCP packets *and* system messages in the same `receive`; a scheduler may want to prioritise high-priority messages over pending calls; a metrics daemon may want to compact its mailbox before processing. When you need the control `GenServer` hides, you drop down to `:proc_lib` and hand-roll the behaviour.

This exercise builds a `TinyServer` — a minimal behaviour that accepts `call`, `cast`, handles `:sys` debug traces and state inspection, links properly into a supervisor, and survives `:code_change` upgrades. The point is to see what `GenServer` is made of.

```
tiny_server/
├── lib/
│   ├── tiny_server.ex              # the behaviour + loop
│   └── counter.ex                  # example callback module
├── test/
│   ├── tiny_server_test.exs
│   └── sys_integration_test.exs
├── bench/
│   └── loop_overhead_bench.exs
└── mix.exs
```

## Why `:proc_lib` and not `spawn_link`

A plain `spawn_link(fun)` process is invisible to supervisors (no synchronous init ack), cannot carry OTP ancestors or initial-call metadata (no crash reports with file:line), and is not recognised by `:sys` debug tools. `:proc_lib.start_link/3` returns only after the child confirms init with `:proc_lib.init_ack/1`; the supervisor sees startup errors synchronously, and the process shows up in `:sys` tooling, `:observer`, and `:recon`.

## Why `:sys` callbacks

`:sys` is the OTP standard for debugging and introspecting *any* OTP-compliant process: `:sys.get_state/1`, `:sys.trace/2`, `:sys.log/2`, `:sys.suspend/1`, `:sys.resume/1`, `:sys.replace_state/2`, `:sys.change_code/4`. Every GenServer supports them because `:gen_server` invokes `:sys.handle_system_msg/6` when it receives a `{:system, from, msg}` tuple. If you want your hand-rolled process to work with the same operational tools (observer, remote shell debugging, live state replacement), you must implement those callbacks yourself.

## Core concepts

### 1. `:proc_lib.start_link/3`
Spawns a child under OTP conventions. Returns `{:ok, pid}` only after the child's init function calls `:proc_lib.init_ack/1`. Failure before the ack becomes `{:error, reason}` to the caller.

### 2. `:sys.handle_system_msg/6`
The one-stop entry point for system messages (`{:system, from, msg}`). It dispatches to your `system_*` callbacks (`system_continue`, `system_terminate`, `system_get_state`, `system_replace_state`, `system_code_change`).

### 3. Debug actions
`:sys.handle_debug/4` allows `:sys.trace(pid, true)` to trace every in/out message.

### 4. Parent linking
You must store the parent pid at init and pass it to `:sys.handle_system_msg/6` so `:sys.suspend/resume` work correctly.

### 5. `:code_change`
On hot upgrade, `:sys` sends `{:system, from, {:change_code, mod, vsn, extra}}`. You must respond or the upgrade deadlocks.

## Design decisions

- **Option A — callback module pattern like GenServer**: user supplies `init/1`, `handle_call/3`, `handle_cast/2`.
- **Option B — single `handle/2` for all messages**: simpler, no dispatch.

→ A. The whole point is to mirror what GenServer hides; a callback module pattern is what "OTP-compliant" means in practice.

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]
```

### Step 1: The behaviour

**Objective**: Hand-roll OTP behaviour via :proc_lib + :sys to expose how GenServer achieves init_ack, system messages, and debug hooks.

```elixir
defmodule TinyServer do
  @moduledoc """
  Hand-rolled OTP-compliant server. Mirrors the minimal GenServer contract
  but is written entirely with :proc_lib + :sys + receive loop so you can
  see how GenServer is built.
  """

  @type from :: {pid(), reference()}

  @callback init(term()) :: {:ok, state :: term()} | {:stop, reason :: term()}
  @callback handle_call(request :: term(), from, state :: term()) ::
              {:reply, term(), term()} | {:stop, term(), term(), term()}
  @callback handle_cast(msg :: term(), state :: term()) ::
              {:noreply, term()} | {:stop, term(), term()}
  @callback handle_info(msg :: term(), state :: term()) ::
              {:noreply, term()} | {:stop, term(), term()}
  @callback terminate(reason :: term(), state :: term()) :: any()
  @callback code_change(term(), term(), term()) :: {:ok, term()}

  @optional_callbacks handle_info: 2, terminate: 2, code_change: 3

  # --- client API ---

  def start_link(mod, arg, opts \\ []) do
    :proc_lib.start_link(__MODULE__, :init_it, [self(), mod, arg, opts])
  end

  def call(server, request, timeout \\ 5_000) do
    ref = Process.monitor(server)
    send(server, {:"$call", {self(), ref}, request})

    receive do
      {^ref, reply} ->
        Process.demonitor(ref, [:flush])
        reply

      {:DOWN, ^ref, :process, _, reason} ->
        exit({reason, {__MODULE__, :call, [server, request]}})
    after
      timeout ->
        Process.demonitor(ref, [:flush])
        exit(:timeout)
    end
  end

  def cast(server, msg), do: send(server, {:"$cast", msg})

  # --- :proc_lib init entry point ---

  def init_it(parent, mod, arg, opts) do
    Process.flag(:trap_exit, true)

    case name_opt(opts) do
      {:ok, name} -> Process.register(self(), name)
      :none -> :ok
      {:error, reason} -> exit(reason)
    end

    case mod.init(arg) do
      {:ok, state} ->
        :proc_lib.init_ack(parent, {:ok, self()})
        loop(parent, mod, state, :sys.debug_options([]))

      {:stop, reason} ->
        :proc_lib.init_ack(parent, {:error, reason})
        exit(reason)
    end
  end

  defp name_opt(opts) do
    case Keyword.get(opts, :name) do
      nil -> :none
      atom when is_atom(atom) -> {:ok, atom}
      other -> {:error, {:bad_name, other}}
    end
  end

  # --- main loop ---

  defp loop(parent, mod, state, debug) do
    receive do
      {:system, from, request} ->
        :sys.handle_system_msg(request, from, parent, __MODULE__, debug, {mod, state})

      {:EXIT, ^parent, reason} ->
        terminate(reason, mod, state, debug)

      {:"$call", {pid, ref} = from, request} ->
        debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:in, request, pid})

        case mod.handle_call(request, from, state) do
          {:reply, reply, new_state} ->
            send(pid, {ref, reply})
            debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:out, reply, pid})
            loop(parent, mod, new_state, debug)

          {:stop, reason, reply, new_state} ->
            send(pid, {ref, reply})
            terminate(reason, mod, new_state, debug)
        end

      {:"$cast", msg} ->
        handle_noreply(mod.handle_cast(msg, state), parent, mod, debug)

      msg ->
        if function_exported?(mod, :handle_info, 2) do
          handle_noreply(mod.handle_info(msg, state), parent, mod, debug)
        else
          loop(parent, mod, state, debug)
        end
    end
  end

  defp handle_noreply({:noreply, new_state}, parent, mod, debug),
    do: loop(parent, mod, new_state, debug)

  defp handle_noreply({:stop, reason, new_state}, _parent, mod, debug),
    do: terminate(reason, mod, new_state, debug)

  defp write_debug(dev, event, name) do
    IO.puts(dev, "~p event = ~p~n" |> :io_lib.format([name, event]) |> IO.iodata_to_binary())
  end

  # --- :sys required callbacks ---

  def system_continue(parent, debug, {mod, state}), do: loop(parent, mod, state, debug)

  def system_terminate(reason, _parent, debug, {mod, state}),
    do: terminate(reason, mod, state, debug)

  def system_get_state({_mod, state}), do: {:ok, state}

  def system_replace_state(fun, {mod, state}) do
    new_state = fun.(state)
    {:ok, new_state, {mod, new_state}}
  end

  def system_code_change({mod, state}, _module, old_vsn, extra) do
    case function_exported?(mod, :code_change, 3) do
      true ->
        case mod.code_change(old_vsn, state, extra) do
          {:ok, new_state} -> {:ok, {mod, new_state}}
          other -> other
        end

      false ->
        {:ok, {mod, state}}
    end
  end

  defp terminate(reason, mod, state, _debug) do
    if function_exported?(mod, :terminate, 2), do: mod.terminate(reason, state)
    exit(reason)
  end
end
```

### Step 2: Example callback module

**Objective**: Implement Counter using TinyServer to verify callback pattern mirrors GenServer compatibility.

```elixir
defmodule Counter do
  @behaviour TinyServer

  def start_link(initial \\ 0, opts \\ []),
    do: TinyServer.start_link(__MODULE__, initial, opts)

  def increment(pid, by \\ 1), do: TinyServer.cast(pid, {:inc, by})
  def read(pid), do: TinyServer.call(pid, :read)
  def reset(pid), do: TinyServer.call(pid, :reset)

  @impl true
  def init(initial), do: {:ok, initial}

  @impl true
  def handle_call(:read, _from, state), do: {:reply, state, state}
  def handle_call(:reset, _from, _state), do: {:reply, :ok, 0}

  @impl true
  def handle_cast({:inc, n}, state), do: {:noreply, state + n}

  @impl true
  def handle_info(_msg, state), do: {:noreply, state}

  @impl true
  def terminate(_reason, _state), do: :ok

  @impl true
  def code_change(_old, state, _extra), do: {:ok, state}
end
```

## Message flow

```
         caller                   TinyServer process
            │                            │
  call ─────┼──▶ send({:"$call",        │  receive
            │         {self,ref}, req}) │  │
            │                            │  pattern-match {:"$call", ...}
            │                            │  mod.handle_call/3
            │ ◀── send({ref, reply}) ────│
            │                            │  loop(new_state)
            │                            │
sys:trace──▶ send({:system, from,        │  receive
            │         get_state})        │  pattern-match {:system, ...}
            │                            │  :sys.handle_system_msg/6
            │ ◀── {pid, {:ok, state}} ───│  → system_get_state
```

## Tests

```elixir
defmodule TinyServerTest do
  use ExUnit.Case, async: true

  describe "call/cast basic" do
    test "cast increments, call reads state" do
      {:ok, pid} = Counter.start_link(0)
      Counter.increment(pid, 3)
      Counter.increment(pid, 4)
      assert Counter.read(pid) == 7
    end

    test "call timeout surfaces as exit" do
      {:ok, pid} = Counter.start_link(0)
      assert catch_exit(TinyServer.call(pid, :no_such_request, 50))
    end
  end

  describe "named process" do
    test "start_link with :name registers" do
      {:ok, _pid} = Counter.start_link(0, name: :counter_named)
      Counter.increment(:counter_named, 10)
      assert Counter.read(:counter_named) == 10
    end
  end
end
```

```elixir
defmodule SysIntegrationTest do
  use ExUnit.Case, async: true

  describe ":sys operations" do
    test "get_state returns current state" do
      {:ok, pid} = Counter.start_link(42)
      assert :sys.get_state(pid) == 42
    end

    test "replace_state mutates via fun" do
      {:ok, pid} = Counter.start_link(10)
      :sys.replace_state(pid, fn s -> s * 2 end)
      assert Counter.read(pid) == 20
    end

    test "suspend / resume stop processing temporarily" do
      {:ok, pid} = Counter.start_link(0)
      :sys.suspend(pid)

      Counter.increment(pid, 5)
      # Message sits in mailbox because loop is suspended.
      # We do not assert timeout to avoid flakes; just resume and verify.
      :sys.resume(pid)
      Process.sleep(10)
      assert Counter.read(pid) == 5
    end
  end
end
```

## Benchmark

```elixir
# bench/loop_overhead_bench.exs
{:ok, tiny} = Counter.start_link(0)

{:ok, gs} =
  GenServer.start_link(
    {:via, :gproc, {:n, :l, :baseline}} |> elem(0) == :via && __MODULE__ ||
      # fallback: a minimal GenServer equivalent
      :gen_server_baseline,
    :ok
  )

# Simpler baseline — define a trivial GenServer inline for fairness.
defmodule BaselineGS do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, 0)
  @impl true
  def init(x), do: {:ok, x}
  @impl true
  def handle_call(:read, _f, s), do: {:reply, s, s}
  @impl true
  def handle_cast({:inc, n}, s), do: {:noreply, s + n}
end

{:ok, gs} = BaselineGS.start_link([])

Benchee.run(
  %{
    "TinyServer call"    => fn -> Counter.read(tiny) end,
    "GenServer call"     => fn -> GenServer.call(gs, :read) end,
    "TinyServer cast"    => fn -> Counter.increment(tiny, 1) end,
    "GenServer cast"     => fn -> GenServer.cast(gs, {:inc, 1}) end
  },
  time: 5,
  warmup: 2
)
```

Expected: TinyServer within 10% of GenServer (GenServer is implemented with the same primitives plus slightly more robust error handling). If TinyServer is *much* faster, check — you are probably missing something `:gen_server` does (e.g. `:sys.handle_debug` on every event).

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Forgetting `:proc_lib.init_ack/1`**
If init does not call `init_ack`, `:proc_lib.start_link/3` waits forever. The caller times out. The child is orphaned.

**2. Not handling `{:system, ...}` tuples**
If the loop receives a `:system` message and treats it like a regular info message, `:sys.get_state/1` hangs. Every OTP-compliant process must pattern-match `{:system, from, req}` before anything else.

**3. Not storing parent pid**
`:sys.suspend/resume` needs the parent. If you lose it across a transition, suspend breaks.

**4. `trap_exit` default**
We enable `trap_exit` so the process cleanly handles its supervisor shutdown. Without it, the parent crash simply kills the child via link, and `terminate` never runs.

**5. `:code_change` callback required for hot upgrades**
On release upgrade, `:sys.handle_system_msg/6` dispatches `{:change_code, ...}` to `system_code_change/4`. If that callback is absent or crashes, the whole release upgrade aborts.

**6. When NOT to roll your own**
For 99% of cases, `use GenServer`. You roll your own only when you need custom dispatch (priority mailboxes, selective receive, mailbox compaction) or when you are implementing a new behaviour (like `:gen_statem`).

## Reflection

OTP's `:gen_server` implements roughly this same loop with more polish (format_status, timeouts, distribution). Find one feature of `GenServer` you relied on recently (e.g. `:timeout` return value, `continue` callback) and sketch how you would add it to `TinyServer`. What does that tell you about the hidden cost of each feature?

## Resources

- [`:proc_lib` reference](https://www.erlang.org/doc/man/proc_lib.html)
- [`:sys` reference](https://www.erlang.org/doc/man/sys.html)
- [OTP Design Principles — "Special Processes"](https://www.erlang.org/doc/design_principles/spec_proc.html) — the official spec for what we built
- [`:gen_server` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/gen_server.erl)
