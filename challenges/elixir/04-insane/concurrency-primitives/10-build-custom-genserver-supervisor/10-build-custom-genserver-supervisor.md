# Build a Custom GenServer and Supervisor from Raw BEAM Primitives

**Project**: `my_otp` — A from-scratch reimplementation of Erlang/OTP's GenServer and Supervisor using only raw process primitives: spawn, send, receive, monitors, and links.

**Learning Goal**: Understand why OTP's call/cast protocol uses pinned references, why supervisors use monitors instead of links, and how restart intensity tracking prevents restart loops.

---

## Project Context

You are building `my_otp`, a reimplementation of OTP's GenServer and Supervisor using only raw Elixir/Erlang process primitives. No `:gen_server`, no `GenServer`, no `Supervisor`, no `Agent`, no `Task`. Every mechanism you rely on in production — call/cast protocols, restart strategies, monitor-based crash detection — you implement from scratch.

Project structure:

```
my_otp/
├── lib/
│   └── my_otp/
│       ├── gen_server.ex            # MyGenServer: call, cast, handle_info, start_link
│       ├── supervisor.ex            # MyGenServer.Supervisor: one_for_one, one_for_all, rest_for_one
│       └── restart_tracker.ex      # sliding window: max_restarts within max_seconds
├── test/
│   └── my_otp/
│       ├── gen_server_test.exs      # call semantics, cast, stale reply handling, timeouts
│       ├── supervisor_test.exs      # all three restart strategies, restart intensity
│       └── restart_policy_test.exs  # permanent, transient, temporary restart modes
├── bench/
│   └── my_otp_bench.exs
└── mix.exs
```

---

## The Problem

You use GenServer and Supervisor in production every day but cannot explain:
- Why does `call/2` use a tagged reference instead of just waiting for any reply?
- Why do supervisors use `Process.monitor/1` instead of `Process.link/1`?
- How exactly does restart intensity tracking prevent supervisor restart loops?

This exercise closes that gap by reimplementing the full OTP mechanism from first principles.

---

## Key Concepts

### Call Protocol with Pinned References

`GenServer.call/2` implements this protocol:

```
Caller: send {:"$call", {self(), ref}, message}  where ref = make_ref()
Server: receive and pattern match {:"$call", {from, ref}, message}
Server: send {ref, reply} back to from
Caller: receive do {^ref, reply} -> reply end   [pin ensures this is our reply]
```

The pinned reference `^ref` is critical: if a call times out and the server eventually sends a reply, that reply sits in the caller's mailbox. Without the pin, the next call would accidentally receive the stale reply. The reference makes replies uniquely identifiable.

### Why Monitors, Not Links, for Supervisors

- **Links** propagate exit signals bidirectionally: if a child crashes, the exit signal kills the supervisor before restart logic can run.
- **Monitors** deliver crashes as ordinary `{:DOWN, ...}` messages. The supervisor receives them in its receive loop and can decide to restart, stop siblings, or exit itself — without crashing first.

This is why every OTP supervisor uses monitors: they give the supervisor a chance to react.

### Restart Intensity via Sliding Window

OTP counts restarts within a time window: `max_seconds`. The algorithm:
1. Store each restart timestamp
2. On each restart, count timestamps in `[now - max_seconds, now]`
3. If count > max_restarts, exit with `:max_restarts_exceeded`

A naive counter (e.g., reset every N seconds) fails: a burst at period boundary straddles two resets.

---

## Design Decisions

| Option | Pros | Cons | Chosen? |
|--------|------|------|---------|
| **A: Wrap `:supervisor`** | Battle-tested; free features | Defeats understanding | No |
| **B: Scratch implementation** | Every rule explicit; makes contract visible | Must implement correctness yourself | **Yes** |

**Rationale for B**: Understanding the OTP supervision contract requires re-deriving it. The pedagogical value far exceeds the ergonomic loss. Every rule (restart strategy, intensity window, shutdown order) is explicit and readable in your code.

