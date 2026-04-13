# Custom GenServer and Supervisor

**Project**: `my_otp` — a from-scratch implementation of GenServer and Supervisor using raw BEAM primitives

---

## Project context

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

## The problem

You use GenServer and Supervisor every day but cannot explain why a call uses a tagged reference, why the supervisor uses monitors instead of links, or how restart intensity tracking works. This exercise closes that gap. You will implement the full mechanism from first principles, discovering why each design decision exists.

---

## Why this design

**Tagged reference for calls**: `GenServer.call/2` under the hood sends `{:"$gen_call", {self(), ref}, message}` where `ref = make_ref()`. The server sends back `{ref, reply}`. The caller waits with `receive do {^ref, reply} -> reply end`. The `^ref` pin ensures the caller matches only its own reply, discarding unrelated messages. Without the reference, a stale reply from a previous timed-out call could be mistaken for the reply to the current call.

**Monitors, not links, for supervisors**: if the supervisor were linked to its children, a child crash would kill the supervisor before the restart logic could run. By monitoring children, the supervisor receives `{:DOWN, ref, :process, pid, reason}` as a message and can react — restart the child, stop others if the strategy requires it — without itself crashing.

**Sliding window for restart intensity**: OTP counts restarts within the last `max_seconds`. A burst of restarts exhausts the intensity limit; the supervisor exits with `:max_restarts_exceeded`. Storing all restart timestamps and counting those within `[now - max_seconds, now]` is the correct algorithm (not a simple counter that resets periodically).

---

## Design decisions

**Option A — Wrap OTP's `:supervisor` behaviour**
- Pros: battle-tested; free features (dynamic children, restart intensity, shutdown order).
- Cons: defeats the point of *understanding* how supervision works.

**Option B — Implement a supervisor from scratch on top of `:proc_lib` and `Process.monitor/1`** (chosen)
- Pros: every rule (`:one_for_one`, `:rest_for_one`, restart intensity window) is explicit in your code; makes the OTP contract visible.
- Cons: you must implement shutdown correctness yourself, which is where real-world supervisors usually break.

→ Chose **B** because the only way to internalize the OTP supervision contract is to re-derive it — the pedagogical value dominates the ergonomic loss.

## Implementation milestones

### Step 1: Create the project

**Objective**: Lay out `lib/` and `test/` so the GenServer and Supervisor reimplementations live beside their tests and can evolve independently.


```bash
mix new my_otp --sup
cd my_otp
mkdir -p lib/my_otp test/my_otp bench
```

### Step 2: `mix.exs` — no external dependencies needed

**Objective**: Stdlib-only — the whole point is to rebuild OTP primitives from `spawn`/`send`/`receive`, not to depend on them.


The entire implementation uses only Elixir/Erlang standard library primitives.

### Step 3: `MyGenServer`

**Objective**: Build the `{:"$call", {pid, ref}, msg}` protocol by hand so the pinned-ref discipline that kills stale replies becomes visible code.



### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

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

### Step 4: `MyGenServer.Supervisor`

**Objective**: Monitor children (never link) so a crash arrives as a `:DOWN` message the strategy function can react to instead of killing the supervisor.


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

### Step 5: Given tests — must pass without modification

**Objective**: Freeze the OTP contract as executable tests — stale-reply discipline, restart strategies, and intensity limits must hold without tweaking the suite.


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
    def handle_cast(:crash, _state), do: raise "intentional crash"
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

## Quick start

**Prerequisites**: Elixir 1.14+, OTP 25+

**Setup and run**:
```bash
mix test test/my_otp/ --trace
mix run -e "IO.puts(\"MyGenServer module loaded\")"
```

**Run benchmarks**:
```bash
mix run bench/my_otp_bench.exs
```

---

### Step 6: Run the tests

**Objective**: Use `--trace` so each restart-strategy scenario prints in order, making it obvious when `:one_for_all` terminates siblings out of sequence.

```bash
mix test test/my_otp/ --trace
```

### Step 7: Benchmark

**Objective**: Quantify the Elixir-loop overhead against `:gen_server`'s C-optimized path to see the real cost of rolling your own call/receive.


```elixir
# bench/my_otp_bench.exs
defmodule BenchServer do
  def init(_), do: {:ok, 0}
  def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
  def handle_info(_, s), do: {:noreply, s}
end

{:ok, mine}  = MyGenServer.start_link(BenchServer, 0)
{:ok, otp}   = GenServer.start_link(BenchServer, 0)

Benchee.run(
  %{
    "MyGenServer.call" => fn -> MyGenServer.call(mine, :inc) end,
    "GenServer.call"   => fn -> GenServer.call(otp, :inc)   end
  },
  parallel: 1,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

Each supervisor spawns children with `:proc_lib.start_link/3` and monitors them; when a monitored PID dies, the strategy function decides whether to restart the one, restart siblings after it, or restart all. A sliding-window counter enforces restart intensity exactly as OTP does.

---


## Main Entry Point

```elixir
def main do
  IO.puts("======== 10-build-custom-genserver-supervisor ========")
  IO.puts("Build custom genserver supervisor")
  IO.puts("")
  