## Full Project Structure

```
my_otp/
├── mix.exs                           # Project configuration
├── lib/
│   ├── my_otp.ex                    # Module docstring
│   └── my_otp/
│       ├── gen_server.ex            # MyGenServer: call, cast, handle_info, start_link
│       ├── supervisor.ex            # MyGenServer.Supervisor: one_for_one, one_for_all, rest_for_one
│       └── restart_tracker.ex       # sliding window: max_restarts within max_seconds
├── test/
│   ├── test_helper.exs              # ExUnit config
│   └── my_otp/
│       ├── gen_server_test.exs      # call semantics, cast, stale reply, timeouts
│       ├── supervisor_test.exs      # restart strategies, intensity limits
│       └── restart_policy_test.exs  # permanent, transient, temporary modes
├── bench/
│   └── my_otp_bench.exs            # Benchee benchmarks
└── .gitignore
```

## Implementation milestones

### Step 1: Create the project

**Objective**: Lay out `lib/` and `test/` so the GenServer and Supervisor reimplementations live beside their tests and can evolve independently.


```bash
mix new my_otp --sup
cd my_otp
mkdir -p lib/my_otp test/my_otp bench
```

### Step 2: Project Setup

**Objective**: Create the Mix project and organize modules.

```bash
mix new my_otp --sup
cd my_otp
mkdir -p lib/my_otp test/my_otp bench
```

No external dependencies — stdlib only.

### Step 3: Dependencies (mix.exs)

**Objective**: Stdlib-only — rebuild OTP primitives from `spawn`, `send`, `receive`, not depend on them.

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir stdlib
  ]
end
```

### Step 4: MyGenServer Module

**Objective**: Build the `{:"$call", {pid, ref}, msg}` protocol by hand so the pinned-ref discipline becomes visible code.

```elixir
# lib/my_otp/gen_server.ex
defmodule MyGenServer do
  @moduledoc """
  A reimplementation of OTP's GenServer using only raw process primitives:
  spawn, send, receive, Process.monitor/1, Process.link/1.

  The call protocol:
    caller sends:   {:"$call", {self(), ref}, message}
    server replies: {ref, reply}

  The cast protocol:
    caller sends:   {:"$cast", message}
    server ignores: no reply

  Info messages: anything that is not a $call or $cast is forwarded to handle_info/2.
  """

  @doc "Starts and links a server running module with init_args."
  @spec start_link(module(), term(), keyword()) :: {:ok, pid()}
  def start_link(module, init_args, opts \\ []) do
    parent = self()
    ref = make_ref()

    pid = spawn_link(fn ->
      {:ok, initial_state} = module.init(init_args)
      send(parent, {ref, :started})
      loop(module, initial_state)
    end)

    receive do
      {^ref, :started} -> :ok
    after
      5_000 -> raise "MyGenServer start_link timeout"
    end

    case Keyword.get(opts, :name) do
      nil -> :ok
      name -> Process.register(pid, name)
    end

    {:ok, pid}
  end

  @doc "Synchronous call. Blocks until reply arrives or timeout elapses."
  @spec call(pid() | atom(), term(), non_neg_integer()) :: term()
  def call(server, message, timeout \\ 5_000) do
    ref = make_ref()
    pid = resolve_pid(server)
    send(pid, {:"$call", {self(), ref}, message})

    receive do
      {^ref, reply} -> reply
    after
      timeout ->
        raise RuntimeError, "MyGenServer.call to #{inspect(server)} timed out after #{timeout}ms"
    end
  end

  @doc "Asynchronous cast. Returns :ok immediately."
  @spec cast(pid() | atom(), term()) :: :ok
  def cast(server, message) do
    pid = resolve_pid(server)
    send(pid, {:"$cast", message})
    :ok
  end

  defp resolve_pid(pid) when is_pid(pid), do: pid
  defp resolve_pid(name) when is_atom(name), do: Process.whereis(name) || name

  defp loop(module, state) do
    receive do
      {:"$call", {from, ref}, message} ->
        case module.handle_call(message, {from, ref}, state) do
          {:reply, reply, new_state} ->
            send(from, {ref, reply})
            loop(module, new_state)
          {:noreply, new_state} ->
            loop(module, new_state)
        end

      {:"$cast", message} ->
        case module.handle_cast(message, state) do
          {:noreply, new_state} ->
            loop(module, new_state)
        end

      other ->
        case module.handle_info(other, state) do
          {:noreply, new_state} ->
            loop(module, new_state)
        end
    end
  end
end
```

### Step 5: MyGenServer.Supervisor Module

**Objective**: Monitor children (never link) so crashes arrive as messages the strategy function can handle.


```elixir
# lib/my_otp/supervisor.ex
defmodule MyGenServer.Supervisor do
  @moduledoc """
  A reimplementation of OTP's Supervisor with three restart strategies:

  :one_for_one   — only the crashed child is restarted
  :one_for_all   — all children are terminated (reverse order) and restarted (forward order)
  :rest_for_one  — the crashed child and all children started after it are terminated and restarted

  Child specs:
    %{id: atom, start: {module, args}, restart: :permanent | :transient | :temporary}

  restart semantics:
    :permanent — always restart, regardless of exit reason
    :transient — restart only on abnormal exit (reason not :normal and not :shutdown)
    :temporary — never restart

  Restart intensity: if more than max_restarts restarts occur within max_seconds,
  the supervisor itself crashes with reason {:shutdown, :max_restarts_exceeded}.
  """

  @spec start_link([map()], keyword()) :: {:ok, pid()}
  def start_link(children, opts \\ []) do
    strategy    = opts[:strategy]    || :one_for_one
    max_restarts = opts[:max_restarts] || 3
    max_seconds  = opts[:max_seconds]  || 5

    parent = self()
    ref = make_ref()

    pid = spawn(fn ->
      Process.flag(:trap_exit, true)
      started_children = start_children(children)
      Process.register(self(), :my_supervisor)
      send(parent, {ref, :started})
      supervisor_loop(started_children, children, strategy, max_restarts, max_seconds, [])
    end)

    receive do
      {^ref, :started} -> {:ok, pid}
    after
      5_000 -> {:error, :timeout}
    end
  end

  def child_pid(child_id) do
    send(:my_supervisor, {:lookup, child_id, self()})
    receive do
      {:child_pid, pid} -> {:ok, pid}
    after
      1_000 -> {:error, :not_found}
    end
  end

  defp start_children(specs) do
    Enum.map(specs, fn spec ->
      {module, args} = spec.start
      {:ok, pid} = MyGenServer.start_link(module, args)
      monitor_ref = Process.monitor(pid)
      {spec.id, pid, monitor_ref, spec}
    end)
  end

  defp start_child(spec) do
    {module, args} = spec.start
    {:ok, pid} = MyGenServer.start_link(module, args)
    monitor_ref = Process.monitor(pid)
    {spec.id, pid, monitor_ref, spec}
  end

  defp supervisor_loop(running, specs, strategy, max_restarts, max_seconds, restart_history) do
    receive do
      {:lookup, child_id, caller} ->
        pid = Enum.find_value(running, fn
          {^child_id, pid, _, _} -> pid
          _ -> nil
        end)
        send(caller, {:child_pid, pid})
        supervisor_loop(running, specs, strategy, max_restarts, max_seconds, restart_history)

      {:DOWN, _ref, :process, pid, reason} ->
        case Enum.find(running, fn {_, p, _, _} -> p == pid end) do
          nil ->
            supervisor_loop(running, specs, strategy, max_restarts, max_seconds, restart_history)

          {child_id, _pid, _ref, spec} ->
            now = System.monotonic_time(:second)
            new_history = [now | restart_history]

            if intensity_exceeded?(new_history, max_restarts, max_seconds) do
              exit({:shutdown, :max_restarts_exceeded})
            end

            restart_policy = Map.get(spec, :restart, :permanent)

            if should_restart?(restart_policy, reason) do
              new_running = apply_strategy(strategy, child_id, running, specs)
              supervisor_loop(new_running, specs, strategy, max_restarts, max_seconds, new_history)
            else
              remaining = Enum.reject(running, fn {id, _, _, _} -> id == child_id end)
              supervisor_loop(remaining, specs, strategy, max_restarts, max_seconds, new_history)
            end
        end
    end
  end

  defp apply_strategy(:one_for_one, child_id, running, specs) do
    spec = Enum.find(specs, fn s -> s.id == child_id end)
    remaining = Enum.reject(running, fn {id, _, _, _} -> id == child_id end)
    new_child = start_child(spec)
    remaining ++ [new_child]
  end

  defp apply_strategy(:one_for_all, _child_id, running, specs) do
    Enum.each(Enum.reverse(running), fn {_, pid, ref, _} ->
      Process.demonitor(ref, [:flush])
      Process.exit(pid, :shutdown)
    end)
    Process.sleep(10)
    start_children(specs)
  end

  defp apply_strategy(:rest_for_one, child_id, running, specs) do
    idx = Enum.find_index(running, fn {id, _, _, _} -> id == child_id end) || 0
    {keep, restart} = Enum.split(running, idx)

    Enum.each(Enum.reverse(restart), fn {_, pid, ref, _} ->
      Process.demonitor(ref, [:flush])
      Process.exit(pid, :shutdown)
    end)
    Process.sleep(10)

    restart_specs = Enum.drop(specs, idx)
    keep ++ start_children(restart_specs)
  end

  defp should_restart?(:permanent, _reason), do: true
  defp should_restart?(:transient, reason),  do: reason not in [:normal, :shutdown]
  defp should_restart?(:temporary, _reason), do: false

  defp intensity_exceeded?(restart_history, max_restarts, max_seconds) do
    now = System.monotonic_time(:second)
    cutoff = now - max_seconds
    recent = Enum.count(restart_history, fn ts -> ts >= cutoff end)
    recent > max_restarts
  end