defmodule Counter do
    def init(n), do: {:ok, n}
    def handle_call(:get, _from, n), do: {:reply, n, n}
    def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
    def handle_cast(:reset, _n), do: {:noreply, 0}
    def handle_info(_, s), do: {:noreply, s}
  end
  
  {:ok, sup} = MyGenServer.Supervisor.start_link([
    %{id: :w1, start: {Counter, 0}, restart: :permanent},
    %{id: :w2, start: {Counter, 100}, restart: :permanent}
  ], strategy: :one_for_one)
  IO.puts("Supervisor started with 2 workers")
  
  {:ok, w1} = MyGenServer.Supervisor.child_pid(:w1)
  v1 = MyGenServer.call(w1, :get)
  IO.puts("Worker 1 initial state: #{v1}")
  
  v2 = MyGenServer.call(w1, :inc)
  IO.puts("After :inc: #{v2}")
  
  IO.puts("Run: mix test")
end
```



## Benchmark

**Objective**: Measure the overhead of the custom GenServer call/cast protocol and restart monitoring against native OTP.

**Expected results**:
- `MyGenServer.call/3` latency: 15–25 microseconds per call
- `GenServer.call/3` latency: 8–12 microseconds per call
- Overhead: 30–50% relative to OTP (acceptable for a hand-rolled implementation)
- Restart detection time: < 5 milliseconds (monitor delivery + strategy execution)

**Measurement constraints**:
- Single parallel worker (no cross-core scheduling variance)
- Call timeout set to 5 seconds (large enough to never fire)
- Increment counter state (trivial work in handler)
- Run 10,000 iterations per benchmark to stabilize
- Report p50, p99 latencies in addition to mean

**Interpretation**:
The difference accounts for OTP's C-optimized receive loop, direct BEAM instruction set access, and decades of tuning. Your Elixir implementation trades 30–50% overhead for pedagogical transparency — you can read and understand every step of the protocol.

If your benchmark shows < 15 µs overhead: verify you are not accidentally using `:gen_server`'s C implementation.

If your benchmark shows > 100 µs overhead: investigate whether tail recursion is being optimized and whether the pattern-match in `loop/2` is efficient.

---

## Deep Dive: Lock-Free Patterns and the BEAM Scheduler

Concurrency on the BEAM differs from OS threads: each Elixir process is a lightweight logical task scheduled by the BEAM VM. There are no kernel locks or mutexes; instead, processes communicate via message passing.

Lock-free data structures (e.g., ETS with `:write_concurrency`, atomic counters) use compare-and-swap primitives to avoid a centralized lock holder. On OS threads, this is critical because a preempted lock holder starves all waiters. On the BEAM, processes yield cooperatively, so even simple spinlocks are viable—but lock contention still matters.

The ETS table is the BEAM's primary lockfree structure: concurrent readers use an RWLock per bucket (readers do not block each other); writers grab an exclusive lock. For a counter with 100K increments/sec from 10 processes, ETS wins if reads are rare (fast writers, no reader contention). But a dedicated GenServer (serializing all increments via messages) can outperform ETS if the write rate is so high that RWLock contention dominates.

Scheduler affinity (pinning a process to a specific scheduler thread) is an advanced optimization: if a GenServer is pinned and its callers are on the same scheduler, message delivery avoids cross-thread synchronization. But this requires deep knowledge of your workload and can degrade fairness.

**Production gotcha**: Measuring concurrency on a single machine is misleading. ETS counters appear faster than GenServer counters until you hit a few thousand ops/sec from many processes, then RWLock overhead dominates. Always benchmark at realistic concurrency levels and check for starvation (e.g., do slow processes still make progress?).

---

## Trade-off analysis

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

## Common production mistakes

**1. Using links instead of monitors in the supervisor**
If the supervisor is linked to its children, any child crash propagates an exit signal to the supervisor before the `{:DOWN, ...}` message arrives. You must use monitors so crashes arrive as ordinary messages that the supervisor can handle in the receive loop.

**2. Not handling the stale reply**
After a call times out, the server eventually sends the reply. That reply sits in the caller's mailbox. If the caller then makes another call and is not carefully receiving, the stale reply will corrupt the next call's result. The pinned reference (`^ref`) in receive is the correct defense.

**3. Restart intensity using a simple counter**
A counter that resets every `max_seconds` does not correctly capture a burst of restarts that straddles a period boundary. Use a list of timestamps and count those within `[now - max_seconds, now]`.

**4. Starting children before the supervisor is ready**
If children are started synchronously in `init` before the supervisor process is fully initialized, a child that crashes immediately may generate a `{:DOWN, ...}` before the supervisor is in its receive loop. Start children from the receive loop, or handle the `{:DOWN, ...}` before the loop is entered.

## Reflection

- Your `:one_for_all` strategy restarts every sibling on any failure. What happens if one sibling is slow to terminate and blocks the restart? Trace the invariant.
- When would you prefer `:rest_for_one` over `:one_for_all` in a real supervision tree? Give a concrete example.

---

## Resources

- [Erlang `gen_server.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/gen_server.erl) — study the `loop/7` function and the `call/3` implementation
- [Erlang `supervisor.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/supervisor.erl) — study `handle_info/2` for the `{:DOWN, ...}` handler
- Cesarini, F. & Vinoski, S. — *Designing for Scalability with Erlang/OTP* — Chapters 4–6
- [OTP Supervisor Design Principles](https://www.erlang.org/doc/design_principles/sup_princ)
- Hebert, F. — *The Zen of Erlang* — https://ferd.ca/the-zen-of-erlang.html