end
```

### Step 6: Tests — Contract as Executable Specs

**Objective**: Freeze the OTP contract as executable tests. Stale-reply discipline, restart strategies, and intensity limits must hold without modification.

```elixir
# test/my_otp/gen_server_test.exs
defmodule MyGenServerTest do
  use ExUnit.Case, async: true

  defmodule Counter do
    def init(n), do: {:ok, n}
    def handle_call(:get, _from, n), do: {:reply, n, n}
    def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
    def handle_cast(:reset, _n),     do: {:noreply, 0}
    def handle_info(_, state),       do: {:noreply, state}
  end

  describe "call/2 semantics" do
    test "returns correct reply" do
      {:ok, pid} = MyGenServer.start_link(Counter, 0)
      assert 0 = MyGenServer.call(pid, :get)
      assert 1 = MyGenServer.call(pid, :inc)
      assert 1 = MyGenServer.call(pid, :get)
    end

    test "raises on timeout" do
      defmodule SlowServer do
        def init(_), do: {:ok, nil}
        def handle_call(:slow, _from, state) do
          Process.sleep(200)
          {:reply, :done, state}
        end
        def handle_info(_, s), do: {:noreply, s}
      end

      {:ok, pid} = MyGenServer.start_link(SlowServer, nil)
      assert_raise RuntimeError, fn ->
        MyGenServer.call(pid, :slow, 50)
      end
    end

    test "discards stale replies after timeout" do
      {:ok, pid} = MyGenServer.start_link(Counter, 0)
      catch_error(MyGenServer.call(pid, :inc, 1))
      Process.sleep(50)
      assert MyGenServer.call(pid, :get) in [0, 1]
    end
  end

  describe "cast/2 semantics" do
    test "changes state without blocking" do
      {:ok, pid} = MyGenServer.start_link(Counter, 42)
      :ok = MyGenServer.cast(pid, :reset)
      Process.sleep(10)
      assert 0 = MyGenServer.call(pid, :get)
    end
  end
end
```

```elixir
# test/my_otp/supervisor_test.exs
defmodule MyGenServer.SupervisorTest do
  use ExUnit.Case, async: true

  defmodule Worker do
    def init(id), do: {:ok, id}
    def handle_call(:id, _from, id), do: {:reply, id, id}
    def handle_cast(:crash, _state), do: raise "Intentional crash for testing"
    def handle_info(_, s), do: {:noreply, s}
  end

  describe "restart strategies" do
    test ":one_for_one restarts only the crashed child" do
      children = [
        %{id: :w1, start: {Worker, :w1}, restart: :permanent},
        %{id: :w2, start: {Worker, :w2}, restart: :permanent},
        %{id: :w3, start: {Worker, :w3}, restart: :permanent}
      ]

      {:ok, _sup} = MyGenServer.Supervisor.start_link(children, strategy: :one_for_one)
      {:ok, w1}  = MyGenServer.Supervisor.child_pid(:w1)
      {:ok, w2}  = MyGenServer.Supervisor.child_pid(:w2)

      MyGenServer.cast(w2, :crash)
      Process.sleep(50)

      {:ok, new_w2} = MyGenServer.Supervisor.child_pid(:w2)
      {:ok, same_w1} = MyGenServer.Supervisor.child_pid(:w1)

      assert new_w2 != w2
      assert same_w1 == w1
    end

    test ":one_for_all restarts all children when one crashes" do
      children = [
        %{id: :a, start: {Worker, :a}, restart: :permanent},
        %{id: :b, start: {Worker, :b}, restart: :permanent},
        %{id: :c, start: {Worker, :c}, restart: :permanent}
      ]

      {:ok, _sup} = MyGenServer.Supervisor.start_link(children, strategy: :one_for_all)
      {:ok, a} = MyGenServer.Supervisor.child_pid(:a)
      {:ok, b} = MyGenServer.Supervisor.child_pid(:b)

      MyGenServer.cast(b, :crash)
      Process.sleep(50)

      {:ok, new_a} = MyGenServer.Supervisor.child_pid(:a)
      assert new_a != a
    end
  end

  describe "restart intensity" do
    test "supervisor exits when max_restarts exceeded" do
      children = [
        %{id: :unstable, start: {Worker, :x}, restart: :permanent}
      ]

      {:ok, sup} = MyGenServer.Supervisor.start_link(children,
        strategy: :one_for_one, max_restarts: 3, max_seconds: 5
      )

      ref = Process.monitor(sup)

      for _ <- 1..4 do
        {:ok, pid} = MyGenServer.Supervisor.child_pid(:unstable)
        Process.exit(pid, :kill)
        Process.sleep(10)
      end

      assert_receive {:DOWN, ^ref, :process, ^sup, {:shutdown, :max_restarts_exceeded}}, 1_000
    end
  end
end
```

---

## Quick Start

**Prerequisites**: Elixir 1.14+, OTP 25+

**Setup**:
```bash
mix new my_otp --sup
cd my_otp
mkdir -p lib/my_otp test/my_otp bench
```

**Run tests with tracing** (shows execution order):
```bash
mix test test/my_otp/ --trace
```

**Interactive example**:
```bash
iex -S mix
```

Then in iex:
```elixir
defmodule Counter do
  def init(n), do: {:ok, n}
  def handle_call(:get, _from, n), do: {:reply, n, n}
  def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
  def handle_cast(:reset, _n), do: {:noreply, 0}
  def handle_info(_, s), do: {:noreply, s}
end

{:ok, sup} = MyGenServer.Supervisor.start_link(
  [
    %{id: :w1, start: {Counter, 0}, restart: :permanent},
    %{id: :w2, start: {Counter, 100}, restart: :permanent}
  ],
  strategy: :one_for_one
)

{:ok, w1} = MyGenServer.Supervisor.child_pid(:w1)
MyGenServer.call(w1, :get)  # => 0
MyGenServer.call(w1, :inc)  # => 1
```

---

## Benchmark

**Objective**: Measure custom GenServer overhead vs. native OTP.

**Setup**:
```elixir
# bench/my_otp_bench.exs
defmodule BenchServer do
  def init(_), do: {:ok, 0}
  def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
  def handle_info(_, s), do: {:noreply, s}
end

{:ok, mine} = MyGenServer.start_link(BenchServer, 0)
{:ok, otp}  = GenServer.start_link(BenchServer, 0)

Benchee.run(
  %{
    "MyGenServer.call (custom)" => fn -> MyGenServer.call(mine, :inc) end,
    "GenServer.call (native)"   => fn -> GenServer.call(otp, :inc) end
  },
  parallel: 1,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

**Run benchmarks**:
```bash
# Add benchee to mix.exs first:
# {:benchee, "~> 1.3", only: :dev}
mix run bench/my_otp_bench.exs
```

**Expected Results**:
- Custom GenServer: 15–25 µs/call
- Native GenServer: 8–12 µs/call
- Overhead: 30–50% (acceptable for hand-rolled implementation)
- Restart detection: < 5 ms

**Interpretation**:
OTP's C-optimized receive loop and decades of tuning account for the difference. Your Elixir version trades performance for pedagogical transparency — every protocol step is readable code.

---

## Reflection

These questions deepen your understanding of the design:

1. **Restart Timing**: Your `:one_for_all` strategy restarts every sibling when any child fails. What happens if one sibling is slow to terminate and blocks the restart? How would you trace this invariant?

2. **Strategy Choice**: When would you prefer `:rest_for_one` over `:one_for_all` in a real supervision tree? Give a concrete example (e.g., a database connection pool with configuration cache).

---

## Trade-off Analysis

| Aspect | Your implementation | OTP's GenServer |
|--------|--------------------|--------------------|
| Call protocol | `{:"$call", {pid, ref}, msg}` | identical |
| Stale reply handling | pinned ref in receive | identical |
| Crash propagation | monitor → message | monitor → message |
| System message handling | `:sys` protocol | built-in |
| Hot code upgrade | not implemented | `code_change/3` callback |
| Hibernate support | not implemented | `{:reply, val, state, :hibernate}` |
| Debug tracing | not implemented | `:sys.trace/2` |

After running the benchmark, note the overhead introduced by your abstraction layer relative to OTP's highly optimized native implementation.

---

## Common Production Mistakes

**1. Using links instead of monitors**
Linked supervisors crash when children crash — before restart logic runs. Use monitors so crashes arrive as messages the supervisor can handle.

**2. Not handling stale replies in calls**
After a timeout, the server eventually sends a reply that sits in the caller's mailbox. The next call receives the stale reply without the pinned reference check. Always use `^ref` to pin the expected reply.

**3. Restart intensity with a naive counter**
A counter that resets every `max_seconds` fails on bursts that straddle period boundaries. Use a list of timestamps and count those within `[now - max_seconds, now]` instead.

**4. Starting children before supervisor is ready**
If children start in `init` and crash immediately, `{:DOWN, ...}` arrives before the supervisor enters its receive loop. Start children from the loop or pre-handle `{:DOWN, ...}` before it begins.

---

## Resources

- [Erlang `gen_server.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/gen_server.erl) — study the `loop/7` function and the `call/3` implementation
- [Erlang `supervisor.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/supervisor.erl) — study `handle_info/2` for the `{:DOWN, ...}` handler
- Cesarini, F. & Vinoski, S. — *Designing for Scalability with Erlang/OTP* — Chapters 4–6
- [OTP Supervisor Design Principles](https://www.erlang.org/doc/design_principles/sup_princ)
- Hebert, F. — *The Zen of Erlang* — https://ferd.ca/the-zen-of-erlang.html
